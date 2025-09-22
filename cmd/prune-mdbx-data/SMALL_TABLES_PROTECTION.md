# Table Protection Strategy

## Overview

This document explains the strategy for protecting database tables from cleanup operations in X Layer Erigon pruning tools, including both small tables and critical dupCursor tables.

## Protected Small Tables

The following 5 tables are **permanently protected** from all pruning operations regardless of mode:

| Table Name | Size | Description | Rationale |
|------------|------|-------------|-----------|
| **block_l1_info_tree_index** | 1.2 MB | L1 info tree index | Small size, index data |
| **plain_state_version** | 8.0 KB | State version tracking | Tiny size, version control |
| **smt_depths** | 8.0 KB | SMT tree depth info | Tiny size, SMT metadata |
| **HeadersTotalDifficulty** | 8.0 KB | Chain total difficulty | Tiny size, chain metadata |
| **MaxTxNum** | 8.0 KB | Maximum transaction number | Tiny size, transaction metadata |

## Protected DupCursor Tables (Node Stability)

The following 2 dupCursor tables are **protected in Aggressive mode** for node stability:

| Table Name | Size | Description | Rationale |
|------------|------|-------------|-----------|
| **CanonicalHeader** | 1.5 GB | Canonical chain headers | Critical for chain state and node startup |
| **hermez_blockBatches** | 779.6 MB | L2 block to batch mappings | Essential for sequence execution |

## Protection Strategy

### Why Small Tables Are Protected

1. **Minimal Space Impact**: Combined size < 10MB, negligible cleanup benefit
2. **Safety First**: Preserving small tables eliminates any risk of breaking functionality
3. **Development Efficiency**: Reduces complexity in pruning logic
4. **User Request**: Explicitly requested by development team

### Why DupCursor Tables Are Protected

1. **Node Stability**: CanonicalHeader deletion breaks chain state recognition
2. **Sequence Execution**: hermez_blockBatches deletion causes "nil pointer dereference" errors
3. **Critical Mappings**: These provide essential L2 operation mappings
4. **Stability Over Space**: ~2.3GB preserved to ensure reliable node operation

### Implementation Details

#### Code Changes for Small Tables
- Added to `getCriticalTables()` function as protected tables
- Removed from all pruning logic functions:
  - `copyBlockData()`: Excluded from batch operations
  - `clearBatchTables()`: Excluded from table clearing
  - `deleteBlockData()`: Excluded from legacy deletion
  - `deleteCompositeKeyData()`: HeadersTotalDifficulty excluded

#### Code Changes for DupCursor Tables
- **CanonicalHeader** and **hermez_blockBatches**:
  - Removed from batch-processing functions to avoid cursor type conflicts
  - Excluded from `pruneHistoricalDupCursorData()` in Aggressive mode
  - Added to `partiallyPrunedTables` to prevent full deletion
  - Preserved existing dupCursor processing functions but disabled their usage

#### Before/After Behavior

**Small Tables - Before (Previous Behavior)**:
```
Moderate Mode: 🔄 Batch-based pruning
Aggressive Mode: 🔄 Batch-based pruning  
```

**Small Tables - After (Current Behavior)**:
```
All Modes: 🛡️ Protected (never touched)
```

**DupCursor Tables - Before (Previous Behavior)**:
```
Moderate Mode: 🛡️ Protected
Aggressive Mode: 🔄 DupCursor-based pruning (caused node crashes)
```

**DupCursor Tables - After (Current Behavior)**:
```
All Modes: 🛡️ Protected (preserved for stability)
```

## Impact Analysis

### Space Savings Impact (Small Tables)
- **Before**: Could save ~10MB total from 5 small tables
- **After**: 0MB saved from small tables
- **Net Impact**: Negligible (<0.01% of typical database size)

### Space Savings Impact (DupCursor Tables)
- **Before**: Could save ~2.3GB from CanonicalHeader + hermez_blockBatches in Aggressive mode
- **After**: 0GB saved from these tables (preserved for stability)
- **Net Impact**: Aggressive mode still saves +12.5GB over Moderate mode from AccountChangeSet + StorageChangeSet historical data

### Risk Reduction
- **Before**: Small risk of breaking edge-case functionality (small tables) + High risk of node crashes (dupCursor tables)
- **After**: Zero risk from protected tables
- **Benefit**: Higher safety margin with minor space trade-off, eliminates critical node startup failures

### Code Maintenance
- **Before**: Complex logic handling small tables in multiple functions
- **After**: Simple exclusion, cleaner code
- **Benefit**: Reduced maintenance burden, fewer edge cases

## Comparison With Other Protection Strategies

### Critical Tables (Always Protected)
- **SMT Tables**: Essential for zkEVM operation (60GB+)
- **System Tables**: Database integrity (Config, DbInfo, etc.)
- **Current State**: PlainState, HashedStorage, etc.

### Small Tables (User-Specified Protection)
- **Size-Based**: < 10MB each
- **Safety-Based**: Better safe than sorry
- **Maintenance-Based**: Reduces code complexity

### DupCursor Tables (Special Handling)
- **AccountChangeSet**: DupCursor handling in Aggressive mode
- **StorageChangeSet**: DupCursor handling in Aggressive mode  
- **CanonicalHeader**: DupCursor handling in Aggressive mode
- **hermez_blockBatches**: DupCursor handling in Aggressive mode

## Future Considerations

### Adding New Small Tables
If new small tables (< 10MB) are identified:
1. Evaluate cleanup benefit vs. risk
2. If benefit < 50MB, consider adding to protection list
3. Update this document and code accordingly

### Removing Protection
To remove protection from any of these tables:
1. Remove from `getCriticalTables()` function
2. Add back to appropriate pruning functions
3. Test thoroughly in staging environment
4. Update all documentation

## Configuration

Both small tables and dupCursor tables are **hard-coded** as protected. There is no configuration option to change this behavior.

### Rationale for Hard-Coding
- **Simplicity**: No configuration complexity
- **Safety**: Prevents accidental enabling of risky operations (especially for dupCursor tables)
- **User Intent**: Explicit request was to never clean small tables, and stability issues forced protection of dupCursor tables
- **Node Stability**: DupCursor table protection is mandatory to prevent runtime crashes

## Testing Verification

To verify protection is working:

```bash
# Run pruning and check these tables are never mentioned in deletion logs
./prune-tool prune-chaindata /path/to/datadir aggressive --yes

# Check logs should NOT contain (small tables):
# - "Clearing table: block_l1_info_tree_index"
# - "Clearing table: plain_state_version"  
# - "Clearing table: smt_depths"
# - "Clearing table: HeadersTotalDifficulty"
# - "Clearing table: MaxTxNum"

# Check logs should NOT contain (dupCursor tables):
# - "Processing 4 dupCursor tables: AccountChangeSet, StorageChangeSet, CanonicalHeader, hermez_blockBatches"
# - "✓ Deleted X CanonicalHeader records"
# - "✓ Deleted X hermez_blockBatches records"

# Instead should see:
# - "⊜ Skipped table: ... (small table protection)"  (for small tables)
# - "Processing 2 dupCursor tables: AccountChangeSet, StorageChangeSet" (for Aggressive mode)
# - "Note: CanonicalHeader and hermez_blockBatches are preserved for node stability"
```

## Related Documentation

- [README.md](README.md): Main tool documentation
- [ACTIVE_TABLES_ANALYSIS.md](ACTIVE_TABLES_ANALYSIS.md): Complete table analysis
- [COMPLETE_TABLE_FORMATS.md](cmd/prune-chaindata/COMPLETE_TABLE_FORMATS.md): Table format reference

---

**Last Updated**: December 2024  
**Change Reason**: User request to protect small tables + Node stability issues requiring dupCursor table protection
