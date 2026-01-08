package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"api-conver/internal/config"
	"api-conver/internal/interface/router"
)

func main() {
	// Load .env for legacy support
	_ = godotenv.Load()

	// Initialize config
	_, _ = config.Load(config.Path())

	// Set Gin mode
	gin.SetMode(gin.ReleaseMode)

	// Create engine
	engine := router.New()

	port := config.Get().Defaults.Port
	if port == "" {
		port = "8080"
	}

	log.Printf("listening on %s", port)
	log.Printf("config loaded from: %s", config.Path())

	if err := engine.Run(":" + port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
