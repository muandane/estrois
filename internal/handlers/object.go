package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/muandane/estrois/internal/cache"
)

// ObjectHandler handles object storage operations
type ObjectHandler struct {
	client *minio.Client
	// cache  *cache.Manager
	logger *slog.Logger
}

// Object request/response types
type GetObjectRequest struct {
	AcceptGzip bool
}

type GetObjectResponse struct {
	Data        []byte
	ContentType string
	Size        int64
	ETag        string
}

type PutObjectRequest struct {
	ContentType     string
	ContentEncoding string
	Data            []byte
}

type DeleteObjectRequest struct{}

type HeadObjectRequest struct{}

type ObjectResponse struct {
	Data            []byte
	ContentType     string
	ContentEncoding string
	Size            int64
	LastModified    time.Time
	ETag            string
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

func (h *ObjectHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/objects/{bucket}/{key}", h.routeRequest())
}

func (h *ObjectHandler) routeRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := h.logger.With(
			"method", r.Method,
			"bucket", r.PathValue("bucket"),
			"key", r.PathValue("key"),
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)

		opts := HandlerOptions{
			Logger: logger,
		}

		var handler http.HandlerFunc

		switch r.Method {
		case http.MethodGet:
			handler = Handle(h.handleGet, opts)
		case http.MethodPut:
			opts.DecodeBody = true
			handler = Handle(h.handlePut, opts)
		case http.MethodDelete:
			handler = Handle(h.handleDelete, opts)
		case http.MethodHead:
			handler = Handle(h.handleHead, opts)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if handler != nil {
			handler(w, r)
		}
	}
}

func (h *ObjectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rw := &responseWriter{ResponseWriter: w}
	handler := h.routeRequest()
	handler(rw, r)
}

func (h *ObjectHandler) handleGet(ctx context.Context, req *Request, input GetObjectRequest) (*Response, error) {
	bucket := req.PathParams["bucket"]
	key := req.PathParams["key"]

	if bucket == "" || key == "" {
		return nil, &ValidationError{Field: "path", Message: "invalid bucket or key"}
	}

	cacheKey := cache.GetCacheKey(bucket, key)
	acceptsGzip := strings.Contains(req.Headers.Get("Accept-Encoding"), "gzip")

	// Check cache
	if entry, found := cache.GetFromCache(cacheKey); found {
		var responseData []byte
		var contentEncoding string
		if acceptsGzip && entry.IsCompressed && entry.CompressedData != nil {
			responseData = entry.CompressedData
			contentEncoding = "gzip"
		} else {
			responseData = entry.Data
			contentEncoding = ""
		}

		headers := http.Header{
			"Content-Type":     []string{entry.ContentType},
			"Content-Length":   []string{fmt.Sprintf("%d", len(responseData))},
			"Last-Modified":    []string{entry.LastModified.UTC().Format(http.TimeFormat)},
			"ETag":             []string{entry.ETag},
			"Content-Encoding": []string{contentEncoding},
		}

		return &Response{
			StatusCode:  http.StatusOK,
			Headers:     headers,
			Body:        responseData,
			ContentType: entry.ContentType,
		}, nil
	}

	h.logger.Info("cache miss, fetching from storage")

	obj, err := h.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}

	info, err := obj.Stat()
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, &NotFoundError{Resource: "object", ID: key}
		}
		return nil, err
	}

	// Read the entire object into memory
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read object data: %w", err)
	}

	h.logger.Info("object retrieved from storage",
		"size", len(data),
		"content_type", info.ContentType,
	)

	// Cache the object
	cache.AddToCache(cacheKey, data, info.ContentType, int64(len(data)), info.LastModified, info.ETag)

	headers := http.Header{
		"Content-Type":  []string{info.ContentType},
		"Last-Modified": []string{info.LastModified.UTC().Format(http.TimeFormat)},
		"ETag":          []string{info.ETag},
	}

	responseData := data
	if acceptsGzip && cache.ShouldCompress(info.ContentType, int64(len(data))) {
		if compressedData, err := cache.CompressData(data); err == nil && len(compressedData) < len(data) {
			h.logger.Info("serving compressed data",
				"original_size", len(data),
				"compressed_size", len(compressedData),
			)
			responseData = compressedData
			headers.Set("Content-Encoding", "gzip")
		}
	}

	// Set Content-Length after compression decision
	headers.Set("Content-Length", fmt.Sprintf("%d", len(responseData)))

	return &Response{
		StatusCode:  http.StatusOK,
		Headers:     headers,
		Body:        responseData,
		ContentType: info.ContentType,
	}, nil
}

func (h *ObjectHandler) handlePut(ctx context.Context, req *Request, input PutObjectRequest) (*Response, error) {
	bucket := req.PathParams["bucket"]
	key := req.PathParams["key"]

	if bucket == "" || key == "" {
		return nil, &ValidationError{Field: "path", Message: "invalid bucket or key"}
	}

	cacheKey := cache.GetCacheKey(bucket, key)
	cache.DeleteFromCache(cacheKey)

	data := input.Data
	contentType := input.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if input.ContentEncoding == "gzip" {
		var err error
		data, err = cache.DecompressData(data)
		if err != nil {
			return nil, &ValidationError{Field: "body", Message: "failed to decompress data"}
		}
		h.logger.Info("decompressed request body", "decompressed_size", len(data))
	}

	_, err := h.client.PutObject(
		ctx,
		bucket,
		key,
		bytes.NewReader(data),
		int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to store object: %w", err)
	}

	h.logger.Info("object stored successfully",
		"size", len(data),
		"content_type", contentType,
	)

	return &Response{
		StatusCode: http.StatusOK,
	}, nil
}

func (h *ObjectHandler) handleDelete(ctx context.Context, req *Request, input DeleteObjectRequest) (*Response, error) {
	bucket := req.PathParams["bucket"]
	key := req.PathParams["key"]

	if bucket == "" || key == "" {
		return nil, &ValidationError{Field: "path", Message: "invalid bucket or key"}
	}

	cacheKey := cache.GetCacheKey(bucket, key)
	cache.DeleteFromCache(cacheKey)
	h.logger.Info("cache entry deleted")

	err := h.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to delete object: %w", err)
	}

	h.logger.Info("object deleted successfully")
	return &Response{
		StatusCode: http.StatusNoContent,
	}, nil
}

func (h *ObjectHandler) handleHead(ctx context.Context, req *Request, input HeadObjectRequest) (*Response, error) {
	bucket := req.PathParams["bucket"]
	key := req.PathParams["key"]

	if bucket == "" || key == "" {
		return nil, &ValidationError{Field: "path", Message: "invalid bucket or key"}
	}

	cacheKey := cache.GetCacheKey(bucket, key)

	if entry, found := cache.GetFromCache(cacheKey); found {
		h.logger.Info("serving head from cache",
			"content_type", entry.ContentType,
			"size", entry.Size,
		)
		return &Response{
			StatusCode: http.StatusOK,
			Headers: http.Header{
				"Content-Type":   []string{entry.ContentType},
				"Content-Length": []string{fmt.Sprintf("%d", entry.Size)},
				"Last-Modified":  []string{entry.LastModified.UTC().Format(http.TimeFormat)},
				"ETag":           []string{entry.ETag},
			},
		}, nil
	}

	info, err := h.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, &NotFoundError{Resource: "object", ID: key}
		}
		return nil, err
	}

	h.logger.Info("object stats retrieved",
		"size", info.Size,
		"content_type", info.ContentType,
		"last_modified", info.LastModified,
	)

	return &Response{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type":   []string{info.ContentType},
			"Content-Length": []string{fmt.Sprintf("%d", info.Size)},
			"Last-Modified":  []string{info.LastModified.UTC().Format(http.TimeFormat)},
			"ETag":           []string{info.ETag},
		},
	}, nil
}

// Helper functions

// func serveFromCache(w http.ResponseWriter, entry *cache.CacheEntry, acceptsGzip bool) {
// 	if acceptsGzip && entry.IsCompressed && entry.CompressedData != nil {
// 		w.Header().Set("Content-Encoding", "gzip")
// 		w.Header().Set("Content-Type", entry.ContentType)
// 		w.Write(entry.CompressedData)
// 		return
// 	}

// 	w.Header().Set("Content-Type", entry.ContentType)
// 	w.Write(entry.Data)
// }

type responseWriter struct {
	http.ResponseWriter
	status int
	size   int64
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += int64(size)
	return size, err
}
