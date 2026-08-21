package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStripImageParts_PureStringContent 测试纯字符串 content 的消息不会被修改。
func TestStripImageParts_PureStringContent(t *testing.T) {
	body := []byte(`{
		"model": "low-model",
		"messages": [
			{"role": "user", "content": "please fix this bug"}
		]
	}`)

	result, modified, err := StripImageParts(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if modified {
		t.Fatal("expected no modification for pure string content")
	}
	if string(result) != string(body) {
		t.Fatal("expected body unchanged for pure string content")
	}
}

// TestStripImageParts_MixedParts 测试 content 数组混合 text 与 image_url 时
// 只移除 image_url part，保留 text part。
func TestStripImageParts_MixedParts(t *testing.T) {
	body := []byte(`{
		"model": "low-model",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "what does this error mean?"},
				{"type": "image_url", "image_url": {"url": "https://example.com/error.png"}}
			]}
		]
	}`)

	result, modified, err := StripImageParts(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatal("expected modification for mixed parts")
	}

	var req ChatCompletionRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	var parts []ContentPart
	if err := json.Unmarshal(req.Messages[0].Content, &parts); err != nil {
		t.Fatalf("parse content parts: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part after strip, got %d", len(parts))
	}
	if parts[0].Type != "text" {
		t.Fatalf("expected remaining part type 'text', got %q", parts[0].Type)
	}
}

// TestStripImageParts_OnlyImage 测试 content 数组只有 image_url 时
// 替换为占位文本字符串，避免空 content。
func TestStripImageParts_OnlyImage(t *testing.T) {
	body := []byte(`{
		"model": "low-model",
		"messages": [
			{"role": "user", "content": [
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,abc123"}}
			]}
		]
	}`)

	result, modified, err := StripImageParts(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatal("expected modification for image-only parts")
	}

	var req ChatCompletionRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	var content string
	if err := json.Unmarshal(req.Messages[0].Content, &content); err != nil {
		t.Fatalf("expected placeholder string content, got %s", string(req.Messages[0].Content))
	}
	if !strings.Contains(content, "图片内容已由前序支持多模态的模型转写处理") {
		t.Fatalf("expected placeholder text, got %q", content)
	}
}

// TestStripImageParts_MultipleMessages 测试多条消息时只修改含图片的消息，
// 其余消息（含 assistant tool_calls 消息）原样保留。
func TestStripImageParts_MultipleMessages(t *testing.T) {
	body := []byte(`{
		"model": "low-model",
		"messages": [
			{"role": "system", "content": "you are a coder"},
			{"role": "user", "content": [
				{"type": "image_url", "image_url": {"url": "https://example.com/a.png"}},
				{"type": "text", "text": "check this"}
			]},
			{"role": "assistant", "content": "processing", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "result ok"}
		]
	}`)

	result, modified, err := StripImageParts(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !modified {
		t.Fatal("expected modification for messages with image")
	}

	var req ChatCompletionRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(req.Messages))
	}

	// 第一条 user 消息：image_url 被移除，仅剩 text part
	var parts []ContentPart
	if err := json.Unmarshal(req.Messages[1].Content, &parts); err != nil {
		t.Fatalf("parse user content parts: %v", err)
	}
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("expected only text part after strip, got %+v", parts)
	}

	// assistant 消息的 tool_calls 必须原样保留
	if len(req.Messages[2].ToolCalls) != 1 {
		t.Fatalf("expected tool_calls preserved, got %d", len(req.Messages[2].ToolCalls))
	}
	if req.Messages[2].ToolCalls[0].ID != "call_1" {
		t.Fatalf("expected tool_call id preserved, got %q", req.Messages[2].ToolCalls[0].ID)
	}

	// tool 消息原样保留
	if req.Messages[3].Role != "tool" || req.Messages[3].ToolCallID != "call_1" {
		t.Fatalf("expected tool message preserved, got %+v", req.Messages[3])
	}
}

// TestStripImageParts_NoImage 测试所有消息均不含图片时返回未修改。
func TestStripImageParts_NoImage(t *testing.T) {
	body := []byte(`{
		"model": "low-model",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "user", "content": [{"type": "text", "text": "only text parts"}]}
		]
	}`)

	_, modified, err := StripImageParts(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if modified {
		t.Fatal("expected no modification when no image_url parts")
	}
}

// TestStripImageParts_InvalidBody 测试非法 JSON 返回错误。
func TestStripImageParts_InvalidBody(t *testing.T) {
	_, _, err := StripImageParts([]byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON body")
	}
}

// TestStripImageParts_MissingMessages 测试缺少 messages 字段返回错误。
func TestStripImageParts_MissingMessages(t *testing.T) {
	_, _, err := StripImageParts([]byte(`{"model": "low-model"}`))
	if err == nil {
		t.Fatal("expected error when messages is missing")
	}
}
