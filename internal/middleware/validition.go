package middleware

import (
	"net/http"
	"strings"
)

type ValidationConfig struct {
	ExcludedPaths []string
	BucketAccess  map[string]string
}

func WithValidation(config ValidationConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, path := range config.ExcludedPaths {
				if strings.HasPrefix(r.URL.Path, path) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Only validate /objects/ paths
			if !strings.HasPrefix(r.URL.Path, "/objects/") {
				next.ServeHTTP(w, r)
				return
			}

			// Extract bucket name from path
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/objects/"), "/")
			if len(parts) < 1 {
				http.Error(w, "invalid path: bucket name required", http.StatusBadRequest)
				return
			}
			bucketName := parts[0]

			// Check bucket access policy
			if policy, exists := config.BucketAccess[bucketName]; exists {
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
				return
			}

			http.Error(w, "bucket access not configured", http.StatusForbidden)
		})
	}
}
