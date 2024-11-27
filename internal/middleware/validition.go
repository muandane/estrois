package middleware

import (
	"net/http"
)

type ValidationConfig struct {
	ExcludedPaths []string
	BucketAccess  map[string]string
}

func WithValidation(config ValidationConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check excluded paths
			for _, path := range config.ExcludedPaths {
				if r.URL.Path == path {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Extract bucket using PathValue if applicable
			bucket := r.PathValue("bucket")
			if bucket == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check bucket access policy
			policy, exists := config.BucketAccess[bucket]
			if !exists {
				http.Error(w, "bucket access not configured", http.StatusForbidden)
				return
			}

			// Validate access based on method
			switch r.Method {
			case http.MethodGet, http.MethodHead:
				if policy == "read" || policy == "all" {
					next.ServeHTTP(w, r)
					return
				}
			case http.MethodPut, http.MethodDelete:
				if policy == "write" || policy == "all" {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "access denied", http.StatusForbidden)
		})
	}
}
