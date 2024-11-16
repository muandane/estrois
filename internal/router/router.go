package router

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/muandane/estrois/internal/config"
	"github.com/muandane/estrois/internal/handlers"
	"github.com/muandane/estrois/internal/middleware"
)

type Router struct {
	mux    *http.ServeMux
	logger *slog.Logger
}

func NewRouter(logger *slog.Logger) *Router {
	return &Router{
		mux:    http.NewServeMux(),
		logger: logger,
	}
}

func (r *Router) Setup(objectHandler *handlers.ObjectHandler) http.Handler {
	// Create middleware instances
	validationConfig := middleware.ValidationConfig{
		MaxKeyLength:   1024,
		AllowedBuckets: strings.Split(config.GetAllowedBuckets().AllowedBuckets, ","),
		AllowedFileTypes: []string{
			"image/jpeg",
			"image/png",
			"application/pdf",
		},
		Enabled: config.GetBucketConfig().EnableBucketPolicies,
	}

	metricsMiddleware := middleware.NewMetricsMiddleware()
	statsHandler := handlers.NewStatsHandler()

	// Register routes
	r.mux.Handle("/health", handlers.NewHealthHandler(r.logger))
	r.mux.Handle("/metrics", metricsMiddleware)
	r.mux.Handle("/stats", statsHandler)
	r.mux.Handle("/objects/", http.StripPrefix("/objects/", objectHandler))

	// Apply middleware chain
	return middleware.Chain(
		r.mux,
		middleware.WithLogging(r.logger),
		middleware.WithRequestValidation(validationConfig),
		metricsMiddleware.WithMetrics,
	)
}
