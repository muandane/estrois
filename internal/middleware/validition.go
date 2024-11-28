package middleware

import (
	"log"
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
			// Check excluded paths first
			for _, path := range config.ExcludedPaths {
				if r.URL.Path == path {
					log.Printf("Path %s is excluded from validation", path)
					next.ServeHTTP(w, r)
					return
				}
			}

			// Explicitly extract bucket from the path for "/objects/{bucket}/{key}"
			pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/objects/"), "/")
			bucket := ""
			if len(pathParts) > 0 {
				bucket = pathParts[0]
			}

			// If no bucket is specified, allow the request
			if bucket == "" {
				log.Println("No bucket specified, allowing request")
				next.ServeHTTP(w, r)
				return
			}

			// Check bucket access policy
			policy, exists := config.BucketAccess[bucket]
			if !exists {
				log.Printf("Bucket %s not configured in access policy", bucket)
				http.Error(w, "bucket access not configured", http.StatusForbidden)
				return
			}

			// Validate access based on HTTP method
			switch r.Method {
			case http.MethodGet, http.MethodHead:
				if policy == "read" || policy == "all" || policy == "write" {
					next.ServeHTTP(w, r)
					return
				}
			case http.MethodPut, http.MethodDelete:
				if policy == "write" || policy == "all" {
					next.ServeHTTP(w, r)
					return
				}
			}

			// If no condition is met, deny access
			log.Printf("Access denied for method %s on bucket %s", r.Method, bucket)
			http.Error(w, "access denied", http.StatusForbidden)
		})
	}
}
