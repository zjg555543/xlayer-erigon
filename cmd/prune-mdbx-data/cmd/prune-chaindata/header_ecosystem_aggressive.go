package main

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
)

// executeAggressiveHeaderEcosystemCleanup performs Header ecosystem cleanup for Aggressive mode
// This extends the existing copy-truncate-restore logic to include CanonicalHeader
func executeAggressiveHeaderEcosystemCleanup(tx kv.RwTx, hermezDb *hermez_db.HermezDbReader, keepFromBatch, latestBatch, genesisHeight uint64) (int, error) {
	fmt.Printf("🏗️ Aggressive Header ecosystem cleanup: CanonicalHeader + HeaderNumber + Header + BlockBody\n")

	// Step 1: Identify all blocks in batches to keep (reuse existing logic)
	var preserveBlocks []uint64
	preserveBatchCount := 0

	for batchNo := keepFromBatch; batchNo <= latestBatch; batchNo++ {
		blockNos, err := hermezDb.GetL2BlockNosByBatch(batchNo)
		if err != nil {
			continue
		}
		preserveBlocks = append(preserveBlocks, blockNos...)
		if len(blockNos) > 0 {
			preserveBatchCount++
		}
	}

	fmt.Printf("Header ecosystem: Found %d blocks in %d batches to preserve\n", len(preserveBlocks), preserveBatchCount)

	if len(preserveBlocks) == 0 {
		fmt.Printf("No blocks to preserve, will clear all Header ecosystem tables\n")
		return clearHeaderEcosystemTablesCompletely(tx)
	}

	// Step 2: Copy data (extending existing copyBlockData to include CanonicalHeader)
	fmt.Printf("Header ecosystem: Copying data for %d blocks...\n", len(preserveBlocks))
	preservedData, err := copyHeaderEcosystemBlockData(tx, preserveBlocks, genesisHeight)
	if err != nil {
		return 0, fmt.Errorf("failed to copy Header ecosystem data: %w", err)
	}

	// Step 3: Clear Header ecosystem tables
	fmt.Printf("Header ecosystem: Clearing Header ecosystem tables...\n")
	err = clearHeaderEcosystemTables(tx)
	if err != nil {
		return 0, fmt.Errorf("failed to clear Header ecosystem tables: %w", err)
	}

	// Step 4: Restore preserved data
	fmt.Printf("Header ecosystem: Restoring preserved data...\n")
	err = restoreHeaderEcosystemBlockData(tx, preservedData)
	if err != nil {
		return 0, fmt.Errorf("failed to restore Header ecosystem data: %w", err)
	}

	fmt.Printf("✅ Header ecosystem cleanup completed: processed %d blocks\n", len(preserveBlocks))
	return len(preserveBlocks), nil
}

// copyHeaderEcosystemBlockData extends copyBlockData to include CanonicalHeader for aggressive mode
func copyHeaderEcosystemBlockData(tx kv.RwTx, blockNos []uint64, genesisHeight uint64) ([]BlockData, error) {
	// Genesis protection: Always preserve genesis block to prevent genesis rewrite
	hasGenesis := false
	for _, blockNo := range blockNos {
		if blockNo == genesisHeight {
			hasGenesis = true
			break
		}
	}
	if !hasGenesis {
		fmt.Printf("🛡️ Protecting Genesis block (%d) to prevent PlainState corruption\n", genesisHeight)
		blockNos = append([]uint64{genesisHeight}, blockNos...)
	}

	var preservedData []BlockData

	// Reuse existing table definitions but ADD CanonicalHeader for aggressive mode
	simpleTables := []string{
		"Receipt",
		"block_info_roots",
		// In Aggressive mode: include CanonicalHeader for complete consistency
	}

	compositeKeyTables := []string{
		"Header",                            // block_num_u64 + hash
		"BlockBody",                         // block_num_u64 + hash
		"TxSender",                          // block_num_u64 + blockHash
		"TransactionLog",                    // block_num_u64 + txIndex + logIndex
		"hermez_intermediate_tx_stateRoots", // l2blockno + txhash
	}

	for i, blockNo := range blockNos {
		if i%1000 == 0 {
			fmt.Printf("Copying Header ecosystem data for block %d (%d/%d)...\n", blockNo, i+1, len(blockNos))
		}

		blockData := BlockData{
			BlockNo: blockNo,
			Data:    make(map[string][]byte),
		}

		blockKey := make([]byte, 8)
		binary.BigEndian.PutUint64(blockKey, blockNo)

		// 1. Copy data from simple tables
		for _, table := range simpleTables {
			data, err := tx.GetOne(table, blockKey)
			if err == nil && data != nil {
				blockData.Data[table] = append([]byte{}, data...) // Deep copy
			}
		}

		// 2. Copy CanonicalHeader data (DupCursor table - CRITICAL for aggressive mode!)
		canonicalHeaderEntries, err := copyCanonicalHeaderDataForBlock(tx, blockNo)
		if err == nil && len(canonicalHeaderEntries) > 0 {
			blockData.Data["CanonicalHeader"] = serializeEntries(canonicalHeaderEntries)
		}

		// 3. Copy data from composite key tables (reuse existing logic)
		for _, table := range compositeKeyTables {
			entries, err := copyCompositeKeyData(tx, table, blockKey)
			if err == nil && len(entries) > 0 {
				blockData.Data[table] = serializeEntries(entries)
			}
		}

		// 4. Copy HeaderNumber data (reuse existing logic)
		headerNumberData, err := copyHeaderNumberData(tx, blockNo)
		if err == nil && len(headerNumberData) > 0 {
			blockData.Data["HeaderNumber"] = serializeEntries(headerNumberData)
		}

		preservedData = append(preservedData, blockData)
	}

	fmt.Printf("Successfully copied Header ecosystem data for %d blocks\n", len(preservedData))
	return preservedData, nil
}

// copyCanonicalHeaderDataForBlock copies CanonicalHeader entries for a specific block using DupCursor
func copyCanonicalHeaderDataForBlock(tx kv.RwTx, blockNo uint64) ([]KeyValueEntry, error) {
	cursor, err := tx.CursorDupSort("CanonicalHeader")
	if err != nil {
		return nil, err // Table might not exist
	}
	defer cursor.Close()

	blockKey := make([]byte, 8)
	binary.BigEndian.PutUint64(blockKey, blockNo)

	var entries []KeyValueEntry

	// Find all entries for this block using DupCursor
	for key, value, err := cursor.Seek(blockKey); key != nil && bytes.Equal(key, blockKey); key, value, err = cursor.NextDup() {
		if err != nil {
			return nil, err
		}

		entries = append(entries, KeyValueEntry{
			Key:   common.Copy(key),
			Value: common.Copy(value),
		})
	}

	return entries, nil
}

// clearHeaderEcosystemTables clears all Header ecosystem tables
func clearHeaderEcosystemTables(tx kv.RwTx) error {
	headerTables := []string{
		"CanonicalHeader",
		"HeaderNumber",
		"Header",
		"BlockBody",
	}

	fmt.Printf("🗑️ Clearing %d Header ecosystem tables...\n", len(headerTables))

	for _, table := range headerTables {
		fmt.Printf("Clearing table: %s\n", table)
		err := tx.ClearBucket(table)
		if err != nil {
			return fmt.Errorf("failed to clear table %s: %w", table, err)
		}
		fmt.Printf("✓ Cleared table: %s\n", table)
	}

	return nil
}

// clearHeaderEcosystemTablesCompletely clears all Header ecosystem tables completely
func clearHeaderEcosystemTablesCompletely(tx kv.RwTx) (int, error) {
	fmt.Printf("Clearing all Header ecosystem tables completely...\n")
	err := clearHeaderEcosystemTables(tx)
	if err != nil {
		return 0, err
	}
	return 1, nil // Return 1 to indicate operation completed
}

// restoreHeaderEcosystemBlockData extends restoreBlockData to handle CanonicalHeader for aggressive mode
func restoreHeaderEcosystemBlockData(tx kv.RwTx, preservedData []BlockData) error {
	// Tables with simple block_number key (key = block_num_u64)
	simpleTables := map[string]bool{
		"Receipt":          true,
		"block_info_roots": true,
	}

	for i, blockData := range preservedData {
		if i%1000 == 0 {
			fmt.Printf("Restoring Header ecosystem data for block %d (%d/%d)...\n", blockData.BlockNo, i+1, len(preservedData))
		}

		blockKey := make([]byte, 8)
		binary.BigEndian.PutUint64(blockKey, blockData.BlockNo)

		// Restore data to each table
		for table, data := range blockData.Data {
			if simpleTables[table] {
				// Simple table: direct key-value restoration
				err := tx.Put(table, blockKey, data)
				if err != nil {
					return fmt.Errorf("failed to restore block %d to simple table %s: %w", blockData.BlockNo, table, err)
				}
			} else {
				// Composite/special/dupCursor table: deserialize and restore multiple entries
				entries, err := deserializeEntries(data)
				if err != nil {
					return fmt.Errorf("failed to deserialize data for table %s block %d: %w", table, blockData.BlockNo, err)
				}

				for _, entry := range entries {
					err := tx.Put(table, entry.Key, entry.Value)
					if err != nil {
						return fmt.Errorf("failed to restore entry to table %s block %d: %w", table, blockData.BlockNo, err)
					}
				}
			}
		}
	}

	fmt.Printf("Successfully restored Header ecosystem data for %d blocks\n", len(preservedData))
	return nil
}
