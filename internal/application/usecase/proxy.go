package usecase

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"api-conver/internal/config"
	"api-conver/internal/domain/model"
	"api-conver/internal/domain/service"
	"api-conver/internal/infrastructure/proxy"
)

// ProxyUseCase handles proxy requests
type ProxyUseCase struct {
	converter *service.Converter
	client    *proxy.Client
}

func NewProxyUseCase() *ProxyUseCase {
	return &ProxyUseCase{
		converter: service.NewConverter(),
		client:    proxy.NewClient(),
	}
}

// HandleOpenAI handles OpenAI /v1/chat/completions request
func (u *ProxyUseCase) HandleOpenAI(c *gin.Context, alias string) {
	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(400, gin.H{"error": "invalid json"})
		return
	}

	// Set default model if not specified
	if modelVal, ok := payload["model"].(string); !ok || modelVal == "" {
		payload["model"] = getDefaultModel(alias)
	}
	if _, ok := payload["stream"]; !ok || payload["stream"] == nil {
		payload["stream"] = false
	}

	out, _ := json.Marshal(payload)

	aliasCfg := getUpstreamConfig(alias)
	upstreamPath := stripAliasPrefix(c.Request.URL.Path, alias)
	respBody, statusCode, headers, err := u.client.ProxyRequest(c, out, "POST", upstreamPath, aliasCfg)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}

	copyHeaders(c, headers)
	c.Status(statusCode)
	c.Data(http.StatusOK, "application/json", respBody)
}

// HandleAnthropic handles Anthropic /v1/messages request
func (u *ProxyUseCase) HandleAnthropic(c *gin.Context, alias string) {
	var req model.AnthropicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid json"})
		return
	}

	// Set default model if not specified
	if req.Model == "" {
		req.Model = getDefaultModel(alias)
	}

	if req.Stream {
		u.handleAnthropicStream(c, req, alias)
		return
	}

	openAIMessages, err := u.converter.ConvertAnthropicToOpenAIMessages(req.System, req.Messages)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	openAIReq := map[string]interface{}{
		"model":    req.Model,
		"messages": openAIMessages,
		"stream":   false,
	}
	if req.MaxTokens > 0 {
		openAIReq["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		openAIReq["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		openAIReq["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		openAIReq["stop"] = req.StopSequences
	}
	if tools := u.converter.ConvertAnthropicTools(req.Tools); len(tools) > 0 {
		openAIReq["tools"] = tools
	}
	if req.ToolChoice != nil {
		openAIReq["tool_choice"] = u.converter.ConvertAnthropicToolChoice(req.ToolChoice)
	}

	out, _ := json.Marshal(openAIReq)

	aliasCfg := getUpstreamConfig(alias)
	respBody, statusCode, headers, err := u.client.ProxyRequest(c, out, "POST", "/v1/chat/completions", aliasCfg)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}

	if statusCode < 200 || statusCode > 299 {
		copyHeaders(c, headers)
		c.Status(statusCode)
		c.Data(http.StatusOK, "application/json", respBody)
		return
	}

	var openAIResp model.OpenAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		c.JSON(502, gin.H{"error": "invalid upstream response"})
		return
	}
	if len(openAIResp.Choices) == 0 || openAIResp.Choices[0].Message == nil {
		c.JSON(502, gin.H{"error": "no choices in response"})
		return
	}

	message := openAIResp.Choices[0].Message
	contentBlocks := u.converter.BuildAnthropicContentBlocks(message)
	anthropicResp := model.AnthropicResponse{
		ID:      openAIResp.ID,
		Type:    "message",
		Role:    "assistant",
		Model:   openAIResp.Model,
		Content: contentBlocks,
	}
	if anthropicResp.Model == "" {
		anthropicResp.Model = req.Model
	}
	anthropicResp.Usage.InputTokens = openAIResp.Usage.PromptTokens
	anthropicResp.Usage.OutputTokens = openAIResp.Usage.CompletionTokens
	hasToolCalls := len(message.ToolCalls) > 0 || message.FunctionCall != nil
	anthropicResp.StopReason = u.converter.MapStopReason(openAIResp.Choices[0].FinishReason, hasToolCalls)

	c.JSON(200, anthropicResp)
}

func (u *ProxyUseCase) handleAnthropicStream(c *gin.Context, req model.AnthropicRequest, alias string) {
	openAIMessages, err := u.converter.ConvertAnthropicToOpenAIMessages(req.System, req.Messages)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	openAIReq := map[string]interface{}{
		"model":    req.Model,
		"messages": openAIMessages,
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}
	if req.MaxTokens > 0 {
		openAIReq["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		openAIReq["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		openAIReq["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		openAIReq["stop"] = req.StopSequences
	}
	if tools := u.converter.ConvertAnthropicTools(req.Tools); len(tools) > 0 {
		openAIReq["tools"] = tools
	}
	if req.ToolChoice != nil {
		openAIReq["tool_choice"] = u.converter.ConvertAnthropicToolChoice(req.ToolChoice)
	}

	out, _ := json.Marshal(openAIReq)

	aliasCfg := getUpstreamConfig(alias)
	resp, err := u.client.ProxyStream(c, out, "POST", "/v1/chat/completions", aliasCfg)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		copyHeaders(c, resp.Header)
		c.Status(resp.StatusCode)
		io.Copy(c.Writer, resp.Body)
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// Stream conversion would go here
	// For simplicity, we passthrough the stream
	io.Copy(c.Writer, resp.Body)
}

// HandleProxy handles generic /v1/* proxy requests
func (u *ProxyUseCase) HandleProxy(c *gin.Context, alias string) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "read body failed"})
		return
	}

	aliasCfg := getUpstreamConfig(alias)
	upstreamPath := stripAliasPrefix(c.Request.URL.Path, alias)
	respBody, statusCode, headers, err := u.client.ProxyRequest(c, body, c.Request.Method, upstreamPath, aliasCfg)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}

	copyHeaders(c, headers)
	c.Status(statusCode)
	c.Data(http.StatusOK, "application/json", respBody)
}

// Helpers

func getDefaultModel(alias string) string {
	cfg := config.GetAliasConfig(resolveAlias(alias))
	if cfg != nil && cfg.DefaultModel != "" {
		return cfg.DefaultModel
	}
	return "tstars2.0"
}

func getUpstreamConfig(alias string) *proxy.UpstreamConfig {
	if cfg := config.GetAliasConfig(resolveAlias(alias)); cfg != nil {
		return &proxy.UpstreamConfig{
			BaseURL:    cfg.BaseURL,
			APIKey:     cfg.APIKey,
			AuthHeader: cfg.AuthHeader,
			AuthPrefix: cfg.AuthPrefix,
		}
	}
	return nil
}

func copyHeaders(c *gin.Context, headers http.Header) {
	for k, v := range headers {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, val := range v {
			c.Header(k, val)
		}
	}
}

func stripAliasPrefix(path, alias string) string {
	if alias == "" {
		return path
	}
	prefix := "/" + alias
	if strings.HasPrefix(path, prefix+"/") {
		return path[len(prefix):]
	}
	if path == prefix {
		return "/"
	}
	return path
}

func resolveAlias(alias string) string {
	if alias != "" {
		return alias
	}
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	if cfg.Defaults.Alias != "" {
		if _, ok := cfg.Aliases[cfg.Defaults.Alias]; ok {
			return cfg.Defaults.Alias
		}
	}
	if len(cfg.Aliases) == 1 {
		for name := range cfg.Aliases {
			return name
		}
	}
	return ""
}
