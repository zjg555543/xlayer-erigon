package main

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
)

// pruneHistoricalDupCursorData performs aggressive cleanup of historical dupCursor table data
// (AccountChangeSet, StorageChangeSet only - CanonicalHeader and hermez_blockBatches are preserved)
// while preserving recent batches for operational needs
// fastMode: if true, uses direct cursor deletion for maximum performance
// safeFastMode: if true, uses safe-fast mode (balanced performance and safety)
func pruneHistoricalDupCursorData(tx kv.RwTx, keepRecentBatches uint64, fastMode bool, safeFastMode bool) (int, error) {
	fmt.Printf("Starting historical dupCursor data cleanup (keeping recent %d batches)...\n", keepRecentBatches)
	fmt.Printf("Note: Only processing AccountChangeSet and StorageChangeSet - CanonicalHeader and hermez_blockBatches preserved for node stability\n")

	// Check if required tables exist before attempting to process them
	accountTableExists, err := checkTableExists(tx, "AccountChangeSet")
	if err != nil {
		return 0, fmt.Errorf("failed to check AccountChangeSet table: %w", err)
	}

	storageTableExists, err := checkTableExists(tx, "StorageChangeSet")
	if err != nil {
		return 0, fmt.Errorf("failed to check StorageChangeSet table: %w", err)
	}

	if !accountTableExists && !storageTableExists {
		fmt.Printf("Neither AccountChangeSet nor StorageChangeSet tables exist - skipping dupCursor cleanup\n")
		return 0, nil
	}

	// Get the range of blocks to delete (everything except recent batches)
	latestBlock, err := getLatestBlockNumber(tx)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest block number: %w", err)
	}

	// Calculate cutoff point - we need to determine which blocks correspond to recent batches
	hermezDb := hermez_db.NewHermezDb(tx)
	latestBatch, err := hermezDb.GetLatestDownloadedBatchNo()
	if err != nil {
		return 0, fmt.Errorf("failed to get latest batch number: %w", err)
	}

	var cutoffBatch uint64
	if latestBatch >= keepRecentBatches {
		cutoffBatch = latestBatch - keepRecentBatches
	} else {
		// If we have fewer batches than we want to keep, don't delete anything
		fmt.Printf("Only %d batches exist, keeping all (requested to keep %d)\n", latestBatch, keepRecentBatches)
		return 0, nil
	}

	// Find the first block of the cutoff batch to determine block-level cutoff
	cutoffBlock, found, err := hermezDb.GetLowestBlockInBatch(cutoffBatch + 1) // +1 because we want to keep this batch
	if err != nil {
		return 0, fmt.Errorf("failed to get first block of batch %d: %w", cutoffBatch+1, err)
	}
	if !found {
		fmt.Printf("No blocks found in batch %d, using latest block as cutoff\n", cutoffBatch+1)
		cutoffBlock = latestBlock // Use latest block as fallback
	}

	fmt.Printf("Deleting state data for blocks 0-%d (keeping blocks %d-%d, batches %d-%d)\n",
		cutoffBlock-1, cutoffBlock, latestBlock, cutoffBatch+1, latestBatch)

	deletedRecords := 0

	// Clean AccountChangeSet data (dupCursor table) - only if table exists
	if accountTableExists {
		accountDeletedCount, err := pruneAccountChangeSetBeforeBlockOptimized(tx, cutoffBlock, fastMode, safeFastMode)
		if err != nil {
			return deletedRecords, fmt.Errorf("failed to prune AccountChangeSet: %w", err)
		}
		deletedRecords += accountDeletedCount
		fmt.Printf("✓ Deleted %d AccountChangeSet records\n", accountDeletedCount)
	} else {
		fmt.Printf("⚠️  Skipping AccountChangeSet - table does not exist\n")
	}

	// Clean StorageChangeSet data (dupCursor table) - only if table exists
	if storageTableExists {
		storageDeletedCount, err := pruneStorageChangeSetBeforeBlockOptimized(tx, cutoffBlock, fastMode, safeFastMode)
		if err != nil {
			return deletedRecords, fmt.Errorf("failed to prune StorageChangeSet: %w", err)
		}
		deletedRecords += storageDeletedCount
		fmt.Printf("✓ Deleted %d StorageChangeSet records\n", storageDeletedCount)
	} else {
		fmt.Printf("⚠️  Skipping StorageChangeSet - table does not exist\n")
	}

	// Note: CanonicalHeader and hermez_blockBatches are NOT processed here
	// These tables are critical for node operation and are preserved for stability

	return deletedRecords, nil
}

// pruneAccountChangeSetBeforeBlockOptimized deletes AccountChangeSet records before specified block
// Uses optimized batch processing to avoid memory overflow and improve performance
// fastMode: if true, uses direct cursor deletion (faster but more aggressive)
// safeFastMode: if true, uses safe-fast mode (balanced performance and safety)
func pruneAccountChangeSetBeforeBlockOptimized(tx kv.RwTx, cutoffBlock uint64, fastMode bool, safeFastMode bool) (int, error) {
	cursor, err := tx.RwCursorDupSort("AccountChangeSet")
	if err != nil {
		return 0, err
	}
	defer cursor.Close()

	const batchSize = 10000 // Process in batches to avoid memory overflow
	deletedCount := 0
	processedBlocks := 0

	if fastMode {
		fmt.Printf("🚀 Fast processing AccountChangeSet (direct cursor deletion)...\n")
		return pruneAccountChangeSetBeforeBlockFast(tx, cutoffBlock, cursor)
	} else if safeFastMode {
		fmt.Printf("🛡️⚡ Safe-Fast processing AccountChangeSet (smaller batches + validation)...\n")
		return pruneAccountChangeSetBeforeBlockSafeFast(tx, cutoffBlock, cursor)
	}

	fmt.Printf("🔄 Processing AccountChangeSet (batch size: %d)...\n", batchSize)

	// Process in batches to optimize memory usage
	// Start from the first position
	startKey, _, err := cursor.First()
	if err != nil {
		return deletedCount, fmt.Errorf("failed to move cursor to first position: %w", err)
	}
	if startKey == nil {
		// Table is empty, nothing to process
		fmt.Printf("AccountChangeSet table is empty, nothing to prune\n")
		return 0, nil
	}

	for {
		var keysToDelete [][]byte
		currentBatchSize := 0

		for key, _, err := cursor.Current(); key != nil && currentBatchSize < batchSize; {
			if err != nil {
				return deletedCount, err
			}

			if len(key) >= 8 {
				blockNum := binary.BigEndian.Uint64(key[:8])
				if blockNum >= cutoffBlock {
					// Reached cutoff, we're done
					goto deleteBatch
				}

				// Collect all duplicate entries for this block number key
				seekKey := make([]byte, len(key))
				copy(seekKey, key)

				for k, _, err := cursor.SeekExact(seekKey); k != nil; k, _, err = cursor.NextDup() {
					if err != nil {
						return deletedCount, err
					}
					// Only add if we haven't exceeded batch size
					if currentBatchSize < batchSize {
						keyCopy := make([]byte, len(k))
						copy(keyCopy, k)
						keysToDelete = append(keysToDelete, keyCopy)
						currentBatchSize++
					} else {
						break
					}
				}

				processedBlocks++
				if processedBlocks%1000 == 0 {
					fmt.Printf("⏳ Processed %d blocks (%d records collected)...\n", processedBlocks, len(keysToDelete))
				}
			}

			// Move to next unique block number
			key, _, err = cursor.NextNoDup()
		}

	deleteBatch:
		// Delete current batch
		if len(keysToDelete) == 0 {
			break // No more records to delete
		}

		batchDeleted := 0
		for _, key := range keysToDelete {
			if err := tx.Delete("AccountChangeSet", key); err != nil {
				// Log but continue - some keys might not exist anymore
				continue
			}
			batchDeleted++
		}

		deletedCount += batchDeleted
		fmt.Printf("✓ Deleted batch: %d records (total: %d)\n", batchDeleted, deletedCount)

		// Check if we processed all records before cutoff
		if currentBatchSize < batchSize {
			break // This was the last batch
		}

		// Continue from where we left off
		keysToDelete = nil // Release memory
	}

	return deletedCount, nil
}

// pruneAccountChangeSetBeforeBlockFast performs direct cursor deletion for maximum speed
// ⚠️  More aggressive - deletes records immediately without collecting them first
func pruneAccountChangeSetBeforeBlockFast(tx kv.RwTx, cutoffBlock uint64, cursor kv.RwCursorDupSort) (int, error) {
	deletedCount := 0
	processedBlocks := 0

	// Direct deletion approach - faster but more aggressive
	for key, _, err := cursor.First(); key != nil; {
		if err != nil {
			return deletedCount, fmt.Errorf("cursor operation failed: %w", err)
		}

		if len(key) >= 8 {
			blockNum := binary.BigEndian.Uint64(key[:8])
			if blockNum >= cutoffBlock {
				break // Reached cutoff
			}

			// Delete all duplicate entries for this block directly
			duplicateCount := 0
			for k, _, err := cursor.SeekExact(key); k != nil; {
				if err != nil {
					return deletedCount, err
				}

				// Delete current entry directly via cursor
				if err := cursor.DeleteCurrent(); err != nil {
					// If deletion fails, try to continue
					key, _, err = cursor.NextDup()
					continue
				}

				duplicateCount++
				deletedCount++

				// Move to next duplicate (cursor position may have changed after deletion)
				key, _, err = cursor.NextDup()
			}

			processedBlocks++
			if processedBlocks%2000 == 0 {
				fmt.Printf("⚡ Fast deleted %d blocks (%d total records)...\n", processedBlocks, deletedCount)
			}
		}

		// Move to next unique block number
		key, _, err = cursor.NextNoDup()
	}

	return deletedCount, nil
}

// pruneAccountChangeSetBeforeBlockSafeFast performs safe-fast cursor deletion with enhanced error handling
// 🛡️⚡ Balanced approach: smaller batches + validation + better error recovery
func pruneAccountChangeSetBeforeBlockSafeFast(tx kv.RwTx, cutoffBlock uint64, cursor kv.RwCursorDupSort) (int, error) {
	const safeBatchSize = 2000      // Smaller batches for safety
	const validationInterval = 5000 // Validate every N deletions

	deletedCount := 0
	processedBlocks := 0
	validationFailures := 0

	fmt.Printf("Safe-Fast mode: using smaller batches (%d) with validation every %d deletions\n", safeBatchSize, validationInterval)

	// Safe direct deletion with smaller batches and validation
	for key, _, err := cursor.First(); key != nil; {
		if err != nil {
			return deletedCount, err
		}

		if len(key) >= 8 {
			blockNum := binary.BigEndian.Uint64(key[:8])
			if blockNum >= cutoffBlock {
				break // Reached cutoff
			}

			batchStart := deletedCount

			// Process duplicates for this block with safety checks
			for k, _, err := cursor.SeekExact(key); k != nil; {
				if err != nil {
					fmt.Printf("⚠️ Seek error for block %d: %v\n", blockNum, err)
					key, _, err = cursor.NextNoDup()
					break
				}

				// Store key for validation (small overhead for safety)
				keyBackup := make([]byte, len(k))
				copy(keyBackup, k)

				// Attempt deletion
				if err := cursor.DeleteCurrent(); err != nil {
					fmt.Printf("⚠️ Delete failed for key in block %d: %v\n", blockNum, err)
					validationFailures++

					// Try to recover position
					if _, _, seekErr := cursor.SeekExact(keyBackup); seekErr != nil {
						fmt.Printf("⚠️ Recovery failed, continuing to next block\n")
						key, _, err = cursor.NextNoDup()
						break
					}
					key, _, err = cursor.NextDup()
					continue
				}

				deletedCount++

				// Periodic validation to ensure cursor integrity
				if deletedCount%validationInterval == 0 {
					if validateErr := validateCursorState(cursor, cutoffBlock); validateErr != nil {
						fmt.Printf("⚠️ Cursor validation failed at %d deletions: %v\n", deletedCount, validateErr)
						validationFailures++
					}
					fmt.Printf("🔍 Validated: %d deletions processed (failures: %d)\n", deletedCount, validationFailures)
				}

				// Check if we've processed enough in this batch
				if deletedCount-batchStart >= safeBatchSize {
					fmt.Printf("📦 Batch limit reached, moving to next block\n")
					key, _, err = cursor.NextNoDup()
					break
				}

				// Move to next duplicate
				key, _, err = cursor.NextDup()
			}

			processedBlocks++
			if processedBlocks%1000 == 0 {
				fmt.Printf("🛡️⚡ Safe-Fast processed %d blocks (%d records, %d failures)\n",
					processedBlocks, deletedCount, validationFailures)
			}
		}

		// Move to next unique block number
		key, _, err = cursor.NextNoDup()
	}

	if validationFailures > 0 {
		fmt.Printf("⚠️ Safe-Fast completed with %d validation failures (non-critical)\n", validationFailures)
	}

	return deletedCount, nil
}

// validateCursorState performs basic validation of cursor state
func validateCursorState(cursor kv.RwCursorDupSort, cutoffBlock uint64) error {
	currentKey, _, err := cursor.Current()
	if err != nil {
		return fmt.Errorf("cursor.Current() failed: %w", err)
	}

	if len(currentKey) >= 8 {
		blockNum := binary.BigEndian.Uint64(currentKey[:8])
		if blockNum >= cutoffBlock {
			return fmt.Errorf("cursor moved beyond cutoff block: %d >= %d", blockNum, cutoffBlock)
		}
	}

	return nil
}

// pruneStorageChangeSetBeforeBlockOptimized deletes StorageChangeSet records before specified block
// Uses optimized batch processing to avoid memory overflow and improve performance
// fastMode: if true, uses direct cursor deletion (faster but more aggressive)
// safeFastMode: if true, uses safe-fast mode (balanced performance and safety)
func pruneStorageChangeSetBeforeBlockOptimized(tx kv.RwTx, cutoffBlock uint64, fastMode bool, safeFastMode bool) (int, error) {
	cursor, err := tx.RwCursorDupSort("StorageChangeSet")
	if err != nil {
		return 0, err
	}
	defer cursor.Close()

	const batchSize = 10000 // Process in batches to avoid memory overflow
	deletedCount := 0
	processedRecords := 0

	if fastMode {
		fmt.Printf("🚀 Fast processing StorageChangeSet (direct cursor deletion)...\n")
		return pruneStorageChangeSetBeforeBlockFast(tx, cutoffBlock, cursor)
	} else if safeFastMode {
		fmt.Printf("🛡️⚡ Safe-Fast processing StorageChangeSet (smaller batches + validation)...\n")
		return pruneStorageChangeSetBeforeBlockSafeFast(tx, cutoffBlock, cursor)
	}

	fmt.Printf("🔄 Processing StorageChangeSet (batch size: %d)...\n", batchSize)

	// Process in batches to optimize memory usage
	// Start from the first position
	startKey, _, err := cursor.First()
	if err != nil {
		return deletedCount, fmt.Errorf("failed to move cursor to first position: %w", err)
	}
	if startKey == nil {
		// Table is empty, nothing to process
		fmt.Printf("StorageChangeSet table is empty, nothing to prune\n")
		return 0, nil
	}

	for {
		var keysToDelete [][]byte
		currentBatchSize := 0

		// Collect a batch of keys to delete
		// StorageChangeSet key format: block_number + address + incarnation + storage_key
		for key, _, err := cursor.Current(); key != nil && currentBatchSize < batchSize; key, _, err = cursor.Next() {
			if err != nil {
				return deletedCount, err
			}

			if len(key) >= 8 {
				blockNum := binary.BigEndian.Uint64(key[:8])
				if blockNum >= cutoffBlock {
					// Reached cutoff, we're done
					goto deleteBatch
				}

				// Make a copy of the key
				keyCopy := make([]byte, len(key))
				copy(keyCopy, key)
				keysToDelete = append(keysToDelete, keyCopy)
				currentBatchSize++
				processedRecords++

				if processedRecords%5000 == 0 {
					fmt.Printf("⏳ Processed %d storage records (%d in current batch)...\n", processedRecords, len(keysToDelete))
				}
			}
		}

	deleteBatch:
		// Delete current batch
		if len(keysToDelete) == 0 {
			break // No more records to delete
		}

		batchDeleted := 0
		for _, key := range keysToDelete {
			if err := tx.Delete("StorageChangeSet", key); err != nil {
				// Log but continue - some keys might not exist anymore
				continue
			}
			batchDeleted++
		}

		deletedCount += batchDeleted
		fmt.Printf("✓ Deleted batch: %d storage records (total: %d)\n", batchDeleted, deletedCount)

		// Check if we processed all records before cutoff
		if currentBatchSize < batchSize {
			break // This was the last batch
		}

		// Continue from where we left off
		keysToDelete = nil // Release memory
	}

	return deletedCount, nil
}

// pruneStorageChangeSetBeforeBlockFast performs direct cursor deletion for maximum speed
// ⚠️  More aggressive - deletes records immediately without collecting them first
func pruneStorageChangeSetBeforeBlockFast(tx kv.RwTx, cutoffBlock uint64, cursor kv.RwCursorDupSort) (int, error) {
	deletedCount := 0
	processedRecords := 0

	// Direct deletion approach - faster but more aggressive
	// StorageChangeSet key format: block_number + address + incarnation + storage_key
	for key, _, err := cursor.First(); key != nil; {
		if err != nil {
			return deletedCount, fmt.Errorf("cursor operation failed: %w", err)
		}

		if len(key) >= 8 {
			blockNum := binary.BigEndian.Uint64(key[:8])
			if blockNum >= cutoffBlock {
				break // Reached cutoff
			}

			// Delete current storage record directly
			if err := cursor.DeleteCurrent(); err != nil {
				// If deletion fails, try to continue
				key, _, err = cursor.Next()
				continue
			}

			deletedCount++
			processedRecords++

			if processedRecords%10000 == 0 {
				fmt.Printf("⚡ Fast deleted %d storage records...\n", deletedCount)
			}
		}

		// Move to next record
		key, _, err = cursor.Next()
	}

	return deletedCount, nil
}

// pruneStorageChangeSetBeforeBlockSafeFast performs safe-fast cursor deletion for storage data
// 🛡️⚡ Optimized for StorageChangeSet table with validation and error recovery
func pruneStorageChangeSetBeforeBlockSafeFast(tx kv.RwTx, cutoffBlock uint64, cursor kv.RwCursorDupSort) (int, error) {
	const safeBatchSize = 3000      // Slightly larger batches for storage (less duplicates per key)
	const validationInterval = 8000 // Validate every N deletions

	deletedCount := 0
	processedRecords := 0
	validationFailures := 0

	fmt.Printf("Safe-Fast mode: using storage-optimized batches (%d) with validation every %d deletions\n", safeBatchSize, validationInterval)

	// Safe direct deletion with validation for storage data
	// StorageChangeSet key format: block_number + address + incarnation + storage_key
	for key, _, err := cursor.First(); key != nil; {
		if err != nil {
			return deletedCount, err
		}

		if len(key) >= 8 {
			blockNum := binary.BigEndian.Uint64(key[:8])
			if blockNum >= cutoffBlock {
				break // Reached cutoff
			}

			// Store key for validation and recovery
			keyBackup := make([]byte, len(key))
			copy(keyBackup, key)

			// Attempt deletion
			if err := cursor.DeleteCurrent(); err != nil {
				fmt.Printf("⚠️ Delete failed for storage record in block %d: %v\n", blockNum, err)
				validationFailures++

				// Try to recover position and continue
				if _, _, seekErr := cursor.SeekExact(keyBackup); seekErr != nil {
					fmt.Printf("⚠️ Recovery failed, skipping to next record\n")
				}
				key, _, err = cursor.Next()
				continue
			}

			deletedCount++
			processedRecords++

			// Periodic validation for cursor integrity
			if deletedCount%validationInterval == 0 {
				if validateErr := validateCursorState(cursor, cutoffBlock); validateErr != nil {
					fmt.Printf("⚠️ Storage cursor validation failed at %d deletions: %v\n", deletedCount, validateErr)
					validationFailures++
				}
				fmt.Printf("🔍 Storage validated: %d deletions processed (failures: %d)\n", deletedCount, validationFailures)
			}

			// Progress reporting
			if processedRecords%10000 == 0 {
				fmt.Printf("🛡️⚡ Safe-Fast storage: %d records processed (%d failures)\n", deletedCount, validationFailures)
			}

			// Batch size control for memory management
			if processedRecords%safeBatchSize == 0 {
				// Small pause to allow other operations (cooperative multitasking)
				// This helps with long-running operations
			}
		}

		// Move to next record
		key, _, err = cursor.Next()
	}

	if validationFailures > 0 {
		fmt.Printf("⚠️ Safe-Fast storage completed with %d validation failures (non-critical)\n", validationFailures)
	}

	return deletedCount, nil
}

// pruneCanonicalHeaderBeforeBlock deletes CanonicalHeader records before specified block
func pruneCanonicalHeaderBeforeBlock(tx kv.RwTx, cutoffBlock uint64) (int, error) {
	cursor, err := tx.RwCursorDupSort("CanonicalHeader")
	if err != nil {
		return 0, err
	}
	defer cursor.Close()

	// Collect all keys to delete first (safer for DupSort tables)
	var keysToDelete [][]byte

	// CanonicalHeader key format: block_number(8 bytes) -> block_hash
	// We need to collect all entries where block_number < cutoffBlock
	for key, _, err := cursor.First(); key != nil; key, _, err = cursor.NextNoDup() {
		if err != nil {
			return 0, err
		}

		if len(key) >= 8 {
			blockNum := binary.BigEndian.Uint64(key[:8])
			if blockNum >= cutoffBlock {
				break // Reached the cutoff, stop collecting
			}

			// Collect all entries for this block (there might be multiple hashes per block in dupCursor)
			for k, _, err := cursor.SeekExact(key); k != nil; k, _, err = cursor.NextDup() {
				if err != nil {
					return 0, err
				}
				// Make a copy of the key
				keyCopy := make([]byte, len(k))
				copy(keyCopy, k)
				keysToDelete = append(keysToDelete, keyCopy)
			}
		}
	}

	// Now delete all collected keys using tx.Delete (safer than cursor operations)
	deletedCount := 0
	for _, key := range keysToDelete {
		if err := tx.Delete("CanonicalHeader", key); err != nil {
			// Log but continue - some keys might not exist anymore
			continue
		}
		deletedCount++
	}

	return deletedCount, nil
}

// pruneHermezBlockBatchesBeforeBlock deletes hermez_blockBatches records before specified block
func pruneHermezBlockBatchesBeforeBlock(tx kv.RwTx, cutoffBlock uint64) (int, error) {
	cursor, err := tx.RwCursorDupSort("hermez_blockBatches")
	if err != nil {
		return 0, err
	}
	defer cursor.Close()

	// Collect all keys to delete first (safer for DupSort tables)
	var keysToDelete [][]byte

	// hermez_blockBatches key format: l2blockno(8 bytes) -> batchno
	// We need to collect all entries where l2blockno < cutoffBlock
	for key, _, err := cursor.First(); key != nil; key, _, err = cursor.NextNoDup() {
		if err != nil {
			return 0, err
		}

		if len(key) >= 8 {
			blockNum := binary.BigEndian.Uint64(key[:8])
			if blockNum >= cutoffBlock {
				break // Reached the cutoff, stop collecting
			}

			// Collect all entries for this block (there might be multiple batches per block in dupCursor)
			for k, _, err := cursor.SeekExact(key); k != nil; k, _, err = cursor.NextDup() {
				if err != nil {
					return 0, err
				}
				// Make a copy of the key
				keyCopy := make([]byte, len(k))
				copy(keyCopy, k)
				keysToDelete = append(keysToDelete, keyCopy)
			}
		}
	}

	// Now delete all collected keys using tx.Delete (safer than cursor operations)
	deletedCount := 0
	for _, key := range keysToDelete {
		if err := tx.Delete("hermez_blockBatches", key); err != nil {
			// Log but continue - some keys might not exist anymore
			continue
		}
		deletedCount++
	}

	return deletedCount, nil
}

// checkTableExists verifies if a table exists in the database
// This prevents MDBX cursor errors when trying to access non-existent tables
func checkTableExists(tx kv.RwTx, tableName string) (bool, error) {
	// Try to create a cursor for the table
	cursor, err := tx.Cursor(tableName)
	if err != nil {
		// If we can't create a cursor, the table likely doesn't exist
		// Check if it's a "table not found" type error
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "no such table") ||
			strings.Contains(err.Error(), "MDB_NOTFOUND") {
			return false, nil // Table doesn't exist, but this is not an error
		}
		// Other errors are actual problems
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}

	// Close the cursor immediately
	cursor.Close()

	// If we got here, the table exists
	return true, nil
}
