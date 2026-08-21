package proxy

import (
	"encoding/json"
	"fmt"

	"smart-coder-switch/internal/protocol/openai"
)

type responsesInputItemType struct {
	Type string `json:"type"`
	Role string `json:"role"`
}

// responsesItemKind 返回 item 的实际类型。
// OpenAI Responses 规范的消息条目只有 role 而没有 type 字段，
// 因此当 type 缺失但存在 role 时按 message 处理。
func responsesItemKind(item responsesInputItemType) string {
	if item.Type != "" {
		return item.Type
	}
	if item.Role != "" {
		return "message"
	}
	return item.Type
}

type responsesInputContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// responsesParseInputItems 将 Responses 的 input 字段解析为原始 item 列表。
// input 为字符串时包装为单条文本条目，保持原有请求格式不变。
func responsesParseInputItems(input json.RawMessage) ([]json.RawMessage, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("input is required")
	}

	if !json.Valid(input) {
		return nil, fmt.Errorf("input is not valid json")
	}

	var text string
	if err := json.Unmarshal(input, &text); err == nil {
		item, err := json.Marshal(map[string]string{
			"type": "text",
			"text": text,
		})
		if err != nil {
			return nil, fmt.Errorf("encode input text item: %w", err)
		}
		return []json.RawMessage{item}, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("parse input items: %w", err)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("input items must not be empty")
	}

	return items, nil
}

// normalizeResponsesMessages 将 Responses input 归一化为 Chat 路由需要的
// []openai.Message 视图，供现有 routing 函数直接复用。
func normalizeResponsesMessages(items []json.RawMessage) ([]openai.Message, error) {
	var out []openai.Message

	for i, raw := range items {
		if len(raw) == 0 {
			continue
		}

		var kind responsesInputItemType
		if err := json.Unmarshal(raw, &kind); err != nil {
			return nil, fmt.Errorf("parse input item %d: %w", i, err)
		}

		switch responsesItemKind(kind) {
		case "message":
			msg, err := normalizeResponsesRoleMessage(raw)
			if err != nil {
				return nil, fmt.Errorf("normalize input item %d: %w", i, err)
			}
			if msg != nil {
				out = append(out, *msg)
			}

		case "function_call":
			var item struct {
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				return nil, fmt.Errorf("parse function_call item %d: %w", i, err)
			}
			out = append(out, openai.Message{
				Role: "assistant",
				ToolCalls: []openai.ToolCall{
					{
						ID:   item.CallID,
						Type: "function",
						Function: openai.ToolFunction{
							Name:      item.Name,
							Arguments: item.Arguments,
						},
					},
				},
			})

		case "function_call_output":
			var item struct {
				CallID string `json:"call_id"`
				Output string `json:"output"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				return nil, fmt.Errorf("parse function_call_output item %d: %w", i, err)
			}
			out = append(out, openai.Message{
				Role:       "tool",
				ToolCallID: item.CallID,
				Content:    marshalStringContent(item.Output),
			})

		default:
			// input_text / input_image / 未知扩展字段统一按 user content 归一化。
			text := extractResponsesItemText(raw, responsesItemKind(kind))
			out = append(out, openai.Message{
				Role:    "user",
				Content: marshalStringContent(text),
			})
		}
	}

	if len(out) == 0 {
		out = nil
	}

	return out, nil
}

func normalizeResponsesRoleMessage(raw json.RawMessage) (*openai.Message, error) {
	var item struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}

	switch item.Role {
	case "user", "assistant":
	case "system", "developer":
		// system/developer 消息参与路由计数时与 Chat 侧对齐为 user。
		item.Role = "user"
	default:
		item.Role = "user"
	}

	content, err := normalizeResponsesContentParts(item.Content)
	if err != nil {
		return nil, err
	}

	if content == nil {
		return nil, nil
	}

	return &openai.Message{
		Role:    item.Role,
		Content: content,
	}, nil
}

func normalizeResponsesContentParts(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return marshalStringContent(""), nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return marshalStringContent(text), nil
	}

	var parts []responsesInputContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return marshalStringContent(""), nil
	}

	var normalized []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	for _, p := range parts {
		switch {
		case p.Type == "input_text":
			normalized = append(normalized, struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			}{Type: "text", Text: p.Text})
		case p.Type == "output_text":
			normalized = append(normalized, struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			}{Type: "text", Text: p.Text})
		case p.Type == "input_image" && p.ImageURL != "":
			normalized = append(normalized, struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			}{Type: "image_url"})
		default:
			if p.Text != "" {
				normalized = append(normalized, struct {
					Type string `json:"type"`
					Text string `json:"text,omitempty"`
				}{Type: "text", Text: p.Text})
			}
		}
	}

	if len(normalized) == 0 {
		return marshalStringContent(""), nil
	}

	return json.Marshal(normalized)
}

func extractResponsesItemText(raw json.RawMessage, itemType string) string {
	if itemType == "text" || itemType == "input_text" {
		var item struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &item); err == nil {
			return item.Text
		}
	}

	if itemType == "output_text" {
		var item struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &item); err == nil {
			return item.Text
		}
	}

	if itemType == "input_image" {
		return "[图片内容已由前序支持多模态的模型转写处理]"
	}

	if itemType == "reasoning" {
		var item struct {
			Summary []struct {
				Text string `json:"text"`
			} `json:"summary"`
		}
		if err := json.Unmarshal(raw, &item); err == nil {
			texts := make([]string, 0, len(item.Summary))
			for _, s := range item.Summary {
				if s.Text != "" {
					texts = append(texts, s.Text)
				}
			}
			if len(texts) > 0 {
				texts[0] = "[reasoning] " + texts[0]
			}
			return joinTexts(texts)
		}
	}

	return ""
}

func joinTexts(texts []string) string {
	if len(texts) == 0 {
		return ""
	}

	var b []byte
	for _, t := range texts {
		if t == "" {
			continue
		}
		if len(b) > 0 {
			b = append(b, '\n')
		}
		b = append(b, t...)
	}

	return string(b)
}

func marshalStringContent(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// lastResponsesItemText 取 Responses input 最后一条的文本，供续接 / DIRECT 判断。
// 与 Chat 路径的 IsLatestUserInputMessage 语义一致：只看最后一条 item，
// 若为 user/developer 消息或 text/input_text 条目则返回其文本，否则返回空。
// 不向后扫描——tool 输出、assistant 回复等非用户输入条目视为对话延续，跳过 DIRECT。
func lastResponsesItemText(items []json.RawMessage) string {
	if len(items) == 0 {
		return ""
	}

	last := items[len(items)-1]

	var kind responsesInputItemType
	if err := json.Unmarshal(last, &kind); err != nil {
		return ""
	}

	switch responsesItemKind(kind) {
	case "message":
		var item struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(last, &item); err != nil {
			return ""
		}
		if item.Role == "user" || item.Role == "developer" {
			return extractResponsesContentText(item.Content)
		}
	case "text", "input_text":
		var item struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(last, &item); err == nil {
			return item.Text
		}
	}

	return ""
}

// isLastResponsesItemUserInput 检查 Responses input 最后一条是否代表人类用户的真实输入。
// 与 Chat 路径中最后一条 message role=user 的语义对齐：
// tool 输出、assistant 回复等非用户输入条目返回 false。
func isLastResponsesItemUserInput(items []json.RawMessage) bool {
	if len(items) == 0 {
		return false
	}

	last := items[len(items)-1]

	var kind responsesInputItemType
	if err := json.Unmarshal(last, &kind); err != nil {
		return false
	}

	switch responsesItemKind(kind) {
	case "message":
		var item struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(last, &item); err != nil {
			return false
		}
		return item.Role == "user" || item.Role == "developer"
	case "text", "input_text":
		return true
	}

	return false
}

func extractResponsesContentText(raw json.RawMessage) string {
	if len(raw) == 0 || !json.Valid(raw) {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	var parts []responsesInputContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}

	for _, p := range parts {
		if p.Type == "input_text" || p.Type == "text" || p.Type == "output_text" {
			if p.Text != "" {
				return p.Text
			}
		}
	}

	return ""
}

// hasResponsesItemImages 检查 Responses input 是否包含图片语义，供 DIRECT 图片提示判断。
func hasResponsesItemImages(items []json.RawMessage) bool {
	for _, raw := range items {
		var kind responsesInputItemType
		if err := json.Unmarshal(raw, &kind); err != nil {
			continue
		}

		itemKind := responsesItemKind(kind)
		if itemKind == "input_image" {
			return true
		}

		if itemKind == "message" {
			var item struct {
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(raw, &item); err == nil {
				if normalizedContainsImage(item.Content) {
					return true
				}
			}
		}
	}

	return false
}

func normalizedContainsImage(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return false
	}

	var parts []responsesInputContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return false
	}

	for _, p := range parts {
		if p.Type == "input_image" {
			return true
		}
	}

	return false
}
