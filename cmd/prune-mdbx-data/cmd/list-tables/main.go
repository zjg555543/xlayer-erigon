package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/c2h5oh/datasize"
	"github.com/ledgerwatch/erigon-lib/kv"
	mdbx "github.com/ledgerwatch/erigon-lib/kv/mdbx"
	mdbxpkg "github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon/smt/pkg/db"

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

// openDatabase opens database at specified path using the safest possible approach
func openDatabase(dbPath string, label kv.Label, log logv3.Logger) (kv.RwDB, error) {
	ctx := context.Background()

	var opts mdbx.MdbxOpts
	if label == kv.ChainDB {
		opts = mdbx.NewMDBX(log).Path(dbPath).Label(label).WithTableCfg(mdbx.WithChaindataTables)
	} else {
		// SMT database uses different configuration
		kv.InitStandaloneSMT(false) // Standalone SMT database
		opts = mdbx.NewMDBX(log).Path(dbPath).Label(label)
	}

	// Use the simplest possible approach: let MDBX use its default configuration
	// This avoids all geometry mismatch issues
	db, err := opts.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("Failed to open database: %w", err)
	}

	fmt.Printf("✓ Database opened successfully with default configuration\n")

	return db, nil
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

// getTableStats gets statistics info of table using database's actual page size
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

		// Get actual page size from the database instance, not from stat
		pageSize := db.PageSize()
		sizeBytes := totalPages * pageSize

		return stat.Entries, sizeBytes, totalPages, nil
	}

	return 0, 0, 0, fmt.Errorf("not MDBX transaction")
}

// getDatabaseSize gets the actual database disk usage (like `du` command)
func getDatabaseSize(dbPath string) (uint64, error) {
	// Get actual disk usage instead of sparse file logical size
	dbFile := filepath.Join(dbPath, "mdbx.dat")
	fileInfo, err := os.Stat(dbFile)
	if err != nil {
		return 0, fmt.Errorf("failed to stat database file %s: %w", dbFile, err)
	}

	// For sparse files, we need to get actual disk usage using syscall
	if stat, ok := fileInfo.Sys().(*syscall.Stat_t); ok {
		// stat.Blocks is in 512-byte blocks on most Unix systems
		actualSize := stat.Blocks * 512
		return uint64(actualSize), nil
	}

	// Fallback to logical size if syscall not available
	return uint64(fileInfo.Size()), nil
}

// abs returns the absolute value of x
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func main() {
	log := logv3.New()
	log.SetHandler(logv3.LvlFilterHandler(logv3.LvlInfo, logv3.StdoutHandler))

	args := os.Args[1:]
	if len(args) != 1 {
		log.Error("Usage: list-tables <db_path>")
		os.Exit(1)
	}

	dbMainDBPath := args[0] + "/chaindata"
	dbSMTDBPath := args[0] + "/smt"

	fmt.Printf("Checking database path: %s\n", args[0])
	fmt.Printf("Chaindata path: %s\n", dbMainDBPath)
	fmt.Printf("SMT path: %s\n", dbSMTDBPath)

	// Check if chaindata database file exists
	if _, err := os.Stat(dbMainDBPath + "/mdbx.dat"); os.IsNotExist(err) {
		log.Error("Chaindata DB path does not exist", "path", dbMainDBPath+"/mdbx.dat")
		os.Exit(1)
	}

	// Check if database separation is performed
	smtSeparated := checkSMTDatabase(dbSMTDBPath)
	if smtSeparated {
		fmt.Printf("\n🔄 Database separation status detected: SMT data separated to independent database\n")
		kv.InitStandaloneSMT(true) // Standalone SMT database mode
	} else {
		fmt.Printf("\n📋 Database separation status detected: All data in unified database\n")
		kv.InitStandaloneSMT(true) // Unified database mode
	}

	// Open chaindata database
	chaindb, err := openDatabase(dbMainDBPath, kv.ChainDB, log)
	if err != nil {
		log.Error("Failed to open chaindata db", "error", err)
		os.Exit(1)
	}
	defer chaindb.Close()

	log.Info("Chaindata database opened successfully")

	// Get chaindata table list
	chainTables, err := getTableList(chaindb)
	if err != nil {
		log.Error("Failed to get chaindata table list", "error", err)
		os.Exit(1)
	}

	// Display chaindata tables
	fmt.Printf("\n=== Tables in Chaindata Database (Total: %d) ===\n", len(chainTables))
	for i, table := range chainTables {
		fmt.Printf("%3d. %s\n", i+1, table)
	}

	// Handle SMT database (if exists)
	var smtTables []string
	var smtdb kv.RwDB

	if smtSeparated {
		smtdb, err = openDatabase(dbSMTDBPath, kv.SmtDB, log) // Use proper SMT configuration
		if err != nil {
			log.Error("Failed to open SMT database", "error", err)
		} else {
			defer smtdb.Close()

			log.Info("SMT database opened successfully")

			smtTables, err = getTableList(smtdb)
			if err != nil {
				log.Error("Failed to get SMT table list", "error", err)
			} else {
				fmt.Printf("\n=== Tables in SMT Database (Total: %d) ===\n", len(smtTables))
				for i, table := range smtTables {
					fmt.Printf("%3d. %s\n", i+1, table)
				}
			}
		}
	}

	// Category analysis
	fmt.Printf("\n=== Table Category Analysis ===\n")

	// Predefined table categories (shared with prune-chaindata)
	categories := map[string][]string{
		"SMT Related Tables": append(db.HermezSmtTables, "HermezSmtLastRoot"),
		"Basic Block Tables": {
			"HeaderNumber", "BadHeaderNumber", "HeadersTotalDifficulty",
			"BlockBody", "Header", "BlockTransaction", "Receipt", "TxSender", "CanonicalHeader",
			"BlockRoot", "BlockRootToBlockHash", "BlockRootToBlockNumber", "BlockRootToKzgCommitments",
			"LastBlock", "LastHeader", "MaxTxNum", "TransactionLog", "NonCanonicalTransaction",
			"BlockTransactionLookup", "BlockBorTransactionLookup", "InnerTx",
		},
		"State Data Tables": {
			"PlainState", "HashedStorage", "StateAccounts", "StateStorage",
			"StateCode", "StateCommitment", "Code", "HashedAccount", "HashedCodeHash",
			"PlainCodeHash", "TEVMCode", "StateEvents", "StateRoot",
			"IncarnationMap", "plain_state_version",
		},
		"History Data Tables": {
			"AccountChangeSet", "StorageChangeSet", "AccountHistory", "StorageHistory",
			"AccountHistoryKeys", "AccountHistoryVals", "StorageHistoryKeys", "StorageHistoryVals",
			"CodeHistoryKeys", "CodeHistoryVals", "CommitmentHistoryKeys", "CommitmentHistoryVals",
		},
		"Index Tables": {
			"LogTopicIndex", "LogAddressIndex", "CallTraceSet", "CallFromIndex", "CallToIndex",
			"LogAddressIdx", "LogAddressKeys", "LogTopicsIdx", "LogTopicsKeys",
			"TracesFromIdx", "TracesFromKeys", "TracesToIdx", "TracesToKeys",
			"CumulativeGasIndex", "CumulativeTransactionIndex",
		},
		"Domain/History Tables": {
			"AccountIdx", "AccountKeys", "AccountVals", "StorageIdx", "StorageKeys", "StorageVals",
			"CodeIdx", "CodeKeys", "CodeVals", "CommitmentIdx", "CommitmentKeys", "CommitmentVals",
			"RAccountIdx", "RAccountKeys", "RCodeIdx", "RCodeKeys", "RStorageIdx", "RStorageKeys",
		},
		"Trie Tables": {
			"TrieAccount", "TrieStorage", "VerkleRoots", "VerkleTrie",
		},
		"ZKEVM Tables": {
			"hermez_l1Verifications", "hermez_l1Sequences", "hermez_forkIds", "hermez_forkIdBlock",
			"hermez_blockBatches", "hermez_globalExitRootsSaved", "hermez_globalExitRoots",
			"hermez_txPricePercentage", "hermez_stateRoots", "l1_info_tree_updates",
			"hermez_intermediate_tx_stateRoots", "hermez_batch_witnesses", "hermez_batch_counters",
			"smt_depths", "invalid_batches", "batch_partially_processed", "local_exit_roots",
			"hermez_globalExitRoots_batches", "batch_blocks", "block_info_roots",
			"block_l1_block_hashes", "block_l1_info_tree_index", "l1_info_leaves", "l1_info_roots",
			"l1_info_tree_updates_by_ger", "latest_used_ger", "fork_history", "pp_rollup_types",
			"batch_ends", "block_l1_info_tree_progress", "confirmed_l1_info_tree_update",
			"l1_batch_data", "l1_injected_batches", "reused_l1_info_tree_index", "rollup_types_forks",
		},
		"Beacon Tables": {
			"BeaconState", "BeaconBlock", "CanonicalBlockRoots", "BlockRootToSlot",
			"BlockRootToStateRoot", "StateRootToBlockRoot", "BlockRootToParentRoot",
			"BeaconBlockHeaders", "HighestFinalized", "Attestetations", "LightClientUpdates",
			"ActiveValidatorIndicies", "BalancesDump", "EffectiveBalancesDump", "ValidatorBalance",
			"ValidatorEffectiveBalance", "ValidatorPublickeys", "ValidatorSlashings", "StaticValidators",
			"InvertedValidatorPublickeys", "InactivityScores", "PreviousEpochParticipation",
			"CurrentEpochParticipation", "NextSyncCommittee", "CurrentSyncCommittee",
			"HistoricalRoots", "HistoricalSummaries", "Eth1DataVotes", "IntraRandaoMixes",
			"RandaoMixes", "BlockProposers", "StatesProcessingProgress", "EpochData", "SlotData",
			"DevEpoch", "DevPendingEpoch", "LastBeaconSnapshot", "KzgCommitmentToBlob",
			"CurrentExecutionPayload", "LastForkchoice",
		},
		"Bor/Polygon Tables": {
			"BorCheckpointEnds", "BorCheckpoints", "BorEventNums", "BorEvents", "BorFinality",
			"BorMilestoneEnds", "BorMilestones", "BorReceipt", "BorSeparate", "BorSpans",
		},
		"Clique Tables": {
			"CliqueLastSnapshot", "CliqueSeparate", "CliqueSnapshot",
		},
		"System Tables": {
			"Config", "DbInfo", "SyncStage", "Migration", "Sequence", "Snapshots",
			"erigon_versions", "Issuance",
		},
		"Debug/Diagnostic Tables": {
			"bad_tx_hashes", "discarded_transactions_by_block", "discarded_transactions_by_hash",
			"just_unwound", "PoolLimbo",
		},
	}

	// Merge all tables for analysis
	allTables := make(map[string]string) // table -> database
	for _, table := range chainTables {
		allTables[table] = "chaindata"
	}
	for _, table := range smtTables {
		allTables[table] = "smt"
	}

	// Analyze each category
	for category, expectedTables := range categories {
		found := make([]struct {
			name string
			db   string
		}, 0)

		for _, expectedTable := range expectedTables {
			if dbName, exists := allTables[expectedTable]; exists {
				found = append(found, struct {
					name string
					db   string
				}{expectedTable, dbName})
			}
		}

		if len(found) > 0 {
			fmt.Printf("\n%s (Found %d/%d):\n", category, len(found), len(expectedTables))
			for _, item := range found {
				fmt.Printf("  - %s (%s)\n", item.name, item.db)
			}
		}
	}

	// Unclassified tables
	unclassified := make([]struct {
		name string
		db   string
	}, 0)

	for tableName, dbName := range allTables {
		found := false
		for _, expectedTables := range categories {
			for _, expectedTable := range expectedTables {
				if tableName == expectedTable {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			unclassified = append(unclassified, struct {
				name string
				db   string
			}{tableName, dbName})
		}
	}

	if len(unclassified) > 0 {
		fmt.Printf("\nUnclassified Tables (Total: %d):\n", len(unclassified))
		for _, item := range unclassified {
			fmt.Printf("  - %s (%s)\n", item.name, item.db)
		}
	}

	// Table size statistics
	fmt.Printf("\n=== Table Size Statistics ===\n")

	// Chaindata table statistics
	fmt.Printf("\nChaindata Database:\n")
	for _, tableName := range chainTables {
		entries, sizeBytes, pages, err := getTableStats(chaindb, tableName)
		if err != nil {
			fmt.Printf("  %-30s: Failed to get stats: %v\n", tableName, err)
			continue
		}

		sizeStr := datasize.ByteSize(sizeBytes).HumanReadable()
		fmt.Printf("  %-30s: %s (%d entries, %d pages)\n", tableName, sizeStr, entries, pages)
	}

	// SMT table statistics
	if smtSeparated && smtdb != nil {
		fmt.Printf("\nSMT Database:\n")
		for _, tableName := range smtTables {
			entries, sizeBytes, pages, err := getTableStats(smtdb, tableName)
			if err != nil {
				fmt.Printf("  %-30s: Failed to get stats: %v\n", tableName, err)
				continue
			}

			sizeStr := datasize.ByteSize(sizeBytes).HumanReadable()
			fmt.Printf("  %-30s: %s (%d entries, %d pages)\n", tableName, sizeStr, entries, pages)
		}
	}

	// Database size comparison
	fmt.Printf("\n=== Database Size Analysis ===\n")

	// Calculate total table sizes for chaindata
	var chainTotalTableSize uint64
	for _, tableName := range chainTables {
		_, sizeBytes, _, err := getTableStats(chaindb, tableName)
		if err == nil {
			chainTotalTableSize += sizeBytes
		}
	}

	// Get actual database size for chaindata
	chainActualSize, err := getDatabaseSize(dbMainDBPath)
	if err != nil {
		fmt.Printf("Failed to get chaindata database size: %v\n", err)
	} else {
		chainTotalStr := datasize.ByteSize(chainTotalTableSize).HumanReadable()
		chainActualStr := datasize.ByteSize(chainActualSize).HumanReadable()
		chainDiff := int64(chainActualSize) - int64(chainTotalTableSize)
		chainDiffStr := datasize.ByteSize(uint64(abs(chainDiff))).HumanReadable()
		chainDiffPercent := float64(chainDiff) / float64(chainActualSize) * 100

		fmt.Printf("Chaindata Database:\n")
		fmt.Printf("  Tables total size:     %s\n", chainTotalStr)
		fmt.Printf("  Database actual size:  %s\n", chainActualStr)
		fmt.Printf("  Difference:           %s%s (%.1f%%)\n",
			map[bool]string{true: "+", false: ""}[chainDiff >= 0],
			chainDiffStr, chainDiffPercent)
	}

	// Calculate for SMT if separated
	if smtSeparated && smtdb != nil {
		var smtTotalTableSize uint64
		for _, tableName := range smtTables {
			_, sizeBytes, _, err := getTableStats(smtdb, tableName)
			if err == nil {
				smtTotalTableSize += sizeBytes
			}
		}

		smtActualSize, err := getDatabaseSize(dbSMTDBPath)
		if err != nil {
			fmt.Printf("Failed to get SMT database size: %v\n", err)
		} else {
			smtTotalStr := datasize.ByteSize(smtTotalTableSize).HumanReadable()
			smtActualStr := datasize.ByteSize(smtActualSize).HumanReadable()
			smtDiff := int64(smtActualSize) - int64(smtTotalTableSize)
			smtDiffStr := datasize.ByteSize(uint64(abs(smtDiff))).HumanReadable()
			smtDiffPercent := float64(smtDiff) / float64(smtActualSize) * 100

			fmt.Printf("\nSMT Database:\n")
			fmt.Printf("  Tables total size:     %s\n", smtTotalStr)
			fmt.Printf("  Database actual size:  %s\n", smtActualStr)
			fmt.Printf("  Difference:           %s%s (%.1f%%)\n",
				map[bool]string{true: "+", false: ""}[smtDiff >= 0],
				smtDiffStr, smtDiffPercent)
		}
	}

	// Summary
	fmt.Printf("\n=== Summary ===\n")
	if smtSeparated {
		fmt.Printf("Database separation: SEPARATED (chaindata + smt databases)\n")
	} else {
		fmt.Printf("Database separation: UNIFIED (single database contains all data)\n")
	}
	fmt.Printf("Chaindata database has %d tables\n", len(chainTables))
	if smtSeparated {
		fmt.Printf("SMT database has %d tables\n", len(smtTables))
	}
	fmt.Printf("Total tables: %d\n", len(allTables))
	fmt.Printf("SMT related table definitions: %v\n", db.HermezSmtTables)
}
