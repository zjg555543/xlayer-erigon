package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/c2h5oh/datasize"
	"github.com/erigontech/mdbx-go/mdbx"
	"github.com/ledgerwatch/erigon-lib/kv"
	mdbx2 "github.com/ledgerwatch/erigon-lib/kv/mdbx"
	logv3 "github.com/ledgerwatch/log/v3"
	"golang.org/x/sync/semaphore"
)

// Define local table configurations to avoid modifying erigon-lib
var localSmtTablesCfg = kv.TableCfg{}

// SMT tables - ONLY the 5 pure SMT tables that actually exist in SMT database
var smtTableNames = []string{
	"HermezSmt",
	"HermezSmtStats",
	"HermezSmtAccountValues",
	"HermezSmtMetadata",
	"HermezSmtHashKey",
}

// analyzeDatabase returns actual disk usage and table data size
func analyzeDatabase(dbPath string, label kv.Label, logger logv3.Logger) (uint64, uint64, error) {
	// Get actual disk usage (like `du` command) instead of sparse file logical size
	dbFile := filepath.Join(dbPath, "mdbx.dat")
	fileInfo, err := os.Stat(dbFile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to stat database file: %w", err)
	}

	var actualFileSize uint64
	// For sparse files, we need to get actual disk usage using syscall
	if stat, ok := fileInfo.Sys().(*syscall.Stat_t); ok {
		// stat.Blocks is in 512-byte blocks on most Unix systems
		actualFileSize = uint64(stat.Blocks * 512)
	} else {
		// Fallback to logical size if syscall not available
		actualFileSize = uint64(fileInfo.Size())
	}

	// Open database for table analysis
	db := mdbx2.NewMDBX(logger).Path(dbPath).
		Label(label).
		WithTableCfg(func(_ kv.TableCfg) kv.TableCfg { return getTableCfgForLabel(label) }).
		Readonly().
		MustOpen()
	defer db.Close()

	ctx := context.Background()
	tx, err := db.BeginRo(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	// Get MDBX transaction for table stats
	mdbxTx, ok := tx.(*mdbx2.MdbxTx)
	if !ok {
		return 0, 0, fmt.Errorf("not MDBX transaction")
	}

	// Calculate table data size
	tables, err := tx.ListBuckets()
	if err != nil {
		return 0, 0, err
	}

	var tableSize uint64
	pageSize := db.PageSize()
	for _, tableName := range tables {
		stat, err := mdbxTx.BucketStat(tableName)
		if err != nil {
			continue // Skip failed tables
		}
		totalPages := stat.LeafPages + stat.BranchPages + stat.OverflowPages
		tableSize += totalPages * pageSize
	}

	// Return actual disk usage and table data size
	return actualFileSize, tableSize, nil
}

// openOptimizedCompactPair creates highly optimized database connections for fast compaction
func openOptimizedCompactPair(from, to string, label kv.Label, logger logv3.Logger) (kv.RoDB, kv.RwDB) {
	const OptimizedThreadsLimit = 16_000 // Increased from default 9_000

	// Source database with maximum read optimization
	src := mdbx2.NewMDBX(logger).Path(from).
		Label(label).
		RoTxsLimiter(semaphore.NewWeighted(OptimizedThreadsLimit)).
		WithTableCfg(func(_ kv.TableCfg) kv.TableCfg { return getTableCfgForLabel(label) }).
		Flags(func(flags uint) uint {
			// Enable read optimizations - remove NoReadahead for better prefetching
			return flags | mdbx.Accede | mdbx.LifoReclaim&^mdbx.NoReadahead
		}).
		MustOpen()

	// Get source info for optimal destination setup
	info, err := src.(*mdbx2.MdbxKV).Env().Info(nil)
	if err != nil {
		panic(err)
	}

	// Destination database with write optimization
	dst := mdbx2.NewMDBX(logger).Path(to).
		Label(label).
		PageSize(datasize.ByteSize(info.PageSize).Bytes()). // Keep same page size
		MapSize(datasize.ByteSize(info.Geo.Upper)).
		GrowthStep(4 * datasize.GB).         // Conservative growth step
		DirtySpace(uint64(1 * datasize.GB)). // Conservative dirty space
		Flags(func(flags uint) uint {
			// Enable write optimizations
			return flags | mdbx.WriteMap | mdbx.LifoReclaim | mdbx.SafeNoSync
		}).
		WithTableCfg(func(_ kv.TableCfg) kv.TableCfg { return getTableCfgForLabel(label) }).
		MustOpen()

	return src, dst
}

// getDatabaseLabel determines the appropriate database label based on database type
func getDatabaseLabel(dbType string) (kv.Label, error) {
	switch dbType {
	case "chaindata":
		return kv.ChainDB, nil
	case "smt":
		return kv.SmtDB, nil
	default:
		return 0, fmt.Errorf("invalid database type: %s, expected 'chaindata' or 'smt'", dbType)
	}
}

// getDatabaseLabelWithSplitDB determines the appropriate database label based on type and split database configuration
func getDatabaseLabelWithSplitDB(dbType string, splitDB bool) (kv.Label, error) {
	switch dbType {
	case "chaindata":
		return kv.ChainDB, nil
	case "smt":
		if splitDB {
			// SMT is in separate database, use SmtDB label
			return kv.SmtDB, nil
		} else {
			// SMT data is stored in chaindata database, use ChainDB label
			// This ensures we don't lose SMT data in integrated mode
			return kv.ChainDB, nil
		}
	default:
		return 0, fmt.Errorf("invalid database type: %s, expected 'chaindata' or 'smt'", dbType)
	}
}

// validateDatabaseConfiguration validates the consistency between database type and split database flag
func validateDatabaseConfiguration(dbType string, splitDB bool) error {
	if dbType == "smt" && !splitDB {
		fmt.Printf("⚠️  Warning: You selected 'smt' type but split-db is disabled.\n")
		fmt.Printf("    This means SMT data is stored together with chaindata in the same database.\n")
		fmt.Printf("    The tool will process the integrated database (chaindata) to preserve all data.\n")
	}

	if dbType == "chaindata" && splitDB {
		fmt.Printf("ℹ️  Info: You selected 'chaindata' type with split-db enabled.\n")
		fmt.Printf("    Only chaindata tables will be processed, SMT tables are in separate database.\n")
	}

	return nil
}

// checkDatabaseExists verifies that the database file exists at the specified path
func checkDatabaseExists(dbPath string) error {
	dbFile := filepath.Join(dbPath, "mdbx.dat")
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		return fmt.Errorf("source database not found at path: %s, file: %s", dbPath, dbFile)
	}
	return nil
}

// resolveDatabasePath handles relative path resolution for database paths
func resolveDatabasePath(dbPath string) string {
	if !filepath.IsAbs(dbPath) {
		// When called from main program, we need to resolve relative paths correctly
		if cwd, err := os.Getwd(); err == nil {
			// If we're in a subdirectory (like cmd/compact-db), go up to main directory
			if contains(cwd, "cmd/compact-db") {
				basePath := filepath.Dir(filepath.Dir(cwd)) // Go up two levels
				return filepath.Join(basePath, dbPath)
			}
		}
	}
	return dbPath
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr
}

// getTableCfgForLabel returns the appropriate table configuration for the given label
func getTableCfgForLabel(label kv.Label) kv.TableCfg {
	if label == kv.SmtDB {
		// Initialize SMT table config if needed
		if len(localSmtTablesCfg) == 0 {
			localSmtTablesCfg = kv.TableCfg{}

			// Add deprecated chaindata tables (as expected by erigon-lib)
			for name, cfg := range kv.ChaindataTablesCfg {
				tmp := cfg
				tmp.IsDeprecated = true // Mark chaindata tables as deprecated in SMT DB
				localSmtTablesCfg[name] = tmp
			}

			// Add NON-deprecated SMT tables
			for _, tableName := range smtTableNames {
				localSmtTablesCfg[tableName] = kv.TableCfgItem{
					Flags:        kv.Default,
					IsDeprecated: false, // CRITICAL: SMT tables must not be deprecated
				}
			}
		}
		return localSmtTablesCfg
	}

	// For chaindata, use the global configuration as-is
	// This includes SMT tables when in integrated mode (standaloneSMT = false)
	return kv.ChaindataTablesCfg
}

// manualSmtCopy bypasses erigon-lib's deprecated table logic for SMT tables
func manualSmtCopy(ctx context.Context, src kv.RoDB, dst kv.RwDB, tables []string, readAheadThreads int, logger logv3.Logger) error {
	srcTx, err := src.BeginRo(ctx)
	if err != nil {
		return err
	}
	defer srcTx.Rollback()

	for _, tableName := range tables {
		// Check if table exists and has data
		srcCursor, err := srcTx.Cursor(tableName)
		if err != nil {
			// Table doesn't exist, skip it
			fmt.Printf("⚠️  Table %s does not exist, skipping\n", tableName)
			continue
		}

		// Check if table has any data
		k, _, err := srcCursor.First()
		if err != nil || k == nil {
			// Table is empty, skip it
			fmt.Printf("🔹 Table %s is empty, skipping\n", tableName)
			continue
		}

		// Table has data, copy it
		fmt.Printf("📋 Copying table %s...\n", tableName)

		// Create and clear destination table first
		if err := dst.Update(ctx, func(tx kv.RwTx) error {
			if err := tx.(kv.BucketMigrator).CreateBucket(tableName); err != nil {
				// Table might already exist, that's OK
			}
			return tx.ClearBucket(tableName)
		}); err != nil {
			return fmt.Errorf("failed to prepare destination table %s: %w", tableName, err)
		}

		// Copy all data
		dstTx, err := dst.BeginRw(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin destination transaction: %w", err)
		}

		dstCursor, err := dstTx.RwCursor(tableName)
		if err != nil {
			dstTx.Rollback()
			return fmt.Errorf("failed to open destination cursor for %s: %w", tableName, err)
		}

		// Reset source cursor and copy all entries
		srcCursor, err = srcTx.Cursor(tableName)
		if err != nil {
			dstTx.Rollback()
			return fmt.Errorf("failed to reopen source cursor for %s: %w", tableName, err)
		}

		entryCount := 0
		for k, v, err := srcCursor.First(); k != nil; k, v, err = srcCursor.Next() {
			if err != nil {
				dstTx.Rollback()
				return fmt.Errorf("failed to read from source table %s: %w", tableName, err)
			}

			if err = dstCursor.Append(k, v); err != nil {
				dstTx.Rollback()
				return fmt.Errorf("failed to write to destination table %s: %w", tableName, err)
			}
			entryCount++
		}

		if err := dstTx.Commit(); err != nil {
			return fmt.Errorf("failed to commit destination transaction for %s: %w", tableName, err)
		}

		fmt.Printf("✅ Copied %d entries in table %s\n", entryCount, tableName)
	}

	logger.Info("SMT manual copy completed")
	return nil
}
