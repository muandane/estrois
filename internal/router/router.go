package router

import (
	"log/slog"
	"net/http"

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
		ExcludedPaths: []string{
			"/health",
			"/metrics",
			"/stats",
		},
		BucketAccess: config.GetAllowedBuckets().AllowedBuckets,
	}

	metricsMiddleware := middleware.NewMetricsMiddleware()
	statsHandler := handlers.NewStatsHandler()

	// Register routes
	r.mux.Handle("/health", handlers.NewHealthHandler(r.logger))
	r.mux.Handle("/metrics", metricsMiddleware)
	r.mux.Handle("/stats", statsHandler)
	r.mux.HandleFunc("/objects/{bucket}/{key...}", func(w http.ResponseWriter, r *http.Request) {
		bucket := r.PathValue("bucket")
		key := r.PathValue("key")
		r.URL.Path = bucket + "/" + key
		objectHandler.ServeHTTP(w, r)
	})

	// Apply middleware chain
	return middleware.Chain(
		r.mux,
		// middleware.WithLogging(r.logger),
		middleware.WithValidation(validationConfig),
		metricsMiddleware.WithMetrics,
	)
}
