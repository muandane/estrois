package handlers

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/muandane/estrois/internal/cache"
)

type ObjectHandler struct {
	client *minio.Client
	logger *slog.Logger
}

func NewObjectHandler(client *minio.Client, logger *slog.Logger) (*ObjectHandler, error) {
	if client == nil {
		return nil, fmt.Errorf("minio client cannot be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ObjectHandler{
		client: client,
		logger: logger,
	}, nil
}

func (h *ObjectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(segments) < 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		h.logger.Error("invalid path",
			"path", r.URL.Path,
			"method", r.Method,
			"remote_addr", r.RemoteAddr,
		)
		return
	}

	bucket := segments[0]
	key := strings.Join(segments[1:], "/")

	logger := h.logger.With(
		"method", r.Method,
		"bucket", bucket,
		"key", key,
		"remote_addr", r.RemoteAddr,
		"user_agent", r.UserAgent(),
	)

	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r, bucket, key, logger)
	case http.MethodPut:
		h.handlePut(w, r, bucket, key, logger)
	case http.MethodDelete:
		h.handleDelete(w, r, bucket, key, logger)
	case http.MethodHead:
		h.handleHead(w, r, bucket, key, logger)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		logger.Error("method not allowed")
		return
	}

	logger.Info("request completed",
		"duration", time.Since(start).String(),
	)
}

func (h *ObjectHandler) handleGet(w http.ResponseWriter, r *http.Request, bucket, key string, logger *slog.Logger) {
	cacheKey := cache.GetCacheKey(bucket, key)
	acceptsGzip := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")

	// Check cache
	if entry, found := cache.GetFromCache(cacheKey); found {
		logger.Info("serving from cache",
			"content_type", entry.ContentType,
			"size", entry.Size,
			"compressed", entry.IsCompressed,
		)
		serveFromCache(w, entry, acceptsGzip)
		return
	}

	logger.Info("cache miss, fetching from storage")

	obj, err := h.client.GetObject(r.Context(), bucket, key, minio.GetObjectOptions{})
	if err != nil {
		logger.Error("failed to get object from storage", "error", err)
		handleError(w, err)
		return
	}

	info, err := obj.Stat()
	if err != nil {
		logger.Error("failed to get object stats", "error", err)
		handleStorageError(w, err)
		return
	}

	data, err := io.ReadAll(obj)
	if err != nil {
		logger.Error("failed to read object data", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Info("object retrieved from storage",
		"size", len(data),
		"content_type", info.ContentType,
	)

	// Cache the object
	cache.AddToCache(cacheKey, data, info.ContentType, info.Size, info.LastModified, info.ETag)

	if acceptsGzip && cache.ShouldCompress(info.ContentType, info.Size) {
		if compressedData, err := cache.CompressData(data); err == nil && len(compressedData) < int(info.Size) {
			logger.Info("serving compressed data",
				"original_size", len(data),
				"compressed_size", len(compressedData),
			)
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", info.ContentType)
			w.Write(compressedData)
			return
		}
	}

	w.Header().Set("Content-Type", info.ContentType)
	w.Write(data)
}

func (h *ObjectHandler) handlePut(w http.ResponseWriter, r *http.Request, bucket, key string, logger *slog.Logger) {
	cacheKey := cache.GetCacheKey(bucket, key)
	cache.DeleteFromCache(cacheKey)

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("failed to read request body", "error", err)
		http.Error(w, fmt.Sprintf("failed to read request body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	logger.Info("request body read", "size", len(body))

	if r.Header.Get("Content-Encoding") == "gzip" {
		body, err = cache.DecompressData(body)
		if err != nil {
			logger.Error("failed to decompress data", "error", err)
			http.Error(w, fmt.Sprintf("failed to decompress data: %v", err), http.StatusBadRequest)
			return
		}
		logger.Info("decompressed request body", "decompressed_size", len(body))
	}

	_, err = h.client.PutObject(
		r.Context(),
		bucket,
		key,
		bytes.NewReader(body),
		int64(len(body)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		logger.Error("failed to store object", "error", err)
		http.Error(w, fmt.Sprintf("failed to store object: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Info("object stored successfully",
		"size", len(body),
		"content_type", contentType,
	)

	w.WriteHeader(http.StatusOK)
}

func (h *ObjectHandler) handleDelete(w http.ResponseWriter, r *http.Request, bucket, key string, logger *slog.Logger) {
	cacheKey := cache.GetCacheKey(bucket, key)
	cache.DeleteFromCache(cacheKey)
	logger.Info("cache entry deleted")

	err := h.client.RemoveObject(r.Context(), bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		logger.Error("failed to delete object", "error", err)
		handleError(w, err)
		return
	}

	logger.Info("object deleted successfully")
	w.WriteHeader(http.StatusNoContent)
}

func (h *ObjectHandler) handleHead(w http.ResponseWriter, r *http.Request, bucket, key string, logger *slog.Logger) {
	cacheKey := cache.GetCacheKey(bucket, key)

	if entry, found := cache.GetFromCache(cacheKey); found {
		logger.Info("serving head from cache",
			"content_type", entry.ContentType,
			"size", entry.Size,
		)
		setObjectHeaders(w, entry)
		return
	}

	info, err := h.client.StatObject(r.Context(), bucket, key, minio.StatObjectOptions{})
	if err != nil {
		logger.Error("failed to get object stats", "error", err)
		handleStorageError(w, err)
		return
	}

	logger.Info("object stats retrieved",
		"size", info.Size,
		"content_type", info.ContentType,
		"last_modified", info.LastModified,
	)

	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size))
	w.Header().Set("Last-Modified", info.LastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", info.ETag)
}

// Helper functions

func serveFromCache(w http.ResponseWriter, entry *cache.CacheEntry, acceptsGzip bool) {
	if acceptsGzip && entry.IsCompressed && entry.CompressedData != nil {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", entry.ContentType)
		w.Write(entry.CompressedData)
		return
	}

	w.Header().Set("Content-Type", entry.ContentType)
	w.Write(entry.Data)
}

func handleError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func handleStorageError(w http.ResponseWriter, err error) {
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		http.NotFound(w, nil)
		return
	}
	handleError(w, err)
}

func setObjectHeaders(w http.ResponseWriter, entry *cache.CacheEntry) {
	w.Header().Set("Content-Type", entry.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", entry.Size))
	w.Header().Set("Last-Modified", entry.LastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", entry.ETag)
}
