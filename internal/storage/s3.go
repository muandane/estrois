package storage

import (
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/muandane/estrois/internal/config"
)

var minioClient *minio.Client

// InitMinioClient initializes the MinIO client with the provided configuration
func InitMinioClient(config *config.StorageConfig) {
	var err error
	minioClient, err = minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.UseSSL,
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize Minio client: %v", err))
	}
}

// GetMinioClient returns the initialized MinIO client
func GetMinioClient() *minio.Client {
	return minioClient
}
