package main

import (
	"github.com/gin-gonic/gin"

	"github.com/muandane/estrois/internal/storage"

	"github.com/muandane/estrois/internal/handlers"

	"github.com/muandane/estrois/internal/config"
)

func main() {
	// Initialize storage client
	storage.InitMinioClient(config.GetStorageConfig())

	// Setup router
	r := gin.Default()

	// Register routes
	r.GET("/objects/:bucket/*key", handlers.GetObject)
	r.PUT("/objects/:bucket/*key", handlers.PutObject)
	r.DELETE("/objects/:bucket/*key", handlers.DeleteObject)
	r.HEAD("/objects/:bucket/*key", handlers.HeadObject)
	r.GET("/health", handlers.HealthCheck)

	// Start server
	r.Run()
}
