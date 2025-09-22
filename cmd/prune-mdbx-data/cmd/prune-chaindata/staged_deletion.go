package main

import (
	"context"
	"fmt"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/ledgerwatch/erigon-lib/kv"
	mdbxpkg "github.com/ledgerwatch/erigon-lib/kv/mdbx"
	logv3 "github.com/ledgerwatch/log/v3"
)

// Staged deletion flow - User suggested optimization
// Stage 1: Delete small tables → Commit → Close DB
// Stage 2: Reopen DB → Delete each large table individually → Commit → Close DB

const (
	// Large table threshold: tables larger than 1GB are considered large tables
	LARGE_TABLE_THRESHOLD = 1 * 1024 * 1024 * 1024 // 1GB
)

// executeStagedTableDeletion executes staged table deletion
func executeStagedTableDeletion(
	dbPath string,
	sortedTables []tableSizeInfo,
	preCollectedStats map[string]struct {
		entries   uint64
		sizeBytes uint64
		pages     uint64
	},
	partiallyPrunedTables map[string]bool,
	pruneLevel PruneLevel,
	log logv3.Logger,
	ctx context.Context,
) (int, int, uint64) {

	if len(sortedTables) == 0 {
		fmt.Printf("\n=== No tables to delete ===\n")
		return 0, 0, 0
	}

	// Classify tables: small tables, large NoSync tables, and large regular tables
	var smallTables, largeNoSyncTables, largeRegularTables []tableSizeInfo

	// NoSync tables that need special handling
	noSyncTables := map[string]bool{
		"hermez_txPricePercentage":          true,
		"hermez_intermediate_tx_stateRoots": true,
		"hermez_blockBatches":               true,
		"hermez_globalExitRoots":            true,
		"hermez_stateRoots":                 true,
		"hermez_batch_witnesses":            true,
		"hermez_batch_counters":             true,
	}

	for _, table := range sortedTables {
		if table.size >= LARGE_TABLE_THRESHOLD {
			if noSyncTables[table.name] {
				largeNoSyncTables = append(largeNoSyncTables, table)
			} else {
				largeRegularTables = append(largeRegularTables, table)
			}
		} else {
			smallTables = append(smallTables, table)
		}
	}

	fmt.Printf("\n=== Staged Deletion Strategy ===\n")
	fmt.Printf("Small tables (<%s): %d tables\n", datasize.ByteSize(LARGE_TABLE_THRESHOLD).HumanReadable(), len(smallTables))
	fmt.Printf("Large NoSync tables (>=%s): %d tables\n", datasize.ByteSize(LARGE_TABLE_THRESHOLD).HumanReadable(), len(largeNoSyncTables))
	fmt.Printf("Large regular tables (>=%s): %d tables\n", datasize.ByteSize(LARGE_TABLE_THRESHOLD).HumanReadable(), len(largeRegularTables))

	var totalDeletedCount, totalActuallyDeletedTables int
	var totalActualDeletedSize uint64

	// === Stage 1: Delete small tables ===
	if len(smallTables) > 0 {
		deletedCount, actuallyDeletedTables, actualDeletedSize := executeSmallTableDeletion(
			dbPath, smallTables, preCollectedStats, partiallyPrunedTables, pruneLevel, log, ctx)

		totalDeletedCount += deletedCount
		totalActuallyDeletedTables += actuallyDeletedTables
		totalActualDeletedSize += actualDeletedSize
	}

	// === Stage 2: Delete large NoSync tables individually ===
	if len(largeNoSyncTables) > 0 {
		deletedCount, actuallyDeletedTables, actualDeletedSize := executeLargeNoSyncTablesDeletion(
			dbPath, largeNoSyncTables, preCollectedStats, log, ctx)

		totalDeletedCount += deletedCount
		totalActuallyDeletedTables += actuallyDeletedTables
		totalActualDeletedSize += actualDeletedSize
	}

	// === Stage 3: Delete large regular tables individually ===
	if len(largeRegularTables) > 0 {
		deletedCount, actuallyDeletedTables, actualDeletedSize := executeLargeRegularTablesDeletion(
			dbPath, largeRegularTables, preCollectedStats, log, ctx)

		totalDeletedCount += deletedCount
		totalActuallyDeletedTables += actuallyDeletedTables
		totalActualDeletedSize += actualDeletedSize
	}

	fmt.Printf("\n=== Staged Deletion Completed ===\n")
	fmt.Printf("Total tables deleted: %d (with actual data: %d)\n", totalDeletedCount, totalActuallyDeletedTables)
	fmt.Printf("Total space freed: %s\n", datasize.ByteSize(totalActualDeletedSize).HumanReadable())

	return totalDeletedCount, totalActuallyDeletedTables, totalActualDeletedSize
}

// executeSmallTableDeletion executes small table deletion (Stage 1)
func executeSmallTableDeletion(
	dbPath string,
	smallTables []tableSizeInfo,
	preCollectedStats map[string]struct {
		entries   uint64
		sizeBytes uint64
		pages     uint64
	},
	partiallyPrunedTables map[string]bool,
	pruneLevel PruneLevel,
	log logv3.Logger,
	ctx context.Context,
) (int, int, uint64) {

	fmt.Printf("\n=== Stage 1: Small Table Deletion (%d tables) ===\n", len(smallTables))

	// Open database
	chaindb, _, err := openDatabase(dbPath, kv.ChainDB, log)
	if err != nil {
		logv3.Error("Failed to open database for small table deletion", "error", err)
		return 0, 0, 0
	}

	// Ensure database is closed
	defer func() {
		fmt.Printf("Stage 1 completed, closing database...\n")
		chaindb.Close()
		fmt.Printf("✓ Stage 1 database closed\n")

		// Give MDBX some time to complete cleanup
		time.Sleep(2 * time.Second)
	}()

	// Start transaction
	tx, err := chaindb.BeginRw(ctx)
	if err != nil {
		logv3.Error("Failed to start transaction for small table deletion", "error", err)
		return 0, 0, 0
	}

	// Ensure transaction is properly handled
	var committed bool
	defer func() {
		if !committed {
			fmt.Printf("Rolling back small table deletion transaction...\n")
			tx.Rollback()
		}
	}()

	// Execute ONLY small table deletion (avoid mixing with large table logic)
	deletedCount, actuallyDeletedTables, actualDeletedSize := executeSimpleTableDeletion(
		tx, smallTables, preCollectedStats)

	// Commit transaction
	fmt.Printf("Committing Stage 1 transaction...\n")
	if err := tx.Commit(); err != nil {
		logv3.Error("Failed to commit small table deletions", "error", err)
		return 0, 0, 0
	}
	committed = true

	fmt.Printf("✓ Stage 1 completed: deleted %d small tables, freed %s\n",
		actuallyDeletedTables, datasize.ByteSize(actualDeletedSize).HumanReadable())

	return deletedCount, actuallyDeletedTables, actualDeletedSize
}

// executeSingleLargeTableDeletion processes a single large table in its own transaction
func executeSingleLargeTableDeletion(
	dbPath string,
	largeTable tableSizeInfo,
	preCollectedStats map[string]struct {
		entries   uint64
		sizeBytes uint64
		pages     uint64
	},
	log logv3.Logger,
	ctx context.Context,
) (int, int, uint64) {

	// Open database for this single table
	chaindb, _, err := openDatabase(dbPath, kv.ChainDB, log)
	if err != nil {
		logv3.Error("Failed to open database for large table deletion", "table", largeTable.name, "error", err)
		return 0, 0, 0
	}

	// Ensure database is closed after processing this table
	defer func() {
		fmt.Printf("Closing database after processing %s...\n", largeTable.name)
		chaindb.Close()
		fmt.Printf("✓ Database closed for %s\n", largeTable.name)
	}()

	// Start transaction for this single table
	tx, err := chaindb.BeginRw(ctx)
	if err != nil {
		logv3.Error("Failed to start transaction for large table deletion", "table", largeTable.name, "error", err)
		return 0, 0, 0
	}

	// Ensure transaction is properly handled
	var committed bool
	defer func() {
		if !committed {
			fmt.Printf("Rolling back transaction for %s...\n", largeTable.name)
			tx.Rollback()
		}
	}()

	// Process only this single large table with simple deletion
	deletedCount, actuallyDeletedTables, actualDeletedSize := executeSingleTableSimpleDeletion(
		tx, largeTable, preCollectedStats)

	// Commit transaction for this table
	fmt.Printf("Committing transaction for %s...\n", largeTable.name)
	if err := tx.Commit(); err != nil {
		logv3.Error("Failed to commit large table deletion", "table", largeTable.name, "error", err)
		return 0, 0, 0
	}
	committed = true

	return deletedCount, actuallyDeletedTables, actualDeletedSize
}

// openDatabaseWithRetry improved database opening function with retry mechanism
func openDatabaseWithRetry(dbPath string, label kv.Label, log logv3.Logger, maxRetries int) (kv.RwDB, error) {
	ctx := context.Background()

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			fmt.Printf("Retrying database open (%d/%d)...\n", i+1, maxRetries)
			time.Sleep(time.Duration(i*2) * time.Second) // Incremental wait time
		}

		var opts mdbxpkg.MdbxOpts
		if label == kv.ChainDB {
			opts = mdbxpkg.NewMDBX(log).Path(dbPath).Label(label).WithTableCfg(mdbxpkg.WithChaindataTables)
		} else {
			opts = mdbxpkg.NewMDBX(log).Path(dbPath).Label(label)
		}

		db, err := opts.Open(ctx)
		if err == nil {
			fmt.Printf("✓ Database opened successfully\n")
			return db, nil
		}

		fmt.Printf("⚠️ Database open failed (attempt %d/%d): %v\n", i+1, maxRetries, err)
	}

	return nil, fmt.Errorf("failed to open database after %d retries", maxRetries)
}

// executeSimpleTableDeletion performs simple table deletion without complex optimization
func executeSimpleTableDeletion(tx kv.RwTx, tables []tableSizeInfo, preCollectedStats map[string]struct {
	entries   uint64
	sizeBytes uint64
	pages     uint64
}) (int, int, uint64) {

	var actualDeletedSize uint64
	actuallyDeletedTables := 0

	fmt.Printf("🗑️ Deleting %d small tables with simple strategy...\n", len(tables))

	for i, table := range tables {
		fmt.Printf("Clearing table (%d/%d): %s\n", i+1, len(tables), table.name)

		// Simple ClearBucket deletion
		err := tx.ClearBucket(table.name)
		if err != nil {
			fmt.Printf("⚠️ Failed to clear table %s: %v\n", table.name, err)
			continue
		}

		// Track actual deleted size
		if stats, exists := preCollectedStats[table.name]; exists {
			actualDeletedSize += stats.sizeBytes
			fmt.Printf("✓ Cleared table: %s (%s, %d entries)\n",
				table.name,
				datasize.ByteSize(stats.sizeBytes).HumanReadable(),
				stats.entries)
		} else {
			fmt.Printf("✓ Cleared table: %s (size unknown)\n", table.name)
		}

		actuallyDeletedTables++
	}

	return len(tables), actuallyDeletedTables, actualDeletedSize
}

// executeSingleTableSimpleDeletion performs simple deletion for a single large table
func executeSingleTableSimpleDeletion(tx kv.RwTx, table tableSizeInfo, preCollectedStats map[string]struct {
	entries   uint64
	sizeBytes uint64
	pages     uint64
}) (int, int, uint64) {

	fmt.Printf("🗑️ Deleting large table: %s (%s)\n",
		table.name,
		datasize.ByteSize(table.size).HumanReadable())

	// Simple ClearBucket deletion for large table
	err := tx.ClearBucket(table.name)
	if err != nil {
		fmt.Printf("⚠️ Failed to clear large table %s: %v\n", table.name, err)
		return 0, 0, 0
	}

	// Track actual deleted size
	var actualDeletedSize uint64
	if stats, exists := preCollectedStats[table.name]; exists {
		actualDeletedSize = stats.sizeBytes
		fmt.Printf("✓ Cleared large table: %s (%s, %d entries)\n",
			table.name,
			datasize.ByteSize(stats.sizeBytes).HumanReadable(),
			stats.entries)
	} else {
		actualDeletedSize = table.size // Use table.size as fallback
		fmt.Printf("✓ Cleared large table: %s (%s)\n",
			table.name,
			datasize.ByteSize(table.size).HumanReadable())
	}

	return 1, 1, actualDeletedSize
}

// executeLargeNoSyncTablesDeletion processes large NoSync tables with proper NoSync transactions
func executeLargeNoSyncTablesDeletion(
	dbPath string,
	largeNoSyncTables []tableSizeInfo,
	preCollectedStats map[string]struct {
		entries   uint64
		sizeBytes uint64
		pages     uint64
	},
	log logv3.Logger,
	ctx context.Context,
) (int, int, uint64) {

	if len(largeNoSyncTables) == 0 {
		return 0, 0, 0
	}

	fmt.Printf("\n=== Stage 2: Large NoSync Table Deletion (%d tables) ===\n", len(largeNoSyncTables))

	var totalDeleted, totalActuallyDeleted int
	var totalDeletedSize uint64

	for i, table := range largeNoSyncTables {
		fmt.Printf("\n🔄 Processing large NoSync table (%d/%d): %s (%s)...\n",
			i+1, len(largeNoSyncTables), table.name, datasize.ByteSize(table.size).HumanReadable())

		deleted, actualDeleted, deletedSize := executeSingleNoSyncTableDeletion(dbPath, table, preCollectedStats, log, ctx)

		totalDeleted += deleted
		totalActuallyDeleted += actualDeleted
		totalDeletedSize += deletedSize

		// Small pause between tables to let MDBX cleanup
		if i < len(largeNoSyncTables)-1 {
			fmt.Printf("⏸️ Pausing 3 seconds before next NoSync table...\n")
			time.Sleep(3 * time.Second)
		}
	}

	fmt.Printf("✓ Stage 2 completed: deleted %d NoSync tables, freed %s\n",
		totalActuallyDeleted, datasize.ByteSize(totalDeletedSize).HumanReadable())

	return totalDeleted, totalActuallyDeleted, totalDeletedSize
}

// executeLargeRegularTablesDeletion processes large regular tables with normal transactions
func executeLargeRegularTablesDeletion(
	dbPath string,
	largeRegularTables []tableSizeInfo,
	preCollectedStats map[string]struct {
		entries   uint64
		sizeBytes uint64
		pages     uint64
	},
	log logv3.Logger,
	ctx context.Context,
) (int, int, uint64) {

	if len(largeRegularTables) == 0 {
		return 0, 0, 0
	}

	fmt.Printf("\n=== Stage 3: Large Regular Table Deletion (%d tables) ===\n", len(largeRegularTables))

	var totalDeleted, totalActuallyDeleted int
	var totalDeletedSize uint64

	for i, table := range largeRegularTables {
		fmt.Printf("\n🔄 Processing large regular table (%d/%d): %s (%s)...\n",
			i+1, len(largeRegularTables), table.name, datasize.ByteSize(table.size).HumanReadable())

		deleted, actualDeleted, deletedSize := executeSingleLargeTableDeletion(dbPath, table, preCollectedStats, log, ctx)

		totalDeleted += deleted
		totalActuallyDeleted += actualDeleted
		totalDeletedSize += deletedSize

		// Small pause between tables to let MDBX cleanup
		if i < len(largeRegularTables)-1 {
			fmt.Printf("⏸️ Pausing 2 seconds before next table...\n")
			time.Sleep(2 * time.Second)
		}
	}

	fmt.Printf("✓ Stage 3 completed: deleted %d regular tables, freed %s\n",
		totalActuallyDeleted, datasize.ByteSize(totalDeletedSize).HumanReadable())

	return totalDeleted, totalActuallyDeleted, totalDeletedSize
}

// executeSingleNoSyncTableDeletion performs NoSync deletion for a single large NoSync table
func executeSingleNoSyncTableDeletion(
	dbPath string,
	table tableSizeInfo,
	preCollectedStats map[string]struct {
		entries   uint64
		sizeBytes uint64
		pages     uint64
	},
	log logv3.Logger,
	ctx context.Context,
) (int, int, uint64) {

	// Open database for this specific NoSync table
	chaindb, err := openDatabaseWithRetry(dbPath, kv.ChainDB, log, 3)
	if err != nil {
		logv3.Error("Failed to open database for NoSync table deletion", "table", table.name, "error", err)
		return 0, 0, 0
	}

	// Ensure database is closed
	defer func() {
		fmt.Printf("NoSync table %s completed, closing database...\n", table.name)
		chaindb.Close()
		fmt.Printf("✓ NoSync table %s database closed\n", table.name)

		// Give MDBX more time for NoSync cleanup
		time.Sleep(3 * time.Second)
	}()

	fmt.Printf("🗑️ Deleting large NoSync table: %s (%s)\n",
		table.name,
		datasize.ByteSize(table.size).HumanReadable())

	// Use NoSync transaction for better performance with NoSync tables
	var actualDeletedSize uint64
	err = chaindb.UpdateNosync(ctx, func(tx kv.RwTx) error {
		// Simple ClearBucket deletion for NoSync table
		err := tx.ClearBucket(table.name)
		if err != nil {
			return fmt.Errorf("failed to clear NoSync table %s: %w", table.name, err)
		}

		// Track actual deleted size
		if stats, exists := preCollectedStats[table.name]; exists {
			actualDeletedSize = stats.sizeBytes
			fmt.Printf("✅ Cleared large NoSync table: %s (%s, %d entries)\n",
				table.name,
				datasize.ByteSize(stats.sizeBytes).HumanReadable(),
				stats.entries)
		} else {
			actualDeletedSize = table.size // Use table.size as fallback
			fmt.Printf("✅ Cleared large NoSync table: %s (%s)\n",
				table.name,
				datasize.ByteSize(table.size).HumanReadable())
		}

		return nil
	})

	if err != nil {
		fmt.Printf("⚠️ Failed to delete NoSync table %s: %v\n", table.name, err)
		return 0, 0, 0
	}

	return 1, 1, actualDeletedSize
}
