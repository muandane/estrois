package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

type Manager struct {
	cache       *sync.Map
	currentSize int64
	maxSize     int64
	lastCleanup time.Time
	mu          sync.RWMutex
}

type Stats struct {
	CurrentSize      int64
	MaxSize          int64
	EntryCount       int
	LastCleanupTime  time.Time
	CompressionRatio float64
}

func (m *Manager) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var entryCount int
	var totalOriginalSize int64
	var totalCompressedSize int64

	m.cache.Range(func(_, value interface{}) bool {
		entryCount++
		entry := value.(*CacheEntry)
		if entry.IsCompressed {
			totalOriginalSize += int64(len(entry.Data))
			totalCompressedSize += int64(len(entry.CompressedData))
		}
		return true
	})

	var compressionRatio float64
	if totalOriginalSize > 0 {
		compressionRatio = float64(totalCompressedSize) / float64(totalOriginalSize)
	}

	return Stats{
		CurrentSize:      atomic.LoadInt64(&m.currentSize),
		MaxSize:          m.maxSize,
		EntryCount:       entryCount,
		LastCleanupTime:  m.lastCleanup,
		CompressionRatio: compressionRatio,
	}
}
