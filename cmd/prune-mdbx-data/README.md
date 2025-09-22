# X Layer MDBX Data Management Tool

A comprehensive database management tool for X Layer (zkEVM) Erigon nodes, providing advanced data pruning and database compaction capabilities.

## Overview

This tool suite addresses the unique challenges of managing X Layer zkEVM node databases:
- **Large database sizes** (often 100GB+ for mainnet)
- **Historical data accumulation** from blockchain operations
- **Database fragmentation** and freelist space
- **zkEVM-specific tables** that require careful handling

## Features

### 🔍 Database Analysis (`list-tables`)
- **Comprehensive table statistics**: size, entries, page count
- **Database separation detection**: automatically handles chaindata/SMT split
- **Size discrepancy analysis**: identifies freelist and fragmentation overhead (shows actual disk usage)
- **Category-based organization**: groups tables by function
- **Accurate disk usage**: Reports real disk usage rather than sparse file virtual size

### 🧹 Data Pruning (`prune-chaindata`) 
- **Moderate mode (default)**: Comprehensive cleanup with batch retention (~52-57GB savings)  
- **Aggressive mode**: Maximum cleanup including historical state data (~64-69GB savings)
- **Batch-based optimization**: Keeps recent zkEVM batches for operational needs
- **zkEVM table protection**: Automatically protects critical SMT and sequencer tables
- **Small table protection**: 5 small tables never cleaned (block_l1_info_tree_index, plain_state_version, smt_depths, HeadersTotalDifficulty, MaxTxNum)
- **DupCursor handling**: Specialized processing for 2 dupCursor tables (AccountChangeSet, StorageChangeSet) in Aggressive mode
- **Copy-Truncate-Restore optimization**: Dramatically faster pruning by preserving recent data instead of deleting old data
- **Safe alternative**: Use `compact-db` for zero-risk cleanup (~30-35% savings)

### 📦 Database Compaction (`compact-db`)
- **Freelist space recovery**: Reclaims space from deleted data
- **Fragmentation elimination**: Reorganizes data for optimal storage
- **Two operation modes**: Copy mode (safe) or in-place mode (space-efficient)
- **Dual database support**: Handles both chaindata and SMT databases

## Quick Start

### 1. Analyze Your Database
```bash
./prune-tool list-tables /path/to/datadir
```

### 2. Prune Unnecessary Data
```bash
# Moderate (default recommended, ~52-57GB savings)  
./prune-tool prune-chaindata /path/to/datadir moderate --keep-recent-batches=10

# Aggressive (maximum, ~64-69GB savings, preserves critical mapping tables)
./prune-tool prune-chaindata /path/to/datadir aggressive --keep-recent-batches=5

# Safe alternative: Database compaction (zero risk, ~30-35% savings)
./prune-tool compact-db -source /path/to/datadir/chaindata -in-place
```

### 3. Compact Database (Optional)
```bash
# Analyze compaction potential (no actual compaction)
./prune-tool compact-db -source /path/to/datadir/chaindata -dry-run

# Method 1: In-place compaction (recommended, requires temporary space)
./prune-tool compact-db -source /path/to/datadir/chaindata -in-place

# Method 2: Copy mode compaction (creates new database)  
./prune-tool compact-db -source /path/to/datadir/chaindata -output /path/to/datadir/chaindata.compact

# Compact SMT database in-place (recommended for large SMT databases)
./prune-tool compact-db -source /path/to/datadir/smt -in-place
```

## Optimal Workflow

### For Maximum Space Savings (Recommended)
```bash
# Step 1: Analyze current state
./prune-tool list-tables /path/to/datadir > before.txt

# Step 2: Prune unnecessary data first
./prune-tool prune-chaindata /path/to/datadir moderate --keep-recent-batches=10

# Step 3: Compact databases in-place (automated replacement)
./prune-tool compact-db -source /path/to/datadir/chaindata -in-place
./prune-tool compact-db -source /path/to/datadir/smt -in-place

# Step 4: Verify results
./prune-tool list-tables /path/to/datadir > after.txt

# Step 5: Clean up automatic backups (after verification)
rm -rf /path/to/datadir/chaindata.backup /path/to/datadir/smt.backup
```

### Alternative: Manual Control Workflow
```bash
# For users who prefer step-by-step manual control
./prune-tool list-tables /path/to/datadir > before.txt
./prune-tool prune-chaindata /path/to/datadir moderate --keep-recent-batches=10

# Analyze compaction potential first
./prune-tool compact-db -source /path/to/datadir/chaindata -dry-run

# Manual compaction with copy mode
./prune-tool compact-db -source /path/to/datadir/chaindata -output /path/to/datadir/chaindata.compact

# Manual replacement
mv /path/to/datadir/chaindata /path/to/datadir/chaindata.backup
mv /path/to/datadir/chaindata.compact /path/to/datadir/chaindata

./prune-tool list-tables /path/to/datadir > after.txt
```

### Why This Order?
1. **Prune first**: Removes unnecessary data (larger impact)
2. **Compact second**: Reclaims fragmentation from deleted data
3. **Result**: Maximum space savings with minimum processing time

## Command Reference

### `list-tables <db_path>`
Analyzes database structure and provides detailed statistics.

**Output includes:**
- Individual table sizes and entry counts
- Database separation status (unified vs separated)  
- Size discrepancy analysis (actual vs table data)
- Category-based table organization

### `prune-chaindata <db_path> [level] [options]`

**Pruning Levels:**
- `conservative`: Only removes obviously unnecessary tables (~45 tables)
- `moderate`: Removes verified unnecessary + batch-based pruning (~80+ tables)
- `aggressive`: Maximum cleanup including historical dupCursor data (~90+ tables)

**Protected Tables:**
- **Small tables**: 5 small tables are never cleaned regardless of mode (data size < 10MB each)
  - `block_l1_info_tree_index`, `plain_state_version`, `smt_depths`, `HeadersTotalDifficulty`, `MaxTxNum`
- **Critical tables**: SMT, zkEVM operational, system configuration tables
- **DupCursor tables**: Special handling in aggressive mode (AccountChangeSet, StorageChangeSet processed; CanonicalHeader, hermez_blockBatches preserved for stability)

**Options:**
- `--keep-recent-batches=N`: Keep N most recent batches (default: 10)
- `--yes, -y`: Auto-confirm, skip interactive prompts

**Examples:**
```bash
# Interactive moderate pruning
./prune-tool prune-chaindata ./datadir moderate

# Non-interactive with custom retention
./prune-tool prune-chaindata ./datadir moderate --keep-recent-batches=5 --yes

# Maximum cleanup (use carefully)
./prune-tool prune-chaindata ./datadir aggressive --keep-recent-batches=3 --yes
```

### `compact-db -source <src> [-output <dst>] [-in-place] [-backup] [options]`

**Operation Modes:**
- **Analysis**: `-source <path> -dry-run` (no output needed)
- **Copy**: `-source <path> -output <newpath>` (creates new database)
- **In-place**: `-source <path> -in-place` (replaces original)

**Options:**
- `-source <path>`: Source database path (required)
- `-output <path>`: Output path (required for copy mode only)
- `-in-place`: Compact and replace original database  
- `-backup`: Create backup before in-place replacement (default: false)
- `-dry-run`: Show analysis without performing compaction

**Examples:**
```bash
# Analyze potential savings (auto-detects database type)
./prune-tool compact-db -source ./datadir/chaindata -dry-run
./prune-tool compact-db -source ./datadir/smt -dry-run

# In-place compaction (fast, no backup - default behavior)
./prune-tool compact-db -source ./datadir/chaindata -in-place
./prune-tool compact-db -source ./datadir/smt -in-place

# In-place compaction with backup (safer but uses more space)
./prune-tool compact-db -source ./datadir/chaindata -in-place -backup

# Copy mode compaction (if you prefer manual control)
./prune-tool compact-db -source ./datadir/chaindata -output ./datadir/chaindata.compact
```

## Expected Space Savings

**⚠️ Important**: Based on realistic mainnet data (Chaindata: 76GB + SMT: 106GB = **182GB total**)

| Operation | Realistic Savings | Mainnet Example (182GB Total) | Breakdown | Notes |
|-----------|------------------|-------------------------------|-----------|-------|
| **Compaction Only** | 30-35% | ~55GB from 182GB | SMT: ~49GB, Chaindata: ~6GB | Zero risk, reclaims freelist space |
| **Moderate Pruning** | 29-31% | ~52-57GB from 182GB | Chaindata table deletion only | Requires compaction to reclaim space |
| **Aggressive Pruning** | 35-38% | ~64-69GB from 182GB | Moderate + historical state cleanup | Requires compaction to reclaim space |  
| **Moderate + Compaction** | 59-62% | ~107-112GB from 182GB | ~52-57GB + ~55GB compaction | Best balance of safety and savings |
| **Aggressive + Compaction** | 66-68% | ~119-124GB from 182GB | ~64-69GB + ~55GB compaction | Maximum practical savings with stability |

### Key Insights
- **🔍 SMT compaction is the biggest win**: ~49GB (46% of SMT file size) due to historical operations leaving massive freelist
- **📊 Pruning != immediate space reclaim**: Table deletion requires compaction to actually free disk space  
- **🎯 Combined approach is essential**: Pruning alone won't significantly reduce file sizes without compaction
- **💾 Total database size matters**: Previous calculations incorrectly ignored the 106GB SMT database

## Safety Guidelines

### Before Any Operation
1. **Stop Erigon node completely**
2. **Create backup**: `cp -r datadir datadir.backup`
3. **Verify disk space**: Ensure adequate free space for operations
4. **Test in staging environment** if possible

### During Operations
- Monitor disk space usage
- Check for error messages in output
- Be patient - large databases take time to process

### After Operations
1. **Test node startup** with modified database
2. **Verify synchronization** resumes properly  
3. **Monitor RPC functionality** if using pruned database
4. **Remove backups** only after confirming stability

### Recovery Procedures
If issues occur:
```bash
# Restore from backup
rm -rf datadir
mv datadir.backup datadir

# Or restore compaction
mv datadir/chaindata.backup datadir/chaindata
```

## Database Architecture

### Unified vs Separated Databases
- **Unified**: Single database contains both chaindata and SMT tables
- **Separated**: Chaindata and SMT in separate database files
- **Detection**: Tool automatically detects and handles both configurations

### Key Database Components
- **Chaindata**: Block headers, transactions, receipts, execution data
- **SMT**: Sparse Merkle Tree data for zkEVM proof generation
- **Freelist**: Deleted data space available for reuse
- **Metadata**: Database system tables and indexes

## Troubleshooting

### Common Issues

**"resource temporarily unavailable" (compact-db)**
- **Fixed**: Database connection management improved to prevent MDBX lock conflicts
- **Cause**: Previously occurred when multiple database connections weren't properly closed  
- **Solution**: Tool now includes proper connection cleanup and timing delays

**"Assertion failed: pgno_align2os_bytes"**
- Cause: MDBX database geometry mismatch
- Solution: Fixed in current version with improved database opening logic

**"Database size analysis shows incorrect values"**  
- **Fixed**: Tools now show actual disk usage instead of sparse file virtual size
- **Example**: Previously might show 8GB when actual usage is 24MB
- **Solution**: Both `list-tables` and `compact-db` use syscall to report real disk consumption

**"Database size analysis shows 0 B"**  
- Cause: Incorrect database label or path
- Solution: Tool now auto-detects database separation and uses correct labels

**"Pruning had no effect"**
- Cause: Database already clean or incorrect table targeting
- Solution: Use `list-tables` to verify which tables contain data

**"Compact operation failed"**
- Cause: Insufficient disk space or permission issues
- Solution: Ensure 2x database size free space and proper permissions

### Performance Tips

**For Large Databases (100GB+):**
- Run operations during maintenance windows
- Consider processing SMT and chaindata separately
- Use SSD storage for better I/O performance
- Monitor system memory usage during operations

**For Space-Constrained Systems:**
- Use `dry-run` mode first to plan space requirements
- Consider processing in stages (prune first, compact later)

### Pruning Performance Optimization

**Copy-Truncate-Restore Algorithm (New)**:

Batch-based pruning now uses an optimized algorithm for dramatically improved performance:

**Legacy Method**: Delete old batches one by one (~500,000 operations)  
**New Method**: Copy recent data → Clear tables → Restore recent data (~1,000 operations)

**Result**: **50-100x faster** for mainnet databases with many historical batches

**Automatic Safety**: Falls back to legacy method if any issues occur
- Clean up temporary files promptly

## Development

### Building from Source
```bash
cd cmd/prune-mdbx-data
go build -o prune-tool main.go
```

### Testing
```bash
# Test on small database first
./prune-tool list-tables /path/to/test/datadir
./prune-tool prune-chaindata /path/to/test/datadir conservative --dry-run
```

## Documentation

- [ACTIVE_TABLES_ANALYSIS.md](ACTIVE_TABLES_ANALYSIS.md): Detailed analysis of all active tables
- [SMALL_TABLES_PROTECTION.md](SMALL_TABLES_PROTECTION.md): Strategy for protecting small tables
- [cmd/prune-chaindata/COMPLETE_TABLE_FORMATS.md](cmd/prune-chaindata/COMPLETE_TABLE_FORMATS.md): Complete table format reference

## Support

For issues specific to X Layer zkEVM:
- Verify SMT database integrity after operations
- Test sequencer functionality before production deployment  
- Monitor zkEVM proof generation performance
- Consider batch retention requirements for your use case

---

**Warning**: Always backup your data before performing any database operations. While these tools are designed to be safe, database operations always carry inherent risks.