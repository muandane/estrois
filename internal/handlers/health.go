package handlers

import (
	"log/slog"
	"net/http"
	"time"
)

type HealthHandler struct {
	logger *slog.Logger
}

func NewHealthHandler(logger *slog.Logger) *HealthHandler {
	return &HealthHandler{
		logger: logger,
	}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"healthy"}`))

	h.logger.Info("health check completed",
		"duration", time.Since(start).String(),
		"remote_addr", r.RemoteAddr,
	)
}
