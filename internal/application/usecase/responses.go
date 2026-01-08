package usecase

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"api-conver/internal/domain/model"
)

type responsesStreamState struct {
	responseID  string
	model       string
	created     int64
	createdSent bool
	text        strings.Builder
	toolCalls   map[int]*toolCallState
	usage       *model.OpenAIUsage
}

type toolCallState struct {
	id        string
	name      string
	arguments strings.Builder
}

func (u *ProxyUseCase) buildChatRequestFromResponses(payload map[string]interface{}, alias string) (map[string]interface{}, bool, error) {
	chatReq := map[string]interface{}{}

	modelVal, _ := payload["model"].(string)
	if strings.TrimSpace(modelVal) == "" {
		modelVal = getDefaultModel(alias)
	}
	chatReq["model"] = modelVal

	stream := false
	if rawStream, ok := payload["stream"]; ok {
		if v, ok := rawStream.(bool); ok {
			stream = v
		}
	}
	chatReq["stream"] = stream
	if stream {
		chatReq["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	instructions, _ := payload["instructions"].(string)
	if strings.TrimSpace(instructions) != "" {
		chatReq["messages"] = []map[string]interface{}{
			{"role": "system", "content": instructions},
		}
	}

	messages, err := parseResponsesInput(payload["input"])
	if err != nil {
		return nil, false, err
	}
	if len(messages) > 0 {
		if existing, ok := chatReq["messages"].([]map[string]interface{}); ok {
			chatReq["messages"] = append(existing, messages...)
		} else {
			chatReq["messages"] = messages
		}
	}
	if _, ok := chatReq["messages"]; !ok {
		return nil, false, errors.New("missing input messages")
	}

	copyIfPresent(payload, chatReq, "temperature")
	copyIfPresent(payload, chatReq, "top_p")
	copyIfPresent(payload, chatReq, "presence_penalty")
	copyIfPresent(payload, chatReq, "frequency_penalty")
	copyIfPresent(payload, chatReq, "seed")
	copyIfPresent(payload, chatReq, "response_format")
	copyIfPresent(payload, chatReq, "tools")
	copyIfPresent(payload, chatReq, "tool_choice")
	copyIfPresent(payload, chatReq, "parallel_tool_calls")

	if maxOutputTokens, ok := payload["max_output_tokens"]; ok {
		chatReq["max_tokens"] = maxOutputTokens
	} else if maxTokens, ok := payload["max_tokens"]; ok {
		chatReq["max_tokens"] = maxTokens
	}

	return chatReq, stream, nil
}

func parseResponsesInput(input interface{}) ([]map[string]interface{}, error) {
	if input == nil {
		return nil, nil
	}

	items := []interface{}{input}
	if list, ok := input.([]interface{}); ok {
		items = list
	}

	messages := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		msg, ok := item.(map[string]interface{})
		if !ok {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				messages = append(messages, map[string]interface{}{
					"role":    "user",
					"content": text,
				})
			}
			continue
		}

		itemType, _ := msg["type"].(string)
		if itemType == "tool_output" || itemType == "tool_result" {
			toolMsg := buildToolMessage(msg)
			if toolMsg != nil {
				messages = append(messages, toolMsg)
			}
			continue
		}

		role, _ := msg["role"].(string)
		if strings.TrimSpace(role) == "" {
			role = "user"
		}
		content := extractTextFromContent(msg["content"])
		message := map[string]interface{}{
			"role":    role,
			"content": content,
		}
		if role == "tool" {
			if toolID, ok := msg["tool_call_id"].(string); ok && strings.TrimSpace(toolID) != "" {
				message["tool_call_id"] = toolID
			}
		}
		if toolCalls, ok := msg["tool_calls"]; ok {
			message["tool_calls"] = toolCalls
		}
		if functionCall, ok := msg["function_call"]; ok {
			message["function_call"] = functionCall
		}
		messages = append(messages, message)
	}

	return messages, nil
}

func buildToolMessage(msg map[string]interface{}) map[string]interface{} {
	toolID := ""
	if v, ok := msg["tool_call_id"].(string); ok {
		toolID = v
	}
	if toolID == "" {
		if v, ok := msg["call_id"].(string); ok {
			toolID = v
		}
	}
	if toolID == "" {
		if v, ok := msg["id"].(string); ok {
			toolID = v
		}
	}
	content := ""
	if v, ok := msg["output"].(string); ok {
		content = v
	} else if v, ok := msg["content"].(string); ok {
		content = v
	} else {
		content = extractTextFromContent(msg["content"])
	}
	if toolID == "" && strings.TrimSpace(content) == "" {
		return nil
	}
	message := map[string]interface{}{
		"role":    "tool",
		"content": content,
	}
	if toolID != "" {
		message["tool_call_id"] = toolID
	}
	return message
}

func extractTextFromContent(content interface{}) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			block, ok := item.(map[string]interface{})
			if !ok {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
				continue
			}
			blockType, _ := block["type"].(string)
			if blockType != "" && blockType != "input_text" && blockType != "text" && blockType != "output_text" {
				continue
			}
			if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
	}
	return ""
}

func (u *ProxyUseCase) convertOpenAIResponseToResponses(openAIResp model.OpenAIResponse, reqModel string) map[string]interface{} {
	modelName := openAIResp.Model
	if strings.TrimSpace(modelName) == "" {
		modelName = reqModel
	}

	var message *model.OpenAIMessage
	var finishReason string
	if len(openAIResp.Choices) > 0 {
		message = openAIResp.Choices[0].Message
		finishReason = openAIResp.Choices[0].FinishReason
	}

	outputItems := []interface{}{}
	messageItem := map[string]interface{}{
		"id":      responseMessageID(openAIResp.ID),
		"type":    "message",
		"role":    "assistant",
		"content": []interface{}{},
	}

	if message != nil {
		text := strings.TrimSpace(u.converter.OpenAIContentToString(message.Content))
		if text != "" {
			messageItem["content"] = []interface{}{
				map[string]interface{}{
					"type": "output_text",
					"text": text,
				},
			}
		}

		toolCallItems := buildResponsesToolCallItems(message.ToolCalls)
		if len(toolCallItems) > 0 {
			messageItem["tool_calls"] = toolCallItems
			for _, item := range toolCallItems {
				outputItems = append(outputItems, item)
			}
		}
	}

	outputItems = append([]interface{}{messageItem}, outputItems...)

	response := map[string]interface{}{
		"id":      openAIResp.ID,
		"object":  "response",
		"created": openAIResp.Created,
		"model":   modelName,
		"output":  outputItems,
		"usage": map[string]interface{}{
			"input_tokens":  openAIResp.Usage.PromptTokens,
			"output_tokens": openAIResp.Usage.CompletionTokens,
			"total_tokens":  openAIResp.Usage.TotalTokens,
		},
	}

	if finishReason != "" {
		response["finish_reason"] = finishReason
	}

	return response
}

func buildResponsesToolCallItems(toolCalls []model.OpenAIToolCall) []map[string]interface{} {
	if len(toolCalls) == 0 {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(toolCalls))
	for _, call := range toolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" && strings.TrimSpace(call.Function.Arguments) == "" {
			continue
		}
		item := map[string]interface{}{
			"id":        call.ID,
			"call_id":   call.ID,
			"type":      "tool_call",
			"name":      name,
			"arguments": parseToolCallArguments(call.Function.Arguments),
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func parseToolCallArguments(raw string) interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]interface{}{}
	}
	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw
	}
	return payload
}

func responseMessageID(responseID string) string {
	if strings.TrimSpace(responseID) == "" {
		return fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	return responseID + "_msg"
}

func copyIfPresent(src map[string]interface{}, dst map[string]interface{}, key string) {
	if val, ok := src[key]; ok {
		dst[key] = val
	}
}

func (u *ProxyUseCase) streamOpenAIToResponses(c *gin.Context, resp *http.Response, reqModel string) error {
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	state := &responsesStreamState{
		model:     reqModel,
		toolCalls: map[int]*toolCallState{},
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var chunk model.OpenAIStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if state.responseID == "" && chunk.ID != "" {
			state.responseID = chunk.ID
		}
		if state.model == "" && chunk.Model != "" {
			state.model = chunk.Model
		}
		if state.created == 0 && chunk.Created != 0 {
			state.created = chunk.Created
		}
		if chunk.Usage != nil {
			state.usage = chunk.Usage
		}

		if !state.createdSent {
			if err := writeResponseCreated(c, state); err != nil {
				return err
			}
			state.created = ensureCreated(state.created)
			state.createdSent = true
		}

		for _, choice := range chunk.Choices {
			delta := choice.Delta
			if delta.Content != "" {
				state.text.WriteString(delta.Content)
				if err := writeOutputTextDelta(c, state.responseID, delta.Content); err != nil {
					return err
				}
			}
			for _, call := range delta.ToolCalls {
				toolState := state.toolCalls[call.Index]
				if toolState == nil {
					toolState = &toolCallState{}
					state.toolCalls[call.Index] = toolState
				}
				if call.ID != "" {
					toolState.id = call.ID
				}
				if call.Function.Name != "" {
					toolState.name = call.Function.Name
				}
				if call.Function.Arguments != "" {
					toolState.arguments.WriteString(call.Function.Arguments)
				}
				if err := writeToolCallDelta(c, state.responseID, toolState, call.Function.Arguments); err != nil {
					return err
				}
			}
		}
	}

	return writeResponseCompleted(c, state)
}

func ensureCreated(created int64) int64 {
	if created != 0 {
		return created
	}
	return time.Now().Unix()
}

func writeResponseCreated(c *gin.Context, state *responsesStreamState) error {
	response := map[string]interface{}{
		"id":      state.responseID,
		"object":  "response",
		"created": ensureCreated(state.created),
		"model":   state.model,
		"output":  []interface{}{},
	}
	payload := map[string]interface{}{
		"type":     "response.created",
		"response": response,
	}
	return writeSSE(c, "response.created", payload)
}

func writeOutputTextDelta(c *gin.Context, responseID string, delta string) error {
	payload := map[string]interface{}{
		"type":          "response.output_text.delta",
		"response_id":   responseID,
		"output_index":  0,
		"content_index": 0,
		"delta":         delta,
	}
	return writeSSE(c, "response.output_text.delta", payload)
}

func writeToolCallDelta(c *gin.Context, responseID string, call *toolCallState, delta string) error {
	if call == nil {
		return nil
	}
	payload := map[string]interface{}{
		"type":        "response.tool_call.delta",
		"response_id": responseID,
		"tool_call": map[string]interface{}{
			"id":        call.id,
			"name":      call.name,
			"arguments": delta,
		},
	}
	return writeSSE(c, "response.tool_call.delta", payload)
}

func writeResponseCompleted(c *gin.Context, state *responsesStreamState) error {
	message := map[string]interface{}{
		"id":      responseMessageID(state.responseID),
		"type":    "message",
		"role":    "assistant",
		"content": []interface{}{},
	}

	text := strings.TrimSpace(state.text.String())
	if text != "" {
		message["content"] = []interface{}{
			map[string]interface{}{
				"type": "output_text",
				"text": text,
			},
		}
	}

	output := []interface{}{message}
	toolItems := buildResponsesToolCallItemsFromState(state.toolCalls)
	if len(toolItems) > 0 {
		message["tool_calls"] = toolItems
		for _, item := range toolItems {
			output = append(output, item)
		}
	}

	response := map[string]interface{}{
		"id":      state.responseID,
		"object":  "response",
		"created": ensureCreated(state.created),
		"model":   state.model,
		"output":  output,
	}

	if state.usage != nil {
		response["usage"] = map[string]interface{}{
			"input_tokens":  state.usage.PromptTokens,
			"output_tokens": state.usage.CompletionTokens,
			"total_tokens":  state.usage.TotalTokens,
		}
	}

	payload := map[string]interface{}{
		"type":     "response.completed",
		"response": response,
	}
	if err := writeSSE(c, "response.completed", payload); err != nil {
		return err
	}
	_, err := c.Writer.Write([]byte("data: [DONE]\n\n"))
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return err
}

func buildResponsesToolCallItemsFromState(toolCalls map[int]*toolCallState) []map[string]interface{} {
	if len(toolCalls) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	items := make([]map[string]interface{}, 0, len(indexes))
	for _, index := range indexes {
		call := toolCalls[index]
		if call == nil {
			continue
		}
		args := strings.TrimSpace(call.arguments.String())
		items = append(items, map[string]interface{}{
			"id":        call.id,
			"call_id":   call.id,
			"type":      "tool_call",
			"name":      call.name,
			"arguments": parseToolCallArguments(args),
		})
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func writeSSE(c *gin.Context, event string, payload map[string]interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte("event: " + event + "\n")); err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := c.Writer.Write(body); err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte("\n\n")); err != nil {
		return err
	}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
