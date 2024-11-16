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
	MaxCacheSize          = 100 * 1024 * 1024 // 100MB
	CleanupInterval       = 1 * time.Minute
	MinSizeForCompression = 1024 // Only compress files larger than 1KB
)
