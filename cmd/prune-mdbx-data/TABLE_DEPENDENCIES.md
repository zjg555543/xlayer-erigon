# X Layer Erigon Table Dependencies and Cleanup Strategy Analysis

## Executive Summary

This document analyzes the complex interdependencies between core blockchain tables in X Layer Erigon database and explains why certain table cleanup operations unexpectedly corrupt account state data, specifically causing nonce queries to return historical values instead of current values.

**Critical Issue Discovered**: Batch cleanup operations corrupt PlainState table, causing account nonces to revert from current values (e.g., 38) to genesis/historical values (e.g., 8).

## Mainnet Table Size Analysis

Based on actual X Layer mainnet database analysis:

### Large Tables (Primary Cleanup Targets)
| Table Name | Size | Description | Cleanup Impact |
|------------|------|-------------|----------------|
| **Header** | 17.1 GB | Block headers with metadata | **HIGH RISK** - Affects state queries |
| **StorageChangeSet** | 10.0 GB | Historical storage state changes | **HIGH RISK** - DupCursor table |
| **StorageHistory** | 6.6 GB | Storage change history index | Protected |
| **BlockTransaction** | 5.2 GB | Complete transaction RLP data | Safe to delete |
| **TransactionLog** | 4.8 GB | Transaction execution logs | Moderate risk |
| **PlainState** | 4.4 GB | **CRITICAL** - Current account/storage state | **NEVER TOUCH** |
| **HashedStorage** | 4.0 GB | Hashed storage keys/values | Protected |
| **AccountChangeSet** | 2.5 GB | Historical account state changes | **HIGH RISK** - DupCursor table |
| **HeaderNumber** | 2.1 GB | Block number to header hash mapping | **HIGH RISK** - Affects lookups |
| **BlockBody** | 1.8 GB | Block bodies with transaction lists | **HIGH RISK** - Affects state |
| **CanonicalHeader** | 1.5 GB | Canonical block headers | **CRITICAL** - Chain ordering |

**Total Potential Cleanup**: ~55-60 GB
**Current Safe Cleanup**: ~8-12 GB (only safe tables)

## Detailed Table Relationship Analysis

### 1. Header Tables Ecosystem

#### Core Structure:
```
Header (17.1 GB)
├── Key: block_number(8) + block_hash(32) 
├── Value: RLP-encoded block header
└── Contains: ALL block headers (canonical + forks)

CanonicalHeader (1.5 GB) 
├── Key: block_number(8)
├── Value: canonical_block_hash(32)
├── Purpose: Defines main chain consensus
└── **CRITICAL**: Referenced by state query logic

HeaderNumber (2.1 GB)
├── Key: block_hash(32) 
├── Value: block_number(8)
├── Purpose: Reverse hash→number lookup
└── **CRITICAL**: Used in block resolution
```

#### Interdependencies:
1. **CanonicalHeader** defines which hash is canonical for each block number
2. **HeaderNumber** enables reverse lookup from hash to number
3. **Header** stores the actual header data for all blocks
4. **State queries depend on canonical chain ordering from CanonicalHeader**

### 2. State Management Tables

#### Historical State Tables (DupCursor):
```
AccountChangeSet (2.5 GB)
├── Key: block_number(8) + address(20)
├── Value: account_data_before_change
├── Type: MDBX DupCursor table
└── **CRITICAL**: Strict key ordering required

StorageChangeSet (10.0 GB) 
├── Key: block_number(8) + address(20) + incarnation(8) + storage_key(32)
├── Value: storage_value_before_change  
├── Type: MDBX DupCursor table
└── **CRITICAL**: Strict key ordering required
```

#### Current State Table:
```
PlainState (4.4 GB)
├── Key: address(20) OR address(20) + incarnation(8) + storage_key(32)
├── Value: current_account_data OR current_storage_value
├── Purpose: **CURRENT** account/storage state
└── **ABSOLUTELY CRITICAL**: Required for all operations
```

### 3. Block Data Tables

#### Block Content:
```
BlockBody (1.8 GB)
├── Key: block_number(8) + block_hash(32)
├── Value: RLP-encoded block body (transactions + uncles)
├── Purpose: Transaction data for block reconstruction
└── **RISK**: May affect state reconstruction logic
```

## Critical Dependencies and Failure Modes

### 1. State Query Chain Analysis

#### Normal Flow:
```
eth_getTransactionCount("latest")
    ↓
GetLatestExecutedBlockNumber() → SyncStageProgress table
    ↓  
CreateStateReader(blockNumber=latest) → Uses current state
    ↓
CachedReader2.ReadAccountData() → Queries PlainState
    ↓
Returns current nonce (e.g., 38)
```

#### Corrupted Flow:
```
eth_getTransactionCount("latest") 
    ↓
GetLatestExecutedBlockNumber() → Returns corrupted value OR
    ↓
CreateStateReader() → Creates historical state reader OR  
    ↓
ReadAccountData() → Reads corrupted PlainState
    ↓  
Returns historical nonce (e.g., 8) ← **PROBLEM**
```

### 2. MDBX DupCursor Requirements

#### Critical Constraints:
- **Sorted insertion**: Keys must be inserted in ascending order
- **Atomic operations**: Related tables must be modified together
- **Referential integrity**: MDBX enforces consistency between related tables

#### Failure Scenarios:
1. **Key order violation**: `MDBX_EKEYMISMATCH` errors during insertion
2. **Partial table clearing**: Breaks referential integrity
3. **Inconsistent restoration**: Historical data overwrites current data

### 3. Observed Corruption Mechanism

#### What Happens During Batch Cleanup:
1. **Copy Phase**: Extract data for recent blocks from multiple tables
2. **Clear Phase**: `ClearBucket()` completely empties target tables
3. **Restore Phase**: Restore copied data to cleared tables

#### Critical Problem:
**The restore phase may be restoring historical account states instead of current states**

#### Evidence:
- PlainState contains nonce=8 (historical value)
- Expected nonce=38 (current value from ongoing transactions)
- **Conclusion**: Restoration overwrote current state with historical state

## Why Header/BlockBody Cleanup Causes Nonce Errors

### Direct Impact Chain:

#### 1. Block Context Corruption
```
Clear Header tables → Lose block ordering information → 
State queries use wrong block context → Read historical state instead of current
```

#### 2. State Reconstruction Issues  
```
Clear BlockBody → Lose transaction history → 
State reconstruction fails → Fallback to genesis/early state
```

#### 3. Referential Integrity Enforcement
```
Clear related tables → MDBX detects inconsistency → 
Automatically reverts dependent tables (including PlainState) → 
Account data reverts to historical values
```

### Root Cause Analysis

#### Most Likely Scenario:
**Our batch restore operations are restoring block-specific account states that overwrite the current global account states in PlainState.**

#### Mechanism:
1. We copy "recent block data" including historical account states
2. During restoration, historical account data gets written to PlainState
3. Current account nonces get overwritten with historical values
4. Result: Account queries return outdated nonce values

## Current Mitigation Strategy

### Conservative Approach: Disable Risky Operations

#### Moderate Mode (Safe):
- **DISABLED**: All batch-based pruning operations
- **DISABLED**: All DupCursor table operations
- **ENABLED**: Only table-by-table deletion with comprehensive protection
- **RESULT**: Minimal space savings (~8-12 GB) but guaranteed data integrity

#### Aggressive Mode (Advanced Users):
- **ENABLED**: All operations including batch pruning and DupCursor cleanup  
- **WARNING**: May cause PlainState corruption
- **RESULT**: Maximum space savings (~55-60 GB) but data corruption risk

### Rationale for Conservative Approach

#### Why We Temporarily Avoid Header/BlockBody Cleanup:

1. **Unidentified Corruption Pathway**: 
   - We know cleanup corrupts PlainState
   - We don't know the exact mechanism
   - Cannot predict which operations are safe

2. **Critical Data Protection**:
   - PlainState corruption breaks all account operations
   - Account nonce errors prevent transaction execution
   - Node becomes unusable for account-based operations

3. **Complex MDBX Behavior**:
   - DupCursor tables have strict consistency requirements
   - MDBX may enforce referential integrity automatically
   - Clearing related tables may trigger unexpected reverts

4. **Investigation Requirements**:
   - Need to understand exact corruption mechanism
   - Develop isolated testing methodology
   - Create safer cleanup strategies

### Trade-off Analysis

#### Benefits of Conservative Approach:
- ✅ **Data Integrity**: PlainState remains uncorrupted
- ✅ **Operational Stability**: Node functions correctly
- ✅ **Account Operations**: Nonce/balance queries work properly
- ✅ **Transaction Processing**: No nonce-related execution errors

#### Costs of Conservative Approach:
- ❌ **Reduced Space Savings**: ~45-50 GB less cleanup (from ~60 GB to ~10 GB)
- ❌ **Incomplete Optimization**: Large tables remain untouched
- ❌ **Manual Intervention**: May require alternative cleanup methods

## Future Investigation Required

### Priority Research Areas:

1. **Restore Logic Audit**:
   - Verify block data restoration doesn't affect PlainState
   - Ensure current state isolation from historical data
   - Implement state validation checkpoints

2. **MDBX Behavior Analysis**:
   - Study ClearBucket effects on related tables
   - Understand referential integrity mechanisms
   - Test isolated table operations

3. **Transaction Boundary Testing**:
   - Verify atomic operations for related table groups
   - Test rollback scenarios under various failure conditions
   - Ensure consistent commit/rollback behavior

## Conclusion

The current approach prioritizes **data integrity over space optimization** due to the critical nature of PlainState corruption.

**Key Decision**: Temporarily avoid Header/BlockBody cleanup because:
- **Unknown Risk**: Cannot predict corruption mechanisms
- **Critical Impact**: PlainState corruption breaks core functionality
- **Investigation Priority**: Need safer methods before enabling risky operations

This conservative strategy ensures node stability while we develop more sophisticated cleanup mechanisms that can safely handle the complex table interdependencies in X Layer Erigon.
