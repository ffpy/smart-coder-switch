package openai

import (
	"encoding/json"
	"testing"
)

const testPlaceholder = "继续处理当前工具调用并基于工具结果完成任务。"

// buildBody 构造带 messages 的请求体。
func buildBody(msgs []map[string]interface{}) []byte {
	body := map[string]interface{}{
		"model":    "deepseek-chat",
		"messages": msgs,
	}
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return b
}

// toolAssistant 构造一条带 tool_calls 的 assistant 消息，reasoningContent 传入 nil 表示缺字段。
func toolAssistant(id string, reasoningContent interface{}) map[string]interface{} {
	m := map[string]interface{}{
		"role":       "assistant",
		"content":    "",
		"tool_calls": []map[string]interface{}{{"id": id, "type": "function"}},
	}
	if reasoningContent != nil {
		m["reasoning_content"] = reasoningContent
	}
	return m
}

func userMsg(content string) map[string]interface{} {
	return map[string]interface{}{"role": "user", "content": content}
}

func toolMsg(id string) map[string]interface{} {
	return map[string]interface{}{"role": "tool", "tool_call_id": id, "content": "result"}
}

// parseMsgs 解析修改后的请求体中的 messages。
func parseMsgs(t *testing.T, body []byte) []map[string]interface{} {
	t.Helper()
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	var msgs []map[string]interface{}
	if err := json.Unmarshal(req["messages"], &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	return msgs
}

// getReasoning 读取消息的 reasoning_content 字段，不存在时 ok=false。
func getReasoning(m map[string]interface{}) (string, bool) {
	v, ok := m["reasoning_content"]
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

func TestFixMissingReasoningContent_OnlyRecentSegment(t *testing.T) {
	// 历史有两段工具调用，中间由 user 消息分隔：
	//   user → assistant(tool_calls, 缺) → tool → user → assistant(tool_calls, 缺) → tool
	// 只应修复最近一个 user 之后的 assistant，更早一段保持不变。
	body := buildBody([]map[string]interface{}{
		userMsg("任务1"),
		toolAssistant("call_old_1", nil),
		toolMsg("call_old_1"),
		userMsg("继续"),
		toolAssistant("call_new_1", nil),
		toolMsg("call_new_1"),
	})

	fixed, modified, err := FixMissingReasoningContent(body, testPlaceholder)
	if err != nil {
		t.Fatalf("FixMissingReasoningContent error: %v", err)
	}
	if !modified {
		t.Fatal("expected modified=true")
	}

	msgs := parseMsgs(t, fixed)
	if len(msgs) != 6 {
		t.Fatalf("unexpected message count: %d", len(msgs))
	}

	// 最近一段的 assistant（index 4）应被补上占位文本
	if got, ok := getReasoning(msgs[4]); !ok || got != testPlaceholder {
		t.Fatalf("recent segment assistant should be fixed, got reasoning=%q ok=%v", got, ok)
	}

	// 更早一段的 assistant（index 1）不应被补
	if _, ok := getReasoning(msgs[1]); ok {
		t.Fatalf("earlier segment assistant should NOT be fixed, got reasoning present")
	}
}

func TestFixMissingReasoningContent_NoUserSeparator(t *testing.T) {
	// 连续工具调用段内没有 user 分隔：所有缺失的 assistant 都应被补（回归原行为）。
	body := buildBody([]map[string]interface{}{
		toolAssistant("call_1", nil),
		toolMsg("call_1"),
		toolAssistant("call_2", nil),
		toolMsg("call_2"),
	})

	fixed, modified, err := FixMissingReasoningContent(body, testPlaceholder)
	if err != nil {
		t.Fatalf("FixMissingReasoningContent error: %v", err)
	}
	if !modified {
		t.Fatal("expected modified=true")
	}

	msgs := parseMsgs(t, fixed)
	if len(msgs) != 4 {
		t.Fatalf("unexpected message count: %d", len(msgs))
	}
	for i := 0; i < len(msgs); i += 2 {
		if got, ok := getReasoning(msgs[i]); !ok || got != testPlaceholder {
			t.Fatalf("assistant at %d should be fixed, got reasoning=%q ok=%v", i, got, ok)
		}
	}
}

func TestFixMissingReasoningContent_KeepsExisting(t *testing.T) {
	// 已有非 null reasoning_content 的 assistant 不应被覆盖。
	body := buildBody([]map[string]interface{}{
		userMsg("任务"),
		toolAssistant("call_1", "真实思考内容"),
		toolMsg("call_1"),
	})

	fixed, modified, err := FixMissingReasoningContent(body, testPlaceholder)
	if err != nil {
		t.Fatalf("FixMissingReasoningContent error: %v", err)
	}
	if modified {
		t.Fatal("expected modified=false when reasoning_content already present")
	}
	if string(fixed) != string(body) {
		t.Fatal("body should be returned unchanged")
	}
}

func TestFixMissingReasoningContent_NullReasoning(t *testing.T) {
	// reasoning_content 显式为 null 时也应被补。
	body := buildBody([]map[string]interface{}{
		userMsg("任务"),
		toolAssistant("call_1", nil),
		toolMsg("call_1"),
	})
	// 显式写入 null 字段
	var msgs []map[string]interface{}
	_ = json.Unmarshal(mustJSON(body)["messages"], &msgs)
	msgs[1]["reasoning_content"] = nil
	body = buildBody(msgs)

	fixed, modified, err := FixMissingReasoningContent(body, testPlaceholder)
	if err != nil {
		t.Fatalf("FixMissingReasoningContent error: %v", err)
	}
	if !modified {
		t.Fatal("expected modified=true for null reasoning_content")
	}
	out := parseMsgs(t, fixed)
	if got, ok := getReasoning(out[1]); !ok || got != testPlaceholder {
		t.Fatalf("null reasoning should be fixed, got reasoning=%q ok=%v", got, ok)
	}
}

func TestFixMissingReasoningContent_SkipsNonToolAssistant(t *testing.T) {
	// 无 tool_calls 的 assistant 不应被补。
	body := buildBody([]map[string]interface{}{
		userMsg("任务"),
		{"role": "assistant", "content": "普通回复"},
	})

	fixed, modified, err := FixMissingReasoningContent(body, testPlaceholder)
	if err != nil {
		t.Fatalf("FixMissingReasoningContent error: %v", err)
	}
	if modified {
		t.Fatal("expected modified=false when no tool_calls assistant")
	}
	if string(fixed) != string(body) {
		t.Fatal("body should be returned unchanged")
	}
}

func TestFixMissingReasoningContent_InvalidBody(t *testing.T) {
	if _, _, err := FixMissingReasoningContent([]byte("{not-json"), testPlaceholder); err == nil {
		t.Fatal("expected error for invalid json")
	}

	// 缺少 messages 字段
	noMsgs, _ := json.Marshal(map[string]interface{}{"model": "deepseek-chat"})
	if _, _, err := FixMissingReasoningContent(noMsgs, testPlaceholder); err == nil {
		t.Fatal("expected error for missing messages")
	}
}

// mustJSON 解析 body 为 map，测试辅助函数。
func mustJSON(body []byte) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		panic(err)
	}
	return m
}
