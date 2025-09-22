package main

// Define pruning levels
type PruneLevel int

const (
	PruneLevelModerate   PruneLevel = iota // Moderate pruning (recommended)
	PruneLevelAggressive                   // Aggressive pruning (includes state data cleanup)
)

// MainConfig holds all configuration for the pruning operation
type MainConfig struct {
	DBPath             string
	PruneLevel         PruneLevel
	KeepRecentBatches  uint64
	AutoYes            bool
	FastDupCursorMode  bool
	SafeFastMode       bool
	GenesisBlockHeight uint64 // Genesis block height to protect (default: 0)
}

// MainPaths holds database path configuration
type MainPaths struct {
	MainPath      string
	ChaindataPath string
	SMTPath       string
	SMTSeparated  bool
}

// MainAnalysis holds the results of database analysis
type MainAnalysis struct {
	AllTables         []string
	ToDelete          []string
	Critical          map[string]bool
	PreCollectedStats map[string]struct {
		entries   uint64
		sizeBytes uint64
		pages     uint64
	}
	SortedTables      []tableSizeInfo
	TotalToDeleteSize uint64
	TotalDbSize       uint64
	SMTTableCount     int
}

// MainStats holds statistics about the pruning operation
type MainStats struct {
	DeletedBatches        int
	DeletedBlocks         int
	DeletedCount          int
	ActuallyDeletedTables int
	ActualDeletedSize     uint64
}
