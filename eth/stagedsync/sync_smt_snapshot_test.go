package stagedsync

import (
	"github.com/stretchr/testify/assert"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/ledgerwatch/log/v3"
)

func TestSmtCacheList(t *testing.T) {
	// Initialize logger
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stdout, log.TerminalFormat())))

	// Helper function to create a test DeltaSmtCache with multiple entries
	newComplexDeltaSmtCache := func(entries map[string]map[string]string) map[string]map[string][]byte {
		result := make(map[string]map[string][]byte)
		for outerKey, innerMap := range entries {
			result[outerKey] = make(map[string][]byte)
			for innerKey, value := range innerMap {
				result[outerKey][innerKey] = []byte(value)
			}
		}
		return result
	}

	tests := []struct {
		name string
		run  func(t *testing.T, list *SmtCacheList)
	}{
		{
			name: "TestPushAndLength",
			run: func(t *testing.T, list *SmtCacheList) {
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1"},
					"key2": {"subkey2": "value2"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key3": {"subkey3": "value3"},
				}))

				if list.Length() != 2 {
					t.Errorf("Expected length 2, got %d", list.Length())
				}
			},
		},
		{
			name: "TestGetCacheShapshot",
			run: func(t *testing.T, list *SmtCacheList) {
				// Test case 1: Basic merge with unique keys
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1"},
					"key2": {"subkey2": "value2"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key3": {"subkey3": "value3"},
					"key4": {"subkey4": "value4"},
				}))
				list.Push(3, newComplexDeltaSmtCache(map[string]map[string]string{
					"key5": {"subkey5": "value5"},
				}))

				cache, found := list.getCacheShapshot(2)
				if !found {
					t.Errorf("Expected to find snapshot for BlockHeight 2")
				}
				expected := newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1"},
					"key2": {"subkey2": "value2"},
					"key3": {"subkey3": "value3"},
					"key4": {"subkey4": "value4"},
				})
				if !reflect.DeepEqual(cache, expected) {
					t.Errorf("Expected cache %v, got %v", expected, cache)
				}

				// Test case 2: Non-existent height
				_, found = list.getCacheShapshot(4)
				if found {
					t.Errorf("Expected not to find snapshot for BlockHeight 4")
				}

				// Test case 3: Complex data with overlapping keys (child priority)
				list = NewSmtCacheList()
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_parent", "subkey2": "value2_parent"},
					"key2": {"subkey3": "value3_parent"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_child", "subkey4": "value4_child"},
					"key3": {"subkey5": "value5_child"},
				}))
				list.Push(3, newComplexDeltaSmtCache(map[string]map[string]string{
					"key4": {"subkey6": "value6"},
				}))

				cache, found = list.getCacheShapshot(2)
				if !found {
					t.Errorf("Expected to find snapshot for BlockHeight 2")
				}
				expected = newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_child", "subkey2": "value2_parent", "subkey4": "value4_child"},
					"key2": {"subkey3": "value3_parent"},
					"key3": {"subkey5": "value5_child"},
				})
				if !reflect.DeepEqual(cache, expected) {
					t.Errorf("Expected cache with child priority from BlockHeight 2 %v, got %v", expected, cache)
				}
			},
		},
		{
			name: "TestGetAllCacheShapshot",
			run: func(t *testing.T, list *SmtCacheList) {
				// Test case 1: Basic merge with unique keys
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1"},
					"key2": {"subkey2": "value2"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key3": {"subkey3": "value3"},
					"key4": {"subkey4": "value4"},
				}))
				list.Push(3, newComplexDeltaSmtCache(map[string]map[string]string{
					"key5": {"subkey5": "value5"},
				}))

				blockHeight, cache, found := list.getAllCacheShapshot()
				if !found {
					t.Errorf("Expected to find snapshot for BlockHeight 2")
				}
				expected := newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1"},
					"key2": {"subkey2": "value2"},
					"key3": {"subkey3": "value3"},
					"key4": {"subkey4": "value4"},
					"key5": {"subkey5": "value5"},
				})
				if !reflect.DeepEqual(cache, expected) {
					t.Errorf("Expected cache %v, got %v", expected, cache)
				}
				assert.Equal(t, uint64(3), blockHeight, "Expect block height for current List is 3")

				// Test case 2: Non-existent height
				_, found = list.getCacheShapshot(4)
				if found {
					t.Errorf("Expected not to find snapshot for BlockHeight 4")
				}

				// Test case 3: Complex data with overlapping keys (child priority)
				list = NewSmtCacheList()
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_parent", "subkey2": "value2_parent"},
					"key2": {"subkey3": "value3_parent"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_child", "subkey4": "value4_child"},
					"key3": {"subkey5": "value5_child"},
				}))
				list.Push(3, newComplexDeltaSmtCache(map[string]map[string]string{
					"key4": {"subkey6": "value6"},
				}))

				_, cache, found = list.getAllCacheShapshot()
				if !found {
					t.Errorf("Expected to find snapshot for BlockHeight 2")
				}
				expected = newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_child", "subkey2": "value2_parent", "subkey4": "value4_child"},
					"key2": {"subkey3": "value3_parent"},
					"key3": {"subkey5": "value5_child"},
					"key4": {"subkey6": "value6"},
				})
				if !reflect.DeepEqual(cache, expected) {
					t.Errorf("Expected cache with child priority from BlockHeight 2 %v, got %v", expected, cache)
				}
			},
		},
		{
			name: "TestDelCache",
			run: func(t *testing.T, list *SmtCacheList) {
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key2": {"subkey2": "value2"},
				}))
				list.Push(3, newComplexDeltaSmtCache(map[string]map[string]string{
					"key3": {"subkey3": "value3"},
				}))

				success := list.delCache(3)
				if !success {
					t.Errorf("Expected successful deletion of BlockHeight 3")
				}
				if list.Length() != 0 {
					t.Errorf("Expected length 0 after deleting head, got %d", list.Length())
				}
				if list.head != nil {
					t.Errorf("Expected head to be nil after deleting head, got %v", list.head)
				}

				_, cache, found := list.getAllCacheShapshot()
				if found {
					t.Errorf("Expected to not find snapshot for empty List")
				}
				assert.Nil(t, cache, "Expect cache is nil")

				cache, found = list.getCacheShapshot(2)
				if found {
					t.Errorf("Expected to not find snapshot for BlockHeight 2")
				}
				assert.Nil(t, cache, "Expect cache is nil")

				list = NewSmtCacheList()
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key2": {"subkey2": "value2"},
				}))
				list.Push(3, newComplexDeltaSmtCache(map[string]map[string]string{
					"key3": {"subkey3": "value3"},
				}))

				success = list.delCache(1)
				if !success {
					t.Errorf("Expected successful deletion of BlockHeight 1")
				}
				if list.Length() != 2 {
					t.Errorf("Expected length 2 after deleting BlockHeight 1, got %d", list.Length())
				}
				if list.head == nil || list.head.BlcokHeight != 3 {
					t.Errorf("Expected head BlockHeight 3, got %v", list.head)
				}

				success = list.delCache(4)
				if success {
					t.Errorf("Expected failure when deleting non-existent BlockHeight 4")
				}
			},
		},
		{
			name: "TestTraverse",
			run: func(t *testing.T, list *SmtCacheList) {
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key2": {"subkey2": "value2"},
				}))
				list.Traverse()
			},
		},
		{
			name: "TestEmptyList",
			run: func(t *testing.T, list *SmtCacheList) {
				if list.Length() != 0 {
					t.Errorf("Expected length 0 for empty list, got %d", list.Length())
				}
				_, found := list.getCacheShapshot(1)
				if found {
					t.Errorf("Expected not to find snapshot in empty list")
				}
				success := list.delCache(1)
				if success {
					t.Errorf("Expected failure when deleting from empty list")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := NewSmtCacheList()
			tt.run(t, list)
		})
	}
}

func TestSmtCacheListAsyncOp(t *testing.T) {
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stdout, log.TerminalFormat())))

	newComplexDeltaSmtCache := func(entries map[string]map[string]string) map[string]map[string][]byte {
		result := make(map[string]map[string][]byte)
		for outerKey, innerMap := range entries {
			result[outerKey] = make(map[string][]byte)
			for innerKey, value := range innerMap {
				result[outerKey][innerKey] = []byte(value)
			}
		}
		return result
	}

	tests := []struct {
		name string
		run  func(t *testing.T, list *SmtCacheList)
	}{
		{
			name: "TestConcurrentDelCacheAndGetAllCacheShapshot",
			run: func(t *testing.T, list *SmtCacheList) {
				for i := 1; i <= 5; i++ {
					list.Push(uint64(i), newComplexDeltaSmtCache(map[string]map[string]string{
						"key" + string(rune(i)): {"subkey": "value" + string(rune(i))},
					}))
				}

				var wg sync.WaitGroup
				wg.Add(1)
				go func() {
					defer wg.Done()
					list.delCache(3)
				}()

				results := make(chan struct {
					block uint64
					cache map[string]map[string][]byte
					found bool
				}, 10)
				for i := 0; i < 5; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						block, cache, found := list.getAllCacheShapshot()
						results <- struct {
							block uint64
							cache map[string]map[string][]byte
							found bool
						}{block, cache, found}
					}()
				}

				wg.Wait()
				close(results)

				for res := range results {
					if !res.found {
						continue
					}
					if res.block != 5 && res.block != 4 {
						t.Errorf("Unexpected head block number %d", res.block)
					}
				}
			},
		},
		{
			name: "TestGetCacheShapshot",
			run: func(t *testing.T, list *SmtCacheList) {
				list.Push(1, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_parent", "subkey2": "value2_parent"},
				}))
				list.Push(2, newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_child"},
				}))

				cache, found := list.getCacheShapshot(2)
				if !found {
					t.Errorf("Expected to find snapshot for BlockHeight 2")
				}
				expected := newComplexDeltaSmtCache(map[string]map[string]string{
					"key1": {"subkey1": "value1_child", "subkey2": "value2_parent"},
				})
				if !reflect.DeepEqual(cache, expected) {
					t.Errorf("Expected cache %v, got %v", expected, cache)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := NewSmtCacheList()
			tt.run(t, list)
		})
	}
}
