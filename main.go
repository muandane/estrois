package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var minioClient *minio.Client

// CacheEntry represents a cached object with metadata
type CacheEntry struct {
	Data           []byte
	CompressedData []byte // Store compressed version if available
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
	defaultCacheDuration  = 5 * time.Minute
	maxCacheSize          = 100 * 1024 * 1024 // 100MB
	cleanupInterval       = 1 * time.Minute
	minSizeForCompression = 1024 // Only compress files larger than 1KB
)

var (
	cache     sync.Map
	cacheSize int64
	cacheMux  sync.Mutex
)

// Compression helper functions
func shouldCompress(contentType string, size int64) bool {
	compressibleTypes := map[string]bool{
		"text/":                  true,
		"application/json":       true,
		"application/javascript": true,
		"application/xml":        true,
		"application/yaml":       true,
		"image/svg":              true,
	}

	if size < minSizeForCompression {
		return false
	}

	for t := range compressibleTypes {
		if strings.HasPrefix(contentType, t) {
			return true
		}
	}
	return false
}

func compressData(data []byte) ([]byte, error) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)

	if _, err := gzipWriter.Write(data); err != nil {
		return nil, err
	}

	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}

	return compressed.Bytes(), nil
}

func decompressData(data []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	return io.ReadAll(gzipReader)
}

// Modified addToCache to handle compression
func addToCache(cacheKey string, data []byte, contentType string, size int64, lastModified time.Time, etag string) {
	cacheMux.Lock()
	defer cacheMux.Unlock()

	if int64(len(data)) > maxCacheSize {
		return
	}

	var compressedData []byte
	var isCompressed bool
	var finalSize int64 = size

	if shouldCompress(contentType, size) {
		var err error
		compressedData, err = compressData(data)
		if err == nil && int64(len(compressedData)) < size {
			isCompressed = true
			finalSize = int64(len(compressedData))
		}
	}

	// Remove old entries if necessary
	if cacheSize+finalSize > maxCacheSize {
		var keysToDelete []interface{}
		cache.Range(func(key, value interface{}) bool {
			keysToDelete = append(keysToDelete, key)
			entry := value.(*CacheEntry)
			cacheSize -= entry.Size
			return cacheSize+finalSize > maxCacheSize
		})
		for _, key := range keysToDelete {
			cache.Delete(key)
		}
	}

	entry := &CacheEntry{
		Data:           data,
		CompressedData: compressedData,
		ContentType:    contentType,
		Size:           size,
		CompressedSize: int64(len(compressedData)),
		LastModified:   lastModified,
		ETag:           etag,
		ExpiresAt:      time.Now().Add(defaultCacheDuration),
		IsCompressed:   isCompressed,
	}
	cache.Store(cacheKey, entry)
	cacheSize += finalSize
}

// Modified getObject to handle compression
func getObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]
	cacheKey := getCacheKey(bucket, key)
	acceptsGzip := strings.Contains(c.GetHeader("Accept-Encoding"), "gzip")

	// Check cache first
	if entry, ok := cache.Load(cacheKey); ok {
		cacheEntry := entry.(*CacheEntry)
		if time.Now().Before(cacheEntry.ExpiresAt) {
			// Serve compressed content if client accepts it and we have it
			if acceptsGzip && cacheEntry.IsCompressed && cacheEntry.CompressedData != nil {
				c.Header("Content-Encoding", "gzip")
				c.DataFromReader(
					http.StatusOK,
					cacheEntry.CompressedSize,
					cacheEntry.ContentType,
					io.NopCloser(bytes.NewReader(cacheEntry.CompressedData)),
					nil,
				)
				return
			}

			// Serve uncompressed content
			c.DataFromReader(
				http.StatusOK,
				cacheEntry.Size,
				cacheEntry.ContentType,
				io.NopCloser(bytes.NewReader(cacheEntry.Data)),
				nil,
			)
			return
		}
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

	// Check if we should serve compressed content
	if acceptsGzip && shouldCompress(info.ContentType, info.Size) {
		compressedData, err := compressData(data)
		if err == nil && int64(len(compressedData)) < info.Size {
			c.Header("Content-Encoding", "gzip")
			c.DataFromReader(
				http.StatusOK,
				int64(len(compressedData)),
				info.ContentType,
				io.NopCloser(bytes.NewReader(compressedData)),
				nil,
			)
			return
		}
	}

	// Serve uncompressed content
	c.DataFromReader(
		http.StatusOK,
		info.Size,
		info.ContentType,
		io.NopCloser(bytes.NewReader(data)),
		nil,
	)
}

// Modified putObject to handle compression
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

	// Read the body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Handle incoming compressed data
	if c.GetHeader("Content-Encoding") == "gzip" {
		body, err = decompressData(body)
		if err != nil {
			c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid gzip data"))
			return
		}
	}

	_, err = minioClient.PutObject(
		context.Background(),
		bucket,
		key,
		bytes.NewReader(body),
		int64(len(body)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.Status(http.StatusOK)
}

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

func main() {
	r := gin.Default()
	r.GET("/objects/:bucket/*key", getObject)
	r.PUT("/objects/:bucket/*key", putObject)
	r.DELETE("/objects/:bucket/*key", deleteObject)
	r.HEAD("/objects/:bucket/*key", headObject)
	r.Run()
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
