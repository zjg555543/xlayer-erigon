package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	logv3 "github.com/ledgerwatch/log/v3"
)

// executeDupCursorOperationsWithCommit executes dupCursor operations with guaranteed commit for Aggressive mode
func executeDupCursorOperationsWithCommit(dbPath string, keepRecentBatches uint64, fastDupCursorMode, safeFastMode bool, log logv3.Logger, ctx context.Context) (int, uint64) {
	fmt.Printf("\n=== Phase 1c: DupCursor Data Cleanup ===\n")

	// Open database for dupCursor operations
	chaindb, _, err := openDatabase(dbPath, kv.ChainDB, log)
	if err != nil {
		logv3.Error("Failed to open database for dupCursor operations", "error", err)
		return 0, 0
	}

	// Ensure database is closed after dupCursor operations
	defer func() {
		fmt.Printf("Phase 1c completed, closing database...\n")
		chaindb.Close()
		fmt.Printf("✓ Phase 1c database closed\n")

		// Give MDBX some time to complete cleanup
		time.Sleep(2 * time.Second)
	}()

	tx, err := chaindb.BeginRw(ctx)
	if err != nil {
		logv3.Error("Failed to start transaction for dupCursor operations", "error", err)
		return 0, 0
	}

	var committed bool
	defer func() {
		if !committed {
			fmt.Printf("Rolling back dupCursor operations transaction...\n")
			tx.Rollback()
		}
	}()

	if fastDupCursorMode {
		fmt.Printf("⚡ Fast dupCursor mode enabled: using direct cursor deletion for maximum performance\n")
	} else if safeFastMode {
		fmt.Printf("🛡️⚡ Safe-Fast dupCursor mode enabled: balanced performance and safety\n")
	} else {
		fmt.Printf("🛡️ Standard dupCursor mode enabled: maximum safety with batch processing\n")
	}

	deletedDupCursorRecords, err := pruneHistoricalDupCursorData(tx, keepRecentBatches, fastDupCursorMode, safeFastMode)
	if err != nil {
		logv3.Error("Failed to perform dupCursor data cleanup", "error", err)
		return 0, 0
	}

	// Commit transaction
	fmt.Printf("Committing Phase 1c (dupCursor operations)...\n")
	if err := tx.Commit(); err != nil {
		logv3.Error("Failed to commit dupCursor operations", "error", err)
		return 0, 0
	}
	committed = true

	fmt.Printf("✓ Phase 1c committed successfully\n")
	fmt.Printf("✓ Historical dupCursor data cleanup completed: %d records deleted\n", deletedDupCursorRecords)

	// Estimate size based on record count (conservative estimate: 100 bytes per record)
	estimatedSize := uint64(deletedDupCursorRecords) * 100
	return deletedDupCursorRecords, estimatedSize
}

// executeHeaderEcosystemCleanupWithCommit executes Header ecosystem cleanup with guaranteed commit for Aggressive mode
// Reuses existing copy-truncate-restore infrastructure for consistency
func executeHeaderEcosystemCleanupWithCommit(dbPath string, keepRecentBatches uint64, genesisHeight uint64, log logv3.Logger, ctx context.Context) (int, uint64) {
	fmt.Printf("\n=== Phase 1b: Header Ecosystem Cleanup ===\n")

	// Open database for Header ecosystem operations
	chaindb, _, err := openDatabase(dbPath, kv.ChainDB, log)
	if err != nil {
		logv3.Error("Failed to open database for Header ecosystem cleanup", "error", err)
		return 0, 0
	}

	// Ensure database is closed after Header ecosystem operations
	defer func() {
		fmt.Printf("Phase 1b completed, closing database...\n")
		chaindb.Close()
		fmt.Printf("✓ Phase 1b database closed\n")

		// Give MDBX some time to complete cleanup
		time.Sleep(3 * time.Second)
	}()

	tx, err := chaindb.BeginRw(ctx)
	if err != nil {
		logv3.Error("Failed to start transaction for Header ecosystem cleanup", "error", err)
		return 0, 0
	}

	var committed bool
	defer func() {
		if !committed {
			fmt.Printf("Rolling back Header ecosystem operations transaction...\n")
			tx.Rollback()
		}
	}()

	fmt.Printf("🏗️ Processing Header ecosystem tables: CanonicalHeader, HeaderNumber, Header, BlockBody\n")
	fmt.Printf("🎯 Strategy: Copy recent %d batches → Clear ALL data → Restore recent data\n", keepRecentBatches)
	fmt.Printf("📋 Reusing proven copy-truncate-restore logic for consistency\n")

	// Get latest batch number
	hermezDb := hermez_db.NewHermezDbReader(tx)
	latestBatch, err := getLatestBatchNumber(tx)
	if err != nil {
		logv3.Error("Failed to get latest batch number", "error", err)
		return 0, 0
	}

	var keepFromBatch uint64
	if latestBatch > keepRecentBatches {
		keepFromBatch = latestBatch - keepRecentBatches + 1
	} else {
		fmt.Printf("No Header ecosystem data to prune\n")
		return 0, 0
	}

	// Execute Header ecosystem cleanup using existing copy-truncate-restore infrastructure
	deletedHeaderRecords, err := executeAggressiveHeaderEcosystemCleanup(tx, hermezDb, keepFromBatch, latestBatch, genesisHeight)
	if err != nil {
		logv3.Error("Failed to perform Header ecosystem cleanup", "error", err)
		return 0, 0
	}

	// Commit transaction
	fmt.Printf("Committing Phase 1b (Header ecosystem operations)...\n")
	if err := tx.Commit(); err != nil {
		logv3.Error("Failed to commit Header ecosystem operations", "error", err)
		return 0, 0
	}
	committed = true

	fmt.Printf("✓ Phase 1b committed successfully\n")
	fmt.Printf("🎉 Header ecosystem records processed: %d\n", deletedHeaderRecords)
	fmt.Printf("🎯 Nonce query consistency should now be resolved\n")

	// Estimate size based on record count (conservative estimate: 200 bytes per header record)
	estimatedSize := uint64(deletedHeaderRecords) * 200
	return deletedHeaderRecords, estimatedSize
}
