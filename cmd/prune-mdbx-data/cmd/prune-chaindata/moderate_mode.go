package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ledgerwatch/erigon-lib/kv"
	logv3 "github.com/ledgerwatch/log/v3"
)

// executeBatchOperationsWithCommit executes batch operations with guaranteed commit for Moderate mode
func executeBatchOperationsWithCommit(dbPath string, keepRecentBatches uint64, genesisHeight uint64, log logv3.Logger, ctx context.Context) (int, int) {
	fmt.Printf("\n=== Phase 1: Batch Operations ===\n")

	// Open database for batch operations
	chaindb, _, err := openDatabase(dbPath, kv.ChainDB, log)
	if err != nil {
		logv3.Error("Failed to open database for batch operations", "error", err)
		return 0, 0
	}

	// Ensure database is closed after batch operations
	defer func() {
		fmt.Printf("Phase 1 completed, closing database...\n")
		chaindb.Close()
		fmt.Printf("✓ Phase 1 database closed\n")

		// Give MDBX some time to complete cleanup
		time.Sleep(2 * time.Second)
	}()

	tx, err := chaindb.BeginRw(ctx)
	if err != nil {
		logv3.Error("Failed to start transaction for batch operations", "error", err)
		return 0, 0
	}

	var committed bool
	defer func() {
		if !committed {
			fmt.Printf("Rolling back batch operations transaction...\n")
			tx.Rollback()
		}
	}()

	deletedBatches, deletedBlocks, err := partialPruneBatchTables(tx, keepRecentBatches, genesisHeight)
	if err != nil {
		logv3.Error("Failed to perform batch operations", "error", err)
		return 0, 0
	}

	// Commit transaction
	fmt.Printf("Committing Phase 1 (batch operations)...\n")
	if err := tx.Commit(); err != nil {
		logv3.Error("Failed to commit batch operations", "error", err)
		return 0, 0
	}
	committed = true

	fmt.Printf("✓ Phase 1 committed successfully\n")
	fmt.Printf("✓ Batch operations completed: %d batches, %d blocks processed\n", deletedBatches, deletedBlocks)

	return deletedBatches, deletedBlocks
}
