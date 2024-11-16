package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var minioClient *minio.Client

// CacheEntry represents a cached object with metadata
type CacheEntry struct {
	Data         []byte
	ContentType  string
	Size         int64
	LastModified time.Time
	ETag         string
	ExpiresAt    time.Time
}

// Cache configuration
const (
	defaultCacheDuration = 5 * time.Minute
	maxCacheSize         = 100 * 1024 * 1024 // 100MB
	cleanupInterval      = 1 * time.Minute
)

var (
	cache     sync.Map
	cacheSize int64
	cacheMux  sync.Mutex
)

func init() {
	endpoint := getEnv("S3_ENDPOINT", "localhost:9000")
	accessKeyID := getEnv("S3_ACCESS_KEY", "minioadmin")
	secretAccessKey := getEnv("S3_SECRET_KEY", "minioadmin")
	useSSL := getEnv("S3_USE_SSL", "false") == "true"

	var err error
	minioClient, err = minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize Minio client: %v", err))
	}

	// Start cache cleanup goroutine
	go cleanupCache()
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func cleanupCache() {
	ticker := time.NewTicker(cleanupInterval)
	for range ticker.C {
		now := time.Now()
		cache.Range(func(key, value interface{}) bool {
			entry := value.(*CacheEntry)
			if now.After(entry.ExpiresAt) {
				cache.Delete(key)
				cacheMux.Lock()
				cacheSize -= entry.Size
				cacheMux.Unlock()
			}
			return true
		})
	}
}

func getCacheKey(bucket, key string) string {
	return fmt.Sprintf("%s/%s", bucket, key)
}

func addToCache(cacheKey string, data []byte, contentType string, size int64, lastModified time.Time, etag string) {
	// Check if adding this item would exceed the max cache size
	cacheMux.Lock()
	defer cacheMux.Unlock()

	if int64(len(data)) > maxCacheSize {
		return // Don't cache files larger than max cache size
	}

	// Remove old entries if necessary
	if cacheSize+int64(len(data)) > maxCacheSize {
		// Remove oldest entries until we have enough space
		var keysToDelete []interface{}
		cache.Range(func(key, value interface{}) bool {
			keysToDelete = append(keysToDelete, key)
			entry := value.(*CacheEntry)
			cacheSize -= entry.Size
			return cacheSize+int64(len(data)) > maxCacheSize
		})
		for _, key := range keysToDelete {
			cache.Delete(key)
		}
	}

	entry := &CacheEntry{
		Data:         data,
		ContentType:  contentType,
		Size:         size,
		LastModified: lastModified,
		ETag:         etag,
		ExpiresAt:    time.Now().Add(defaultCacheDuration),
	}
	cache.Store(cacheKey, entry)
	cacheSize += size
}

func main() {
	r := gin.Default()
	r.GET("/objects/:bucket/*key", getObject)
	r.PUT("/objects/:bucket/*key", putObject)
	r.DELETE("/objects/:bucket/*key", deleteObject)
	r.HEAD("/objects/:bucket/*key", headObject)
	r.Run()
}

func getObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]
	cacheKey := getCacheKey(bucket, key)

	// Check cache first
	if entry, ok := cache.Load(cacheKey); ok {
		cacheEntry := entry.(*CacheEntry)
		if time.Now().Before(cacheEntry.ExpiresAt) {
			c.DataFromReader(
				http.StatusOK,
				cacheEntry.Size,
				cacheEntry.ContentType,
				io.NopCloser(io.NewSectionReader(bytes.NewReader(cacheEntry.Data), 0, cacheEntry.Size)),
				nil,
			)
			return
		}
		// Cache expired, remove it
		cache.Delete(cacheKey)
		cacheMux.Lock()
		cacheSize -= cacheEntry.Size
		cacheMux.Unlock()
	}

	// Cache miss, get from S3
	obj, err := minioClient.GetObject(context.Background(), bucket, key, minio.GetObjectOptions{})
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	info, err := obj.Stat()
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Read the entire object
	data, err := io.ReadAll(obj)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Cache the object
	addToCache(cacheKey, data, info.ContentType, info.Size, info.LastModified, info.ETag)

	// Send response
	c.DataFromReader(
		http.StatusOK,
		info.Size,
		info.ContentType,
		io.NopCloser(bytes.NewReader(data)),
		nil,
	)
}

func putObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]
	cacheKey := getCacheKey(bucket, key)

	// Remove from cache if exists
	if entry, ok := cache.LoadAndDelete(cacheKey); ok {
		cacheMux.Lock()
		cacheSize -= entry.(*CacheEntry).Size
		cacheMux.Unlock()
	}

	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := minioClient.PutObject(context.Background(), bucket, key, c.Request.Body, -1,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.Status(http.StatusOK)
}

func deleteObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]
	cacheKey := getCacheKey(bucket, key)

	// Remove from cache if exists
	if entry, ok := cache.LoadAndDelete(cacheKey); ok {
		cacheMux.Lock()
		cacheSize -= entry.(*CacheEntry).Size
		cacheMux.Unlock()
	}

	err := minioClient.RemoveObject(context.Background(), bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func headObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]
	cacheKey := getCacheKey(bucket, key)

	// Check cache first
	if entry, ok := cache.Load(cacheKey); ok {
		cacheEntry := entry.(*CacheEntry)
		if time.Now().Before(cacheEntry.ExpiresAt) {
			c.Header("Content-Type", cacheEntry.ContentType)
			c.Header("Content-Length", fmt.Sprintf("%d", cacheEntry.Size))
			c.Header("Last-Modified", cacheEntry.LastModified.UTC().Format(http.TimeFormat))
			c.Header("ETag", cacheEntry.ETag)
			c.Status(http.StatusOK)
			return
		}
		// Cache expired, remove it
		cache.Delete(cacheKey)
		cacheMux.Lock()
		cacheSize -= cacheEntry.Size
		cacheMux.Unlock()
	}

	// Cache miss, get from S3
	info, err := minioClient.StatObject(context.Background(), bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.Header("Content-Type", info.ContentType)
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size))
	c.Header("Last-Modified", info.LastModified.UTC().Format(http.TimeFormat))
	c.Header("ETag", info.ETag)
	c.Status(http.StatusOK)
}
