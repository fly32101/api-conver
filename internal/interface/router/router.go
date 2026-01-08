package router

import (
	"github.com/gin-gonic/gin"

	"api-conver/internal/application/usecase"
	"api-conver/internal/interface/handler"
)

// New creates a new Gin router
func New() *gin.Engine {
	engine := gin.New()

	// Middleware
	engine.Use(gin.Recovery())
	engine.Use(gin.Logger())

	// Create handlers
	proxyUC := usecase.NewProxyUseCase()
	chatHandler := handler.NewChatHandler(proxyUC)
	messagesHandler := handler.NewMessagesHandler(proxyUC)
	proxyHandler := handler.NewProxyHandler(proxyUC)
	healthHandler := handler.NewHealthHandler()

	// Health check routes
	engine.GET("/healthz", healthHandler.Handle)

	// Legacy routes (no alias)
	v1 := engine.Group("/v1")
	{
		v1.POST("/chat/completions", chatHandler.Handle)
		v1.POST("/messages", messagesHandler.Handle)
		v1.POST("", proxyHandler.Handle)
		v1.POST("/", proxyHandler.Handle)
	}

	// Alias routes
	alias := engine.Group("/:alias")
	{
		// Health check per alias
		alias.GET("/healthz", healthHandler.HandleAlias)

		// API routes
		v1Alias := alias.Group("/v1")
		{
			v1Alias.POST("/chat/completions", chatHandler.HandleAlias)
			v1Alias.POST("/messages", messagesHandler.HandleAlias)
			v1Alias.POST("", proxyHandler.HandleAlias)
			v1Alias.POST("/", proxyHandler.HandleAlias)
		}
	}

	// Fallback proxy for non-/v1 endpoints under an alias
	engine.NoRoute(proxyHandler.HandleAliasFallback)

	return engine
}
