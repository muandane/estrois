package cache

import (
	"fmt"
	"sync"
	"time"
)

var (
	cache     sync.Map
	cacheSize int64
	cacheMux  sync.Mutex
)

func AddToCache(cacheKey string, data []byte, contentType string, size int64, lastModified time.Time, etag string) {
	cacheMux.Lock()
	defer cacheMux.Unlock()

	if int64(len(data)) > MaxCacheSize {
		return
	}

	var compressedData []byte
	var isCompressed bool
	var finalSize int64 = size

	if ShouldCompress(contentType, size) {
		var err error
		compressedData, err = CompressData(data)
		if err == nil && int64(len(compressedData)) < size {
			isCompressed = true
			finalSize = int64(len(compressedData))
		}
	}

	cleanupCacheIfNeeded(finalSize)

	entry := &CacheEntry{
		Data:           data,
		CompressedData: compressedData,
		ContentType:    contentType,
		Size:           size,
		CompressedSize: int64(len(compressedData)),
		LastModified:   lastModified,
		ETag:           etag,
		ExpiresAt:      time.Now().Add(DefaultCacheDuration),
		IsCompressed:   isCompressed,
	}
	cache.Store(cacheKey, entry)
	cacheSize += finalSize
}

// GetFromCache retrieves an object from the cache
func GetFromCache(cacheKey string) (*CacheEntry, bool) {
	if entry, ok := cache.Load(cacheKey); ok {
		cacheEntry := entry.(*CacheEntry)
		if time.Now().Before(cacheEntry.ExpiresAt) {
			return cacheEntry, true
		}
		DeleteFromCache(cacheKey)
	}
	return nil, false
}

// DeleteFromCache removes an object from the cache
func DeleteFromCache(cacheKey string) {
	if entry, ok := cache.LoadAndDelete(cacheKey); ok {
		cacheMux.Lock()
		cacheSize -= entry.(*CacheEntry).Size
		cacheMux.Unlock()
	}
}

// InitCache starts the cache cleanup routine
func InitCache() {
	go cleanupCacheRoutine()
}

func cleanupCacheIfNeeded(newSize int64) {
	if cacheSize+newSize > MaxCacheSize {
		var keysToDelete []interface{}
		cache.Range(func(key, value interface{}) bool {
			keysToDelete = append(keysToDelete, key)
			entry := value.(*CacheEntry)
			cacheSize -= entry.Size
			return cacheSize+newSize > MaxCacheSize
		})
		for _, key := range keysToDelete {
			cache.Delete(key)
		}
	}
}

func cleanupCacheRoutine() {
	ticker := time.NewTicker(CleanupInterval)
	for range ticker.C {
		now := time.Now()
		cache.Range(func(key, value interface{}) bool {
			entry := value.(*CacheEntry)
			if now.After(entry.ExpiresAt) {
				DeleteFromCache(key.(string))
			}
			return true
		})
	}
}

func GetCacheKey(bucket, key string) string {
	return fmt.Sprintf("%s/%s", bucket, key)
}
