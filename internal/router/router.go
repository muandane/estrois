package router

import (
	"log/slog"
	"net/http"
	"strings"

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
	// Register health check
	healthHandler := handlers.NewHealthHandler(r.logger)
	r.mux.Handle("/health", healthHandler)

	// Handle object routes
	r.mux.HandleFunc("/objects/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/objects/")
		if path == "" {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		r.URL.Path = "/" + path
		objectHandler.ServeHTTP(w, r)
	})

	// Apply middleware chain
	return middleware.Chain(
		r.mux,
		middleware.WithLogging(r.logger),
		// Add more middleware here as needed
	)
}
