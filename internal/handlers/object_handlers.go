package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"

	"github.com/muandane/estrois/internal/cache"
	"github.com/muandane/estrois/internal/storage"
)

// GetObject handles GET requests for objects
func GetObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]
	cacheKey := cache.GetCacheKey(bucket, key)
	acceptsGzip := strings.Contains(c.GetHeader("Accept-Encoding"), "gzip")

	// Check cache
	if entry, found := cache.GetFromCache(cacheKey); found {
		serveFromCache(c, entry, acceptsGzip)
		return
	}

	// Cache miss, get from storage
	serveFromStorage(c, bucket, key, cacheKey, acceptsGzip)
}

func PutObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]
	cacheKey := cache.GetCacheKey(bucket, key)
	cache.DeleteFromCache(cacheKey)

	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Read body with a size limit to prevent memory exhaustion
	maxSize := int64(50 * 1024 * 1024) // 50MB limit
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSize))
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("failed to read request body: %w", err))
		return
	}

	// Handle gzip decompression if needed
	if c.GetHeader("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("invalid gzip data: %v", err),
			})
			return
		}
		defer gz.Close()

		decompressed, err := io.ReadAll(gz)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("failed to decompress data: %v", err),
			})
			return
		}
		body = decompressed
	}

	// Upload to storage
	_, err = storage.GetMinioClient().PutObject(
		context.Background(),
		bucket,
		key,
		bytes.NewReader(body),
		int64(len(body)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to store object: %v", err),
		})
		return
	}

	c.Status(http.StatusOK)
}

// DeleteObject handles DELETE requests for objects
func DeleteObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]

	// Remove from cache
	cache.DeleteFromCache(cache.GetCacheKey(bucket, key))

	// Delete from storage
	err := storage.GetMinioClient().RemoveObject(context.Background(), bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// HeadObject handles HEAD requests for objects
func HeadObject(c *gin.Context) {
	bucket := c.Param("bucket")
	key := c.Param("key")[1:]
	cacheKey := cache.GetCacheKey(bucket, key)

	// Check cache
	if entry, found := cache.GetFromCache(cacheKey); found {
		setObjectHeaders(c, entry.ContentType, entry.Size, entry.LastModified, entry.ETag)
		c.Status(http.StatusOK)
		return
	}

	// Cache miss, check storage
	info, err := storage.GetMinioClient().StatObject(context.Background(), bucket, key, minio.StatObjectOptions{})
	if err != nil {
		handleStorageError(c, err)
		return
	}

	setObjectHeaders(c, info.ContentType, info.Size, info.LastModified, info.ETag)
	c.Status(http.StatusOK)
}

// HealthCheck handles health check requests
func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

// Helper functions

func serveFromCache(c *gin.Context, entry *cache.CacheEntry, acceptsGzip bool) {
	if acceptsGzip && entry.IsCompressed && entry.CompressedData != nil {
		c.Header("Content-Encoding", "gzip")
		c.DataFromReader(
			http.StatusOK,
			entry.CompressedSize,
			entry.ContentType,
			io.NopCloser(bytes.NewReader(entry.CompressedData)),
			nil,
		)
		return
	}

	c.DataFromReader(
		http.StatusOK,
		entry.Size,
		entry.ContentType,
		io.NopCloser(bytes.NewReader(entry.Data)),
		nil,
	)
}

func serveFromStorage(c *gin.Context, bucket, key, cacheKey string, acceptsGzip bool) {
	obj, err := storage.GetMinioClient().GetObject(context.Background(), bucket, key, minio.GetObjectOptions{})
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	info, err := obj.Stat()
	if err != nil {
		handleStorageError(c, err)
		return
	}

	data, err := io.ReadAll(obj)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Cache the object
	cache.AddToCache(cacheKey, data, info.ContentType, info.Size, info.LastModified, info.ETag)

	if acceptsGzip && cache.ShouldCompress(info.ContentType, info.Size) {
		compressedData, err := cache.CompressData(data)
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

	c.DataFromReader(
		http.StatusOK,
		info.Size,
		info.ContentType,
		io.NopCloser(bytes.NewReader(data)),
		nil,
	)
}

func processRequestBody(c *gin.Context) ([]byte, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}

	if c.GetHeader("Content-Encoding") == "gzip" {
		return cache.DecompressData(body)
	}

	return body, nil
}

func handleStorageError(c *gin.Context, err error) {
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.AbortWithError(http.StatusInternalServerError, err)
}

func setObjectHeaders(c *gin.Context, contentType string, size int64, lastModified time.Time, etag string) {
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", fmt.Sprintf("%d", size))
	c.Header("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	c.Header("ETag", etag)
}
