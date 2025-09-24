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

### 1. 模式判断逻辑
```go
func (mgr *HybridTxManager) DetermineMode(blockRange int) TxMode {
    if blockRange > 10 {
        mgr.logger.Info("Using DirectDB mode", "blockRange", blockRange)
        return DirectDB  // 大批量处理，承受597ms延迟
    }
    mgr.logger.Info("Using CacheMode", "blockRange", blockRange) 
    return CacheMode     // 单块追赶，使用异步缓存
}

func getCurrentBlockRange(db kv.RwDB) int {
    // 实现逻辑：计算当前区块到目标区块的距离
    currentBlock := getCurrentBlockHeight(db)
    targetBlock := getTargetBlockHeight() // 从网络或其他源获取
    return int(targetBlock - currentBlock)
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
    
    blockRange := getCurrentBlockRange(db)
    hybridMgr := GetOrCreateHybridTxManager(db, logger)
    mode := hybridMgr.DetermineMode(blockRange)
    
    // ====== 第二步：创建对应事务 ======
    switch mode {
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
    
    switch mode {
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

## 📝 实施计划

### 第一阶段：基础框架（1-2天）
1. 实现`HybridTxManager`基础结构
2. 实现模式判断逻辑
3. 集成到`StageLoopIteration`

### 第二阶段：DirectDB模式（0.5天）
1. 确保DirectDB模式与原有逻辑一致
2. 添加性能监控
3. 测试大批量同步场景

### 第三阶段：CacheMode模式（2-3天）
1. 实现`HybridTx`和`PendingCache`
2. 实现异步落盘机制
3. 测试单块追赶场景

### 第四阶段：缓存链和优化（1-2天）
1. 实现`CacheChain`
2. 添加监控和告警
3. 性能测试和调优

### 第五阶段：生产验证（1天）
1. 灰度测试
2. 性能对比
3. 稳定性验证

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
