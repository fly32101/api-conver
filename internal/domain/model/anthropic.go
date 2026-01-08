package model

// Anthropic Models

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
