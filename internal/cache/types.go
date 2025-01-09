package cache

import "time"

// CacheEntry represents a cached object with metadata
type CacheEntry struct {
	Data           []byte
	CompressedData []byte
	ContentType    string
	Size           int64
	CompressedSize int64
	LastModified   time.Time
	ETag           string
	ExpiresAt      time.Time
	IsCompressed   bool
}

// Cache configuration
const (
	DefaultCacheDuration  = 5 * time.Minute
	MaxCacheSize          = 300 * 1024 * 1024 // 300MB
	CleanupInterval       = 1 * time.Minute
	MinSizeForCompression = 1 * 1024 * 1024 // Only compress files larger than 1MB
)
