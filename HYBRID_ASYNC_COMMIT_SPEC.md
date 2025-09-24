# X Layer Erigon 异步提交技术规范

## 🎯 核心目标

**解决X Layer区块处理性能瓶颈，通过Stage异步落盘和SMT异步刷新双重优化，实现高性能区块链节点。**

### 性能目标
- **Stage提交时间**：从597ms降到~1ms（99.8%提升）
- **SMT提交时间**：从153ms降到~5ms（97%提升）
- **内存使用**：优化20-30%，从5.2GB降到3-4GB

## 📋 设计原则

1. **双重异步**：Stage异步落盘 + SMT异步刷新并行优化
2. **场景驱动**：根据区块处理数量和性能历史选择策略
3. **风险可控**：只在单块追赶时使用异步策略，多块同步时保持稳定
4. **数据一致性**：确保Stage间和SMT数据的完整性
5. **优雅退出**：双重异步机制的安全退出保障

## ⚙️ 配置参数

### 双重异步配置

#### Stage异步落盘配置
```yaml
# 启用Stage异步落盘（仅RPC节点）
zkevm:
  enable-async-commit: true  # 主开关
  
# Stage异步落盘参数
stage-async:
  max-cache-blocks: 20       # 最大缓存区块数
  flush-timeout: 10s         # 异步落盘超时
  max-concurrent-flush: 5    # 最大并发落盘数
  cache-memory-limit: 1GB    # 缓存内存限制
```

#### SMT异步刷新配置
```yaml
# SMT异步刷新参数（复用现有配置）
xlayer:
  enable-async-commit: true  # SMT异步刷新开关
  standalone-smt-database: true  # 独立SMT数据库
  
# SMT缓存参数
smt-cache:
  flush-period: 100          # 刷新周期（区块数）
  cache-channel-size: 1000   # 缓存通道大小
  max-cache-size: 2GB        # 最大缓存大小
  max-block-height-diff: 1000 # 最大区块高度差，超过则强制同步模式
```

#### 监控配置
```yaml
# 监控指标配置
metrics:
  async-commit-enabled: true
  stage-metrics-enabled: true
  smt-metrics-enabled: true
  
# 告警阈值
alerts:
  stage-flush-timeout: 10s
  smt-flush-timeout: 30s
  cache-memory-threshold: 80%
```

### 配置验证规则
- **互斥检查**：Sequencer节点不能启用Stage异步落盘
- **依赖检查**：SMT异步刷新需要`standalone-smt-database: true`
- **资源检查**：缓存内存限制不能超过系统可用内存的50%

## 🏗️ 系统架构

### 双重异步架构概览

```
X Layer异步提交系统
├── Stage异步落盘子系统
│   ├── HybridTxManager - 混合事务管理器
│   ├── PendingCache - 待落盘缓存
│   ├── HybridTx - 混合事务接口
│   └── CacheChain - 缓存链管理
└── SMT异步刷新子系统
    ├── SmtCache - SMT缓存管理
    ├── FlushSmtCache - 异步刷新机制
    ├── WaitGroup - 优雅退出控制
    └── EriCacheDb - 缓存数据库接口
```

## 🔄 Stage异步落盘子系统

### 核心组件

#### 1. HybridTxManager - 混合事务管理器
- **职责**：根据场景选择DirectDB或CacheMode，管理缓存链和异步落盘
- **核心字段**：数据库连接、缓存链、异步落盘通道、优雅退出控制
- **模式切换**：基于本地状态判断，避免远程查询延迟

#### 2. PendingCache - 待落盘缓存  
- **职责**：存储单个区块的所有数据变更，支持三种状态（Pending/Flushing/Completed）
- **数据结构**：table -> key -> value的三层映射，按区块高度组织
- **状态流转**：Pending -> Flushing -> Completed

#### 3. HybridTx - 混合事务接口
- **职责**：统一事务接口，根据模式路由到数据库事务或缓存
- **查询策略**：当前缓存 -> 历史缓存链 -> 数据库回退
- **透明切换**：Stage代码无需感知模式差异

#### 4. CacheChain - 缓存链管理
- **职责**：管理多个区块的缓存，支持历史数据查询和自动清理
- **查询范围**：最多向前查找10个区块，超出范围回退到数据库
- **自动清理**：超出最大缓存区块数时自动清理老缓存

### 模式切换原则（避免远程查询）

**核心原则：基于本地状态判断，避免网络延迟**

| 判断依据 | DirectDB模式 | CacheMode模式 | 原因 |
|---------|-------------|---------------|------|
| **历史提交时间** | >300ms | ≤300ms | 基于本地性能历史 |
| **同步状态** | 初始同步 | 正常追块 | 避免远程查询目标高度 |
| **本地队列** | >10个区块 | ≤10个区块 | 基于本地待处理数据 |
| **区块高度** | <1000 | ≥1000 | 初始同步判断 |
| **最后更新** | >1小时前 | 最近更新 | 节点活跃度判断 |

#### 判断策略（按优先级）：
1. **历史性能优先**：最近5次提交平均时间>300ms → DirectDB
2. **初始同步检测**：区块高度<1000 或 最后更新>1小时 → DirectDB  
3. **本地队列检查**：待处理区块>10个 → DirectDB
4. **默认策略**：正常追块情况 → CacheMode

#### 核心优势：
- ✅ **零网络延迟**：所有判断基于本地状态和历史数据
- ✅ **自适应学习**：根据实际性能自动调整策略
- ✅ **简单可靠**：避免复杂的远程状态查询

### StageLoopIteration执行流程

#### 执行步骤：
1. **模式判断**：调用HybridTxManager.DetermineMode()获取执行模式
2. **事务创建**：
   - DirectDB模式：创建标准数据库事务
   - CacheMode模式：创建混合缓存事务
3. **Stage执行**：正常执行所有Stage，无需修改现有逻辑
4. **提交处理**：
   - DirectDB模式：同步提交，承受597ms延迟
   - CacheMode模式：立即提交到缓存，启动异步落盘，立即返回

#### 关键设计：
- **向后兼容**：禁用异步提交时使用原有逻辑
- **透明切换**：Stage代码无需感知模式差异
- **性能优化**：CacheMode下提交时间从597ms降到~1ms

### 核心接口实现

#### 1. HybridTx接口适配
- **写操作**：DirectDB模式直接写数据库，CacheMode模式写入内存缓存
- **读操作**：CacheMode下三层查询策略（当前缓存 -> 历史缓存链 -> 数据库回退）
- **提交操作**：DirectDB同步提交，CacheMode立即提交到缓存并标记为Pending状态

#### 2. 异步落盘机制
- **状态管理**：Pending -> Flushing -> Completed 三状态流转
- **批量写入**：遍历缓存中所有table和key-value，批量写入数据库事务
- **错误重试**：失败时重置为Pending状态，支持后续重试
- **延迟清理**：完成后10秒延迟清理，确保数据安全

#### 3. 缓存链查询
- **查询范围**：从指定区块向前最多查找10个区块
- **状态过滤**：只查询非Completed状态的缓存
- **自动清理**：超出最大缓存区块数时自动清理老缓存

## 🔧 SMT异步刷新子系统

### 核心组件

#### 1. SmtCache - SMT缓存管理
- **职责**：管理SMT数据的内存缓存，支持多层缓存机制
- **数据结构**：SmtCacheSnapshotList、PreBatchSnapshotImage、CurrentBatchBlockSnapshotList
- **状态管理**：PushedHeap、ConfirmedHeap管理缓存生命周期

#### 2. FlushSmtCache - 异步刷新机制
- **职责**：将SMT缓存异步刷新到磁盘，支持批量和强制刷新
- **刷新策略**：基于区块高度差和grace参数决定刷新时机
- **通道机制**：SmtCacheDataCh用于异步数据传输

#### 3. WaitGroup - 优雅退出控制
- **职责**：确保所有SMT异步任务完成后才退出
- **机制**：FlushSmtCacheSignalInc() + FlushSmtCacheDone() + FlushSmtCacheWait()
- **安全保障**：防止系统退出时SMT数据丢失

#### 4. EriCacheDb - 缓存数据库接口
- **职责**：提供SMT缓存的数据库接口，支持RetriveAndCleanCache()
- **模式切换**：AC模式使用只读事务+缓存，同步模式使用读写事务
- **数据提取**：支持缓存数据的提取和清理

### 现状分析
- **性能瓶颈**：IntermediateHashes=153ms，占用内存5.2GB
- **已有基础**：sequencer模式已实现SMT异步提交（AC）机制
- **架构优势**：分离数据库（db + dbsmt）和SmtCache缓存系统

### 实施策略
1. **复用现有AC架构**：利用已验证的SMT异步提交机制
2. **模式自适应切换**：基于区块数量和性能历史自动选择
3. **缓存优先策略**：AC模式下使用只读SMT事务 + 内存缓存
4. **异步刷新机制**：后台异步将SMT缓存刷新到磁盘
5. **安全防护原则**：区块高度差>1000时强制使用同步模式，防止OOM

### SpawnZkIntermediateHashesStage执行流程

```
SMT异步刷新执行流程：
├── 模式判断：enableAsyncCommit && cfg.zk.XLayer.EnableAsyncCommit && 区块高度差≤1000
├── AC模式（安全条件满足）：
│   ├── 创建缓存事务：NewEriCacheDb(ctx, txsmt_readonly, tx)
│   ├── SMT计算：zkIncrementIntermediateHashes() -> 写入缓存
│   ├── 缓存提取：eridb.RetriveAndCleanCache()
│   ├── 设置缓存：s.SetSmtCache(blockHeight, blockCache)
│   └── 异步刷新：go s.FlushSmtCache() -> 立即返回
└── 同步模式（区块高度差>1000或其他条件不满足）：
    ├── 创建读写事务：NewEriDb(txsmt_rw, tx)
    ├── SMT计算：zkIncrementIntermediateHashes() -> 直接写数据库
    └── 同步提交：txsmt.Commit() -> 等待153ms
```

### 性能预期
- **提交时间**：从153ms降到~5ms（97%提升）
- **内存优化**：从5.2GB降到3-4GB（20-30%优化）
- **吞吐量**：Stage流水线不再被SMT阻塞

### 风险控制
- **区块高度差检查**：>1000个区块时强制使用同步模式，防止OOM
- **状态根校验**：异步刷新失败时重试机制
- **内存监控**：SMT缓存大小告警和强制同步fallback
- **数据一致性**：利用现有SmtCache的多层缓存机制
- **安全回退**：检测到内存压力时自动切换到同步模式

## 📊 性能指标与监控

### 预期性能对比

| 场景 | 模式 | Stage提交时间 | SMT提交时间 | 数据一致性 | 内存使用 |
|------|------|------------|------------|------------|----------|
| 初次同步（>10区块） | DirectDB | 597ms | 153ms | 完美 | 低 |
| 正常追块（1区块） | 异步模式 | ~1ms | ~5ms | 缓存链保证 | ~10MB/区块 |
| **总体提升** | **双重异步** | **99.8%↑** | **97%↑** | **保持一致** | **优化20-30%** |

### 监控指标体系

#### Stage异步落盘指标
- `stage_async_mode_total{mode="DirectDB|CacheMode"}` - 各模式使用次数
- `stage_async_flush_duration_seconds` - Stage异步落盘耗时
- `stage_cache_chain_size` - Stage缓存链长度
- `stage_async_flush_errors_total` - Stage异步落盘失败次数
- `stage_pending_cache_count` - Stage待落盘缓存数量

#### SMT异步刷新指标
- `smt_intermediate_hashes_duration_seconds` - SMT计算耗时
- `smt_cache_size_bytes` - SMT缓存占用内存
- `smt_async_flush_duration_seconds` - SMT异步刷新耗时
- `smt_async_flush_errors_total` - SMT异步刷新失败次数
- `smt_mode_total{mode="sync|async"}` - SMT模式使用统计
- `smt_pending_flush_count` - SMT待刷新任务数量
- `smt_block_height_diff` - 当前区块高度差
- `smt_oom_protection_triggered_total` - OOM保护触发次数

#### 系统整体指标
- `async_commit_total_duration_seconds` - 双重异步总耗时
- `async_commit_memory_usage_bytes` - 异步提交内存使用
- `async_commit_success_rate` - 异步提交成功率

## 🛑 双重异步优雅退出方案

### 核心原则：确保Stage和SMT双重数据零丢失

**双重退出流程**：停止接收新任务 → 等待双重异步完成 → 强制落盘所有数据 → 安全退出

### 三阶段退出策略

#### 阶段1：停止接收（0-1秒）
**Stage异步落盘**：
- 标记HybridTxManager关闭状态
- 拒绝新的异步提交请求
- 停止创建新的PendingCache

**SMT异步刷新**：
- 停止接收新的SMT缓存任务
- 标记SmtCache停止接收状态
- 关闭SmtCacheDataCh通道

#### 阶段2：等待完成（1-30秒）
**Stage异步落盘**：
- 等待所有进行中的Stage异步落盘任务完成
- 监控PendingCache状态从Flushing到Completed
- 统计剩余待落盘缓存数量

**SMT异步刷新**：
- 调用`FlushSmtCacheWait()`等待所有SMT异步刷新完成
- WaitGroup确保所有goroutine完成
- 监控SmtCacheDataCh通道清空

#### 阶段3：强制落盘（30-60秒）
**Stage异步落盘**：
- 并行强制落盘所有剩余的Stage Pending/Flushing缓存
- 最多5个并发强制落盘，避免磁盘I/O过载
- 记录强制落盘失败的缓存，不阻止退出

**SMT异步刷新**：
- 强制刷新SMT缓存：`FlushSmtCache(batchPush=true, grace=true)`
- 清理未完成的SMT批次：`ResetCurrentBatchCache()`
- 确保所有SMT数据写入磁盘

### 系统集成要点

#### Backend集成
- 在Ethereum.Stop()中集成双重异步退出机制
- Stage和SMT并行退出，总共60秒超时保护
- 信号处理：捕获SIGINT/SIGTERM触发优雅退出

#### 核心机制
- **原子状态管理**：使用atomic操作确保线程安全的关闭状态检查
- **并发控制**：Stage和SMT异步退出并行执行
- **超时保护**：总共60秒超时，确保系统能够及时退出
- **错误容忍**：部分落盘失败不阻止系统退出，记录错误供排查

#### 退出监控指标
- `stage_shutdown_duration_seconds` - Stage退出耗时
- `stage_pending_caches_at_shutdown` - Stage退出时待落盘缓存数量
- `stage_shutdown_flush_errors_total` - Stage退出时落盘错误计数
- `smt_shutdown_wait_duration_seconds` - SMT退出等待时间
- `smt_shutdown_pending_flushes` - SMT退出时待刷新任务数
- `async_commit_shutdown_success_rate` - 双重异步退出成功率

### 可选增强机制

#### 缓存持久化
- **Stage缓存持久化**：将PendingCache写入临时文件作为备份
- **SMT缓存持久化**：将SmtCache写入临时文件
- **恢复机制**：系统重启时可恢复未落盘的缓存数据
- **适用场景**：对数据丢失零容忍的生产环境

## 🔒 双重异步安全保障

### Stage异步落盘安全机制

#### 1. 数据一致性保障
- **缓存链机制**：确保Stage间数据连续性，支持历史数据查询
- **三层回退策略**：当前缓存 -> 历史缓存链 -> 数据库回退
- **状态管理**：跟踪每个PendingCache的落盘状态（Pending/Flushing/Completed）
- **原子操作**：确保缓存状态变更的线程安全

#### 2. 错误处理机制
- **异步落盘失败**：记录错误，保持缓存状态为Pending，支持自动重试
- **缓存溢出保护**：限制最大缓存区块数，自动清理老缓存
- **内存监控**：监控缓存内存使用，超限时强制同步fallback
- **系统崩溃恢复**：缓存数据丢失，但数据库状态保持一致

### SMT异步刷新安全机制

#### 1. 状态根一致性保障
- **多层缓存机制**：SmtCacheSnapshotList、PreBatchSnapshotImage确保数据完整性
- **状态根校验**：异步刷新失败时重新计算和校验状态根
- **缓存备份**：PreBatchSnapshotImage作为状态根计算的备份数据源
- **原子刷新**：FlushSmtCache操作的原子性保证

#### 2. 异步刷新保护
- **WaitGroup机制**：确保所有SMT异步任务完成后才继续
- **通道保护**：SmtCacheDataCh满载时返回错误，避免数据丢失
- **批次管理**：基于区块高度差控制刷新频率，避免频繁I/O
- **强制刷新**：grace参数支持强制刷新，确保关键时刻数据落盘

### 监控告警体系

#### Stage异步落盘告警
- **异步落盘延迟**：超过10秒告警
- **缓存链过长**：超过20个区块告警  
- **落盘失败率**：超过1%告警
- **内存使用率**：超过80%告警

#### SMT异步刷新告警
- **SMT刷新延迟**：超过30秒告警
- **SMT缓存积压**：超过1GB告警
- **状态根校验失败**：任何失败立即告警
- **WaitGroup超时**：超过60秒告警
- **区块高度差过大**：超过1000个区块告警
- **OOM保护触发**：任何触发立即告警

## ⚠️ 双重异步技术风险分析

### 🔴 高风险（可能导致数据丢失或系统崩溃）

#### Stage异步落盘高风险

##### 1. Stage异步落盘失败风险
- **风险**：CacheMode下Stage异步落盘失败，区块数据永久丢失
- **场景**：磁盘满、权限错误、MDBX损坏、网络中断
- **影响**：区块数据丢失，节点状态不一致，需要重新同步
- **缓解**：重试机制、监控告警、fallback到DirectDB同步模式

##### 2. Stage缓存链数据不一致
- **风险**：Stage读取到错误的历史数据，导致后续处理错误
- **场景**：缓存链中某个区块的数据损坏或丢失
- **影响**：后续区块处理错误，可能需要重新同步
- **缓解**：数据校验、状态回滚机制、缓存链完整性检查

#### SMT异步刷新高风险

##### 3. SMT状态根不一致风险
- **风险**：SMT异步刷新失败导致状态根计算错误
- **场景**：SMT缓存数据损坏、异步刷新中断、状态根校验失败
- **影响**：区块验证失败，状态根与实际SMT数据不匹配
- **缓解**：状态根校验机制、SMT缓存备份、重试和回滚机制

##### 4. SMT缓存数据丢失风险
- **风险**：SMT异步刷新进行中时系统崩溃，缓存数据丢失
- **场景**：系统崩溃、FlushSmtCacheWait()超时、通道阻塞
- **影响**：状态根计算完成但SMT数据未落盘，数据不一致
- **缓解**：WaitGroup机制、强制刷新、缓存持久化

#### 双重异步共同风险

##### 5. 内存溢出风险
- **风险**：Stage和SMT双重缓存占用过多内存，导致OOM
- **场景**：异步落盘速度跟不上处理速度，双重缓存积压
- **影响**：节点崩溃，需要重启，数据可能丢失
- **缓解**：内存限制、强制同步fallback、缓存大小监控

### 🟡 中风险（可能导致性能问题）

#### 6. 缓存链查询性能下降
- **风险**：Stage缓存链过长时，查询性能下降
- **场景**：大量未落盘区块，缓存链>20个区块
- **影响**：Stage执行变慢，反而降低性能
- **缓解**：限制缓存链长度、LRU淘汰、并行查询

#### 7. 双重异步锁竞争
- **风险**：Stage和SMT异步操作的锁竞争，影响并发性能
- **场景**：高频的Stage执行和SMT刷新操作
- **影响**：系统吞吐量下降，延迟增加
- **缓解**：细粒度锁、无锁数据结构、分离锁域

#### 8. 模式判断开销（已优化）
- **风险**：~~频繁远程查询导致模式判断延迟~~（已解决）
- **场景**：~~每次Stage执行都查询远程目标高度~~（已避免）
- **影响**：~~增加Stage执行延迟，抵消异步收益~~（已消除）
- **缓解**：✅ **基于本地状态判断，零网络延迟**

### 🟢 低风险（可能导致功能异常）

#### 9. 接口兼容性问题
- **风险**：HybridTx未完全实现kv.RwTx接口
- **场景**：Stage使用了未实现的接口方法
- **影响**：运行时panic，系统崩溃
- **缓解**：完整接口实现、单元测试覆盖、接口兼容性检查

#### 10. 监控数据不准确
- **风险**：双重异步性能指标统计错误，误导优化方向
- **场景**：并发统计、时间计算错误、指标重复计算
- **影响**：错误的性能判断，优化方向偏差
- **缓解**：指标验证、对比测试、独立监控验证

### 🔥 最大风险：双重数据丢失

#### Stage数据丢失场景
1. Stage认为提交成功（立即返回）
2. 异步落盘失败（磁盘问题、崩溃等）
3. PendingCache数据丢失，但Stage已继续
4. 导致区块链状态不一致

#### SMT数据丢失场景
1. SMT异步刷新进行中时系统崩溃
2. FlushSmtCacheWait()超时但仍有未完成任务
3. 状态根计算完成但SMT数据未落盘
4. 导致状态根与实际SMT数据不匹配

#### 综合风险缓解策略
- **双重重试机制**：Stage和SMT异步失败后自动重试
- **双重持久化缓存**：将Stage和SMT缓存写入临时文件
- **双重健康检查**：定期检查Stage和SMT落盘状态
- **紧急fallback**：检测到风险时切换到同步模式
- **双重优雅退出**：系统关闭时强制同步落盘所有缓存
- **WaitGroup保护**：确保所有异步任务完成后才退出
- **强制刷新机制**：关键时刻使用强制参数确保数据落盘

## 🚨 双重异步严格执行要求

### 开发原则
1. **不允许偏离此规范**：任何修改必须先更新此文档
2. **不允许过度设计**：严格按照最小可行方案实现双重异步
3. **不允许影响RPC**：RPC保持简单，不使用Stage和SMT缓存
4. **必须有双重监控**：Stage和SMT每个关键路径都要有指标
5. **必须有全面测试**：每个组件都要有单元测试和集成测试

### 实现要求
- **Stage异步落盘**：必须实现HybridTxManager、PendingCache、CacheChain
- **SMT异步刷新**：必须复用现有SmtCache、FlushSmtCache机制
- **双重优雅退出**：必须实现Stage和SMT并行安全退出
- **风险控制**：必须实现双重重试、监控、fallback机制

## 🗺️ 实施路线图

### 阶段1：SMT异步刷新优化（优先级：高）
**时间**：1-2周
**原因**：复用现有AC架构，风险低，收益大（153ms→5ms）

#### 1.1 SpawnZkIntermediateHashesStage改造
- 集成现有AC模式判断逻辑
- 适配EriCacheDb缓存接口
- 实现异步刷新触发机制

#### 1.2 监控和测试
- 添加SMT异步刷新监控指标
- 创建SMT性能对比测试
- 验证状态根一致性

### 阶段2：Stage异步落盘实现（优先级：中）
**时间**：3-4周
**原因**：需要新建架构，复杂度高，但收益最大（597ms→1ms）

#### 2.1 核心组件开发
- 实现HybridTxManager混合事务管理器
- 开发PendingCache待落盘缓存
- 构建CacheChain缓存链管理

#### 2.2 StageLoop集成
- 修改StageLoopIteration执行流程
- 实现模式切换逻辑
- 集成异步落盘机制

#### 2.3 HybridTx接口适配
- 实现kv.RwTx接口完整适配
- 开发三层查询策略
- 确保Stage代码透明切换

### 阶段3：双重异步集成（优先级：中）
**时间**：1-2周
**原因**：集成两个子系统，确保协调工作

#### 3.1 双重优雅退出
- 实现Stage和SMT并行退出机制
- 开发双重异步监控体系
- 集成Backend退出流程

#### 3.2 性能调优
- 优化双重异步并发性能
- 调整缓存大小和刷新策略
- 验证整体性能目标

### 阶段4：生产环境验证（优先级：高）
**时间**：2-3周
**原因**：确保生产环境稳定性和性能

#### 4.1 压力测试
- 高并发场景测试
- 长时间稳定性测试
- 异常场景恢复测试

#### 4.2 性能验证
- 验证Stage提交时间<1ms
- 验证SMT提交时间<5ms
- 验证内存优化20-30%

### 总体时间线：6-8周

## 🧪 测试策略

### 单元测试（UT）
#### Stage异步落盘UT
- **HybridTxManager测试**：模式切换逻辑、缓存管理
- **PendingCache测试**：状态流转、数据完整性
- **CacheChain测试**：查询性能、自动清理
- **HybridTx测试**：接口兼容性、查询回退

#### SMT异步刷新UT
- **FlushSmtCache测试**：异步刷新逻辑、错误处理
- **SmtCache测试**：多层缓存机制、数据一致性
- **WaitGroup测试**：优雅退出、超时处理

### 集成测试（E2E）
#### 性能对比测试
- **Stage性能测试**：597ms vs 1ms提交时间对比
- **SMT性能测试**：153ms vs 5ms提交时间对比
- **内存使用测试**：5.2GB vs 3-4GB内存对比

#### 稳定性测试
- **长时间运行测试**：24小时连续运行
- **异常恢复测试**：系统崩溃、网络中断恢复
- **并发压力测试**：高并发Stage执行

#### 数据一致性测试
- **Stage缓存链测试**：历史数据查询一致性
- **SMT状态根测试**：异步刷新后状态根校验
- **双重异步测试**：Stage和SMT同时异步的一致性

### 压力测试
- **高频区块处理**：模拟主网高峰期区块处理
- **内存压力测试**：缓存积压场景下的内存管理
- **磁盘I/O压力**：异步落盘高并发场景

## 🎯 双重异步成功标准

### 性能目标
1. **Stage性能提升**：
   - 提交时间：从597ms降到~1ms（99.8%提升）
   - 缓存链查询：<1ms平均延迟
   - 异步落盘成功率：>99%

2. **SMT性能提升**：
   - 提交时间：从153ms降到<5ms（97%提升）
   - 异步刷新成功率：>99%
   - 状态根校验通过率：100%

3. **系统整体优化**：
   - 内存使用：优化20-30%，从5.2GB降到3-4GB
   - 双重异步总延迟：<10ms
   - 系统吞吐量：提升80%以上

### 稳定性要求
- **数据一致性**：Stage缓存链和SMT多层缓存保证100%一致性
- **容错能力**：单点异步失败不影响系统整体运行
- **优雅退出**：60秒内完成双重异步安全退出，零数据丢失

### 兼容性标准
- **向后兼容**：不影响现有功能，支持动态模式切换
- **RPC隔离**：RPC查询不依赖Stage和SMT缓存
- **配置灵活**：支持独立开启/关闭Stage和SMT异步功能

### 可观测性要求
- **双重监控体系**：Stage和SMT独立监控指标
- **实时告警**：关键指标异常时立即告警
- **性能追踪**：端到端性能链路追踪
- **故障诊断**：完整的错误日志和调试信息

---

**此规范为X Layer异步提交最终技术标准，后续实现必须严格遵循，确保Stage异步落盘和SMT异步刷新双重优化目标的实现！**
