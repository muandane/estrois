package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/muandane/estrois/internal/config"
	"github.com/muandane/estrois/internal/handlers"
	"github.com/muandane/estrois/internal/storage"
)

func setupLogger() *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize timestamp format
			if a.Key == slog.TimeKey {
				return slog.Attr{
					Key:   slog.TimeKey,
					Value: slog.StringValue(a.Value.Time().Format(time.RFC3339)),
				}
			}
			return a
		},
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(handler)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	length     int
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := lrw.ResponseWriter.Write(b)
	lrw.length += n
	return n, err
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := newLoggingResponseWriter(w)

		// Process request
		next.ServeHTTP(lrw, r)

		// Log request details
		logger.Info("http request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lrw.statusCode,
			"duration", time.Since(start).String(),
			"size", lrw.length,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
	})
}

func main() {
	// Setup logger
	logger := setupLogger()
	slog.SetDefault(logger)

	logger.Info("starting application")

	// Initialize storage client
	storage.InitMinioClient(config.GetStorageConfig())

	logger.Info("storage client initialized")

	// Create object handler
	objectHandler, err := handlers.NewObjectHandler(storage.GetMinioClient(), logger)
	if err != nil {
		logger.Error("failed to create object handler", "error", err)
		os.Exit(1)
	}

	// Setup router
	mux := http.NewServeMux()

	// Register routes
	mux.HandleFunc("/health", handlers.HealthHandler)

	// Handle object routes
	mux.HandleFunc("/objects/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/objects/")
		if path == "" {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		r.URL.Path = "/" + path
		objectHandler.ServeHTTP(w, r)
	})

	// Add logging middleware
	handler := loggingMiddleware(logger, mux)

	// Start server
	addr := ":8080"
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	logger.Info("server starting", "addr", addr)
	if err := server.ListenAndServe(); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
