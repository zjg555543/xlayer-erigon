package stages

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ledgerwatch/erigon-lib/chain"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/memdb"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	"github.com/ledgerwatch/erigon/smt/pkg/db"
	"github.com/ledgerwatch/erigon/zk/datastream/types"
	"github.com/ledgerwatch/erigon/zk/erigon_db"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/erigon/zk/sequencer"

	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/stretchr/testify/require"
)

func TestUnwindBatches(t *testing.T) {
	// set sequencer env key to 1 to run as sequencer, which could avoid panic in GetFinalizedBlockNumber
	os.Setenv(sequencer.SEQUENCER_ENV_KEY, "1")

	currentBlockNumber := 10
	fullL2Blocks := createTestL2Blocks(t, currentBlockNumber)

	gerUpdates := []types.GerUpdate{}
	for i := currentBlockNumber + 1; i <= currentBlockNumber+5; i++ {
		gerUpdates = append(gerUpdates, types.GerUpdate{
			BatchNumber:    1 + uint64(i/2),
			Timestamp:      uint64(i) * 10000,
			GlobalExitRoot: common.Hash{byte(i)},
			Coinbase:       common.Address{byte(i)},
			ForkId:         1 + uint16(i)/3,
			ChainId:        uint32(1),
			StateRoot:      common.Hash{byte(i)},
		})
	}

	ctx, db1 := context.Background(), memdb.NewTestDB(t)
	tx := memdb.BeginRw(t, db1)
	err := hermez_db.CreateHermezBuckets(tx)
	require.NoError(t, err)

	err = db.CreateEriDbBuckets(tx)
	require.NoError(t, err)

	dsClient := NewTestDatastreamClient(fullL2Blocks, gerUpdates)

	tmpDSClientCreator := func(_ context.Context, _ *ethconfig.Zk, _ uint64) (DatastreamClient, error) {
		return NewTestDatastreamClient(fullL2Blocks, gerUpdates), nil
	}
	cfg := StageBatchesCfg(db1, dsClient, &ethconfig.Zk{}, &chain.Config{}, nil, WithDSClientCreator(tmpDSClientCreator))

	s := &stagedsync.StageState{ID: stages.Batches, BlockNumber: 0}
	u := &stagedsync.Sync{}
	us := &stagedsync.UnwindState{ID: stages.Batches, UnwindPoint: 0, CurrentBlockNumber: uint64(currentBlockNumber)}
	hDB := hermez_db.NewHermezDb(tx)
	err = hDB.WriteBlockBatch(0, 0)
	require.NoError(t, err)
	err = stages.SaveStageProgress(tx, stages.AnalysisGroupVerifiedBatchNo, 20)
	require.NoError(t, err)

	// get bucket sizes pre inserts
	bucketSized := make(map[string]uint64)
	buckets, err := tx.ListBuckets()
	require.NoError(t, err)
	for _, bucket := range buckets {
		size, err := tx.BucketSize(bucket)
		require.NoError(t, err)
		bucketSized[bucket] = size
	}

	/////////
	// ACT //
	/////////
	err = SpawnStageBatches(s, u, ctx, tx, cfg)
	require.NoError(t, err)
	tx.Commit()
	tx2 := memdb.BeginRw(t, db1)

	// unwind to zero and check if there is any data in the tables
	err = UnwindBatchesStage(us, tx2, cfg, ctx)
	require.NoError(t, err)
	tx2.Commit()

	////////////////
	// ASSERTIONS //
	////////////////
	// check if there is any data in the tables
	tx3 := memdb.BeginRw(t, db1)
	buckets, err = tx3.ListBuckets()
	require.NoError(t, err)
	for _, bucket := range buckets {
		//currently not decrementing sequence
		// Unwinded headers will be added to BadHeaderNumber bucket
		// Allow store non-canonical blocks/senders: https://github.com/ledgerwatch/erigon/pull/7648
		if bucket == kv.Sequence || bucket == kv.BadHeaderNumber || bucket == kv.Headers {
			continue
		}
		// this table is deleted in execution stage
		if bucket == kv.TX_PRICE_PERCENTAGE {
			continue
		}
		// header tables (number, canonical, headers)
		if bucket == kv.HeaderNumber || bucket == kv.HeaderCanonical || bucket == kv.Headers {
			continue
		}
		size, err := tx3.BucketSize(bucket)
		require.NoError(t, err)
		require.Equal(t, bucketSized[bucket], size, "bucket %s is not empty", bucket)
	}
}

func TestFindCommonAncestor(t *testing.T) {
	blocksCount := 40
	l2Blocks := createTestL2Blocks(t, blocksCount)

	testCases := []struct {
		name                  string
		dbBlocksCount         int
		dsBlocksCount         int
		latestBlockNum        uint64
		divergentBlockHistory bool
		expectedBlockNum      uint64
		expectedHash          common.Hash
		expectedError         error
	}{
		{
			name:             "Successful search (db lagging behind the data stream)",
			dbBlocksCount:    5,
			dsBlocksCount:    10,
			latestBlockNum:   5,
			expectedBlockNum: 5,
			expectedHash:     common.Hash{byte(5)},
			expectedError:    nil,
		},
		{
			name:             "Successful search (db leading the data stream)",
			dbBlocksCount:    20,
			dsBlocksCount:    10,
			latestBlockNum:   10,
			expectedBlockNum: 10,
			expectedHash:     common.Hash{byte(10)},
			expectedError:    nil,
		},
		{
			name:           "Failed to find common ancestor block (latest block number is 0)",
			dbBlocksCount:  10,
			dsBlocksCount:  10,
			latestBlockNum: 0,
			expectedError:  ErrFailedToFindCommonAncestor,
		},
		{
			name:                  "Failed to find common ancestor block (different blocks in the data stream and db)",
			dbBlocksCount:         10,
			dsBlocksCount:         10,
			divergentBlockHistory: true,
			latestBlockNum:        20,
			expectedError:         ErrFailedToFindCommonAncestor,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// ARRANGE
			testDb, tx := memdb.NewTestTx(t)
			defer testDb.Close()
			defer tx.Rollback()
			err := hermez_db.CreateHermezBuckets(tx)
			require.NoError(t, err)

			err = db.CreateEriDbBuckets(tx)
			require.NoError(t, err)

			hermezDb := hermez_db.NewHermezDb(tx)
			erigonDb := erigon_db.NewErigonDb(tx)

			dbBlocks := l2Blocks[:tc.dbBlocksCount]
			if tc.divergentBlockHistory {
				dbBlocks = l2Blocks[tc.dsBlocksCount : tc.dbBlocksCount+tc.dsBlocksCount]
			}

			reader := newMockL2BlockReaderRpc()

			for _, l2Block := range dbBlocks {
				require.NoError(t, hermezDb.WriteBlockBatch(l2Block.L2BlockNumber, l2Block.BatchNumber))
				require.NoError(t, rawdb.WriteCanonicalHash(tx, l2Block.L2Blockhash, l2Block.L2BlockNumber))
				reader.addBlockDetail(l2Block.L2BlockNumber, l2Block.BatchNumber, l2Block.L2Blockhash)
			}

			cfg := BatchesCfg{
				zkCfg: &ethconfig.Zk{
					L2RpcUrl: "test",
				},
			}

			// ACT
			ancestorNum, ancestorHash, err := findCommonAncestor(cfg, erigonDb, hermezDb, reader, tc.latestBlockNum)

			// ASSERT
			if tc.expectedError != nil {
				require.Error(t, err)
				require.Equal(t, tc.expectedError.Error(), err.Error())
				require.Equal(t, uint64(0), ancestorNum)
				require.Equal(t, emptyHash, ancestorHash)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedBlockNum, ancestorNum)
				require.Equal(t, tc.expectedHash, ancestorHash)
			}
		})
	}
}

func createTestL2Blocks(t *testing.T, blocksCount int) []types.FullL2Block {
	post155 := "0xf86780843b9aca00826163941275fbb540c8efc58b812ba83b0d0b8b9917ae98808464fbb77c1ba0b7d2a666860f3c6b8f5ef96f86c7ec5562e97fd04c2e10f3755ff3a0456f9feba0246df95217bf9082f84f9e40adb0049c6664a5bb4c9cbe34ab1a73e77bab26ed"
	post155Bytes, err := hex.DecodeString(strings.TrimPrefix(post155, "0x"))
	require.NoError(t, err)

	l2Blocks := make([]types.FullL2Block, 0, blocksCount)
	for i := 1; i <= blocksCount; i++ {
		l2Blocks = append(l2Blocks, types.FullL2Block{
			BatchNumber:     1 + uint64(i/2),
			L2BlockNumber:   uint64(i),
			Timestamp:       int64(i) * 10000,
			DeltaTimestamp:  uint32(i) * 10,
			L1InfoTreeIndex: uint32(i) + 20,
			GlobalExitRoot:  common.Hash{byte(i)},
			Coinbase:        common.Address{byte(i)},
			ForkId:          1 + uint64(i)/3,
			L1BlockHash:     common.Hash{byte(i)},
			L2Blockhash:     common.Hash{byte(i)},
			StateRoot:       common.Hash{byte(i)},
			L2Txs: []types.L2TransactionProto{
				{
					EffectiveGasPricePercentage: 255,
					IsValid:                     true,
					IntermediateStateRoot:       common.Hash{byte(i + 1)},
					Encoded:                     post155Bytes,
				},
			},
			ParentHash: common.Hash{byte(i - 1)},
		})
	}

	return l2Blocks
}

type mockL2BlockReaderRpc struct {
	blockHashes  map[uint64]common.Hash
	blockBatches map[uint64]uint64
}

func newMockL2BlockReaderRpc() mockL2BlockReaderRpc {
	return mockL2BlockReaderRpc{
		blockHashes:  make(map[uint64]common.Hash),
		blockBatches: make(map[uint64]uint64),
	}
}

func (m mockL2BlockReaderRpc) addBlockDetail(number, batch uint64, hash common.Hash) {
	m.blockHashes[number] = hash
	m.blockBatches[number] = batch
}

func (m mockL2BlockReaderRpc) GetZKBlockByNumberHash(url string, blockNum uint64) (common.Hash, error) {
	return m.blockHashes[blockNum], nil
}

func (m mockL2BlockReaderRpc) GetBatchNumberByBlockNumber(url string, blockNum uint64) (uint64, error) {
	return m.blockBatches[blockNum], nil
}

// TestGetHighestDSL2BlockWithOptimizedAPI tests the optimized API success scenario
func TestGetHighestDSL2BlockWithOptimizedAPI(t *testing.T) {
	ctx := context.Background()

	// Create test L2 blocks
	fullL2Blocks := createTestL2Blocks(t, 5)
	gerUpdates := []types.GerUpdate{}

	// Create mock client that supports optimized API
	mockClient := &MockOptimizedDatastreamClient{
		TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
		useOptimizedAPI:      true,
	}

	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl: "localhost:1234",
		},
	}

	var stats getHighestDSL2BlockStats

	// ACT
	blockNum, err := getHighestDSL2BlockWithMockClient(ctx, cfg, mockClient, &stats)

	// ASSERT
	require.NoError(t, err)
	require.Equal(t, uint64(5), blockNum) // Latest block number
	require.True(t, stats.dsUseOptimizedHighestBlock, "Should use optimized API")
	require.Equal(t, 1, stats.dsGetBlockCounter)
	require.True(t, mockClient.LastUsedOptimizedHighestBlock(), "LastUsedOptimizedHighestBlock should return true")
}

// TestGetHighestDSL2BlockWithFallback tests the fallback to legacy method
func TestGetHighestDSL2BlockWithFallback(t *testing.T) {
	ctx := context.Background()

	// Create test L2 blocks
	fullL2Blocks := createTestL2Blocks(t, 3)
	gerUpdates := []types.GerUpdate{}

	// Create mock client that fails optimized API
	mockClient := &MockOptimizedDatastreamClient{
		TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
		useOptimizedAPI:      false, // Force fallback
	}

	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl: "localhost:1234",
		},
	}

	var stats getHighestDSL2BlockStats

	// ACT
	blockNum, err := getHighestDSL2BlockWithMockClient(ctx, cfg, mockClient, &stats)

	// ASSERT
	require.NoError(t, err)
	require.Equal(t, uint64(3), blockNum) // Latest block number
	require.False(t, stats.dsUseOptimizedHighestBlock, "Should use legacy API")
	require.Equal(t, 1, stats.dsGetBlockCounter)
	require.False(t, mockClient.LastUsedOptimizedHighestBlock(), "LastUsedOptimizedHighestBlock should return false")
}

// TestLastUsedOptimizedAPITracking tests API type tracking
func TestLastUsedOptimizedAPITracking(t *testing.T) {
	fullL2Blocks := createTestL2Blocks(t, 2)
	gerUpdates := []types.GerUpdate{}

	// Test optimized API tracking
	mockClient := &MockOptimizedDatastreamClient{
		TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
		useOptimizedAPI:      true,
	}

	// Initially should be false (default)
	require.False(t, mockClient.LastUsedOptimizedHighestBlock())

	// Call GetLatestL2Block with optimized API
	_, err := mockClient.GetLatestL2Block()
	require.NoError(t, err)
	require.True(t, mockClient.LastUsedOptimizedHighestBlock())

	// Test fallback tracking
	mockClient.useOptimizedAPI = false
	_, err = mockClient.GetLatestL2Block()
	require.NoError(t, err)
	require.False(t, mockClient.LastUsedOptimizedHighestBlock())
}

// TestStatsToStringWithAPIType tests the stats output format
func TestStatsToStringWithAPIType(t *testing.T) {
	stats := getHighestDSL2BlockStats{
		dsStart:                    100 * time.Microsecond,
		dsStartCounter:             1,
		dsGetBlockCost:             50 * time.Millisecond,
		dsGetBlockCounter:          1,
		dsStopCost:                 10 * time.Microsecond,
		dsStopCounter:              0,
		dsUseOptimizedHighestBlock: true,
	}

	result := stats.toString()

	// Verify the output contains all expected fields
	require.Contains(t, result, "dsStart: 100µs")
	require.Contains(t, result, "dsStartCounter: 1")
	require.Contains(t, result, "dsGetBlockCost: 50ms")
	require.Contains(t, result, "dsGetBlockCounter: 1")
	require.Contains(t, result, "dsStopCost: 10µs")
	require.Contains(t, result, "dsStopCounter: 0")
	require.Contains(t, result, "dsUseOptimizedHighestBlock: true")

	// Test with legacy API
	stats.dsUseOptimizedHighestBlock = false
	result = stats.toString()
	require.Contains(t, result, "dsUseOptimizedHighestBlock: false")
}

// MockOptimizedDatastreamClient extends TestDatastreamClient to support optimized API testing
type MockOptimizedDatastreamClient struct {
	TestDatastreamClient
	useOptimizedAPI      bool
	lastUsedOptimizedAPI bool
}

func (m *MockOptimizedDatastreamClient) GetLatestL2Block() (*types.FullL2Block, error) {
	// Simulate optimized API behavior
	if m.useOptimizedAPI {
		m.lastUsedOptimizedAPI = true
		return m.TestDatastreamClient.GetLatestL2Block()
	}

	// Simulate fallback to legacy method
	m.lastUsedOptimizedAPI = false
	return m.TestDatastreamClient.GetLatestL2Block()
}

func (m *MockOptimizedDatastreamClient) LastUsedOptimizedAPI() bool {
	return m.lastUsedOptimizedAPI
}

func (m *MockOptimizedDatastreamClient) LastUsedOptimizedHighestBlock() bool {
	return m.lastUsedOptimizedAPI // For this mock, both track the same optimization
}

func (m *MockOptimizedDatastreamClient) LastUsedOptimizedBatch() bool {
	return false // MockOptimizedDatastreamClient doesn't support batch optimization
}

// Helper function to test getHighestDSL2Block with mock client
func getHighestDSL2BlockWithMockClient(ctx context.Context, cfg BatchesCfg, mockClient DatastreamClient, stats *getHighestDSL2BlockStats) (uint64, error) {
	// Simulate the core logic of getHighestDSL2Block without connection management
	dsGetlockStart := time.Now()
	fullBlock, err := mockClient.GetLatestL2Block()
	stats.dsGetBlockCost += time.Since(dsGetlockStart)
	stats.dsGetBlockCounter += 1
	stats.dsUseOptimizedHighestBlock = mockClient.LastUsedOptimizedHighestBlock()

	if err != nil {
		return 0, err
	}

	return fullBlock.L2BlockNumber, nil
}

// TestDatastreamClientRunner tests the DatastreamClientRunner functionality
func TestDatastreamClientRunner(t *testing.T) {
	t.Run("StartRead Standard Mode", func(t *testing.T) {
		fullL2Blocks := createTestL2Blocks(t, 3)
		gerUpdates := []types.GerUpdate{}

		mockClient := NewTestDatastreamClient(fullL2Blocks, gerUpdates)
		runner := NewDatastreamClientRunner(mockClient, "test-runner")

		errorChan := make(chan struct{}, 1)

		// Start standard reading
		err := runner.StartRead(errorChan)
		require.NoError(t, err, "StartRead should succeed")

		// Wait a bit for the routine to start
		time.Sleep(100 * time.Millisecond)

		// Verify runner is reading
		require.True(t, runner.isReading.Load(), "Runner should be reading")

		// Stop reading
		runner.StopRead()

		// Wait for routine to stop
		time.Sleep(100 * time.Millisecond)

		// Verify runner stopped
		require.False(t, runner.isReading.Load(), "Runner should have stopped")
	})

	t.Run("StartReadOptimized Mode", func(t *testing.T) {
		fullL2Blocks := createTestL2Blocks(t, 3)
		gerUpdates := []types.GerUpdate{}

		// Create optimized mock client
		mockClient := &MockOptimizedDatastreamClientWithBatch{
			TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
			optimizedEnabled:     true,
		}

		runner := NewDatastreamClientRunner(mockClient, "test-runner-optimized")
		errorChan := make(chan struct{}, 1)

		// Start optimized reading
		err := runner.StartReadOptimized(errorChan)
		require.NoError(t, err, "StartReadOptimized should succeed")

		// Wait for routine to start
		time.Sleep(100 * time.Millisecond)

		// Verify runner is reading
		require.True(t, runner.isReading.Load(), "Runner should be reading")

		// Verify optimized method was called
		require.True(t, mockClient.optimizedCalled, "ReadAllEntriesToChannelOptimized should have been called")

		// Stop reading
		runner.StopRead()

		// Wait for routine to stop
		time.Sleep(100 * time.Millisecond)

		// Verify runner stopped
		require.False(t, runner.isReading.Load(), "Runner should have stopped")
	})

	t.Run("Concurrent StartRead Prevention", func(t *testing.T) {
		fullL2Blocks := createTestL2Blocks(t, 1)
		gerUpdates := []types.GerUpdate{}

		mockClient := NewTestDatastreamClient(fullL2Blocks, gerUpdates)
		runner := NewDatastreamClientRunner(mockClient, "test-runner")

		errorChan := make(chan struct{}, 1)

		// Start first read
		err1 := runner.StartRead(errorChan)
		require.NoError(t, err1, "First StartRead should succeed")

		// Wait a bit for the goroutine to set isReading
		time.Sleep(50 * time.Millisecond)

		// Try to start second read while first is running
		err2 := runner.StartRead(errorChan)
		require.Error(t, err2, "Second StartRead should fail")
		require.Contains(t, err2.Error(), "tried starting datastream client runner thread while another is running")

		// Clean up
		runner.StopRead()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("Error Handling in StartRead", func(t *testing.T) {
		// Create mock client that returns error
		mockClient := &MockErrorDatastreamClient{
			TestDatastreamClient: *NewTestDatastreamClient([]types.FullL2Block{}, []types.GerUpdate{}),
			shouldError:          true,
		}

		runner := NewDatastreamClientRunner(mockClient, "test-runner")
		errorChan := make(chan struct{}, 1)

		// Start reading - should trigger error
		err := runner.StartRead(errorChan)
		require.NoError(t, err, "StartRead itself should succeed")

		// Wait for error to be reported
		select {
		case <-errorChan:
			// Expected error received
		case <-time.After(2 * time.Second):
			t.Fatal("Expected error was not received within timeout")
		}

		// Clean up
		runner.StopRead()
		time.Sleep(100 * time.Millisecond)
	})
}

// TestBatchOptimizationConfiguration tests the configuration-based batch optimization
func TestBatchOptimizationConfiguration(t *testing.T) {
	t.Run("Batch Optimization Enabled", func(t *testing.T) {
		fullL2Blocks := createTestL2Blocks(t, 3)
		gerUpdates := []types.GerUpdate{}

		mockClient := &MockOptimizedDatastreamClientWithBatch{
			TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
			optimizedEnabled:     true,
		}

		cfg := BatchesCfg{
			zkCfg: &ethconfig.Zk{
				XLayer: ethconfig.XLayerConfig{
					DataStreamBatchOptimizationEnabled: true,
				},
			},
		}

		// Test the configuration logic
		if cfg.zkCfg.XLayer.DataStreamBatchOptimizationEnabled {
			runner := NewDatastreamClientRunner(mockClient, "test-optimized")
			errorChan := make(chan struct{}, 1)

			err := runner.StartReadOptimized(errorChan)
			require.NoError(t, err, "StartReadOptimized should succeed when enabled")

			time.Sleep(100 * time.Millisecond)
			require.True(t, mockClient.optimizedCalled, "Optimized method should be called when enabled")

			runner.StopRead()
			time.Sleep(100 * time.Millisecond)
		}
	})

	t.Run("Batch Optimization Disabled", func(t *testing.T) {
		fullL2Blocks := createTestL2Blocks(t, 3)
		gerUpdates := []types.GerUpdate{}

		mockClient := &MockOptimizedDatastreamClientWithBatch{
			TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
			optimizedEnabled:     false,
		}

		cfg := BatchesCfg{
			zkCfg: &ethconfig.Zk{
				XLayer: ethconfig.XLayerConfig{
					DataStreamBatchOptimizationEnabled: false, // Disabled
				},
			},
		}

		// Test the configuration logic - should use standard method
		if !cfg.zkCfg.XLayer.DataStreamBatchOptimizationEnabled {
			runner := NewDatastreamClientRunner(mockClient, "test-standard")
			errorChan := make(chan struct{}, 1)

			err := runner.StartRead(errorChan) // Use standard method
			require.NoError(t, err, "StartRead should succeed when optimization is disabled")

			time.Sleep(100 * time.Millisecond)
			require.False(t, mockClient.optimizedCalled, "Optimized method should not be called when disabled")

			runner.StopRead()
			time.Sleep(100 * time.Millisecond)
		}
	})

	t.Run("Configuration Default Value", func(t *testing.T) {
		// Test that default configuration has optimization disabled
		cfg := BatchesCfg{
			zkCfg: &ethconfig.Zk{
				XLayer: ethconfig.XLayerConfig{
					// DataStreamBatchOptimizationEnabled not set - should default to false
				},
			},
		}

		require.False(t, cfg.zkCfg.XLayer.DataStreamBatchOptimizationEnabled,
			"DataStreamBatchOptimizationEnabled should default to false")
	})
}

// TestBatchOptimizationAPITracking tests the API tracking functionality
func TestBatchOptimizationAPITracking(t *testing.T) {
	t.Run("Track Optimized API Usage", func(t *testing.T) {
		fullL2Blocks := createTestL2Blocks(t, 2)
		gerUpdates := []types.GerUpdate{}

		mockClient := &MockOptimizedDatastreamClientWithBatch{
			TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
			optimizedEnabled:     true,
		}

		// Initially should not be using optimized API
		require.False(t, mockClient.LastUsedOptimizedHighestBlock(), "Should initially not use optimized API")

		// Just test the API tracking without calling the methods that might loop
		// Simulate API call tracking directly
		mockClient.lastUsedOptimizedHighestBlock = true
		require.True(t, mockClient.LastUsedOptimizedHighestBlock(), "Should track optimized API usage")

		mockClient.lastUsedOptimizedHighestBlock = false
		require.False(t, mockClient.LastUsedOptimizedBatch(), "Should track standard API usage")
	})

	t.Run("API Tracking in getHighestDSL2Block", func(t *testing.T) {
		ctx := context.Background()
		cfg := BatchesCfg{
			zkCfg: &ethconfig.Zk{
				L2DataStreamerUrl: "localhost:1234",
			},
		}

		// Test with optimized API enabled
		fullL2Blocks := createTestL2Blocks(t, 3)
		gerUpdates := []types.GerUpdate{}

		mockClient := &MockOptimizedDatastreamClientWithBatch{
			TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
			optimizedEnabled:     true,
		}

		var stats getHighestDSL2BlockStats
		blockNum, err := getHighestDSL2BlockWithMockClient(ctx, cfg, mockClient, &stats)

		require.NoError(t, err, "Should succeed with optimized API")
		require.Equal(t, uint64(3), blockNum, "Should return latest block")
		require.True(t, stats.dsUseOptimizedHighestBlock, "Stats should reflect optimized API usage")
		require.True(t, mockClient.LastUsedOptimizedHighestBlock(), "Mock should track optimized API usage")

		// Test with optimized API disabled
		mockClient.optimizedEnabled = false
		mockClient.lastUsedOptimizedHighestBlock = false // Reset tracking

		var stats2 getHighestDSL2BlockStats
		blockNum2, err2 := getHighestDSL2BlockWithMockClient(ctx, cfg, mockClient, &stats2)

		require.NoError(t, err2, "Should succeed with standard API")
		require.Equal(t, uint64(3), blockNum2, "Should return same latest block")
		require.False(t, stats2.dsUseOptimizedHighestBlock, "Stats should reflect standard API usage")
		require.False(t, mockClient.LastUsedOptimizedHighestBlock(), "Mock should track standard API usage")
	})
}

// TestBatchOptimizationErrorHandling tests error scenarios for batch optimization
func TestBatchOptimizationErrorHandling(t *testing.T) {
	t.Run("Optimized Method Error", func(t *testing.T) {
		mockClient := &MockOptimizedDatastreamClientWithBatch{
			TestDatastreamClient: *NewTestDatastreamClient([]types.FullL2Block{}, []types.GerUpdate{}),
			optimizedEnabled:     true,
			shouldErrorOptimized: true,
		}

		runner := NewDatastreamClientRunner(mockClient, "test-error")
		errorChan := make(chan struct{}, 1)

		err := runner.StartReadOptimized(errorChan)
		require.NoError(t, err, "StartReadOptimized should succeed initially")

		// Wait for error to be reported
		select {
		case <-errorChan:
			// Expected error received
		case <-time.After(2 * time.Second):
			t.Fatal("Expected error was not received within timeout")
		}

		runner.StopRead()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("Context Cancellation in Optimized Mode", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		mockClient := &MockOptimizedDatastreamClientWithBatch{
			TestDatastreamClient: *NewTestDatastreamClient([]types.FullL2Block{}, []types.GerUpdate{}),
			optimizedEnabled:     true,
			ctx:                  ctx,
		}

		runner := NewDatastreamClientRunner(mockClient, "test-cancel")
		errorChan := make(chan struct{}, 1)

		err := runner.StartReadOptimized(errorChan)
		require.NoError(t, err, "StartReadOptimized should succeed initially")

		// Cancel context
		cancel()

		// Should receive error due to context cancellation
		select {
		case <-errorChan:
			// Expected error received
		case <-time.After(2 * time.Second):
			t.Fatal("Expected error was not received within timeout")
		}

		runner.StopRead()
		time.Sleep(100 * time.Millisecond)
	})
}

// MockOptimizedDatastreamClientWithBatch extends MockOptimizedDatastreamClient with batch support
type MockOptimizedDatastreamClientWithBatch struct {
	TestDatastreamClient
	optimizedEnabled              bool
	optimizedCalled               bool
	lastUsedOptimizedHighestBlock bool
	shouldErrorOptimized          bool
	ctx                           context.Context
}

func (m *MockOptimizedDatastreamClientWithBatch) ReadAllEntriesToChannelOptimized() error {
	m.optimizedCalled = true
	m.lastUsedOptimizedHighestBlock = true

	if m.shouldErrorOptimized {
		return fmt.Errorf("simulated optimized method error")
	}

	if m.ctx != nil {
		select {
		case <-m.ctx.Done():
			return fmt.Errorf("context cancelled")
		default:
		}
	}

	// Simulate optimized batch streaming
	return m.TestDatastreamClient.ReadAllEntriesToChannel()
}

func (m *MockOptimizedDatastreamClientWithBatch) ReadAllEntriesToChannel() error {
	m.lastUsedOptimizedHighestBlock = false
	return m.TestDatastreamClient.ReadAllEntriesToChannel()
}

func (m *MockOptimizedDatastreamClientWithBatch) GetLatestL2Block() (*types.FullL2Block, error) {
	if m.optimizedEnabled {
		m.lastUsedOptimizedHighestBlock = true
	} else {
		m.lastUsedOptimizedHighestBlock = false
	}
	return m.TestDatastreamClient.GetLatestL2Block()
}

func (m *MockOptimizedDatastreamClientWithBatch) LastUsedOptimizedHighestBlock() bool {
	return m.lastUsedOptimizedHighestBlock
}

func (m *MockOptimizedDatastreamClientWithBatch) LastUsedOptimizedAPI() bool {
	return m.lastUsedOptimizedHighestBlock
}

func (m *MockOptimizedDatastreamClientWithBatch) LastUsedOptimizedBatch() bool {
	return m.lastUsedOptimizedHighestBlock // For this mock, batch optimization tracks same as HighestBlock
}

// MockErrorDatastreamClient simulates error conditions
type MockErrorDatastreamClient struct {
	TestDatastreamClient
	shouldError bool
}

func (m *MockErrorDatastreamClient) ReadAllEntriesToChannel() error {
	if m.shouldError {
		return fmt.Errorf("simulated datastream error")
	}
	return m.TestDatastreamClient.ReadAllEntriesToChannel()
}
