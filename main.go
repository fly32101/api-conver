package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type AnthropicToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema,omitempty"`
	Type        string      `json:"type,omitempty"`
}

type AnthropicRequest struct {
	Model         string                    `json:"model"`
	Messages      []AnthropicMessage        `json:"messages"`
	System        interface{}               `json:"system"`
	MaxTokens     int                       `json:"max_tokens"`
	Temperature   *float64                  `json:"temperature"`
	TopP          *float64                  `json:"top_p"`
	TopK          *int                      `json:"top_k"`
	Stream        bool                      `json:"stream"`
	Tools         []AnthropicToolDefinition `json:"tools"`
	ToolChoice    interface{}               `json:"tool_choice"`
	StopSequences []string                  `json:"stop_sequences"`
}

type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAIFunctionCall `json:"function"`
}

type OpenAIMessage struct {
	Role         string              `json:"role"`
	Content      interface{}         `json:"content"`
	ToolCalls    []OpenAIToolCall    `json:"tool_calls,omitempty"`
	FunctionCall *OpenAIFunctionCall `json:"function_call,omitempty"`
}

type OpenAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int            `json:"index"`
		Message      *OpenAIMessage `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type OpenAIStreamResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Usage   *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type AnthropicContentBlock struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	ID    string      `json:"id,omitempty"`
	Name  string      `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`
}

type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Model        string                  `json:"model"`
	Content      []AnthropicContentBlock `json:"content"`
	StopReason   string                  `json:"stop_reason,omitempty"`
	StopSequence string                  `json:"stop_sequence,omitempty"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func main() {
	_ = godotenv.Load()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/chat/completions", handleOpenAI)
	mux.HandleFunc("/v1/messages", handleAnthropic)
	mux.HandleFunc("/v1", handleV1Proxy)
	mux.HandleFunc("/v1/", handleV1Proxy)

	addr := ":" + getEnv("PORT", "8080")
	log.Printf("listening on %s", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func handleOpenAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	modelVal, _ := payload["model"].(string)
	if strings.TrimSpace(modelVal) == "" {
		payload["model"] = getEnvFirst([]string{"OPENAI_MODEL_DEFAULT", "IFLOW_MODEL_DEFAULT"}, "tstars2.0")
	}
	if _, ok := payload["stream"]; !ok || payload["stream"] == nil {
		payload["stream"] = false
	}

	out, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "encode json failed", http.StatusInternalServerError)
		return
	}

	proxyRequest(w, r, out, http.MethodPost, r.URL.Path)
}

func handleAnthropic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req AnthropicRequest
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Stream {
		handleAnthropicStream(w, r, req)
		return
	}

	if req.Model == "" {
		req.Model = getEnvFirst([]string{"OPENAI_MODEL_DEFAULT", "IFLOW_MODEL_DEFAULT"}, "tstars2.0")
	}

	openAIMessages, err := convertAnthropicToOpenAIMessages(req.System, req.Messages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	if req.TopK != nil {
		openAIReq["top_k"] = *req.TopK
	}
	if len(req.StopSequences) > 0 {
		openAIReq["stop"] = req.StopSequences
	}
	if tools := convertAnthropicTools(req.Tools); len(tools) > 0 {
		openAIReq["tools"] = tools
	}
	if req.ToolChoice != nil {
		openAIReq["tool_choice"] = convertAnthropicToolChoice(req.ToolChoice)
	}

	out, err := json.Marshal(openAIReq)
	if err != nil {
		http.Error(w, "encode json failed", http.StatusInternalServerError)
		return
	}

	respBody, statusCode, headers, err := proxyRequestRaw(r, out, http.MethodPost, "/v1/chat/completions")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if statusCode < 200 || statusCode > 299 {
		copyHeaders(w, headers)
		w.WriteHeader(statusCode)
		_, _ = w.Write(respBody)
		return
	}

	var openAIResp OpenAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		log.Printf("invalid upstream response: status=%d encoding=%s content-type=%s body=%s", statusCode, headers.Get("Content-Encoding"), headers.Get("Content-Type"), truncateBody(respBody, 2000))
		http.Error(w, "invalid upstream response", http.StatusBadGateway)
		return
	}
	if len(openAIResp.Choices) == 0 || openAIResp.Choices[0].Message == nil {
		log.Printf("no choices in upstream response: status=%d encoding=%s content-type=%s body=%s", statusCode, headers.Get("Content-Encoding"), headers.Get("Content-Type"), truncateBody(respBody, 2000))
		http.Error(w, "no choices in response", http.StatusBadGateway)
		return
	}

	message := openAIResp.Choices[0].Message
	contentBlocks := buildAnthropicContentBlocks(message)
	anthropicResp := AnthropicResponse{
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
	anthropicResp.StopReason = mapStopReason(openAIResp.Choices[0].FinishReason, hasToolCalls)

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(anthropicResp)
}

func handleAnthropicStream(w http.ResponseWriter, r *http.Request, req AnthropicRequest) {
	if req.Model == "" {
		req.Model = getEnvFirst([]string{"OPENAI_MODEL_DEFAULT", "IFLOW_MODEL_DEFAULT"}, "tstars2.0")
	}

	openAIMessages, err := convertAnthropicToOpenAIMessages(req.System, req.Messages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	if req.TopK != nil {
		openAIReq["top_k"] = *req.TopK
	}
	if len(req.StopSequences) > 0 {
		openAIReq["stop"] = req.StopSequences
	}
	if tools := convertAnthropicTools(req.Tools); len(tools) > 0 {
		openAIReq["tools"] = tools
	}
	if req.ToolChoice != nil {
		openAIReq["tool_choice"] = convertAnthropicToolChoice(req.ToolChoice)
	}

	out, err := json.Marshal(openAIReq)
	if err != nil {
		http.Error(w, "encode json failed", http.StatusInternalServerError)
		return
	}

	resp, err := proxyRequestStream(r, out, http.MethodPost, "/v1/chat/completions")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		copyHeaders(w, resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	if err := streamOpenAIToAnthropic(w, resp.Body, flusher, req.Model); err != nil && !errors.Is(err, io.EOF) {
		log.Printf("stream conversion error: %v", err)
	}
}

func handleV1Proxy(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	proxyRequest(w, r, body, r.Method, r.URL.Path)
}

func proxyRequest(w http.ResponseWriter, r *http.Request, body []byte, method string, upstreamPath string) {
	respBody, statusCode, headers, err := proxyRequestRaw(r, body, method, upstreamPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyHeaders(w, headers)
	w.WriteHeader(statusCode)
	_, _ = w.Write(respBody)
}

func proxyRequestStream(r *http.Request, body []byte, method string, upstreamPath string) (*http.Response, error) {
	baseURL := strings.TrimSuffix(getEnvFirst([]string{"OPENAI_BASE_URL", "IFLOW_BASE_URL"}, "https://api.openai.com/v1"), "/")
	url := buildUpstreamURL(baseURL, upstreamPath, r.URL.RawQuery)

	req, err := http.NewRequestWithContext(r.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyRequestHeaders(req, r)
	if req.Header.Get("Content-Type") == "" && len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	applyAuthHeader(req, r)

	client := &http.Client{Timeout: 0}
	return client.Do(req)
}

func proxyRequestRaw(r *http.Request, body []byte, method string, upstreamPath string) ([]byte, int, http.Header, error) {
	baseURL := strings.TrimSuffix(getEnvFirst([]string{"OPENAI_BASE_URL", "IFLOW_BASE_URL"}, "https://api.openai.com/v1"), "/")
	url := buildUpstreamURL(baseURL, upstreamPath, r.URL.RawQuery)

	req, err := http.NewRequestWithContext(r.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, nil, err
	}
	copyRequestHeaders(req, r)
	if req.Header.Get("Content-Type") == "" && len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	applyAuthHeader(req, r)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
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

func buildUpstreamURL(baseURL, path, rawQuery string) string {
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

func copyRequestHeaders(dst *http.Request, src *http.Request) {
	for k, v := range src.Header {
		if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Accept-Encoding") {
			continue
		}
		for _, val := range v {
			dst.Header.Add(k, val)
		}
	}
}

func applyAuthHeader(req *http.Request, incoming *http.Request) {
	apiKey := strings.TrimSpace(getEnvFirst([]string{"OPENAI_API_KEY", "IFLOW_API_KEY"}, ""))
	authHeader := getEnvFirst([]string{"OPENAI_AUTH_HEADER", "IFLOW_AUTH_HEADER"}, "Authorization")
	authPrefix := getEnvFirst([]string{"OPENAI_AUTH_PREFIX", "IFLOW_AUTH_PREFIX"}, "Bearer")

	req.Header.Del(authHeader)
	if apiKey != "" {
		if authPrefix != "" {
			req.Header.Set(authHeader, authPrefix+" "+apiKey)
			return
		}
		req.Header.Set(authHeader, apiKey)
		return
	}

	incomingAuth := incoming.Header.Get(authHeader)
	if incomingAuth != "" {
		req.Header.Set(authHeader, incomingAuth)
	}
}

func copyHeaders(w http.ResponseWriter, headers http.Header) {
	for k, v := range headers {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
}

func convertAnthropicToOpenAIMessages(system interface{}, messages []AnthropicMessage) ([]map[string]interface{}, error) {
	openAIMessages := make([]map[string]interface{}, 0, len(messages)+1)
	sysText := flattenAnthropicText(system)
	if strings.TrimSpace(sysText) != "" {
		openAIMessages = append(openAIMessages, map[string]interface{}{
			"role":    "system",
			"content": sysText,
		})
	}

	for _, msg := range messages {
		converted, err := convertAnthropicMessage(msg)
		if err != nil {
			return nil, err
		}
		openAIMessages = append(openAIMessages, converted...)
	}

	return openAIMessages, nil
}

func convertAnthropicMessage(msg AnthropicMessage) ([]map[string]interface{}, error) {
	textParts, toolCalls, toolResults := parseAnthropicContent(msg.Content)

	messages := []map[string]interface{}{}
	if len(textParts) > 0 || len(toolCalls) > 0 {
		mainMsg := map[string]interface{}{
			"role":    msg.Role,
			"content": strings.Join(textParts, "\n"),
		}
		if len(toolCalls) > 0 {
			mainMsg["tool_calls"] = toolCalls
		}
		messages = append(messages, mainMsg)
	} else if len(toolResults) == 0 {
		messages = append(messages, map[string]interface{}{
			"role":    msg.Role,
			"content": "",
		})
	}

	if len(toolResults) > 0 {
		messages = append(messages, toolResults...)
	}

	return messages, nil
}

func parseAnthropicContent(content interface{}) ([]string, []map[string]interface{}, []map[string]interface{}) {
	textParts := []string{}
	toolCalls := []map[string]interface{}{}
	toolResults := []map[string]interface{}{}

	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			textParts = append(textParts, v)
		}
	case []interface{}:
		for _, item := range v {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			parseAnthropicBlock(block, &textParts, &toolCalls, &toolResults)
		}
	case map[string]interface{}:
		parseAnthropicBlock(v, &textParts, &toolCalls, &toolResults)
	case nil:
		return textParts, toolCalls, toolResults
	default:
		fallback := flattenAnthropicText(v)
		if strings.TrimSpace(fallback) != "" {
			textParts = append(textParts, fallback)
		}
	}

	return textParts, toolCalls, toolResults
}

func parseAnthropicBlock(block map[string]interface{}, textParts *[]string, toolCalls *[]map[string]interface{}, toolResults *[]map[string]interface{}) {
	typeVal, _ := block["type"].(string)
	switch typeVal {
	case "text":
		text, _ := block["text"].(string)
		if strings.TrimSpace(text) != "" {
			*textParts = append(*textParts, text)
		}
	case "tool_use":
		name, _ := block["name"].(string)
		id, _ := block["id"].(string)
		if strings.TrimSpace(id) == "" {
			id = generateToolCallID()
		}
		input := block["input"]
		argsBytes := []byte("{}")
		if input != nil {
			if b, err := json.Marshal(input); err == nil {
				argsBytes = b
			}
		}
		*toolCalls = append(*toolCalls, map[string]interface{}{
			"id":   id,
			"type": "function",
			"function": map[string]interface{}{
				"name":      name,
				"arguments": string(argsBytes),
			},
		})
	case "tool_result":
		toolUseID, _ := block["tool_use_id"].(string)
		*toolResults = append(*toolResults, map[string]interface{}{
			"role":         "tool",
			"tool_call_id": toolUseID,
			"content":      stringifyToolResult(block["content"]),
		})
	default:
		text, _ := block["text"].(string)
		if strings.TrimSpace(text) != "" {
			*textParts = append(*textParts, text)
		}
	}
}

func flattenAnthropicText(v interface{}) string {
	parts := extractTextParts(v)
	return strings.Join(parts, "\n")
}

func extractTextParts(v interface{}) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return []string{t}
	case []interface{}:
		parts := []string{}
		for _, item := range t {
			if text, ok := item.(string); ok {
				if strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
				continue
			}
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if typeVal, _ := block["type"].(string); typeVal == "text" {
				if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
				continue
			}
			if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return parts
	case map[string]interface{}:
		if typeVal, _ := t["type"].(string); typeVal == "text" {
			if text, ok := t["text"].(string); ok && strings.TrimSpace(text) != "" {
				return []string{text}
			}
		}
		if text, ok := t["text"].(string); ok && strings.TrimSpace(text) != "" {
			return []string{text}
		}
		return nil
	default:
		return nil
	}
}

func stringifyToolResult(content interface{}) string {
	if content == nil {
		return ""
	}
	if text, ok := content.(string); ok {
		return text
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(payload)
}

func convertAnthropicTools(tools []AnthropicToolDefinition) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}
	openAITools := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		function := map[string]interface{}{
			"name": name,
		}
		if strings.TrimSpace(tool.Description) != "" {
			function["description"] = tool.Description
		}
		if tool.InputSchema != nil {
			function["parameters"] = tool.InputSchema
		}
		openAITools = append(openAITools, map[string]interface{}{
			"type":     "function",
			"function": function,
		})
	}
	if len(openAITools) == 0 {
		return nil
	}
	return openAITools
}

func convertAnthropicToolChoice(choice interface{}) interface{} {
	switch v := choice.(type) {
	case string:
		switch v {
		case "any":
			return "required"
		case "auto":
			return "auto"
		default:
			return v
		}
	case map[string]interface{}:
		if v["type"] == "tool" {
			if name, ok := v["name"].(string); ok && strings.TrimSpace(name) != "" {
				return map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name": name,
					},
				}
			}
		}
		return v
	default:
		return choice
	}
}

func buildAnthropicContentBlocks(message *OpenAIMessage) []AnthropicContentBlock {
	if message == nil {
		return []AnthropicContentBlock{{Type: "text", Text: ""}}
	}

	blocks := []AnthropicContentBlock{}
	text := openAIContentToString(message.Content)
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, AnthropicContentBlock{Type: "text", Text: text})
	}

	for _, call := range message.ToolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" && strings.TrimSpace(call.Function.Arguments) == "" {
			continue
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			id = generateToolCallID()
		}
		blocks = append(blocks, AnthropicContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: parseToolCallArgs(call.Function.Arguments),
		})
	}

	if message.FunctionCall != nil {
		name := strings.TrimSpace(message.FunctionCall.Name)
		if name != "" || strings.TrimSpace(message.FunctionCall.Arguments) != "" {
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    generateToolCallID(),
				Name:  name,
				Input: parseToolCallArgs(message.FunctionCall.Arguments),
			})
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, AnthropicContentBlock{Type: "text", Text: ""})
	}

	return blocks
}

func openAIContentToString(content interface{}) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		parts := []string{}
		for _, item := range v {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if typeVal, _ := block["type"].(string); typeVal == "text" {
				if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func parseToolCallArgs(args string) interface{} {
	if strings.TrimSpace(args) == "" {
		return map[string]interface{}{}
	}
	var payload interface{}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return map[string]interface{}{"arguments": args}
	}
	return payload
}

func mapStopReason(finish string, hasToolCalls bool) string {
	switch finish {
	case "length":
		return "max_tokens"
	case "stop":
		return "end_turn"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		if hasToolCalls {
			return "tool_use"
		}
		return "end_turn"
	}
}

type anthropicStreamState struct {
	messageID        string
	model            string
	started          bool
	textBlockIndex   int
	nextBlockIndex   int
	textBlockStarted bool
	toolBlocks       map[int]*anthropicToolBlock
	promptTokens     int
	completionTokens int
}

type anthropicToolBlock struct {
	index       int
	id          string
	name        string
	arguments   strings.Builder
	started     bool
	finished    bool
	openAIIndex int
}

func streamOpenAIToAnthropic(w http.ResponseWriter, body io.Reader, flusher http.Flusher, fallbackModel string) error {
	reader := bufio.NewReader(body)
	state := &anthropicStreamState{
		textBlockIndex: -1,
		nextBlockIndex: 0,
		toolBlocks:     make(map[int]*anthropicToolBlock),
	}
	var lastFinishReason *string
	var hasToolCalls bool

	for {
		data, err := readSSEData(reader)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if data == "" && errors.Is(err, io.EOF) {
			return io.EOF
		}
		if data == "" {
			if errors.Is(err, io.EOF) {
				return io.EOF
			}
			continue
		}
		if data == "[DONE]" {
			break
		}

		var chunk OpenAIStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			log.Printf("invalid stream chunk: %v data=%s", err, truncateBody([]byte(data), 2000))
			if errors.Is(err, io.EOF) {
				return io.EOF
			}
			continue
		}

		if !state.started {
			state.messageID = chunk.ID
			if state.messageID == "" {
				state.messageID = "msg_" + strconv.FormatInt(time.Now().UnixNano(), 10)
			}
			state.model = chunk.Model
			if state.model == "" {
				state.model = fallbackModel
			}
			writeAnthropicEvent(w, "message_start", map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":            state.messageID,
					"type":          "message",
					"role":          "assistant",
					"model":         state.model,
					"content":       []interface{}{},
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage": map[string]interface{}{
						"input_tokens":  0,
						"output_tokens": 0,
					},
				},
			})
			flusher.Flush()
			state.started = true
		}

		if chunk.Usage != nil {
			state.promptTokens = chunk.Usage.PromptTokens
			state.completionTokens = chunk.Usage.CompletionTokens
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil {
				lastFinishReason = choice.FinishReason
			}

			if choice.Delta.Content != "" {
				if !state.textBlockStarted {
					state.textBlockIndex = state.nextBlockIndex
					state.nextBlockIndex++
					writeAnthropicEvent(w, "content_block_start", map[string]interface{}{
						"type":  "content_block_start",
						"index": state.textBlockIndex,
						"content_block": map[string]interface{}{
							"type": "text",
							"text": "",
						},
					})
					state.textBlockStarted = true
				}
				writeAnthropicEvent(w, "content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": state.textBlockIndex,
					"delta": map[string]interface{}{
						"type": "text_delta",
						"text": choice.Delta.Content,
					},
				})
				flusher.Flush()
			}

			for _, call := range choice.Delta.ToolCalls {
				hasToolCalls = true
				block, ok := state.toolBlocks[call.Index]
				if !ok {
					block = &anthropicToolBlock{
						index:       state.nextBlockIndex,
						id:          call.ID,
						name:        call.Function.Name,
						openAIIndex: call.Index,
					}
					if block.id == "" {
						block.id = generateToolCallID()
					}
					state.toolBlocks[call.Index] = block
					state.nextBlockIndex++
				}
				if call.Function.Name != "" {
					block.name = call.Function.Name
				}
				if call.Function.Arguments != "" {
					block.arguments.WriteString(call.Function.Arguments)
				}
				if !block.started {
					writeAnthropicEvent(w, "content_block_start", map[string]interface{}{
						"type":  "content_block_start",
						"index": block.index,
						"content_block": map[string]interface{}{
							"type":  "tool_use",
							"id":    block.id,
							"name":  block.name,
							"input": map[string]interface{}{},
						},
					})
					block.started = true
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}

	if state.textBlockStarted {
		writeAnthropicEvent(w, "content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": state.textBlockIndex,
		})
	}
	for _, block := range state.toolBlocks {
		if block.started {
			args := strings.TrimSpace(block.arguments.String())
			if args != "" {
				writeAnthropicEvent(w, "content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": block.index,
					"delta": map[string]interface{}{
						"type":         "input_json_delta",
						"partial_json": args,
					},
				})
			}
		}
		if block.started && !block.finished {
			writeAnthropicEvent(w, "content_block_stop", map[string]interface{}{
				"type":  "content_block_stop",
				"index": block.index,
			})
			block.finished = true
		}
	}
	if lastFinishReason != nil {
		stopReason := mapStopReason(*lastFinishReason, hasToolCalls)
		usage := map[string]interface{}{
			"output_tokens": state.completionTokens,
		}
		if state.promptTokens > 0 {
			usage["input_tokens"] = state.promptTokens
		}
		writeAnthropicEvent(w, "message_delta", map[string]interface{}{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			"usage": usage,
		})
	}
	writeAnthropicEvent(w, "message_stop", map[string]interface{}{
		"type": "message_stop",
	})
	flusher.Flush()
	return nil
}

func readSSEData(r *bufio.Reader) (string, error) {
	var dataLines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		if len(line) == 0 && errors.Is(err, io.EOF) {
			if len(dataLines) == 0 {
				return "", io.EOF
			}
			return strings.Join(dataLines, "\n"), io.EOF
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return strings.Join(dataLines, "\n"), err
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			dataLines = append(dataLines, data)
		}
		if errors.Is(err, io.EOF) {
			return strings.Join(dataLines, "\n"), io.EOF
		}
	}
}

func writeAnthropicEvent(w http.ResponseWriter, event string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("encode stream event failed: %v", err)
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func generateToolCallID() string {
	return "call_" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func getEnv(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}

func getEnvFirst(keys []string, fallback string) string {
	for _, key := range keys {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return val
		}
	}
	return fallback
}

func truncateBody(body []byte, limit int) string {
	if limit <= 0 || len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "...(truncated)"
}
