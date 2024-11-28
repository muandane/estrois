package config

import (
	"errors"
	"log"
	"os"
	"strings"
)

type StorageConfig struct {
	Endpoint             string
	AccessKeyID          string
	SecretAccessKey      string
	AllowedBuckets       map[string]string
	EnableBucketPolicies bool
	UseSSL               bool
}

func GetAllowedBuckets() *StorageConfig {
	bucketAccess, err := parseBucketAccess(getEnv("ALLOWED_BUCKETS", "public:read,private:all,local:all"))
	if err != nil {
		log.Printf("Error parsing bucket access: %v", err)
		log.Printf("Raw ALLOWED_BUCKETS value: %s", os.Getenv("ALLOWED_BUCKETS"))
		log.Printf("Parsed Bucket Access: %+v", bucketAccess)
		log.Fatal(err)
	}
	return &StorageConfig{
		AllowedBuckets: bucketAccess,
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

func parseBucketAccess(policy string) (map[string]string, error) {
	bucketAccessMap := make(map[string]string)
	policyPairs := strings.Split(policy, ",")
	for _, policyPair := range policyPairs {
		parts := strings.Split(policyPair, ":")
		if len(parts) != 2 {
			return nil, errors.New("invalid bucket access policy format")
		}
		bucket := strings.TrimSpace(parts[0])
		access := strings.TrimSpace(parts[1])
		if bucket == "" || access == "" {
			return nil, errors.New("bucket name or access level cannot be empty")
		}
		bucketAccessMap[bucket] = access
	}
	return bucketAccessMap, nil
}
