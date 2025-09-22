package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "list-tables":
		if len(os.Args) != 3 {
			fmt.Println("Usage: prune-mdbx-data list-tables <db_path>")
			os.Exit(1)
		}
		runListTables(os.Args[2])
	case "prune-chaindata":
		runPruneChaindata(os.Args[2:])
	case "compact-db":
		runCompactDB(os.Args[2:])
	case "help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func runListTables(dbPath string) {
	// Try local tool first, then system PATH
	toolName := "list-tables-tool"
	if _, err := os.Stat("./" + toolName); os.IsNotExist(err) {
		// Use tool from PATH (Docker environment)
		toolName = toolName
	} else {
		// Use local tool (development environment)
		toolName = "./" + toolName
	}

	runCmd := exec.Command(toolName, dbPath)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		fmt.Printf("Error: failed to run list-tables: %v\n", err)
		os.Exit(1)
	}
}

func runPruneChaindata(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: prune-mdbx-data prune-chaindata <db_path> [level] [options]")
		fmt.Println("Run 'prune-mdbx-data help' for more information")
		os.Exit(1)
	}

	// Try local tool first, then system PATH
	toolName := "prune-chaindata-tool"
	if _, err := os.Stat("./" + toolName); os.IsNotExist(err) {
		// Use tool from PATH (Docker environment)
		toolName = toolName
	} else {
		// Use local tool (development environment)
		toolName = "./" + toolName
	}

	runCmd := exec.Command(toolName, args...)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		fmt.Printf("Error: failed to run prune-chaindata: %v\n", err)
		os.Exit(1)
	}
}

func runCompactDB(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: prune-mdbx-data compact-db -source <source_db_path> [-output <output_path>] [-in-place] [-backup] [-dry-run]")
		fmt.Println("Run 'prune-mdbx-data help' for more information")
		os.Exit(1)
	}

	// Try local tool first, then system PATH
	toolName := "compact-db-tool"
	if _, err := os.Stat("./" + toolName); os.IsNotExist(err) {
		// Use tool from PATH (Docker environment)
		toolName = toolName
	} else {
		// Use local tool (development environment)
		toolName = "./" + toolName
	}

	runCmd := exec.Command(toolName, args...)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		fmt.Printf("Error: failed to run compact-db: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("🗂️  X Layer MDBX Data Pruning Tool")
	fmt.Println()
	fmt.Println("A comprehensive database management tool for X Layer (zkEVM) Erigon nodes")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  prune-mdbx-data <command> [arguments]")
	fmt.Println()
	fmt.Println("COMMANDS:")
	fmt.Println("  list-tables <db_path>                   📋 List all database tables with sizes and statistics")
	fmt.Println("  prune-chaindata <db_path> [level] [options]  🧹 Prune unnecessary data from chaindata")
	fmt.Println("  compact-db -source <src> [-output <dst>] [-in-place] [options] 📦 Compact database to reclaim freelist space")
	fmt.Println("  help                                     ❓ Show detailed help information")
	fmt.Println()
	fmt.Println("PRUNING LEVELS (prune-chaindata):")
	fmt.Println("  moderate (default)                      ⚖️  Moderate - comprehensive cleanup with batch optimization")
	fmt.Println("                                             Strategy: Delete verified unnecessary tables + batch-based pruning")
	fmt.Println("                                             Deletes: History, Index, Trie, Beacon, Diagnostic tables + old batch data")
	fmt.Println("                                             Space savings: ~52-57GB (29-31% of 182GB total)")
	fmt.Println("                                             zkEVM optimized: Balance space saving with operational capability")
	fmt.Println()
	fmt.Println("  aggressive                              🚀 Aggressive - maximum cleanup including historical data")
	fmt.Println("                                             Strategy: Moderate + historical state data cleanup")
	fmt.Println("                                             Space savings: ~62-67GB (34-37% of 182GB total)")
	fmt.Println("                                             ⚠️  May affect some historical RPC queries")
	fmt.Println()
	fmt.Println("PRUNING OPTIONS:")
	fmt.Println("  --keep-recent-batches=N                 🎯 Keep N most recent batches (default: 10)")
	fmt.Println("  --yes, -y                               ⚡ Auto-confirm, skip interactive confirmation")
	fmt.Println()
	fmt.Println("COMPACTION OPTIONS (compact-db):")
	fmt.Println("  -source <path>                          📂 Source database path (required)")
	fmt.Println("  -output <path>                          📁 Output path for compacted database (optional with -in-place)")
	fmt.Println("  -in-place                               🔄 Compact database in-place (replaces original)")
	fmt.Println("  -backup                                 💾 Create backup before in-place replacement (default: false)")
	fmt.Println("  -dry-run                                👁️  Show analysis without compacting (auto-detects database type)")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  # List all database tables")
	fmt.Println("  prune-mdbx-data list-tables ./datadir")
	fmt.Println()
	fmt.Println("  # Default moderate pruning (interactive)")
	fmt.Println("  prune-mdbx-data prune-chaindata ./datadir")
	fmt.Println()
	fmt.Println("  # Moderate pruning with custom batch retention")
	fmt.Println("  prune-mdbx-data prune-chaindata ./datadir moderate --keep-recent-batches=5")
	fmt.Println()
	fmt.Println("  # Non-interactive moderate pruning")
	fmt.Println("  prune-mdbx-data prune-chaindata ./datadir moderate --keep-recent-batches=10 --yes")
	fmt.Println()
	fmt.Println("  # Copy mode compaction (creates new database)")
	fmt.Println("  prune-mdbx-data compact-db -source ./datadir/chaindata -output ./datadir/chaindata.compact")
	fmt.Println()
	fmt.Println("  # In-place compaction (replaces original directly, no backup)")
	fmt.Println("  prune-mdbx-data compact-db -source ./datadir/chaindata -in-place")
	fmt.Println()
	fmt.Println("  # In-place compaction with backup (safer but uses more space)")
	fmt.Println("  prune-mdbx-data compact-db -source ./datadir/chaindata -in-place -backup")
	fmt.Println()
	fmt.Println("  # Analyze potential space savings (dry run)")
	fmt.Println("  prune-mdbx-data compact-db -source ./datadir/chaindata -dry-run")
	fmt.Println()
	fmt.Println("  # Compact SMT database in-place (auto-detects database type)")
	fmt.Println("  prune-mdbx-data compact-db -source ./datadir/smt -in-place")
	fmt.Println()
	fmt.Println("SAFETY NOTES:")
	fmt.Println("  ⚠️  Always backup your data before pruning or compacting")
	fmt.Println("  ⚠️  Stop Erigon node before performing database operations")
	fmt.Println("  ⚠️  Moderate level uses batch-based deletion optimized for zkEVM")
	fmt.Println("  ⚠️  All zkEVM critical tables are automatically protected")
	fmt.Println("  ⚠️  Database compaction auto-detects database type and reclaims freelist space (5-15% typical savings)")
	fmt.Println("  ⚠️  Use --yes flag carefully in production environments")
	fmt.Println()
	fmt.Println("For more information, see: cmd/prune-mdbx-data/README.md")
}
