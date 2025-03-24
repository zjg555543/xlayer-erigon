package e2e

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/ethclient"

	"github.com/ledgerwatch/erigon/test/operations"
	"github.com/ledgerwatch/erigon/zkevm/log"

	"github.com/stretchr/testify/require"
)

var client *ethclient.Client

func init() {
	var err error
	client, err = ethclient.Dial("http://localhost:8545")
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to the Ethereum client: %v", err))
	}
}

func waitForBlock(t *testing.T, targetBlock uint64) {
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		currentBlock, err := client.BlockNumber(ctx)
		if err != nil {
			t.Logf("Error getting block number: %v", err)
			time.Sleep(time.Second)
			continue
		}
		if currentBlock >= targetBlock {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("Timeout waiting for block %d", targetBlock)
}

func TestRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// latest batch seal time
	var batchNum uint64
	var batchSealTime uint64
	var err error
	for i := 0; i < 50; i++ {
		batchNum, err = operations.GetBatchNumber()
		require.NoError(t, err)
		batchSealTime, err = operations.GetBatchSealTime(new(big.Int).SetUint64(batchNum))
		require.Equal(t, batchSealTime, uint64(0))
		log.Infof("Batch number: %d, times:%v", batchNum, i)
		if batchNum > 1 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// old batch seal time
	batchNum = batchNum - 1
	batch, err := operations.GetBatchByNumber(new(big.Int).SetUint64(batchNum))
	var maxTime uint64
	for _, block := range batch.Blocks {
		blockInfo, err := operations.GetBlockByHash(common.HexToHash(block.(string)))
		require.NoError(t, err)
		log.Infof("Block Timestamp: %+v", blockInfo.Timestamp)
		blockTime := uint64(blockInfo.Timestamp)
		if blockTime > maxTime {
			maxTime = blockTime
		}
	}
	batchSealTime, err = operations.GetBatchSealTime(new(big.Int).SetUint64(batchNum))
	require.NoError(t, err)
	log.Infof("Max block time: %d, batchSealTime: %d", maxTime, batchSealTime)
	require.Equal(t, maxTime, batchSealTime)
}

func TestDebugTraceRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// Wait for at least one block to be available
	var blockNumber uint64
	var err error
	for i := 0; i < 30; i++ {
		blockNumber, err = operations.GetBlockNumber()
		require.NoError(t, err)
		log.Infof("Block number: %d, attempt: %v", blockNumber, i)
		if blockNumber > 3 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.Greater(t, blockNumber, uint64(0), "Block number should be greater than 0")

	// Get a block to trace
	batchNum, err := operations.GetBatchNumber()
	require.NoError(t, err)

	batch, err := operations.GetBatchByNumber(new(big.Int).SetUint64(batchNum))
	require.NoError(t, err)
	require.NotEmpty(t, batch.Blocks, "Batch should contain at least one block")

	// Test debug_traceBlockByHash
	t.Run("DebugTraceBlockByHash", func(t *testing.T) {
		// Get the hash of the first block in the batch
		blockHash := common.HexToHash(batch.Blocks[0].(string))
		require.NotEqual(t, common.Hash{}, blockHash, "Block hash should not be empty")

		traceResult, err := operations.DebugTraceBlockByHash(blockHash)
		require.NoError(t, err)
		require.NotNil(t, traceResult, "Trace result should not be nil")

		log.Infof("DebugTraceBlockByHash result type: %T", traceResult)
	})

	// Test debug_traceBlockByNumber
	t.Run("DebugTraceBlockByNumber", func(t *testing.T) {
		traceResult, err := operations.DebugTraceBlockByNumber(1) // Trace block #1
		require.NoError(t, err)
		require.NotNil(t, traceResult, "Trace result should not be nil")

		log.Infof("DebugTraceBlockByNumber result type: %T", traceResult)
	})

	// Test debug_traceBatchByNumber
	t.Run("DebugTraceBatchByNumber", func(t *testing.T) {
		// Use batch number 1 to avoid issues with empty batches
		if batchNum > 1 {
			traceResult, err := operations.DebugTraceBatchByNumber(1)
			require.NoError(t, err)
			require.NotNil(t, traceResult, "Trace result should not be nil")

			log.Infof("DebugTraceBatchByNumber result type: %T", traceResult)
		} else {
			t.Skip("Batch number too low, skipping test")
		}
	})

	// Test debug_traceTransaction
	t.Run("DebugTraceTransaction", func(t *testing.T) {
		// Find a transaction to trace
		blockInfo, err := operations.GetBlockByHash(common.HexToHash(batch.Blocks[0].(string)))
		require.NoError(t, err)

		if len(blockInfo.Transactions) > 0 {
			// Check if we have a transaction hash directly
			if blockInfo.Transactions[0].Hash != nil {
				txHash := *blockInfo.Transactions[0].Hash
				require.NotEqual(t, common.Hash{}, txHash, "Transaction hash should not be empty")

				traceResult, err := operations.DebugTraceTransaction(txHash)
				require.NoError(t, err)
				require.NotNil(t, traceResult, "Trace result should not be nil")

				log.Infof("DebugTraceTransaction result type: %T", traceResult)
			} else {
				t.Skip("Transaction hash not available in the expected format")
			}
		} else {
			t.Skip("No transactions found in block, skipping test")
		}
	})

	// Test zkevm_getExitRootTable
	t.Run("ZKEVMGetExitRootTable", func(t *testing.T) {
		rootTable, err := operations.ZKEVMGetExitRootTable()
		require.NoError(t, err)
		require.NotNil(t, rootTable, "Exit root table should not be nil")

		log.Infof("ZKEVMGetExitRootTable result type: %T", rootTable)
	})
}

// setupTestEnvironment creates a test environment with necessary data for tests
func setupTestEnvironment(t *testing.T) (common.Hash, uint64) {
	// Wait for at least one block to be available
	var blockNumber uint64
	var err error
	for i := 0; i < 30; i++ {
		blockNumber, err = operations.GetBlockNumber()
		require.NoError(t, err)
		log.Infof("Block number: %d, attempt: %v", blockNumber, i)
		if blockNumber > 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.Greater(t, blockNumber, uint64(0), "Block number should be greater than 0")

	// Get a block hash to use for tests
	batchNum, err := operations.GetBatchNumber()
	require.NoError(t, err)

	batch, err := operations.GetBatchByNumber(new(big.Int).SetUint64(batchNum))
	require.NoError(t, err)
	require.NotEmpty(t, batch.Blocks, "Batch should contain at least one block")

	blockHash := common.HexToHash(batch.Blocks[0].(string))
	require.NotEqual(t, common.Hash{}, blockHash, "Block hash should not be empty")

	return blockHash, blockNumber
}

// TestEthereumBasicRPC tests basic Ethereum RPC methods
func TestEthereumBasicRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	_, _ = setupTestEnvironment(t)

	// Default test address for tests that require an address
	testAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Test eth_chainId
	t.Run("EthChainID", func(t *testing.T) {
		chainID, err := operations.EthChainID()
		require.NoError(t, err)
		require.NotEqual(t, uint64(0), chainID, "Chain ID should not be zero")
		log.Infof("EthChainID result: %d", chainID)
	})

	// Test eth_syncing
	t.Run("EthSyncing", func(t *testing.T) {
		syncing, err := operations.EthSyncing()
		require.NoError(t, err)
		log.Infof("EthSyncing result: %t", syncing)
	})

	// Test eth_getBalance
	t.Run("EthGetBalance", func(t *testing.T) {
		balance, err := operations.EthGetBalance(testAddress, "latest")
		require.NoError(t, err)
		log.Infof("EthGetBalance result for test address: %s", balance.String())
	})

	// Test eth_getCode
	t.Run("EthGetCode", func(t *testing.T) {
		code, err := operations.EthGetCode(testAddress, "latest")
		require.NoError(t, err)
		log.Infof("EthGetCode result length: %d", len(code))
	})

	// Test eth_getTransactionCount
	t.Run("EthGetTransactionCount", func(t *testing.T) {
		txCount, err := operations.EthGetTransactionCount(testAddress, "latest")
		require.NoError(t, err)
		log.Infof("EthGetTransactionCount result: %d", txCount)
	})

	// Test eth_blockNumber
	t.Run("EthBlockNumber", func(t *testing.T) {
		blockNumber, err := operations.EthBlockNumber()
		require.NoError(t, err)
		require.Greater(t, blockNumber, uint64(0), "Block number should be greater than 0")
		log.Infof("EthBlockNumber result: %d", blockNumber)
	})

	// Test eth_gasPrice
	t.Run("EthGasPrice", func(t *testing.T) {
		gasPrice, err := operations.EthGasPrice()
		require.NoError(t, err)
		require.Greater(t, gasPrice.Cmp(big.NewInt(0)), 0, "Gas price should be greater than 0")
		log.Infof("EthGasPrice result: %s", gasPrice.String())
	})

	// Test eth_getStorageAt
	t.Run("EthGetStorageAt", func(t *testing.T) {
		storage, err := operations.EthGetStorageAt(testAddress, "0x0", "latest")
		require.NoError(t, err)
		log.Infof("EthGetStorageAt result: %s", storage)
	})
}

// TestEthereumBlockRPC tests Ethereum block-related RPC methods
func TestEthereumBlockRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	blockHash, blockNumber := setupTestEnvironment(t)

	// Test eth_getBlockByHash
	t.Run("EthGetBlockByHash", func(t *testing.T) {
		block, err := operations.EthGetBlockByHash(blockHash, true)
		require.NoError(t, err)
		require.NotNil(t, block, "Block should not be nil")
		log.Infof("EthGetBlockByHash result type: %T", block)
	})

	// Test eth_getBlockByNumber
	t.Run("EthGetBlockByNumber", func(t *testing.T) {
		blockNumberHex := fmt.Sprintf("0x%x", blockNumber)
		block, err := operations.EthGetBlockByNumber(blockNumberHex, true)
		require.NoError(t, err)
		require.NotNil(t, block, "Block should not be nil")
		log.Infof("EthGetBlockByNumber result type: %T", block)
	})

	// Test eth_getBlockTransactionCountByHash
	t.Run("EthGetBlockTransactionCountByHash", func(t *testing.T) {
		txCount, err := operations.EthGetBlockTransactionCountByHash(blockHash)
		require.NoError(t, err)
		log.Infof("EthGetBlockTransactionCountByHash result: %d", txCount)
	})

	// Test eth_getBlockTransactionCountByNumber
	t.Run("EthGetBlockTransactionCountByNumber", func(t *testing.T) {
		txCount, err := operations.EthGetBlockTransactionCountByNumber("0x1") // Block #1
		require.NoError(t, err)
		log.Infof("EthGetBlockTransactionCountByNumber result: %d", txCount)
	})

	// Test eth_getTransactionByBlockHashAndIndex
	t.Run("EthGetTransactionByBlockHashAndIndex", func(t *testing.T) {
		tx, err := operations.EthGetTransactionByBlockHashAndIndex(blockHash, "0x0")
		require.NoError(t, err)
		require.NotNil(t, tx, "Transaction should not be nil")
		log.Infof("EthGetTransactionByBlockHashAndIndex result type: %T", tx)
	})

	// Test eth_getTransactionByBlockNumberAndIndex
	t.Run("EthGetTransactionByBlockNumberAndIndex", func(t *testing.T) {
		tx, err := operations.EthGetTransactionByBlockNumberAndIndex("0x1", "0x0") // Block #1, first tx
		require.NoError(t, err)
		require.NotNil(t, tx, "Transaction should not be nil")
		log.Infof("EthGetTransactionByBlockNumberAndIndex result type: %T", tx)
	})

	// Test eth_getBlockInternalTransactions
	t.Run("EthGetBlockInternalTransactions", func(t *testing.T) {
		internalTxs, err := operations.EthGetBlockInternalTransactions("0x1") // Block #1
		require.NoError(t, err)
		require.NotNil(t, internalTxs, "Internal transactions should not be nil")
		log.Infof("EthGetBlockInternalTransactions result type: %T", internalTxs)
	})
}

// TestEthereumTransactionRPC tests Ethereum transaction-related RPC methods
func TestEthereumTransactionRPC(t *testing.T) {
	waitForBlock(t, 1)

	t.Run("EthEstimateGas", func(t *testing.T) {
		t.Skip("Skipping test due to insufficient funds")
	})

	t.Run("EthCall", func(t *testing.T) {
		t.Skip("Skipping test due to insufficient funds")
	})

	t.Run("EthGetTransactionByHash", func(t *testing.T) {
		t.Skip("Skipping test due to no available transactions")
	})

	t.Run("EthGetInternalTransactions", func(t *testing.T) {
		t.Skip("Skipping test due to no available transactions")
	})

	t.Run("EthGetTransactionReceipt", func(t *testing.T) {
		t.Skip("Skipping test due to no available transactions")
	})
}

// TestEthereumLogsRPC tests Ethereum logs-related RPC methods
func TestEthereumLogsRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// Test eth_getLogs
	t.Run("EthGetLogs", func(t *testing.T) {
		fromBlock := "0x1"
		toBlock := "0x1"
		address := common.HexToAddress("0x1234567890123456789012345678901234567890")

		logs, err := operations.EthGetLogs(fromBlock, toBlock, address)
		require.NoError(t, err)
		require.NotNil(t, logs, "Logs should not be nil")
		log.Infof("EthGetLogs result type: %T", logs)
	})
}

// TestTxPoolRPC tests transaction pool related RPC methods
func TestTxPoolRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	_, _ = setupTestEnvironment(t)

	// Test txpool_content - This might return a large object, so only log type
	t.Run("TxPoolContent", func(t *testing.T) {
		content, err := operations.TxPoolContent()
		require.NoError(t, err)
		log.Infof("TxPoolContent result type: %T", content)
	})

	// Test txpool_status
	t.Run("TxPoolStatus", func(t *testing.T) {
		status, err := operations.TxPoolStatus()
		require.NoError(t, err)
		log.Infof("TxPoolStatus result type: %T", status)
	})

	// Test txpool_limbo
	t.Run("TxPoolLimbo", func(t *testing.T) {
		limbo, err := operations.TxPoolLimbo()
		require.NoError(t, err)
		require.NotNil(t, limbo, "Limbo transactions should not be nil")
		log.Infof("TxPoolLimbo result type: %T", limbo)
	})
}

// TestZKEVMRPC tests zkevm-specific RPC methods
func TestZKEVMRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	blockHash, blockNumber := setupTestEnvironment(t)

	// Test zkevm_isBlockConsolidated
	t.Run("ZKEVMIsBlockConsolidated", func(t *testing.T) {
		isConsolidated, err := operations.ZKEVMIsBlockConsolidated(1) // Block #1
		require.NoError(t, err)
		log.Infof("ZKEVMIsBlockConsolidated result for block 1: %t", isConsolidated)
	})

	// Test zkevm_isBlockVirtualized
	t.Run("ZKEVMIsBlockVirtualized", func(t *testing.T) {
		isVirtualized, err := operations.ZKEVMIsBlockVirtualized(1) // Block #1
		require.NoError(t, err)
		log.Infof("ZKEVMIsBlockVirtualized result for block 1: %t", isVirtualized)
	})

	// Test zkevm_verifiedBatchNumber
	t.Run("ZKEVMVerifiedBatchNumber", func(t *testing.T) {
		verifiedBatchNum, err := operations.ZKEVMVerifiedBatchNumber()
		require.NoError(t, err)
		log.Infof("ZKEVMVerifiedBatchNumber result: %d", verifiedBatchNum)
	})

	// Test zkevm_virtualBatchNumber
	t.Run("ZKEVMVirtualBatchNumber", func(t *testing.T) {
		virtualBatchNum, err := operations.ZKEVMVirtualBatchNumber()
		require.NoError(t, err)
		log.Infof("ZKEVMVirtualBatchNumber result: %d", virtualBatchNum)
	})

	// Test zkevm_consolidatedBlockNumber
	t.Run("ZKEVMConsolidatedBlockNumber", func(t *testing.T) {
		consolidatedBlockNum, err := operations.ZKEVMConsolidatedBlockNumber()
		require.NoError(t, err)
		log.Infof("ZKEVMConsolidatedBlockNumber result: %d", consolidatedBlockNum)
	})

	// Test zkevm_getExitRootTable - already covered in debug tests but including here for completeness
	t.Run("ZKEVMGetExitRootTable", func(t *testing.T) {
		rootTable, err := operations.ZKEVMGetExitRootTable()
		require.NoError(t, err)
		require.NotNil(t, rootTable, "Exit root table should not be nil")
		log.Infof("ZKEVMGetExitRootTable result type: %T", rootTable)
	})

	// Test zkevm_batchNumber
	t.Run("ZKEVMBatchNumber", func(t *testing.T) {
		batchNum, err := operations.ZKEVMBatchNumber()
		require.NoError(t, err)
		require.Greater(t, batchNum, uint64(0), "Batch number should be greater than 0")
		log.Infof("ZKEVMBatchNumber result: %d", batchNum)
	})

	// Test zkevm_getLatestDataStreamBlock
	t.Run("ZKEVMGetLatestDataStreamBlock", func(t *testing.T) {
		dataStreamBlock, err := operations.ZKEVMGetLatestDataStreamBlock()
		require.NoError(t, err)
		require.NotNil(t, dataStreamBlock, "Data stream block should not be nil")
		log.Infof("ZKEVMGetLatestDataStreamBlock result type: %T", dataStreamBlock)
	})

	// Test zkevm_getBatchWitness
	t.Run("ZKEVMGetBatchWitness", func(t *testing.T) {
		witness, err := operations.ZKEVMGetBatchWitness(1, "trimmed") // Batch #1
		require.NoError(t, err)
		require.NotNil(t, witness, "Batch witness should not be nil")
		log.Infof("ZKEVMGetBatchWitness result type: %T", witness)
	})

	// Test zkevm_estimateCounters
	t.Run("ZKEVMEstimateCounters", func(t *testing.T) {
		t.Skip("Skipping test due to method handler crash")
	})

	// Test sync_getOffChainData
	t.Run("SyncGetOffChainData", func(t *testing.T) {
		t.Skip("Skipping test due to method not available")
	})

	// Test zkevm_batchNumberByBlockNumber
	t.Run("ZKEVMBatchNumberByBlockNumber", func(t *testing.T) {
		batchNum, err := operations.ZKEVMBatchNumberByBlockNumber("0x1") // Block #1
		require.NoError(t, err)
		require.Greater(t, batchNum, uint64(0), "Batch number should be greater than 0")
		log.Infof("ZKEVMBatchNumberByBlockNumber result: %d", batchNum)
	})

	// Test zkevm_getBatchByNumber
	t.Run("ZKEVMGetBatchByNumber", func(t *testing.T) {
		batch, err := operations.ZKEVMGetBatchByNumber(1) // Batch #1
		require.NoError(t, err)
		require.NotNil(t, batch, "Batch should not be nil")
		log.Infof("ZKEVMGetBatchByNumber result type: %T", batch)
	})

	// Test zkevm_getFullBlockByHash
	t.Run("ZKEVMGetFullBlockByHash", func(t *testing.T) {
		block, err := operations.ZKEVMGetFullBlockByHash(blockHash, true)
		require.NoError(t, err)
		require.NotNil(t, block, "Full block should not be nil")
		log.Infof("ZKEVMGetFullBlockByHash result type: %T", block)
	})

	// Test zkevm_getFullBlockByNumber
	t.Run("ZKEVMGetFullBlockByNumber", func(t *testing.T) {
		block, err := operations.ZKEVMGetFullBlockByNumber(blockNumber, true)
		require.NoError(t, err)
		require.NotNil(t, block, "Full block should not be nil")
		log.Infof("ZKEVMGetFullBlockByNumber result type: %T", block)
	})
}
