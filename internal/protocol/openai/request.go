package openai

import (
	"encoding/json"
	"fmt"
)

type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type Message struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ReasoningContent json.RawMessage `json:"reasoning_content"`
	ToolCalls        []ToolCall      `json:"tool_calls"`
	ToolCallID       string          `json:"tool_call_id"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ContentPart 表示消息 content 数组中的单个部分。
// 用于多模态消息解析，提取 type 字段判断是否包含图片。
type ContentPart struct {
	Type string `json:"type"`
}

func ParseRequest(rawBody []byte) (*ChatCompletionRequest, error) {
	var req ChatCompletionRequest

	if err := json.Unmarshal(rawBody, &req); err != nil {
		return nil, fmt.Errorf("parse chat completion request: %w", err)
	}

	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}

	return &req, nil
}

func RewriteModel(rawBody []byte, model string) ([]byte, error) {
	var body map[string]json.RawMessage

	if err := json.Unmarshal(rawBody, &body); err != nil {
		return nil, fmt.Errorf("parse request body: %w", err)
	}

	modelValue, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("encode model: %w", err)
	}

	body["model"] = modelValue

	result, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}

	return result, nil
}
