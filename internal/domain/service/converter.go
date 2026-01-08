package service

import (
	"encoding/json"
	"strings"

	"api-conver/internal/domain/model"
)

// Converter handles protocol conversion between Anthropic and OpenAI
type Converter struct{}

func NewConverter() *Converter {
	return &Converter{}
}

// ConvertAnthropicToOpenAIMessages converts Anthropic messages to OpenAI format
func (c *Converter) ConvertAnthropicToOpenAIMessages(system interface{}, messages []model.AnthropicMessage) ([]map[string]interface{}, error) {
	openAIMessages := make([]map[string]interface{}, 0, len(messages)+1)
	sysText := c.FlattenAnthropicText(system)
	if strings.TrimSpace(sysText) != "" {
		openAIMessages = append(openAIMessages, map[string]interface{}{
			"role":    "system",
			"content": sysText,
		})
	}

	for _, msg := range messages {
		converted, err := c.ConvertAnthropicMessage(msg)
		if err != nil {
			return nil, err
		}
		openAIMessages = append(openAIMessages, converted...)
	}

	return openAIMessages, nil
}

// ConvertAnthropicMessage converts a single Anthropic message to OpenAI format
func (c *Converter) ConvertAnthropicMessage(msg model.AnthropicMessage) ([]map[string]interface{}, error) {
	textParts, toolCalls, toolResults := c.ParseAnthropicContent(msg.Content)

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

// ParseAnthropicContent parses Anthropic content into text, tool calls, and tool results
func (c *Converter) ParseAnthropicContent(content interface{}) ([]string, []map[string]interface{}, []map[string]interface{}) {
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
			c.parseAnthropicBlock(block, &textParts, &toolCalls, &toolResults)
		}
	case map[string]interface{}:
		c.parseAnthropicBlock(v, &textParts, &toolCalls, &toolResults)
	case nil:
		return textParts, toolCalls, toolResults
	default:
		fallback := c.FlattenAnthropicText(v)
		if strings.TrimSpace(fallback) != "" {
			textParts = append(textParts, fallback)
		}
	}

	return textParts, toolCalls, toolResults
}

func (c *Converter) parseAnthropicBlock(block map[string]interface{}, textParts *[]string, toolCalls *[]map[string]interface{}, toolResults *[]map[string]interface{}) {
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
			id = GenerateToolCallID()
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
			"content":      c.StringifyToolResult(block["content"]),
		})
	default:
		text, _ := block["text"].(string)
		if strings.TrimSpace(text) != "" {
			*textParts = append(*textParts, text)
		}
	}
}

// FlattenAnthropicText converts Anthropic content to plain text
func (c *Converter) FlattenAnthropicText(v interface{}) string {
	parts := c.ExtractTextParts(v)
	return strings.Join(parts, "\n")
}

// ExtractTextParts extracts text parts from various content types
func (c *Converter) ExtractTextParts(v interface{}) []string {
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

// StringifyToolResult converts tool result content to string
func (c *Converter) StringifyToolResult(content interface{}) string {
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

// ConvertAnthropicTools converts Anthropic tools to OpenAI format
func (c *Converter) ConvertAnthropicTools(tools []model.AnthropicToolDefinition) []map[string]interface{} {
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

// ConvertAnthropicToolChoice converts Anthropic tool choice to OpenAI format
func (c *Converter) ConvertAnthropicToolChoice(choice interface{}) interface{} {
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

// BuildAnthropicContentBlocks converts OpenAI message to Anthropic content blocks
func (c *Converter) BuildAnthropicContentBlocks(message *model.OpenAIMessage) []model.AnthropicContentBlock {
	if message == nil {
		return []model.AnthropicContentBlock{{Type: "text", Text: ""}}
	}

	blocks := []model.AnthropicContentBlock{}
	text := c.OpenAIContentToString(message.Content)
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, model.AnthropicContentBlock{Type: "text", Text: text})
	}

	for _, call := range message.ToolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" && strings.TrimSpace(call.Function.Arguments) == "" {
			continue
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			id = GenerateToolCallID()
		}
		blocks = append(blocks, model.AnthropicContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: c.ParseToolCallArgs(call.Function.Arguments),
		})
	}

	if message.FunctionCall != nil {
		name := strings.TrimSpace(message.FunctionCall.Name)
		if name != "" || strings.TrimSpace(message.FunctionCall.Arguments) != "" {
			blocks = append(blocks, model.AnthropicContentBlock{
				Type:  "tool_use",
				ID:    GenerateToolCallID(),
				Name:  name,
				Input: c.ParseToolCallArgs(message.FunctionCall.Arguments),
			})
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, model.AnthropicContentBlock{Type: "text", Text: ""})
	}

	return blocks
}

// OpenAIContentToString converts OpenAI content to string
func (c *Converter) OpenAIContentToString(content interface{}) string {
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

// ParseToolCallArgs parses tool call arguments
func (c *Converter) ParseToolCallArgs(args string) interface{} {
	if strings.TrimSpace(args) == "" {
		return map[string]interface{}{}
	}
	var payload interface{}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return map[string]interface{}{"arguments": args}
	}
	return payload
}

// MapStopReason maps OpenAI stop reason to Anthropic format
func (c *Converter) MapStopReason(finish string, hasToolCalls bool) string {
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
