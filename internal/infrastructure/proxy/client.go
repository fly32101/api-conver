package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type UpstreamConfig struct {
	BaseURL    string
	APIKey     string
	AuthHeader string
	AuthPrefix string
}

type Client struct {
	client *http.Client
}

func NewClient() *Client {
	return &Client{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// ProxyRequest makes a proxy request to upstream
func (c *Client) ProxyRequest(ctx *gin.Context, body []byte, method string, upstreamPath string, cfg *UpstreamConfig) ([]byte, int, http.Header, error) {
	baseURL := c.getBaseURL(cfg)
	url := c.buildUpstreamURL(baseURL, upstreamPath, ctx.Request.URL.RawQuery)

	req, err := http.NewRequestWithContext(ctx.Request.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, nil, err
	}

	c.copyRequestHeaders(req, ctx.Request)
	if req.Header.Get("Content-Type") == "" && len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	c.applyAuthHeader(req, ctx.Request, cfg)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, nil, err
	}

	log.Printf("upstream response: method=%s path=%s status=%d encoding=%s content-type=%s body=%s",
		method, upstreamPath, resp.StatusCode, resp.Header.Get("Content-Encoding"), resp.Header.Get("Content-Type"),
		truncateBody(respBody, 2000),
	)

	return respBody, resp.StatusCode, resp.Header, nil
}

// ProxyStream makes a streaming proxy request to upstream
func (c *Client) ProxyStream(ctx *gin.Context, body []byte, method string, upstreamPath string, cfg *UpstreamConfig) (*http.Response, error) {
	baseURL := c.getBaseURL(cfg)
	url := c.buildUpstreamURL(baseURL, upstreamPath, ctx.Request.URL.RawQuery)

	req, err := http.NewRequestWithContext(ctx.Request.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	c.copyRequestHeaders(req, ctx.Request)
	if req.Header.Get("Content-Type") == "" && len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	c.applyAuthHeader(req, ctx.Request, cfg)

	client := &http.Client{Timeout: 0}
	return client.Do(req)
}

func (c *Client) getBaseURL(cfg *UpstreamConfig) string {
	if cfg != nil {
		baseURL := strings.TrimSpace(cfg.BaseURL)
		if baseURL != "" {
			return strings.TrimSuffix(baseURL, "/")
		}
	}
	return "https://api.openai.com/v1"
}

func (c *Client) buildUpstreamURL(baseURL, path, rawQuery string) string {
	upstreamPath := path
	if strings.HasSuffix(baseURL, "/v1") && strings.HasPrefix(upstreamPath, "/v1") {
		upstreamPath = strings.TrimPrefix(upstreamPath, "/v1")
		if upstreamPath == "" {
			upstreamPath = "/"
		}
	}
	if !strings.HasPrefix(upstreamPath, "/") {
		upstreamPath = "/" + upstreamPath
	}
	url := baseURL + upstreamPath
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	return url
}

func (c *Client) copyRequestHeaders(dst *http.Request, src *http.Request) {
	hopByHop := map[string]struct{}{
		"connection":          {},
		"proxy-connection":    {},
		"keep-alive":          {},
		"proxy-authenticate":  {},
		"proxy-authorization": {},
		"te":                  {},
		"trailer":             {},
		"transfer-encoding":   {},
		"upgrade":             {},
		"http2-settings":      {},
	}

	for _, token := range strings.Split(src.Header.Get("Connection"), ",") {
		if token = strings.TrimSpace(strings.ToLower(token)); token != "" {
			hopByHop[token] = struct{}{}
		}
	}

	for k, v := range src.Header {
		keyLower := strings.ToLower(k)
		if _, skip := hopByHop[keyLower]; skip {
			continue
		}
		if keyLower == "host" || keyLower == "content-length" || keyLower == "accept-encoding" {
			continue
		}
		for _, val := range v {
			dst.Header.Add(k, val)
		}
	}
}

func (c *Client) applyAuthHeader(req *http.Request, incoming *http.Request, cfg *UpstreamConfig) {
	var apiKey, authHeader, authPrefix string

	if cfg != nil {
		apiKey = strings.TrimSpace(cfg.APIKey)
		authHeader = strings.TrimSpace(cfg.AuthHeader)
		authPrefix = strings.TrimSpace(cfg.AuthPrefix)
	}

	// Fallback to env vars if not set in config
	if apiKey == "" {
		apiKey = getEnvFirst([]string{"OPENAI_API_KEY", "IFLOW_API_KEY"}, "")
	}
	if authHeader == "" {
		authHeader = getEnvFirst([]string{"OPENAI_AUTH_HEADER", "IFLOW_AUTH_HEADER"}, "Authorization")
	}
	if authPrefix == "" {
		authPrefix = getEnvFirst([]string{"OPENAI_AUTH_PREFIX", "IFLOW_AUTH_PREFIX"}, "Bearer")
	}

	req.Header.Del(authHeader)
	if apiKey != "" {
		if authPrefix != "" {
			req.Header.Set(authHeader, authPrefix+" "+apiKey)
			return
		}
		req.Header.Set(authHeader, apiKey)
		return
	}

	// Passthrough from incoming request
	incomingAuth := incoming.Header.Get(authHeader)
	if incomingAuth != "" {
		req.Header.Set(authHeader, incomingAuth)
	}
}

func getEnvFirst(keys []string, fallback string) string {
	for _, key := range keys {
		if val := strings.TrimSpace(getEnv(key)); val != "" {
			return val
		}
	}
	return fallback
}

func getEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func truncateBody(body []byte, limit int) string {
	if limit <= 0 || len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "...(truncated)"
}
