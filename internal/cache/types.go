package cache

import (
	"time"

	"github.com/muandane/estrois/internal/config"
)

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
	CleanupInterval       = 1 * time.Minute
	MinSizeForCompression = 1 * 1024 * 1024 // Only compress files larger than 1MB
)

var MaxCacheSize = config.GetEnvWithDefaultInt("MAX_CACHE_SIZE", 300)
