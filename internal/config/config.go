package config

import "os"

type StorageConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
}

func GetStorageConfig() *StorageConfig {
	return &StorageConfig{
		Endpoint:        getEnv("S3_ENDPOINT", "localhost:9000"),
		AccessKeyID:     getEnv("S3_ACCESS_KEY", "minioadmin"),
		SecretAccessKey: getEnv("S3_SECRET_KEY", "minioadmin"),
		UseSSL:          getEnv("S3_USE_SSL", "false") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
