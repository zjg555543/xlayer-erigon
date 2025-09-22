package main

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
)

// executeLegacyPruning falls back to the old method if the new method fails
func executeLegacyPruning(tx kv.RwTx, hermezDb *hermez_db.HermezDbReader, pruneBefore uint64) (int, int, error) {
	fmt.Printf("Using legacy pruning method...\n")

	deletedBatches := 0
	deletedBlocks := 0

	// Iterate through all batches to delete
	for batchNo := uint64(0); batchNo < pruneBefore; batchNo++ {
		// Get all blocks in this batch
		blockNos, err := hermezDb.GetL2BlockNosByBatch(batchNo)
		if err != nil {
			// Skip if batch doesn't exist
			continue
		}

		if len(blockNos) == 0 {
			continue
		}

		fmt.Printf("Deleting batch %d, containing %d blocks\n", batchNo, len(blockNos))

		// Delete all block data in this batch
		for _, blockNo := range blockNos {
			err := deleteBlockData(tx, blockNo)
			if err != nil {
				fmt.Printf("Warning: failed to delete block %d data: %v\n", blockNo, err)
				continue
			}
			deletedBlocks++
		}

		// Delete batch-related metadata
		err = deleteBatchMetadata(tx, batchNo)
		if err != nil {
			fmt.Printf("Warning: failed to delete batch %d metadata: %v\n", batchNo, err)
		}

		deletedBatches++
	}

	fmt.Printf("Legacy pruning completed: deleted %d batches, %d blocks\n", deletedBatches, deletedBlocks)
	return deletedBatches, deletedBlocks, nil
}

// deleteBlockData deletes all data related to a specific block
func deleteBlockData(tx kv.RwTx, blockNo uint64) error {
	blockKey := make([]byte, 8)
	binary.BigEndian.PutUint64(blockKey, blockNo)

	// Delete simple block-related table data (key = block_num_u64)
	// Note: small tables (block_l1_info_tree_index, plain_state_version, smt_depths, MaxTxNum) excluded from cleanup
	// Note: dupCursor tables (CanonicalHeader, hermez_blockBatches) excluded - need special handling
	simpleTables := []string{
		"Receipt",
		// zkEVM specific tables with block_number keys
		"block_info_roots", // block number -> block info root hash
	}
	for _, table := range simpleTables {
		err := tx.Delete(table, blockKey)
		if err != nil {
			return fmt.Errorf("failed to delete %s for block %d: %w", table, blockNo, err)
		}
	}

	// Special handling for Header table (key = block_num_u64 + hash)
	err := deleteHeaderData(tx, blockNo)
	if err != nil {
		return fmt.Errorf("failed to delete header for block %d: %w", blockNo, err)
	}

	// Special handling for HeaderNumber table (key = header_hash -> block_num)
	err = deleteHeaderNumber(tx, blockNo)
	if err != nil {
		return fmt.Errorf("failed to delete header number for block %d: %w", blockNo, err)
	}

	// Special handling for BlockBody table (key = block_num_u64 + hash)
	err = deleteBlockBodyData(tx, blockNo)
	if err != nil {
		return fmt.Errorf("failed to delete block body for block %d: %w", blockNo, err)
	}

	// Special handling for TxSender table (key = block_num_u64 + blockHash)
	err = deleteTxSenderData(tx, blockNo)
	if err != nil {
		return fmt.Errorf("failed to delete tx sender for block %d: %w", blockNo, err)
	}

	// Special handling for TransactionLog table (needs iteration)
	err = deleteTransactionLogs(tx, blockNo)
	if err != nil {
		return fmt.Errorf("failed to delete transaction logs for block %d: %w", blockNo, err)
	}

	// Special handling for composite key tables (block_number + hash)
	err = deleteCompositeKeyData(tx, blockNo)
	if err != nil {
		return fmt.Errorf("failed to delete composite key data for block %d: %w", blockNo, err)
	}

	return nil
}

// deleteHeaderData deletes header data for a specific block (key = block_num_u64 + hash)
func deleteHeaderData(tx kv.RwTx, blockNo uint64) error {
	cursor, err := tx.RwCursor("Header")
	if err != nil {
		return err
	}
	defer cursor.Close()

	// Header key format: blockNum(8) + hash(32)
	blockPrefix := make([]byte, 8)
	binary.BigEndian.PutUint64(blockPrefix, blockNo)

	var keysToDelete [][]byte
	for key, _, err := cursor.Seek(blockPrefix); key != nil; key, _, err = cursor.Next() {
		if err != nil {
			return err
		}
		// Check if key starts with our block number prefix
		if len(key) < 8 || !bytes.Equal(key[:8], blockPrefix) {
			break // No more entries for this block
		}
		keysToDelete = append(keysToDelete, common.Copy(key))
	}

	// Delete collected keys (two-phase deletion for safety)
	for _, key := range keysToDelete {
		if err := tx.Delete("Header", key); err != nil {
			return err
		}
	}

	return nil
}

// deleteHeaderNumber deletes header number entries for a specific block (key = header_hash -> block_num)
func deleteHeaderNumber(tx kv.RwTx, blockNo uint64) error {
	cursor, err := tx.RwCursor("HeaderNumber")
	if err != nil {
		return err
	}
	defer cursor.Close()

	// HeaderNumber table: header_hash -> block_num_u64
	// We need to find entries where value equals our block number
	targetBlockBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(targetBlockBytes, blockNo)

	var keysToDelete [][]byte
	for key, value, err := cursor.First(); key != nil; key, value, err = cursor.Next() {
		if err != nil {
			return err
		}
		// Check if value equals our target block number
		if len(value) == 8 && bytes.Equal(value, targetBlockBytes) {
			keysToDelete = append(keysToDelete, common.Copy(key))
		}
	}

	// Delete collected keys (two-phase deletion for safety)
	for _, key := range keysToDelete {
		if err := tx.Delete("HeaderNumber", key); err != nil {
			return err
		}
	}

	return nil
}

// deleteBlockBodyData deletes block body data for a specific block (key = block_num_u64 + hash)
func deleteBlockBodyData(tx kv.RwTx, blockNo uint64) error {
	cursor, err := tx.RwCursor("BlockBody")
	if err != nil {
		return err
	}
	defer cursor.Close()

	// BlockBody key format: blockNum(8) + hash(32)
	blockPrefix := make([]byte, 8)
	binary.BigEndian.PutUint64(blockPrefix, blockNo)

	var keysToDelete [][]byte
	for key, _, err := cursor.Seek(blockPrefix); key != nil; key, _, err = cursor.Next() {
		if err != nil {
			return err
		}
		// Check if key starts with our block number prefix
		if len(key) < 8 || !bytes.Equal(key[:8], blockPrefix) {
			break // No more entries for this block
		}
		keysToDelete = append(keysToDelete, common.Copy(key))
	}

	// Delete collected keys (two-phase deletion for safety)
	for _, key := range keysToDelete {
		if err := tx.Delete("BlockBody", key); err != nil {
			return err
		}
	}

	return nil
}

// deleteTxSenderData deletes tx sender data for a specific block (key = block_num_u64 + blockHash)
func deleteTxSenderData(tx kv.RwTx, blockNo uint64) error {
	cursor, err := tx.RwCursor("TxSender")
	if err != nil {
		return err
	}
	defer cursor.Close()

	// TxSender key format: blockNum(8) + blockHash(32)
	blockPrefix := make([]byte, 8)
	binary.BigEndian.PutUint64(blockPrefix, blockNo)

	var keysToDelete [][]byte
	for key, _, err := cursor.Seek(blockPrefix); key != nil; key, _, err = cursor.Next() {
		if err != nil {
			return err
		}
		// Check if key starts with our block number prefix
		if len(key) < 8 || !bytes.Equal(key[:8], blockPrefix) {
			break // No more entries for this block
		}
		keysToDelete = append(keysToDelete, common.Copy(key))
	}

	// Delete collected keys (two-phase deletion for safety)
	for _, key := range keysToDelete {
		if err := tx.Delete("TxSender", key); err != nil {
			return err
		}
	}

	return nil
}

// deleteTransactionLogs deletes transaction logs for a specific block
func deleteTransactionLogs(tx kv.RwTx, blockNo uint64) error {
	cursor, err := tx.RwCursor("TransactionLog")
	if err != nil {
		return err
	}
	defer cursor.Close()

	// TransactionLog format: blockNum(8) + txIndex(4) + logIndex(4)
	blockPrefix := make([]byte, 8)
	binary.BigEndian.PutUint64(blockPrefix, blockNo)

	var keysToDelete [][]byte
	for key, _, err := cursor.Seek(blockPrefix); key != nil; key, _, err = cursor.Next() {
		if err != nil {
			return err
		}

		if len(key) < 8 {
			break
		}

		// Check if it belongs to current block
		keyBlockNo := binary.BigEndian.Uint64(key[:8])
		if keyBlockNo != blockNo {
			break
		}

		keysToDelete = append(keysToDelete, common.Copy(key))
	}

	// Delete all found keys
	for _, key := range keysToDelete {
		err := tx.Delete("TransactionLog", key)
		if err != nil {
			return err
		}
	}

	return nil
}

// deleteCompositeKeyData deletes data from tables with composite keys (block_number + hash)
func deleteCompositeKeyData(tx kv.RwTx, blockNo uint64) error {
	// Tables with composite key format: block_number_u64 + hash
	// Note: HeadersTotalDifficulty excluded as it's a small table that doesn't need cleanup
	compositeKeyTables := []string{
		"Header",                            // block_num_u64 + hash -> header (RLP)
		"BlockBody",                         // block_num_u64 + hash -> block body
		"TxSender",                          // block_num_u64 + blockHash -> sendersList
		"hermez_intermediate_tx_stateRoots", // l2blockno + txhash -> stateRoot
	}

	blockPrefix := make([]byte, 8)
	binary.BigEndian.PutUint64(blockPrefix, blockNo)

	for _, tableName := range compositeKeyTables {
		err := deleteTableWithBlockPrefix(tx, tableName, blockPrefix)
		if err != nil {
			// Log warning but continue - some tables might not exist or have no data for this block
			continue
		}
	}

	return nil
}

// deleteTableWithBlockPrefix deletes all entries from a table that start with given block prefix
func deleteTableWithBlockPrefix(tx kv.RwTx, tableName string, blockPrefix []byte) error {
	cursor, err := tx.RwCursor(tableName)
	if err != nil {
		return err // Table might not exist
	}
	defer cursor.Close()

	var keysToDelete [][]byte
	for key, _, err := cursor.Seek(blockPrefix); key != nil; key, _, err = cursor.Next() {
		if err != nil {
			return err
		}

		// Check if key starts with our block prefix
		if len(key) < len(blockPrefix) || !bytes.HasPrefix(key, blockPrefix) {
			break // No more entries for this block
		}

		keysToDelete = append(keysToDelete, common.Copy(key))
	}

	// Delete all found keys
	for _, key := range keysToDelete {
		err := cursor.Delete(key)
		if err != nil {
			return err
		}
	}

	return nil
}

// deleteBatchMetadata deletes batch-related metadata
func deleteBatchMetadata(tx kv.RwTx, batchNo uint64) error {
	batchKey := hermez_db.Uint64ToBytes(batchNo)

	// Delete batch-related hermez table data
	batchTables := []string{
		hermez_db.BATCH_BLOCKS,
		hermez_db.FORKIDS,
		hermez_db.STATE_ROOTS,
		hermez_db.GLOBAL_EXIT_ROOTS_BATCHES,
		hermez_db.BATCH_WITNESSES,
		hermez_db.BATCH_COUNTERS,
		hermez_db.L1_BATCH_DATA,
		hermez_db.LATEST_USED_GER,
		hermez_db.BATCH_ENDS,
	}

	for _, table := range batchTables {
		err := tx.Delete(table, batchKey)
		if err != nil {
			// Some tables may not have corresponding batch data, this is normal
			continue
		}
	}

	return nil
}
