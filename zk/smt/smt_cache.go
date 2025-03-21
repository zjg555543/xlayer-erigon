package smt

import "errors"

type SmtCache struct {
	SmtCacheDataCh        chan SmtCacheToWrite
	FinishedBlockHeightCh chan uint64
	SmtCacheSnapshotList  *SmtCacheList

	LongLivedSmtCache map[string]map[string][]byte
	LastCleanHeight   uint64
}

type SmtCacheToWrite struct {
	SmtCacheData   map[string]map[string][]byte
	MaxBlockHeight uint64
}

func CreateNewSmtCache() *SmtCache {
	return &SmtCache{
		SmtCacheDataCh:        make(chan SmtCacheToWrite, 1),
		FinishedBlockHeightCh: make(chan uint64, 1000),
		SmtCacheSnapshotList:  NewSmtCacheList(),
		LongLivedSmtCache:     make(map[string]map[string][]byte),
		LastCleanHeight:       uint64(0),
	}
}

// TruncateSmtCacheList delete all the block snapshot cache that is lower than the target blockHeight
func (cache *SmtCache) TruncateSmtCacheList(blockHeight uint64) {
	cache.SmtCacheSnapshotList.delCache(blockHeight)
}

func (cache *SmtCache) GetSmtCache() map[string]map[string][]byte {
	return cache.LongLivedSmtCache
}

func (cache *SmtCache) GetSmtSnapshotCache(blockNumber uint64) map[string]map[string][]byte {
	cacheData, _ := cache.SmtCacheSnapshotList.getCacheShapshot(blockNumber, false)
	if cacheData == nil {
		cacheData = map[string]map[string][]byte{}
	}

	return cacheData
}

func (cache *SmtCache) SetSmtCache(blockNumber uint64, longLivedCache, blockCache map[string]map[string][]byte) {
	if cache.SmtCacheSnapshotList == nil {
		cache.SmtCacheSnapshotList = NewSmtCacheList()
	}

	cache.SmtCacheSnapshotList.Push(blockNumber, blockCache)

	if blockNumber-cache.LastCleanHeight > 10000 {
		_, deltaSmtCache, _ := cache.SmtCacheSnapshotList.getAllCacheShapshot(true)
		if deltaSmtCache == nil {
			deltaSmtCache = map[string]map[string][]byte{}
		}

		// Reset LongLivedSmtCache, prevent excessive memory usage.
		cache.LongLivedSmtCache = deltaSmtCache
		cache.LastCleanHeight = blockNumber
	} else {
		cache.LongLivedSmtCache = longLivedCache
	}
}

func (cache *SmtCache) CachedBlockLen() int {
	return cache.SmtCacheSnapshotList.Length()
}

func (cache *SmtCache) FlushSmtCache() error {
	blockHeight, deltaSmtCache, _ := cache.SmtCacheSnapshotList.getAllCacheShapshot(false)
	if deltaSmtCache == nil {
		return nil
	}

	cacheData := SmtCacheToWrite{
		deltaSmtCache,
		blockHeight,
	}

	select {
	case cache.SmtCacheDataCh <- cacheData:
		return nil
	default:
		return errors.New("failed to flush: channel is full or no receiver")
	}
}
