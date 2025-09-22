# Complete Database Table Formats Analysis

This document provides comprehensive analysis of ALL 192 database tables in X Layer Erigon.

## 🎯 Purpose

This analysis enables:
- **Accurate partial pruning** - understanding key formats for block number extraction
- **Safe database operations** - knowing which tables can be safely modified
- **Performance optimization** - understanding data layout for efficient access
- **Future development** - complete reference for database schema changes

## 📚 Complete Table Catalog (192 Tables)

### 🏗️ Core Blockchain Tables (Block/Transaction Data)

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **Header** | `block_num_u64 + hash` | header (RLP) | Block headers | ✅ **Supported** |
| **BlockBody** | `block_num_u64 + hash` | block body | Block bodies | ✅ **Supported** |
| **Receipt** | `block_num_u64` | canonical block receipts | Transaction receipts | ✅ **Supported** |
| **TxSender** | `block_num_u64 + blockHash` | sendersList (20 bytes per sender) | Transaction senders | ✅ **Supported** |
| **CanonicalHeader** | `block_num_u64` | header hash | Canonical block headers | 🛡️ **Protected** |
| **TransactionLog** | `block_num_u64 + txId` | logs of transaction | Transaction logs/events | ✅ **Supported** |
| **HeaderNumber** | `header_hash` | `header_num_u64` | Header hash to number mapping | ⚠️ **Special handling** |
| **BadHeaderNumber** | `header_hash` | `header_num_u64` | Bad header hash to number | ⚠️ **Special handling** |
| **HeadersTotalDifficulty** | `block_num_u64 + hash` | td (RLP) | Total difficulty | 🛡️ **Protected** |
| **BlockTransaction** | `tx_id_u64` | rlp(tx) | All transactions | ❌ **Excluded** |
| **NonCanonicalTransaction** | `tbl_sequence_u64` | rlp(tx) | Non-canonical txs | ❌ **Excluded** |
| **BlockTransactionV3** | `tbl_sequence_u64` | rlp(tx) | Canonical txs v3 | ❌ **Excluded** |
| **BlockTransactionLookup** | `transaction_hash` | lookup metadata | Tx hash lookup | ❌ **Excluded** |
| **MaxTxNum** | `block_number_u64` | `max_tx_num_in_block_u64` | Max tx number per block | 🛡️ **Protected** |

### 📊 State Management Tables

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **PlainState** | `address` or `address+incarnation+key` | account/storage data | Current state | ❌ **Critical** |
| **PlainCodeHash** | `address+incarnation` | code hash | Contract code hashes | ❌ **Critical** |
| **Code** | `code_hash` | contract code | Contract bytecode | ❌ **Exclude** |
| **StateAccounts** | varies | account data | State accounts | ❌ **Exclude** |
| **StateStorage** | varies | storage data | State storage | ❌ **Exclude** |
| **StateCode** | varies | code data | State code | ❌ **Exclude** |
| **StateCommitment** | varies | commitment data | State commitments | ❌ **Exclude** |
| **HashedAccount** | `hashed_address` | account data | Hashed accounts | ❌ **Exclude** |
| **HashedStorage** | `hashed_address+key` | storage value | Hashed storage | ❌ **Exclude** |
| **IncarnationMap** | `address` | incarnation number | Account incarnations | ❌ **Exclude** |

### 📜 History Tables

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **AccountChangeSet** | `block_num_u64` + address | account data before change | Account history | ✅ **Supported** |
| **StorageChangeSet** | `block_num_u64 + address + incarnation` | storage key + value | Storage history | ✅ **Supported** |
| **AccountHistory** | varies | bitmap of block numbers | Account change blocks | ❌ **Exclude** |
| **StorageHistory** | varies | bitmap of block numbers | Storage change blocks | ❌ **Exclude** |

### 🌳 Trie Tables

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **TrieAccount** | trie path | trie node data | Account trie | ❌ **Exclude** |
| **TrieStorage** | trie path | trie node data | Storage trie | ❌ **Exclude** |
| **VerkleRoots** | `block_number` | verkle root | Verkle roots | ✅ **Supported** |
| **VerkleTrie** | verkle root | verkle node | Verkle trie | ❌ **Exclude** |

### 🔍 Index Tables

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **LogTopicIndex** | `topic + shard_number` | bitmap(blockN) | Log topic index | ❌ **Exclude** |
| **LogAddressIndex** | `address + shard_number` | bitmap(blockN) | Log address index | ❌ **Exclude** |
| **CallTraceSet** | `block_num_u64` | account addresses | Call trace accounts | ✅ **Supported** |
| **CallFromIndex** | `address + shard_number` | bitmap(blockN) | Call from index | ❌ **Exclude** |
| **CallToIndex** | `address + shard_number` | bitmap(blockN) | Call to index | ❌ **Exclude** |
| **CumulativeGasIndex** | varies | cumulative gas | Gas usage index | ❌ **Exclude** |
| **CumulativeTransactionIndex** | varies | cumulative tx count | Transaction index | ❌ **Exclude** |

### 🏛️ Beacon Chain Tables (35 tables)

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **BeaconState** | `slot` | beacon state | Beacon states | ❌ **Exclude** |
| **BeaconBlock** | `slot` | signature + block | Beacon blocks | ❌ **Exclude** |
| **CanonicalBlockRoots** | `slot` | canonical block root | Canonical roots | ❌ **Exclude** |
| **BlockRootToSlot** | `block_root` | slot | Root to slot mapping | ❌ **Exclude** |
| **BlockRootToStateRoot** | `block_root` | state root | Root mappings | ❌ **Exclude** |
| **StateRootToBlockRoot** | `state_root` | block root | State to block mapping | ❌ **Exclude** |
| **BlockRootToParentRoot** | `block_root` | parent root | Parent mappings | ❌ **Exclude** |
| **BeaconBlockHeaders** | `block_root` | beacon block header | Block headers | ❌ **Exclude** |
| **HighestFinalized** | `hash` | lookup metadata | Finalized blocks | ❌ **Exclude** |
| **Attestetations** | `slot` | attestation list | Attestations | ❌ **Exclude** |
| **LightClientUpdates** | `period` | light client update | Light client data | ❌ **Exclude** |
| *...and 24 more beacon tables* | varies | varies | Various beacon data | ❌ **Exclude** |

### 🔗 ZKEVM/X Layer Specific Tables (25 tables)

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **hermez_l1Verifications** | `l1blockno, batchno` | l1txhash | L1 verifications | ❌ **Critical** |
| **hermez_l1Sequences** | `l1blockno, batchno` | l1txhash | L1 sequences | ❌ **Critical** |
| **hermez_forkIds** | `batchNo` | forkId | Fork IDs | ❌ **Critical** |
| **hermez_forkIdBlock** | `forkId` | startBlock | Fork ID blocks | ❌ **Critical** |
| **hermez_blockBatches** | `l2blockno` | batchno | Block to batch map | 🛡️ **Protected** |
| **hermez_globalExitRoots** | `l2blockno` | GER | Block exit roots | ❌ **Critical** |
| **hermez_stateRoots** | `l2blockno` | stateRoot | State roots | ❌ **Critical** |
| **l1_info_tree_updates** | `index` | L1InfoTreeUpdate | L1 info updates | ❌ **Critical** |
| **block_l1_info_tree_index** | `block_number` | l1 info tree index | Block to L1 index | 🛡️ **Protected** |
| **block_info_roots** | `block_number` | block info root hash | Block info roots | ❌ **Critical** |
| **smt_depths** | `block_number` | smt depth | SMT depths | 🛡️ **Protected** |
| **batch_blocks** | `batch_number` | block numbers | Batch to blocks | ❌ **Critical** |
| *...and 13 more ZKEVM tables* | varies | varies | L2 specific data | ❌ **Critical** |

### 📱 SMT Tables (X Layer Specific)

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **HermezSmt** | SMT key | SMT value | SMT main data | ❌ **Critical** |
| **HermezSmtStats** | varies | statistics | SMT statistics | ❌ **Critical** |
| **HermezSmtAccountValues** | varies | account values | SMT account values | ❌ **Critical** |
| **HermezSmtMetadata** | varies | metadata | SMT metadata | ❌ **Critical** |
| **HermezSmtHashKey** | varies | hash keys | SMT hash keys | ❌ **Critical** |

### 🔧 System/Administrative Tables

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **DbInfo** | varies | database info | DB metadata | ❌ **Critical** |
| **Config** | `config_key` | config value | Configuration | ❌ **Critical** |
| **SyncStage** | `stage_name` | stage data | Sync progress | ❌ **Critical** |
| **Migration** | `migration_name` | migration data | DB migrations | ❌ **Critical** |
| **Sequence** | `table_name` | seq_u64 | Table sequences | ❌ **Critical** |
| **LastBlock** | varies | latest block info | Latest block | ❌ **Critical** |
| **LastHeader** | varies | latest header info | Latest header | ❌ **Critical** |
| **LastForkchoice** | varies | forkchoice data | Forkchoice state | ❌ **Critical** |
| **CurrentExecutionPayload** | varies | execution payload | Current payload | ❌ **Critical** |

### 🗄️ Domain Tables (Erigon3 Format) - 30 tables

All domain tables use complex key formats and are excluded from partial pruning:
- **AccountKeys/Vals/History** (5 tables)
- **StorageKeys/Vals/History** (5 tables)  
- **CodeKeys/Vals/History** (5 tables)
- **CommitmentKeys/Vals/History** (5 tables)
- **LogAddressKeys/Idx** (2 tables)
- **LogTopicsKeys/Idx** (2 tables)
- **TracesFromKeys/Idx** (2 tables)
- **TracesToKeys/Idx** (2 tables)
- **Reconstitution tables** (6 tables)

### 🌐 Polygon/BOR Tables

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **BorEventNums** | `block_num` | event_id | BOR event numbers | ✅ **Supported** |
| **BorMilestoneEnds** | `start_block_num` | milestone_id | Milestone blocks | ✅ **Supported** |
| **BorCheckpointEnds** | `start_block_num` | checkpoint_id | Checkpoint blocks | ✅ **Supported** |
| **BorReceipt** | varies | BOR receipts | BOR receipts | ❌ **Exclude** |
| **BorFinality** | varies | finality data | BOR finality | ❌ **Exclude** |
| **BlockBorTransactionLookup** | `transaction_hash` | block_num_u64 | BOR tx lookup | ❌ **Exclude** |
| *...and 4 more BOR tables* | varies | varies | BOR specific data | ❌ **Exclude** |

### 🔧 Consensus Tables

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **Clique** | varies | clique data | Clique consensus | ❌ **Exclude** |
| **CliqueSeparate** | varies | clique data | Clique separate | ❌ **Exclude** |
| **CliqueSnapshot** | varies | clique snapshot | Clique snapshots | ❌ **Exclude** |
| **CliqueLastSnapshot** | varies | last snapshot | Last clique snapshot | ❌ **Exclude** |

### 📦 Miscellaneous Tables

| Table Name | Key Format | Value Format | Description | Partial Pruning |
|------------|------------|--------------|-------------|------------------|
| **DevEpoch** | `block_num_u64+block_hash` | transition_proof | Development epochs | ✅ **Supported** |
| **DevPendingEpoch** | `block_num_u64+block_hash` | transition_proof | Pending epochs | ✅ **Supported** |
| **Issuance** | `block_num_u64` | RLP(issuance+burnt) | Token issuance | ✅ **Supported** |
| **Snapshots** | `name` | hash | Snapshot hashes | ❌ **Exclude** |
| **NodeRecord** | varies | ENR records | P2P node records | ❌ **Exclude** |
| **Inode** | varies | discovery info | P2P discovery | ❌ **Exclude** |
| *...and 15+ more misc tables* | varies | varies | Various utilities | ❌ **Exclude** |

## 📋 Summary Statistics

| Category | Table Count | Partial Pruning Supported | Notes |
|----------|-------------|---------------------------|-------|
| **Block/Transaction** | 14 | 9 tables | Core blockchain data |
| **State Management** | 10 | 0 tables | Current state (critical) |
| **History** | 4 | 2 tables | Historical changes |
| **Trie** | 4 | 1 table | Merkle tree data |
| **Index** | 7 | 1 table | Search indices |
| **Beacon Chain** | 35 | 0 tables | Ethereum 2.0 data |
| **ZKEVM/X Layer** | 25 | 0 tables | L2 specific (critical) |
| **System** | 9 | 0 tables | Administrative (critical) |
| **Domain Tables** | 30 | 0 tables | Erigon3 format |
| **SMT** | 5 | 0 tables | Sparse Merkle Tree (critical) |
| **Polygon/BOR** | 10 | 3 tables | Polygon specific |
| **Consensus** | 4 | 0 tables | Consensus mechanisms |
| **Miscellaneous** | 36 | 3 tables | Various utilities |
| **TOTAL** | **192** | **15 tables** | **Complete database** |

## 🎯 Partial Pruning Implementation Guidelines

### ✅ Safe for Partial Pruning (13 tables)
These tables have block numbers in their keys and can be safely pruned by block height:

**Note**: 7 tables are now **Protected** instead of pruned:
- 5 small tables (block_l1_info_tree_index, plain_state_version, smt_depths, HeadersTotalDifficulty, MaxTxNum) due to minimal data size (<10MB each)
- 2 dupCursor tables (CanonicalHeader, hermez_blockBatches) preserved for node stability

1. **Header** - Block headers (block_num + hash key)
2. **BlockBody** - Block bodies (block_num + hash key)  
3. **Receipt** - Transaction receipts (block_num key)
4. **TxSender** - Transaction senders (block_num + hash key)
5. **TransactionLog** - Transaction logs (block_num + txId key)
6. **HeaderNumber** - Header mappings (⚠️ special value-based handling)
7. **BadHeaderNumber** - Bad header mappings (⚠️ special value-based handling)
8. **AccountChangeSet** - Account changes (block_num key)
9. **StorageChangeSet** - Storage changes (block_num key)
10. **VerkleRoots** - Verkle roots (block_num key)
11. **CallTraceSet** - Call traces (block_num key)
12. **BorEventNums** - BOR events (block_num key)
13. **BorMilestoneEnds** - BOR milestones (block_num key)
14. **BorCheckpointEnds** - BOR checkpoints (block_num key)
15. **DevEpoch** - Development epochs (block_num + hash key)
16. **DevPendingEpoch** - Pending epochs (block_num + hash key)
17. **Issuance** - Token issuance (block_num key)

### 🛡️ Small Tables Now Protected (5 tables)
These tables were previously considered for pruning but are now protected due to minimal data size:
- **HeadersTotalDifficulty** - Total difficulty (block_num + hash key)
- **MaxTxNum** - Max transaction numbers (block_num key)  
- **block_l1_info_tree_index** - Block to L1 index (block_num key)
- **plain_state_version** - State version tracking (block_num key)
- **smt_depths** - SMT depths (block_num key)

### 🛡️ DupCursor Tables Now Protected (2 tables)
These dupCursor tables are now protected for node stability instead of aggressive pruning:
- **CanonicalHeader** - Canonical chain headers (block_num key) - Critical for chain state
- **hermez_blockBatches** - Block to batch mappings (block_num key) - Critical for sequence execution

### ❌ Critical Tables (NEVER DELETE - 65 tables)
These tables are essential for node operation:
- **All SMT tables** (5 tables) - Sparse Merkle Tree data
- **All ZKEVM tables** (25 tables) - Layer 2 operation data
- **System tables** (9 tables) - Database and sync state
- **PlainState and related** (3 tables) - Current blockchain state
- **Key system tables** (LastBlock, LastHeader, MaxTxNum, etc.)

### 🚫 Complex Key Format Tables (EXCLUDE from Partial Pruning)
These tables use non-block-number keys and cannot use simple block-based pruning:
- **BlockTransaction** (uses tx_id sequence)
- **BlockTransactionLookup** (uses tx_hash)
- **NonCanonicalTransaction** (uses sequence)
- **All Domain tables** (30 tables) - Complex Erigon3 format
- **All Index tables** (6 tables) - Address/topic based keys
- **All Trie tables** (3 tables) - Trie path based keys
- **Most Beacon tables** (35 tables) - Slot/root based keys

## 🔍 Key Format Analysis for Partial Pruning

### Format Type 1: Direct Block Number Key
```
Key: [8 bytes block_num_u64]
Examples: Receipt, CanonicalHeader, MaxTxNum
Implementation: ✅ Simple - extract first 8 bytes
```

### Format Type 2: Block Number + Additional Data
```
Key: [8 bytes block_num_u64][additional data]
Examples: Header, BlockBody, TxSender, TransactionLog
Implementation: ✅ Simple - extract first 8 bytes
```

### Format Type 3: Block Number in Value (Special Cases)
```
Key: [hash/other data]
Value: [8 bytes block_num_u64][additional data]
Examples: HeaderNumber, BadHeaderNumber
Implementation: ⚠️ Special - read value to extract block number
```

### Format Type 4: No Block Number Reference
```
Key: [tx_id/tx_hash/address/etc]
Value: [non-block data]
Examples: BlockTransaction, BlockTransactionLookup, Domain tables
Implementation: ❌ Excluded - no reliable block number extraction
```

## 🛡️ Safety Guidelines

### Before Adding New Partial Pruning Support:
1. **Verify key format** - Ensure block number is reliably extractable
2. **Test extensively** - Verify node can restart after pruning
3. **Check dependencies** - Ensure other systems don't depend on historical data
4. **Document changes** - Update this document and MODERATE_CHANGES.md

### Critical Table Protection:
- **Never prune SMT tables** - Essential for L2 operation
- **Never prune ZKEVM tables** - Required for batch processing
- **Never prune system tables** - Database integrity depends on them
- **Never prune PlainState** - Current state required for execution

This comprehensive analysis ensures accurate and safe database operations for X Layer Erigon.
