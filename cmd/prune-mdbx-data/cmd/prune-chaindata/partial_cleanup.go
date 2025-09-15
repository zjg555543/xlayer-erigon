package main

import (
	"encoding/binary"
	"fmt"

	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
)

// LatestBlockInfo stores essential information of the latest block
type LatestBlockInfo struct {
	Number      uint64
	Hash        common.Hash
	Header      *types.Header
	TxCount     int
	NeedRestore bool
}

// BlockData represents data for a single block across all tables
type BlockData struct {
	BlockNo uint64
	Data    map[string][]byte // table -> data
}

// getLatestBlockNumber reads the latest block number from CanonicalHeader table
func getLatestBlockNumber(tx kv.RwTx) (uint64, error) {
	cursor, err := tx.Cursor("CanonicalHeader")
	if err != nil {
		return 0, fmt.Errorf("failed to open CanonicalHeader cursor: %w", err)
	}
	defer cursor.Close()

	// Move to last entry
	key, _, err := cursor.Last()
	if err != nil {
		return 0, fmt.Errorf("failed to get last entry: %w", err)
	}
	if len(key) == 0 {
		return 0, fmt.Errorf("no blocks found in CanonicalHeader table")
	}

	// CanonicalHeader key is block number (8 bytes, big endian)
	if len(key) != 8 {
		return 0, fmt.Errorf("invalid key length in CanonicalHeader: %d", len(key))
	}

	blockNumber := binary.BigEndian.Uint64(key)
	return blockNumber, nil
}

// getLatestBatchNumber reads the latest batch number from BLOCKBATCHES table
func getLatestBatchNumber(tx kv.Tx) (uint64, error) {
	c, err := tx.Cursor(hermez_db.BLOCKBATCHES)
	if err != nil {
		return 0, err
	}
	defer c.Close()

	// get the last entry from the table
	k, v, err := c.Last()
	if err != nil {
		return 0, err
	}
	if k == nil {
		return 0, nil
	}

	return hermez_db.BytesToUint64(v), nil
}

// partialPruneBatchTables performs batch-based pruning on block-related tables
func partialPruneBatchTables(tx kv.RwTx, keepRecentBatches uint64, genesisHeight uint64) (int, int, error) {
	hermezDb := hermez_db.NewHermezDbReader(tx)

	// 1. Get latest batch number
	latestBatch, err := getLatestBatchNumber(tx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get latest batch number: %w", err)
	}

	fmt.Printf("Latest batch number: %d\n", latestBatch)
	fmt.Printf("Keeping recent %d batches\n", keepRecentBatches)

	// 2. Calculate batch range to keep
	var pruneBefore uint64
	if latestBatch > keepRecentBatches {
		pruneBefore = latestBatch - keepRecentBatches + 1
	} else {
		fmt.Printf("No batches to prune (total batches <= keep recent batches)\n")
		return 0, 0, nil
	}

	fmt.Printf("Pruning batches 0 to %d (before batch %d)\n", pruneBefore-1, pruneBefore)

	// 3. Check if any batches need to be pruned
	if pruneBefore == 0 {
		fmt.Printf("No batches to prune\n")
		return 0, 0, nil
	}

	// Early exit check
	if pruneBefore > latestBatch {
		fmt.Printf("Error: pruneBefore (%d) > latestBatch (%d)\n", pruneBefore, latestBatch)
		return 0, 0, nil
	}

	// 4. Execute the pruning with optimized copy-truncate-restore strategy
	fmt.Printf("🚀 Using optimized copy-truncate-restore strategy\n")
	fmt.Printf("📋 Strategy: Copy data for batches %d-%d → Clear ALL → Restore copied data\n", pruneBefore, latestBatch)

	// Determine batches to keep (pruneBefore and later)
	keepFromBatch := pruneBefore
	fmt.Printf("Preserving batches %d to %d, deleting batches 0 to %d\n", keepFromBatch, latestBatch, pruneBefore-1)

	// Use optimized strategy: copy recent data, truncate tables, restore data
	return executeCopyTruncateRestore(tx, hermezDb, keepFromBatch, latestBatch, pruneBefore, genesisHeight)
}
