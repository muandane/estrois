package middleware

import (
	"log/slog"
	"net/http"
	"strings"
)

type BucketPolicy struct {
	AllowedOperations map[string][]string // bucket -> operations
	AllowedIPs        map[string][]string // bucket -> IP ranges
}

func WithBucketAccessControl(policy BucketPolicy, logger *slog.Logger, enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			logger.Info("Bucket policies are disabled")
			return next
		}
		logger.Info("Bucket access control middleware enabled")
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pathSegments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(pathSegments) < 2 {
				http.Error(w, "Invalid path", http.StatusBadRequest)
				return
			}
			bucketName := pathSegments[0]

			if !isOperationAllowed(policy, bucketName, r.Method) {
				http.Error(w, "Operation not allowed", http.StatusForbidden)
				return
			}

			if !isIPAllowed(policy, bucketName, r.RemoteAddr) {
				http.Error(w, "Access denied", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isOperationAllowed(policy BucketPolicy, bucketName, method string) bool {
	if operations, exists := policy.AllowedOperations[bucketName]; exists {
		for _, operation := range operations {
			if operation == method {
				return true
			}
		}
	}
	return false
}

func isIPAllowed(policy BucketPolicy, bucketName, remoteAddr string) bool {
	if allowedIPs, exists := policy.AllowedIPs[bucketName]; exists {
		clientIP := strings.Split(remoteAddr, ":")[0]
		for _, ipPrefix := range allowedIPs {
			if strings.HasPrefix(clientIP, ipPrefix) {
				return true
			}
		}
	}
	return false
}
