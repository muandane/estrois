package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/muandane/estrois/internal/config"
	"github.com/muandane/estrois/internal/handlers"
	"github.com/muandane/estrois/internal/router"
	"github.com/muandane/estrois/internal/storage"
)

func setupLogger() *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
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

	// Setup router with middleware
	r := router.NewRouter(logger)
	handler := r.Setup(objectHandler)

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
