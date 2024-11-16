package middleware

import (
	"fmt"
	"net/http"
	"strings"
)

type ValidationConfig struct {
	MaxKeyLength     int
	AllowedBuckets   []string
	AllowedFileTypes []string
	Enabled          bool
}

func WithRequestValidation(config ValidationConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !config.Enabled {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bucketName := extractBucketName(r.URL.Path) // Assume you extract bucket from path

			if !isBucketAllowed(config.AllowedBuckets, bucketName) {
				fmt.Println("Bucket not allowed:", bucketName)
				http.Error(w, "Not authorized bucket", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isBucketAllowed(allowedBuckets []string, bucket string) bool {
	for _, allowed := range allowedBuckets {
		if bucket == allowed {
			return true
		}
	}
	return false
}
func extractBucketName(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
