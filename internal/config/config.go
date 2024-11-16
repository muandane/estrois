package config

import (
	"os"
)

type StorageConfig struct {
	Endpoint             string
	AccessKeyID          string
	SecretAccessKey      string
	AllowedBuckets       string
	EnableBucketPolicies bool
	UseSSL               bool
}

func GetAllowedBuckets() *StorageConfig {
	return &StorageConfig{
		AllowedBuckets: getEnv("ALLOWED_BUCKETS", "public,private,local"),
	}
}

func GetStorageConfig() *StorageConfig {
	return &StorageConfig{
		Endpoint:        getEnv("S3_ENDPOINT", "localhost:9000"),
		AccessKeyID:     getEnv("S3_ACCESS_KEY", "minioadmin"),
		SecretAccessKey: getEnv("S3_SECRET_KEY", "minioadmin"),
		UseSSL:          getEnv("S3_USE_SSL", "false") == "true",
	}
}

func GetBucketConfig() *StorageConfig {
	return &StorageConfig{
		EnableBucketPolicies: getEnv("ENABLE_BUCKET_POLICIES", "false") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
