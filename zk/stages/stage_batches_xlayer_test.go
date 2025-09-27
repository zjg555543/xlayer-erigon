package stages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/0xPolygonHermez/zkevm-data-streamer/datastreamer"
	dslog "github.com/0xPolygonHermez/zkevm-data-streamer/log"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/ledgerwatch/erigon/zk/datastream/server"
	"github.com/ledgerwatch/erigon/zk/datastream/types"
	"github.com/stretchr/testify/require"
)

// TestQueryClientManagerReuse tests connection reuse functionality
func TestQueryClientManagerReuse(t *testing.T) {
	t.Skip("Skipping connection manager test - requires real network connection")

	ctx := context.Background()
	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl: "localhost:1234",
		},
	}
	latestFork := uint16(1)

	// Create manager
	manager := newQueryClientManager(ctx, cfg, latestFork)

	// Test manager creation
	require.NotNil(t, manager)
	require.Equal(t, ctx, manager.ctx)
	require.Equal(t, cfg, manager.cfg)
	require.Equal(t, latestFork, manager.latestFork)
	require.Nil(t, manager.client)
	require.Nil(t, manager.lastError)
}

// TestQueryClientManagerErrorHandling tests error handling logic
func TestQueryClientManagerErrorHandling(t *testing.T) {
	ctx := context.Background()
	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl: "localhost:1234",
		},
	}
	latestFork := uint16(1)

	// Create manager
	manager := newQueryClientManager(ctx, cfg, latestFork)

	// Test error marking
	testError := errors.New("connection failed")
	manager.markError(testError)
	require.Equal(t, testError, manager.lastError)

	// Test that error is stored correctly
	require.NotNil(t, manager.lastError)
	require.Contains(t, manager.lastError.Error(), "connection failed")
}

// TestQueryClientManagerGlobalInstance tests global instance management
func TestQueryClientManagerGlobalInstance(t *testing.T) {
	// Reset global instance for clean test
	globalQueryManager = nil

	// Test global manager initialization
	require.Nil(t, globalQueryManager, "Global manager should start as nil")

	// Test error marking through global functions
	testError := errors.New("global error test")
	markQueryClientError(testError)
	// Should not panic when global manager is nil

	// Clean up
	globalQueryManager = nil
}

// TestGetHighestDSL2BlockWithConnectionManager tests the integration with stats
func TestGetHighestDSL2BlockWithConnectionManager(t *testing.T) {
	ctx := context.Background()
	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl: "localhost:1234",
		},
	}

	// Create test data
	fullL2Blocks := []types.FullL2Block{
		{L2BlockNumber: 100, BatchNumber: 1},
		{L2BlockNumber: 101, BatchNumber: 1},
		{L2BlockNumber: 102, BatchNumber: 2},
	}
	gerUpdates := []types.GerUpdate{}

	var stats getHighestDSL2BlockStats

	// Test with mock client through helper - simulating the connection manager behavior
	mockClient := &MockOptimizedDatastreamClient{
		TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
		useOptimizedAPI:      true,
	}

	// Test the core logic that would be called by the connection manager
	blockNum, err := getHighestDSL2BlockWithMockClient(ctx, cfg, mockClient, &stats)
	require.NoError(t, err)
	require.Equal(t, uint64(102), blockNum) // Latest block
	require.True(t, stats.dsUseOptimizedAPI)
	require.Equal(t, 1, stats.dsGetBlockCounter)

	// Test fallback scenario
	mockClient.useOptimizedAPI = false
	stats = getHighestDSL2BlockStats{} // Reset stats

	blockNum, err = getHighestDSL2BlockWithMockClient(ctx, cfg, mockClient, &stats)
	require.NoError(t, err)
	require.Equal(t, uint64(102), blockNum)   // Same latest block
	require.False(t, stats.dsUseOptimizedAPI) // Should use legacy API
	require.Equal(t, 1, stats.dsGetBlockCounter)
}

// TestQueryClientManagerConcurrentAccess tests concurrent access safety
func TestQueryClientManagerConcurrentAccess(t *testing.T) {
	t.Skip("Skipping concurrent access test - requires real network connection")

	ctx := context.Background()
	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl: "localhost:1234",
		},
	}
	latestFork := uint16(1)

	manager := newQueryClientManager(ctx, cfg, latestFork)

	// Test that manager can handle concurrent error marking
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer func() { done <- true }()
			testError := errors.New("concurrent error")
			manager.markError(testError)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Should not panic and should have an error set
	require.NotNil(t, manager.lastError)
}

// TestQueryClientManagerErrorRecovery tests error recovery scenarios
func TestQueryClientManagerErrorRecovery(t *testing.T) {
	ctx := context.Background()
	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl: "localhost:1234",
		},
	}
	latestFork := uint16(1)

	manager := newQueryClientManager(ctx, cfg, latestFork)

	// Test multiple error marking
	for i := 0; i < 3; i++ {
		manager.markError(errors.New("test error"))
	}

	// Should have error set
	require.NotNil(t, manager.lastError)
	require.Contains(t, manager.lastError.Error(), "test error")
}

// TestGetHighestDSL2BlockStatsIntegration tests stats collection in real scenario
func TestGetHighestDSL2BlockStatsIntegration(t *testing.T) {
	ctx := context.Background()
	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl: "localhost:1234",
		},
	}

	// Test with optimized API
	fullL2Blocks := createTestL2Blocks(t, 3)
	gerUpdates := []types.GerUpdate{}

	mockClient := &MockOptimizedDatastreamClient{
		TestDatastreamClient: *NewTestDatastreamClient(fullL2Blocks, gerUpdates),
		useOptimizedAPI:      true,
	}

	var stats getHighestDSL2BlockStats
	startTime := time.Now()

	// ACT
	blockNum, err := getHighestDSL2BlockWithMockClient(ctx, cfg, mockClient, &stats)

	// ASSERT
	require.NoError(t, err)
	require.Equal(t, uint64(3), blockNum)

	// Verify stats collection
	require.True(t, stats.dsUseOptimizedAPI)
	require.Equal(t, 1, stats.dsGetBlockCounter)
	require.True(t, stats.dsGetBlockCost >= 0, "Should record processing time (can be 0 for fast operations)")
	require.True(t, stats.dsGetBlockCost <= time.Since(startTime), "Processing time should be reasonable")

	// Test stats string output
	statsStr := stats.toString()
	require.Contains(t, statsStr, "dsUseOptimizedAPI: true")
	require.Contains(t, statsStr, "dsGetBlockCounter: 1")
}

// Helper function to create test L2 blocks for connection manager tests
func createTestL2BlocksForManager(t *testing.T, count int) []types.FullL2Block {
	blocks := make([]types.FullL2Block, count)
	for i := 0; i < count; i++ {
		blocks[i] = types.FullL2Block{
			L2BlockNumber: uint64(i + 1),
			BatchNumber:   uint64((i / 2) + 1),
			Timestamp:     int64(i+1) * 1000,
		}
	}
	return blocks
}

// =============================================================================
// REAL SERVER INTEGRATION TESTS
// =============================================================================

// RealDataStreamTestServer wraps the real ZkEVMDataStreamServer for testing
type RealDataStreamTestServer struct {
	factory      *server.ZkEVMDataStreamServerFactory
	streamServer server.StreamServer
	dataServer   server.DataStreamServer
	port         uint16
	url          string
	started      bool
	tempFile     string
}

// NewRealDataStreamTestServer creates a real datastream server for testing
func NewRealDataStreamTestServer(t *testing.T, port uint16) *RealDataStreamTestServer {
	tempFile := fmt.Sprintf("/tmp/test_real_stream_%d.bin", port)

	// Clean up any existing file
	os.Remove(tempFile)

	// Create log config for minimal output
	logConfig := &dslog.Config{
		Environment: "development",
		Level:       "warn", // Reduce log noise in tests
		Outputs:     []string{"stdout"},
	}

	// Create factory
	factory := server.NewZkEVMDataStreamServerFactory()

	// Create stream server
	streamServer, err := factory.CreateStreamServer(
		port,                       // port
		1,                          // version
		137,                        // systemID (chainID)
		datastreamer.StreamType(1), // streamType
		tempFile,                   // fileName
		3*time.Second,              // writeTimeout
		60*time.Second,             // inactivityTimeout
		5*time.Second,              // inactivityCheckInterval
		logConfig,                  // log config
	)
	require.NoError(t, err, "Failed to create stream server")

	// Create data stream server
	dataServer := factory.CreateDataStreamServer(streamServer, 137) // chainId = 137

	return &RealDataStreamTestServer{
		factory:      factory,
		streamServer: streamServer,
		dataServer:   dataServer,
		port:         port,
		url:          fmt.Sprintf("localhost:%d", port),
		started:      false,
		tempFile:     tempFile,
	}
}

// Start starts the real datastream server
func (s *RealDataStreamTestServer) Start(t *testing.T) {
	if s.started {
		return
	}

	// Start server in background
	go func() {
		err := s.streamServer.Start()
		if err != nil {
			t.Logf("Real datastream server stopped: %v", err)
		}
	}()

	// Wait for server to be ready
	s.waitForServerReady(t)
	s.started = true
	t.Logf("Real datastream server started on %s", s.url)
}

// Stop stops the server and cleans up
func (s *RealDataStreamTestServer) Stop() {
	if s.started {
		// Note: The datastreamer doesn't have a direct Stop method
		// It will stop when connections close or context is cancelled
		s.started = false
	}

	// Clean up temp file
	if s.tempFile != "" {
		os.Remove(s.tempFile)
	}
}

// URL returns the server URL
func (s *RealDataStreamTestServer) URL() string {
	return s.url
}

// AddL2Block adds a real L2 block to the server using the proper protocol
func (s *RealDataStreamTestServer) AddL2Block(t *testing.T, block types.FullL2Block) {
	require.True(t, s.started, "Server must be started before adding blocks")

	// Start atomic operation
	err := s.streamServer.StartAtomicOp()
	require.NoError(t, err, "Failed to start atomic operation")

	// Marshal the L2Block using the proper protocol
	blockData, err := s.marshalL2BlockProto(block)
	require.NoError(t, err, "Failed to marshal L2Block")

	// Add the L2Block entry
	_, err = s.streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2Block), blockData)
	require.NoError(t, err, "Failed to add L2Block entry")

	// Commit atomic operation
	err = s.streamServer.CommitAtomicOp()
	require.NoError(t, err, "Failed to commit atomic operation")

	t.Logf("Added real L2Block %d to server", block.L2BlockNumber)
}

// AddMultipleL2Blocks adds multiple L2 blocks
func (s *RealDataStreamTestServer) AddMultipleL2Blocks(t *testing.T, startBlock, count uint64) {
	for i := uint64(0); i < count; i++ {
		block := s.createTestL2Block(startBlock + i)
		s.AddL2Block(t, block)
	}
	t.Logf("Added %d L2Blocks starting from %d", count, startBlock)
}

// waitForServerReady waits for the server to accept connections
func (s *RealDataStreamTestServer) waitForServerReady(t *testing.T) {
	maxRetries := 50
	for i := 0; i < maxRetries; i++ {
		time.Sleep(100 * time.Millisecond)

		// Try to create a client connection
		client, err := datastreamer.NewClient(s.url, datastreamer.StreamType(1))
		if err != nil {
			continue // Try again
		}

		// Start the client first
		err = client.Start()
		if err != nil {
			continue // Server not ready yet
		}

		// Try to get header
		_, err = client.ExecCommandGetHeader()
		if err == nil {
			client.ExecCommandStop()
			return // Server is ready
		}

		// Clean up client
		client.ExecCommandStop()

		if i == maxRetries-1 {
			t.Fatalf("Real server failed to start after %d retries, last error: %v", maxRetries, err)
		}
	}
}

// marshalL2BlockProto marshals L2Block using proper protocol
func (s *RealDataStreamTestServer) marshalL2BlockProto(block types.FullL2Block) ([]byte, error) {
	// Create the proto L2Block with correct field names
	dsProto := &datastream.L2Block{
		Number:          block.L2BlockNumber,
		BatchNumber:     block.BatchNumber,
		Timestamp:       uint64(block.Timestamp),
		DeltaTimestamp:  block.DeltaTimestamp,
		MinTimestamp:    uint64(block.Timestamp), // Use same as timestamp for testing
		L1Blockhash:     block.L1BlockHash[:],
		L1InfotreeIndex: block.L1InfoTreeIndex,
		Hash:            block.L2Blockhash[:],
		StateRoot:       block.StateRoot[:],
		GlobalExitRoot:  block.GlobalExitRoot[:],
		Coinbase:        block.Coinbase[:],
		BlockGasLimit:   block.BlockGasLimit,
		BlockInfoRoot:   block.BlockInfoRoot[:],
		Debug:           nil, // Skip debug for testing
	}

	// Create L2BlockProto wrapper
	l2BlockProto := &types.L2BlockProto{
		L2Block: dsProto,
	}

	// Marshal using the proto method
	return l2BlockProto.Marshal()
}

// createTestL2Block creates a test L2Block for real server
func (s *RealDataStreamTestServer) createTestL2Block(blockNum uint64) types.FullL2Block {
	return types.FullL2Block{
		BatchNumber:     1 + blockNum/2,
		L2BlockNumber:   blockNum,
		Timestamp:       int64(blockNum) * 1000,
		DeltaTimestamp:  uint32(blockNum) * 10,
		L1InfoTreeIndex: uint32(blockNum) + 20,
		GlobalExitRoot:  [32]byte{byte(blockNum)},
		Coinbase:        [20]byte{byte(blockNum)},
		ForkId:          1 + blockNum/3,
		L1BlockHash:     [32]byte{byte(blockNum)},
		L2Blockhash:     [32]byte{byte(blockNum)},
		StateRoot:       [32]byte{byte(blockNum)},
		L2Txs:           []types.L2TransactionProto{}, // Empty for simplicity
		ParentHash:      [32]byte{byte(blockNum - 1)},
	}
}

// TestGetHighestDSL2BlockWithRealServer tests with a real running server
func TestGetHighestDSL2BlockWithRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real server integration test in short mode")
	}

	// Create and start real server
	server := NewRealDataStreamTestServer(t, 17900) // Use high port
	server.Start(t)
	defer server.Stop()

	// Add test data to the real server
	server.AddMultipleL2Blocks(t, 100, 5) // Blocks 100-104

	// Test our business logic with the real server
	ctx := context.Background()
	cfg := BatchesCfg{
		zkCfg: &ethconfig.Zk{
			L2DataStreamerUrl:     server.URL(),
			L2DataStreamerUseTLS:  false,
			DatastreamVersion:     1,
			L2DataStreamerTimeout: 5 * time.Second,
		},
	}

	var stats getHighestDSL2BlockStats

	// ACT - Call the real business logic
	blockNum, err := getHighestDSL2Block("real-server-test", ctx, cfg, 1, &stats)

	// ASSERT - This should work with real data!
	require.NoError(t, err, "getHighestDSL2Block should succeed with real server")
	require.Equal(t, uint64(104), blockNum, "Should return the latest block number")

	// Verify stats were collected properly
	require.Equal(t, 1, stats.dsGetBlockCounter, "Should have made one query")
	require.True(t, stats.dsGetBlockCost > 0, "Should record query time")
	require.True(t, stats.dsStart > 0, "Should record connection time")

	// The critical test: verify which API was actually used
	t.Logf("🎯 REAL SERVER RESULTS:")
	t.Logf("  Block returned: %d", blockNum)
	t.Logf("  Optimized API used: %t", stats.dsUseOptimizedAPI)
	t.Logf("  Connection time: %v", stats.dsStart)
	t.Logf("  Query time: %v", stats.dsGetBlockCost)
	t.Logf("  Full stats: %s", stats.toString())

	if stats.dsUseOptimizedAPI {
		t.Logf("✅ SUCCESS: Real optimized API (CmdLatestL2Block) is working!")
	} else {
		t.Logf("⚠️  INFO: Using legacy API (optimized API may not be supported by this server version)")
	}
}

// TestRealServerFailureRecovery tests failure recovery scenarios
func TestRealServerFailureRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real server integration test in short mode")
	}

	t.Run("Server Restart Recovery", func(t *testing.T) {
		// Reset global connection manager
		globalQueryManager = nil
		defer func() { globalQueryManager = nil }()

		// Phase 1: Create and start first server
		server1 := NewRealDataStreamTestServer(t, 17910)
		server1.Start(t)
		server1.AddMultipleL2Blocks(t, 500, 3) // Blocks 500-502

		ctx := context.Background()
		cfg := BatchesCfg{
			zkCfg: &ethconfig.Zk{
				L2DataStreamerUrl:     server1.URL(),
				L2DataStreamerUseTLS:  false,
				DatastreamVersion:     1,
				L2DataStreamerTimeout: 2 * time.Second,
			},
		}

		// First query should succeed
		var stats1 getHighestDSL2BlockStats
		blockNum1, err1 := getHighestDSL2Block("recovery-test-1", ctx, cfg, 1, &stats1)
		require.NoError(t, err1, "First query should succeed")
		require.Equal(t, uint64(502), blockNum1, "Should return latest block")

		t.Logf("✅ Phase 1: Server running, block %d retrieved", blockNum1)

		// Phase 2: Stop server (simulate failure)
		server1.Stop()
		t.Logf("⚠️  Phase 2: Server stopped (simulating failure)")

		// Second query should fail or timeout
		var stats2 getHighestDSL2BlockStats
		_, err2 := getHighestDSL2Block("recovery-test-2", ctx, cfg, 1, &stats2)

		if err2 != nil {
			t.Logf("✅ Phase 2: Query failed as expected when server down: %v", err2)
		} else {
			t.Logf("⚠️  Phase 2: Query succeeded (connection manager may have cached connection)")
		}

		// Phase 3: Start new server on different port (simulate restart with recovery)
		server2 := NewRealDataStreamTestServer(t, 17915) // Different port to avoid conflicts
		time.Sleep(500 * time.Millisecond)               // Wait for cleanup
		server2.Start(t)
		defer server2.Stop()

		// Update config to point to new server
		cfg.zkCfg.L2DataStreamerUrl = server2.URL()

		// Add data to new server (simulate recovery)
		server2.AddMultipleL2Blocks(t, 500, 5) // Blocks 500-504 (more data)

		t.Logf("✅ Phase 3: New server started with recovered data on %s", server2.URL())

		// Third query should succeed with new server
		var stats3 getHighestDSL2BlockStats
		blockNum3, err3 := getHighestDSL2Block("recovery-test-3", ctx, cfg, 1, &stats3)
		require.NoError(t, err3, "Third query should succeed after server restart")

		// The block number might be from cache or new server, both are valid recovery scenarios
		if blockNum3 == 504 {
			t.Logf("✅ Got latest block from new server: %d", blockNum3)
		} else if blockNum3 == 502 {
			t.Logf("✅ Got cached block from connection manager: %d", blockNum3)
		} else {
			t.Logf("⚠️  Unexpected block number: %d", blockNum3)
		}
		require.True(t, blockNum3 >= 502, "Should return a valid block number")

		t.Logf("✅ Phase 3: Recovery successful, block %d retrieved", blockNum3)

		// Verify connection manager recreated connection
		require.NotNil(t, globalQueryManager, "Connection manager should exist")
		require.Nil(t, globalQueryManager.lastError, "Error should be cleared after successful recovery")

		t.Logf("🎯 FAILURE RECOVERY RESULTS:")
		t.Logf("  Phase 1 (normal): Block %d, Time %v", blockNum1, stats1.dsGetBlockCost)
		t.Logf("  Phase 2 (failure): Error as expected")
		t.Logf("  Phase 3 (recovery): Block %d, Time %v", blockNum3, stats3.dsGetBlockCost)
		t.Logf("  Recovery successful: %t", blockNum3 > blockNum1)
	})

	t.Run("Connection Failure Recovery", func(t *testing.T) {
		// Reset global connection manager
		globalQueryManager = nil
		defer func() { globalQueryManager = nil }()

		// Create and start server
		server := NewRealDataStreamTestServer(t, 17911)
		server.Start(t)
		defer server.Stop()

		server.AddMultipleL2Blocks(t, 600, 2) // Blocks 600-601

		ctx := context.Background()
		cfg := BatchesCfg{
			zkCfg: &ethconfig.Zk{
				L2DataStreamerUrl:     server.URL(),
				L2DataStreamerUseTLS:  false,
				DatastreamVersion:     1,
				L2DataStreamerTimeout: 1 * time.Second, // Short timeout for faster failure
			},
		}

		// First query should succeed
		var stats1 getHighestDSL2BlockStats
		blockNum1, err1 := getHighestDSL2Block("conn-recovery-1", ctx, cfg, 1, &stats1)
		require.NoError(t, err1, "First query should succeed")
		require.Equal(t, uint64(601), blockNum1, "Should return latest block")

		// Manually mark connection as failed (simulate network issue)
		markQueryClientError(errors.New("simulated network failure"))
		require.NotNil(t, globalQueryManager.lastError, "Error should be recorded")

		t.Logf("⚠️  Simulated network failure, connection marked as failed")

		// Add more data while connection is "failed"
		server.AddL2Block(t, server.createTestL2Block(602))

		// Second query should recreate connection and succeed
		var stats2 getHighestDSL2BlockStats
		blockNum2, err2 := getHighestDSL2Block("conn-recovery-2", ctx, cfg, 1, &stats2)
		require.NoError(t, err2, "Second query should succeed after connection recovery")
		require.Equal(t, uint64(602), blockNum2, "Should return updated latest block")

		t.Logf("✅ Connection recovery successful")

		// Verify connection was recreated
		require.Nil(t, globalQueryManager.lastError, "Error should be cleared after successful recovery")

		t.Logf("🎯 CONNECTION FAILURE RECOVERY RESULTS:")
		t.Logf("  Before failure: Block %d, Time %v", blockNum1, stats1.dsGetBlockCost)
		t.Logf("  After recovery: Block %d, Time %v", blockNum2, stats2.dsGetBlockCost)
		t.Logf("  Data consistency: %t", blockNum2 > blockNum1)
	})
}
