package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	mdbx2 "github.com/erigontech/mdbx-go/mdbx"
	"github.com/ledgerwatch/erigon-lib/kv"
	mdbxpkg "github.com/ledgerwatch/erigon-lib/kv/mdbx"
	logv3 "github.com/ledgerwatch/log/v3"
)

// checkSMTDatabase checks if SMT database exists and contains data
func checkSMTDatabase(smtPath string) bool {
	if _, err := os.Stat(smtPath + "/mdbx.dat"); os.IsNotExist(err) {
		return false
	}

	// Check file size, if file is very small it might be just an empty database file
	if info, err := os.Stat(smtPath + "/mdbx.dat"); err == nil {
		// If file size is less than 1MB, consider it as empty database
		return info.Size() > 1024*1024
	}

	return true
}

// getTableList gets list of tables in database
func getTableList(db kv.RwDB) ([]string, error) {
	ctx := context.Background()
	tx, err := db.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	tables, err := tx.ListBuckets()
	if err != nil {
		return nil, err
	}

	sort.Strings(tables)
	return tables, nil
}

// getActiveTableList returns list of tables that actually contain data (size > 0)
func getActiveTableList(db kv.RwDB) ([]string, error) {
	ctx := context.Background()
	tx, err := db.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	allTables, err := tx.ListBuckets()
	if err != nil {
		return nil, err
	}

	var activeTables []string
	for _, tableName := range allTables {
		// Check if table has any data
		if hasTableData(tx, tableName) {
			activeTables = append(activeTables, tableName)
		}
	}

	sort.Strings(activeTables)
	return activeTables, nil
}

// hasTableData checks if a table contains any data
func hasTableData(tx kv.Tx, tableName string) bool {
	// Try to get the first key from the table
	cursor, err := tx.Cursor(tableName)
	if err != nil {
		return false
	}
	defer cursor.Close()

	// Check if there's at least one entry
	k, _, err := cursor.First()
	return err == nil && len(k) > 0
}

// contains checks if a slice contains a given string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// getTableStats gets statistics info of table (using default pageSize)
func getTableStats(db kv.RwDB, tableName string) (uint64, uint64, uint64, error) {
	ctx := context.Background()
	tx, err := db.BeginRo(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()

	if mdbxTx, ok := tx.(*mdbxpkg.MdbxTx); ok {
		stat, err := mdbxTx.BucketStat(tableName)
		if err != nil {
			return 0, 0, 0, err
		}

		totalPages := stat.LeafPages + stat.BranchPages + stat.OverflowPages
		// Use default MDBX page size of 8192 bytes
		const defaultPageSize = 8192
		sizeBytes := totalPages * defaultPageSize

		return stat.Entries, sizeBytes, totalPages, nil
	}

	return 0, 0, 0, fmt.Errorf("not MDBX transaction")
}

// openDatabase opens database at specified path using the safest possible approach
func openDatabase(dbPath string, label kv.Label, log logv3.Logger) (kv.RwDB, *mdbx2.EnvInfo, error) {
	ctx := context.Background()

	var opts mdbxpkg.MdbxOpts
	if label == kv.ChainDB {
		opts = mdbxpkg.NewMDBX(log).Path(dbPath).Label(label).WithTableCfg(mdbxpkg.WithChaindataTables)
	} else {
		// SMT database uses different configuration
		kv.InitStandaloneSMT(false) // Standalone SMT database
		opts = mdbxpkg.NewMDBX(log).Path(dbPath).Label(label)
	}

	// Use the simplest possible approach: let MDBX use its default configuration
	// This avoids all geometry mismatch issues
	db, err := opts.Open(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}

	fmt.Printf("✓ Database opened successfully with default configuration\n")

	return db, nil, nil
}
