package handler

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"api-conver/internal/application/usecase"
	"api-conver/internal/config"
)

// ChatHandler handles OpenAI /v1/chat/completions requests
type ChatHandler struct {
	uc *usecase.ProxyUseCase
}

func NewChatHandler(uc *usecase.ProxyUseCase) *ChatHandler {
	return &ChatHandler{uc: uc}
}

// Handle handles POST /v1/chat/completions
func (h *ChatHandler) Handle(c *gin.Context) {
	alias := getAliasFromPath(c)
	if alias == "" || alias == "v1" {
		h.uc.HandleOpenAI(c, "")
		return
	}
	if !config.IsValidAlias(alias) {
		if alias == "healthz" {
			c.Status(http.StatusNotFound)
			return
		}
		log.Printf("unknown alias: %s", alias)
		c.Status(http.StatusNotFound)
		c.Data(http.StatusNotFound, "text/plain", []byte("unknown alias: "+alias))
		return
	}
	h.uc.HandleOpenAI(c, alias)
}

// HandleAlias handles POST /:alias/v1/chat/completions
func (h *ChatHandler) HandleAlias(c *gin.Context) {
	alias := c.Param("alias")
	h.uc.HandleOpenAI(c, alias)
}

// MessagesHandler handles Anthropic /v1/messages requests
type MessagesHandler struct {
	uc *usecase.ProxyUseCase
}

func NewMessagesHandler(uc *usecase.ProxyUseCase) *MessagesHandler {
	return &MessagesHandler{uc: uc}
}

// Handle handles POST /v1/messages
func (h *MessagesHandler) Handle(c *gin.Context) {
	alias := getAliasFromPath(c)
	if alias == "" || alias == "v1" {
		h.uc.HandleAnthropic(c, "")
		return
	}
	if !config.IsValidAlias(alias) {
		if alias == "healthz" {
			c.Status(http.StatusNotFound)
			return
		}
		log.Printf("unknown alias: %s", alias)
		c.Status(http.StatusNotFound)
		c.Data(http.StatusNotFound, "text/plain", []byte("unknown alias: "+alias))
		return
	}
	h.uc.HandleAnthropic(c, alias)
}

// HandleAlias handles POST /:alias/v1/messages
func (h *MessagesHandler) HandleAlias(c *gin.Context) {
	alias := c.Param("alias")
	h.uc.HandleAnthropic(c, alias)
}

// ProxyHandler handles generic /v1/* proxy requests
type ProxyHandler struct {
	uc *usecase.ProxyUseCase
}

func NewProxyHandler(uc *usecase.ProxyUseCase) *ProxyHandler {
	return &ProxyHandler{uc: uc}
}

// Handle handles POST /v1
func (h *ProxyHandler) Handle(c *gin.Context) {
	alias := getAliasFromPath(c)
	if alias == "" || alias == "v1" {
		h.uc.HandleProxy(c, "")
		return
	}
	if !config.IsValidAlias(alias) {
		if alias == "healthz" {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusNotFound)
		c.Data(http.StatusNotFound, "text/plain", []byte("unknown alias: "+alias))
		return
	}
	h.uc.HandleProxy(c, alias)
}

// HandleAlias handles POST /:alias/v1
func (h *ProxyHandler) HandleAlias(c *gin.Context) {
	alias := c.Param("alias")
	h.uc.HandleProxy(c, alias)
}

// HandleAliasFallback handles unmatched /:alias/* requests.
func (h *ProxyHandler) HandleAliasFallback(c *gin.Context) {
	alias := getAliasFromPath(c)
	if alias == "" || alias == "v1" {
		c.Status(http.StatusNotFound)
		return
	}
	if !config.IsValidAlias(alias) {
		if alias == "healthz" {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusNotFound)
		c.Data(http.StatusNotFound, "text/plain", []byte("unknown alias: "+alias))
		return
	}
	h.uc.HandleProxy(c, alias)
}

// HealthHandler handles health check requests
type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Handle handles GET /healthz
func (h *HealthHandler) Handle(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

// HandleAlias handles GET /:alias/healthz
func (h *HealthHandler) HandleAlias(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

func getAliasFromPath(c *gin.Context) string {
	path := c.Request.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return ""
}
