# X Layer Erigon 混合异步提交技术规范

## 🎯 核心目标

**解决Stage提交597ms延迟问题，通过混合策略在性能和稳定性间取得平衡。**

## 📋 设计原则

1. **场景驱动**：根据区块处理数量选择策略
2. **风险可控**：只在单块追赶时使用异步策略
3. **简单有效**：避免过度设计，直接解决核心问题
4. **数据一致性**：确保Stage间数据连续性

## 🏗️ 系统架构

### 核心组件

#### 1. HybridTxManager - 混合事务管理器
```go
package stages

type HybridTxManager struct {
    db          kv.RwDB
    logger      log.Logger
    
    // 缓存管理
    pendingCache *PendingCache
    cacheChain   *CacheChain
    
    // 状态管理
    currentMode  TxMode
    asyncFlushCh chan *FlushTask
}

type TxMode int
const (
    DirectDB  TxMode = iota  // 直接数据库模式（>10个高度）
    CacheMode                // 缓存模式（1个高度）
)
```

#### 2. PendingCache - 待落盘缓存
```go
type PendingCache struct {
    mu          sync.RWMutex
    data        map[string]map[string][]byte // table -> key -> value
    blockHeight uint64
    timestamp   time.Time
    status      CacheStatus
}

type CacheStatus int
const (
    Pending   CacheStatus = iota // 等待落盘
    Flushing                     // 正在落盘
    Completed                    // 已完成
)
```

#### 3. HybridTx - 混合事务
```go
type HybridTx struct {
    mode     TxMode
    dbTx     kv.RwTx        // DirectDB模式使用
    cache    *PendingCache  // CacheMode模式使用
    mgr      *HybridTxManager
    db       kv.RoDB        // 用于CacheMode的读取回退
}
```

#### 4. CacheChain - 缓存链管理
```go
type CacheChain struct {
    mu     sync.RWMutex
    caches map[uint64]*PendingCache // blockHeight -> cache
    
    // 清理策略
    maxCacheBlocks int // 最大缓存区块数
    cleanupTicker  *time.Ticker
}
```

## 🔄 执行流程

### 0. 模式切换原则（避免远程查询）

**核心原则：基于本地状态判断，避免网络延迟**

| 判断依据 | DirectDB模式 | CacheMode模式 | 原因 |
|---------|-------------|---------------|------|
| **历史提交时间** | >300ms | ≤300ms | 基于本地性能历史 |
| **同步状态** | 初始同步 | 正常追块 | 避免远程查询目标高度 |
| **本地队列** | >10个区块 | ≤10个区块 | 基于本地待处理数据 |
| **区块高度** | <1000 | ≥1000 | 初始同步判断 |
| **最后更新** | >1小时前 | 最近更新 | 节点活跃度判断 |

**优势**：
- ✅ **零网络延迟**：所有判断基于本地状态
- ✅ **自适应**：根据历史性能自动调整
- ✅ **简单可靠**：避免复杂的远程状态查询

### 1. 模式判断逻辑（优化版）
```go
type ModeDecision struct {
    mode        TxMode
    reason      string
    blockRange  int
    lastCommit  time.Duration
}

func (mgr *HybridTxManager) DetermineMode(ctx context.Context) ModeDecision {
    // ====== 策略1: 基于历史提交时间 ======
    lastCommitTime := mgr.getLastCommitTime()
    if lastCommitTime > 300*time.Millisecond {
        // 上次提交超过300ms，说明数据量大，继续用DirectDB
        return ModeDecision{
            mode:       DirectDB,
            reason:     "last_commit_slow",
            lastCommit: lastCommitTime,
        }
    }
    
    // ====== 策略2: 基于本地状态判断 ======
    localInfo := mgr.getLocalBlockInfo()
    
    // 检查是否在初始同步阶段
    if mgr.isInitialSync(localInfo) {
        return ModeDecision{
            mode:   DirectDB,
            reason: "initial_sync",
        }
    }
    
    // 检查是否有大量待处理区块（基于本地队列）
    pendingBlocks := mgr.getPendingBlocksCount()
    if pendingBlocks > 10 {
        return ModeDecision{
            mode:       DirectDB,
            reason:     "large_batch_local",
            blockRange: pendingBlocks,
        }
    }
    
    // ====== 策略3: 默认使用CacheMode ======
    // 正常追块情况，优先使用异步模式
    return ModeDecision{
        mode:   CacheMode,
        reason: "normal_catchup",
    }
}

// 本地状态检查，避免远程查询
func (mgr *HybridTxManager) getLocalBlockInfo() LocalBlockInfo {
    return LocalBlockInfo{
        currentHeight:    mgr.getCurrentLocalHeight(),
        lastUpdateTime:   mgr.getLastBlockTime(),
        syncStatus:       mgr.getSyncStatus(),
        pendingTxCount:   mgr.getPendingTxCount(),
    }
}

func (mgr *HybridTxManager) isInitialSync(info LocalBlockInfo) bool {
    // 判断是否在初始同步：
    // 1. 当前高度很低（< 1000）
    // 2. 最后更新时间很久（> 1小时前）
    // 3. 同步状态为"syncing"
    if info.currentHeight < 1000 {
        return true
    }
    if time.Since(info.lastUpdateTime) > time.Hour {
        return true
    }
    return info.syncStatus == "syncing"
}

func (mgr *HybridTxManager) getPendingBlocksCount() int {
    // 基于本地队列或内存池判断待处理区块数
    // 避免远程网络查询
    return mgr.blockQueue.Size() + mgr.txPool.PendingCount()/1000
}

func (mgr *HybridTxManager) getLastCommitTime() time.Duration {
    // 从历史记录中获取最近几次提交的平均时间
    return mgr.commitHistory.GetAverageTime(5) // 最近5次平均
}
```

### 2. StageLoopIteration修改
```go
func StageLoopIteration(ctx context.Context, db kv.RwDB, txc wrap.TxContainer, 
    sync *stagedsync.Sync, initialCycle bool, logger log.Logger, 
    blockReader services.FullBlockReader, hook *Hook, 
    forcePartialCommit bool, enableAsyncCommit bool) (err error) {
    
    // ====== 第一步：模式判断 ======
    if !enableAsyncCommit {
        // 禁用异步提交，使用原有逻辑
        return originalStageLoopIteration(ctx, db, txc, sync, initialCycle, 
            logger, blockReader, hook, forcePartialCommit)
    }
    
    hybridMgr := GetOrCreateHybridTxManager(db, logger)
    decision := hybridMgr.DetermineMode(ctx)
    
    logger.Info("Mode decision made", 
        "mode", decision.mode, 
        "reason", decision.reason,
        "lastCommit", decision.lastCommit,
        "blockRange", decision.blockRange)
    
    // ====== 第二步：创建对应事务 ======
    switch decision.mode {
    case DirectDB:
        // 大批量：使用原有的数据库事务
        txc.Tx, err = db.BeginRwNosync(ctx)
        if err != nil {
            return err
        }
        defer txc.Tx.Rollback()
        
    case CacheMode:
        // 单块：使用混合缓存事务
        currentBlock := getCurrentBlockHeight(db)
        hybridTx, err := hybridMgr.BeginCacheTx(ctx, currentBlock+1)
        if err != nil {
            return err
        }
        txc.Tx = hybridTx
        defer hybridTx.Rollback()
    }
    
    // ====== 第三步：正常执行Stage ======
    _, err = sync.Run(db, txc, initialCycle)
    if err != nil {
        return err
    }
    
    // ====== 第四步：提交处理 ======
    commitStart := time.Now()
    
    switch decision.mode {
    case DirectDB:
        // 直接提交，承受597ms延迟
        err = txc.Tx.Commit()
        commitTime := time.Since(commitStart)
        logger.Info("DirectDB commit completed", "commitTime", commitTime)
        return err
        
    case CacheMode:
        // 立即返回，异步落盘
        err = hybridTx.CommitToCache()
        if err != nil {
            return err
        }
        
        // 启动异步落盘
        go hybridMgr.AsyncFlushToDatabase(ctx, currentBlock+1)
        
        commitTime := time.Since(commitStart)
        logger.Info("CacheMode commit completed", "commitTime", commitTime)
        return nil // 立即返回！
    }
}
```

## 🔧 核心接口实现

### 1. HybridTx接口实现
```go
func (tx *HybridTx) Put(table string, k, v []byte) error {
    switch tx.mode {
    case DirectDB:
        return tx.dbTx.Put(table, k, v)
    case CacheMode:
        return tx.cache.Put(table, k, v)
    }
    return fmt.Errorf("unknown tx mode: %d", tx.mode)
}

func (tx *HybridTx) GetOne(table string, k []byte) ([]byte, error) {
    switch tx.mode {
    case DirectDB:
        return tx.dbTx.GetOne(table, k)
    case CacheMode:
        // 缓存链查询：当前缓存 -> 历史缓存 -> 数据库
        if val := tx.cache.Get(table, k); val != nil {
            return val, nil
        }
        
        // 查询历史缓存
        if val := tx.mgr.cacheChain.GetValue(table, k, tx.cache.blockHeight-1); val != nil {
            return val, nil
        }
        
        // 最后回退到数据库
        return tx.db.View(ctx, func(dbTx kv.Tx) error {
            return dbTx.GetOne(table, k)
        })
    }
    return nil, fmt.Errorf("unknown tx mode: %d", tx.mode)
}

func (tx *HybridTx) Commit() error {
    switch tx.mode {
    case DirectDB:
        return tx.dbTx.Commit()
    case CacheMode:
        return tx.CommitToCache()
    }
    return fmt.Errorf("unknown tx mode: %d", tx.mode)
}

func (tx *HybridTx) CommitToCache() error {
    tx.cache.status = Pending
    tx.cache.timestamp = time.Now()
    tx.mgr.cacheChain.AddCache(tx.cache.blockHeight, tx.cache)
    return nil
}
```

### 2. 异步落盘实现
```go
type FlushTask struct {
    blockHeight uint64
    cache       *PendingCache
    callback    func(error)
}

func (mgr *HybridTxManager) AsyncFlushToDatabase(ctx context.Context, blockHeight uint64) {
    cache := mgr.cacheChain.GetCache(blockHeight)
    if cache == nil {
        mgr.logger.Error("Cache not found for async flush", "blockHeight", blockHeight)
        return
    }
    
    // 更新状态
    cache.status = Flushing
    
    // 创建数据库事务
    dbTx, err := mgr.db.BeginRw(ctx)
    if err != nil {
        mgr.logger.Error("Failed to begin database transaction for flush", 
            "blockHeight", blockHeight, "error", err)
        cache.status = Pending // 重置状态，可以重试
        return
    }
    defer dbTx.Rollback()
    
    // 批量写入数据
    totalItems := 0
    for table, tableData := range cache.data {
        for key, value := range tableData {
            if err := dbTx.Put(table, []byte(key), value); err != nil {
                mgr.logger.Error("Failed to put data during flush", 
                    "table", table, "key", key, "error", err)
                cache.status = Pending
                return
            }
            totalItems++
        }
    }
    
    // 提交到数据库
    flushStart := time.Now()
    if err := dbTx.Commit(); err != nil {
        mgr.logger.Error("Failed to commit during async flush", 
            "blockHeight", blockHeight, "error", err)
        cache.status = Pending
        return
    }
    
    flushTime := time.Since(flushStart)
    cache.status = Completed
    
    mgr.logger.Info("Async flush completed", 
        "blockHeight", blockHeight, 
        "flushTime", flushTime,
        "totalItems", totalItems)
    
    // 清理已完成的缓存（延迟清理，确保安全）
    time.AfterFunc(10*time.Second, func() {
        mgr.cacheChain.RemoveCache(blockHeight)
    })
}
```

### 3. 缓存链实现
```go
func (chain *CacheChain) GetValue(table string, key []byte, asOfBlock uint64) []byte {
    chain.mu.RLock()
    defer chain.mu.RUnlock()
    
    // 从指定区块开始向前查找
    for block := asOfBlock; block > 0 && block > asOfBlock-10; block-- {
        if cache, exists := chain.caches[block]; exists && cache.status != Completed {
            if val := cache.Get(table, string(key)); val != nil {
                return val
            }
        }
    }
    return nil
}

func (chain *CacheChain) AddCache(blockHeight uint64, cache *PendingCache) {
    chain.mu.Lock()
    defer chain.mu.Unlock()
    
    chain.caches[blockHeight] = cache
    
    // 清理过老的缓存
    if len(chain.caches) > chain.maxCacheBlocks {
        chain.cleanupOldCaches()
    }
}
```

## 📊 性能指标

### 预期性能
| 场景 | 模式 | 提交时间 | 数据一致性 | 内存使用 |
|------|------|----------|------------|----------|
| 初次同步（>10区块） | DirectDB | 597ms | 完美 | 低 |
| 正常追块（1区块） | CacheMode | ~1ms | 缓存链保证 | ~10MB/区块 |

### 监控指标
- `hybrid_tx_mode_total{mode="DirectDB|CacheMode"}` - 各模式使用次数
- `async_flush_duration_seconds` - 异步落盘耗时
- `cache_chain_size` - 缓存链长度
- `async_flush_errors_total` - 异步落盘失败次数

## 🔒 安全保障

### 1. 数据一致性
- **缓存链机制**：确保Stage间数据连续性
- **回退策略**：缓存 -> 历史缓存 -> 数据库
- **状态管理**：跟踪每个缓存的落盘状态

### 2. 错误处理
- **异步落盘失败**：记录错误，保持缓存状态为Pending，支持重试
- **缓存溢出**：限制最大缓存区块数，自动清理
- **系统崩溃**：缓存数据丢失，但数据库状态一致

### 3. 监控告警
- **异步落盘延迟**：超过10秒告警
- **缓存链过长**：超过20个区块告警
- **落盘失败率**：超过1%告警

## ⚠️ 技术风险分析

### 🔴 高风险（可能导致数据丢失或系统崩溃）

#### 1. 异步落盘失败风险
- **风险**：CacheMode下，异步落盘到数据库失败，数据永久丢失
- **场景**：磁盘满、权限错误、MDBX损坏
- **影响**：区块数据丢失，节点状态不一致
- **缓解**：重试机制、监控告警、fallback到同步模式

#### 2. 缓存链数据不一致
- **风险**：Stage读取到错误的历史数据，导致状态错误
- **场景**：缓存链中某个区块的数据损坏或丢失
- **影响**：后续区块处理错误，可能需要重新同步
- **缓解**：数据校验、状态回滚机制

#### 3. 内存溢出风险
- **风险**：大量未落盘缓存占用过多内存，导致OOM
- **场景**：异步落盘速度跟不上Stage处理速度
- **影响**：节点崩溃，需要重启
- **缓解**：内存限制、强制同步fallback

### 🟡 中风险（可能导致性能问题）

#### 4. 缓存链查询性能
- **风险**：缓存链过长时，查询性能下降
- **场景**：大量未落盘区块，缓存链>20个区块
- **影响**：Stage执行变慢，反而降低性能
- **缓解**：限制缓存链长度、LRU淘汰

#### 5. 锁竞争问题
- **风险**：缓存链的读写锁竞争，影响并发性能
- **场景**：高频的Stage执行和异步落盘操作
- **影响**：系统吞吐量下降
- **缓解**：细粒度锁、无锁数据结构

#### 6. 模式判断延迟（已优化）
- **风险**：~~频繁远程查询导致模式判断延迟~~（已解决）
- **场景**：~~每次Stage执行都查询远程目标高度~~（已避免）
- **影响**：~~增加Stage执行延迟，抵消异步收益~~（已消除）
- **缓解**：✅ **基于本地状态判断，零网络延迟**

### 🟢 低风险（可能导致功能异常）

#### 7. 接口兼容性
- **风险**：HybridTx未完全实现kv.RwTx接口
- **场景**：Stage使用了未实现的接口方法
- **影响**：运行时panic
- **缓解**：完整接口实现、单元测试覆盖

#### 8. 监控数据不准确
- **风险**：性能指标统计错误，误导优化方向
- **场景**：并发统计、时间计算错误
- **影响**：错误的性能判断
- **缓解**：指标验证、对比测试

### 🔥 最大风险：数据丢失

**最严重的风险是CacheMode下的数据丢失**：
1. Stage认为提交成功（立即返回）
2. 异步落盘失败（磁盘问题、崩溃等）
3. 缓存数据丢失，但Stage已继续
4. 导致区块链状态不一致

**风险缓解策略**：
- **重试机制**：异步落盘失败后自动重试
- **持久化缓存**：将缓存写入临时文件
- **健康检查**：定期检查落盘状态
- **紧急fallback**：检测到风险时切换到同步模式

## 🚨 严格执行要求

1. **不允许偏离此规范**：任何修改必须先更新此文档
2. **不允许过度设计**：严格按照最小可行方案实现
3. **不允许影响RPC**：RPC保持简单，不使用缓存
4. **必须有监控**：每个关键路径都要有指标
5. **必须有测试**：每个组件都要有单元测试

## 🎯 成功标准

1. **性能提升**：单块追赶场景提交时间从597ms降到<5ms
2. **稳定性**：异步落盘成功率>99%
3. **兼容性**：不影响现有功能
4. **可观测性**：完整的监控和告警

---

**此规范为最终版本，后续实现必须严格遵循，不得随意修改！**
