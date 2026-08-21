package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/protocol/openai"
	"smart-coder-switch/internal/trace"
)

func medProb(v float64) *float64  { return &v }
func highProb(v float64) *float64 { return &v }
func strPtr(s string) *string     { return &s }
func boolPtr(b bool) *bool        { return &b }

// newTestRecorder 创建 trace recorder；若测试未配置 Trace.Directory，
// 则使用 t.TempDir() 作为默认目录，避免 MkdirAll("") 报错。
func newTestRecorder(t *testing.T, cfg *config.Config) *trace.Recorder {
	t.Helper()
	if cfg.Trace.Directory == "" {
		cfg.Trace.Directory = t.TempDir()
	}
	if cfg.Trace.MaxRecords == 0 {
		cfg.Trace.MaxRecords = 100
	}
	if cfg.Trace.MaxBodySize == 0 {
		cfg.Trace.MaxBodySize = 20 * 1024 * 1024
	}
	recorder, err := trace.NewRecorder(cfg.Trace)
	if err != nil {
		t.Fatal(err)
	}
	return recorder
}

// TestDirectModelDirectRoute 测试：
// 当 DirectModel 配置且最后一条消息是 user 时，
// 应路由到指定模型，并默认注入 DIRECT 首轮说明提示。
func TestDirectModelDirectRoute(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	directModel := "gpt-5.6-terra"
	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0.10),
				HighProbability:        highProb(0.01),
				DirectModel: strPtr(directModel),
				// DirectPromptEnabled 默认 true
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	// 使用固定随机值 0.99，如果是概率路由会走 LOW，
	// 如果走 direct 规则则无视随机值
	handler := NewHandler(cfg, upstream, recorder, nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := []byte(`{
		"model": "coder1",
		"messages": [
			{"role": "system", "content": "you are a coder"},
			{"role": "user", "content": "write a function"}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	if capturedModel != directModel {
		t.Fatalf("expected model %q, got %q", directModel, capturedModel)
	}

	// DIRECT 默认注入首轮说明：2 条原始消息 + 1 条提示 = 3
	if len(capturedMessages) != 3 {
		t.Fatalf("expected 3 messages (2 original + 1 DIRECT prompt), got %d", len(capturedMessages))
	}

	lastMsg := capturedMessages[len(capturedMessages)-1]
	if lastMsg.Role != "user" {
		t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
	}
	// 检查提示内容包含"先说明"
	if !strings.Contains(string(lastMsg.Content), "先") {
		t.Fatalf("expected DIRECT prompt to contain '先', got %s", string(lastMsg.Content))
	}
}

// TestDirectModelSkipsWhenNotUser 测试：
// 当 DirectModel 配置但最后消息不是 user 时，
// 应走原有概率路由（不强制走 direct）。
func TestDirectModelSkipsWhenNotUser(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	directModel := "gpt-5.6-terra"
	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0.10),
				HighProbability:        highProb(0.01),
				DirectModel: strPtr(directModel),
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	// 固定随机值 0.99 → 概率路由走 LOW
	handler := NewHandler(cfg, upstream, recorder, nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := []byte(`{
		"model": "coder1",
		"messages": [
			{"role": "system", "content": "you are a coder"},
			{"role": "user", "content": "write a function"},
			{"role": "assistant", "content": "here is the code"},
			{"role": "tool", "content": "result ok"}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	// 最后消息是 tool，不走 direct，应走 LOW => low-coder1
	if capturedModel != "low-coder1" {
		t.Fatalf("expected model %q (LOW via probability), got %q", "low-coder1", capturedModel)
	}
}

// TestDirectModelSkipsAngleBracket 测试：
// 最后一条是 role=user 但内容以 < 开头（如 <system-reminder>），不应触发 direct。
func TestDirectModelSkipsAngleBracket(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	directModel := "gpt-5.6-terra"
	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0.10),
				HighProbability:        highProb(0.01),
				DirectModel: strPtr(directModel),
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	handler := NewHandler(cfg, upstream, recorder, nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := []byte(`{
		"model": "coder1",
		"messages": [
			{"role": "system", "content": "you are a coder"},
			{"role": "user", "content": "<system-reminder>\n# Plan Mode\n..."}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	// 末条虽是 user 但以 < 开头，应走概率路由 => low-coder1
	if capturedModel != "low-coder1" {
		t.Fatalf("expected model %q (LOW via probability), got %q", "low-coder1", capturedModel)
	}
}

// TestDirectModelSkipsSquareBracket 测试：
// 最后一条是 role=user 但内容以 [ 开头（如 [Compressed conversation section]），不应触发 direct。
func TestDirectModelSkipsSquareBracket(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	directModel := "gpt-5.6-terra"
	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0.10),
				HighProbability:        highProb(0.01),
				DirectModel: strPtr(directModel),
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	handler := NewHandler(cfg, upstream, recorder, nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := []byte(`{
		"model": "coder1",
		"messages": [
			{"role": "system", "content": "you are a coder"},
			{"role": "user", "content": "[Compressed conversation section]\n## Session\n..."}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	// 末条虽是 user 但以 [ 开头，应走概率路由 => low-coder1
	if capturedModel != "low-coder1" {
		t.Fatalf("expected model %q (LOW via probability), got %q", "low-coder1", capturedModel)
	}
}

// TestDirectModelNotConfigured 测试：
// 当未配置 DirectModel 时，即使最后消息是 user 也走概率路由。
func TestDirectModelNotConfigured(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               "low-coder1",
				MediumModel:            "medium-coder1",
				HighModel:              "high-coder1",
				MediumProbability: medProb(0.10),
				HighProbability:   highProb(0.01),
				// DirectModel 未配置
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	handler := NewHandler(cfg, upstream, recorder, nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := []byte(`{
		"model": "coder1",
		"messages": [
			{"role": "system", "content": "you are a coder"},
			{"role": "user", "content": "write a function"}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	// 未配置 direct，应走 LOW => low-coder1
	if capturedModel != "low-coder1" {
		t.Fatalf("expected model %q (LOW via probability), got %q", "low-coder1", capturedModel)
	}
}

// TestInjectPlanPrompt 测试 DIRECT 首轮说明注入行为。
func TestInjectPlanPrompt(t *testing.T) {
	directModel := "gpt-5.6-terra"
	lowModel := "low-coder1"

	t.Run("direct injects first-response prompt by default", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    lowModel,
					MediumModel:                 lowModel,
					HighModel:                   lowModel,
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "write a function"}
			]
		}`)

		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected DIRECT model %q, got %q", directModel, capturedModel)
		}

		// 默认注入 DIRECT 首轮说明：2 条原始消息 + 1 条提示 = 3
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (2 original + 1 DIRECT prompt), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		// 检查是 DIRECT 提示而非 Plan
		if !strings.Contains(string(lastMsg.Content), "先") {
			t.Fatalf("expected DIRECT first-response prompt, got %s", string(lastMsg.Content))
		}
	})

	t.Run("direct skips injection when explicitly disabled", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                            lowModel,
					MediumModel:                         lowModel,
					HighModel:                           lowModel,
					MediumProbability:              medProb(0.10),
					HighProbability:                highProb(0.01),
					DirectModel:         strPtr(directModel),
					DirectPromptEnabled: boolPtr(false),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "write a function"}
			]
		}`)

		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected DIRECT model %q, got %q", directModel, capturedModel)
		}

		// 显式关闭：只有 2 条原始消息
		if len(capturedMessages) != 2 {
			t.Fatalf("expected 2 messages (prompt disabled), got %d", len(capturedMessages))
		}
	})

	t.Run("direct does not affect HIGH review", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Trace: config.TraceConfig{
				Directory:   t.TempDir(),
				MaxRecords:  100,
				MaxBodySize: 20 * 1024 * 1024,
			},
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "medium-coder1",
					HighModel:              "high-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.001 } // → HIGH

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "write a function"}
			]
		}`)

		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != "high-coder1" {
			t.Fatalf("expected HIGH model %q, got %q", "high-coder1", capturedModel)
		}

		// HIGH 注入 Review（追加到末尾）：system + user + review = 3
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (system + review + user), got %d", len(capturedMessages))
		}
	})
}

// TestContinuationMessageSkipsDirect 测试：
// 最后一条 user 消息是预定义续接短语时，即使配置了 direct-model，
// 也不走 DIRECT，而是走概率路由。
func TestContinuationMessageSkipsDirect(t *testing.T) {
	tests := []struct {
		name         string
		userContent  string
		expectDirect bool
	}{
		{"continuation 继续", "继续", false},
		{"continuation 继续吧", "继续吧", false},
		{"continuation 继续处理", "继续处理", false},
		{"continuation continue", "continue", false},
		{"continuation go on", "go on", false},
		{"continuation ok", "ok", false},
		{"continuation 好的", "好的", false},
		{"continuation 嗯", "嗯", false},
		{"normal task 继续修复问题", "继续修复问题", true},
		{"normal task write a function", "write a function", true},
	}

	directModel := "gpt-5.6-terra"
	lowModel := "low-coder1"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedModel string
			var capturedMessages []openai.Message

			upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
			defer testServer.Close()

			cfg := &config.Config{
				Models: map[string]config.ModelProfile{
					"coder1": {
						LowModel:                    lowModel,
						MediumModel:                 lowModel,
						HighModel:                   lowModel,
						MediumProbability:      medProb(0.10),
						HighProbability:        highProb(0.01),
						DirectModel: strPtr(directModel),
					},
				},
			}

			recorder := newTestRecorder(t, cfg)

			handler := NewHandler(cfg, upstream, recorder, nil, nil)
			// 固定随机值 0.99 走 LOW
			handler.randomFunc = func() float64 { return 0.99 }

			body := `{
				"model": "coder1",
				"messages": [
					{"role": "system", "content": "you are a coder"},
					{"role": "assistant", "content": "some previous output"},
					{"role": "user", "content": "` + tt.userContent + `"}
				]
			}`

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				bytes.NewReader([]byte(body)),
			)
			rec := httptest.NewRecorder()
			handler.ChatCompletions(rec, req)

			if tt.expectDirect {
				if capturedModel != directModel {
					t.Fatalf("expected DIRECT model %q, got %q", directModel, capturedModel)
				}
			} else {
				if capturedModel != lowModel {
					t.Fatalf("expected LOW model %q (not DIRECT), got %q", lowModel, capturedModel)
				}
			}
		})
	}
}

// TestGuidanceFollowupInjection 测试：
// LOW/MEDIUM 请求历史中包含【Review】或【Plan】时，追加"执行、不复述"指令。
func TestGuidanceFollowupInjection(t *testing.T) {
	t.Run("inject on LOW with 【Review】 marker", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                         "low-coder1",
					MediumModel:                      "low-coder1",
					HighModel:                        "low-coder1",
					MediumProbability:           medProb(0.10),
					HighProbability:             highProb(0.01),
					AntiRepetitionPromptEnabled: boolPtr(true),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 } // → LOW

		// 历史中包含 【Review】 的 assistant 消息
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != "low-coder1" {
			t.Fatalf("expected model %q, got %q", "low-coder1", capturedModel)
		}

		// 3 原始消息 + 1 指导提示 = 4
		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (3 original + 1 guidance), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		if !strings.Contains(string(lastMsg.Content), "历史中的【Review】") {
			t.Fatalf("expected guidance followup prompt to mention 【Review】, got %s", string(lastMsg.Content))
		}
	})

	t.Run("inject on LOW with 【Plan】 marker", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                         "low-coder1",
					MediumModel:                      "low-coder1",
					HighModel:                        "low-coder1",
					MediumProbability:           medProb(0.10),
					HighProbability:             highProb(0.01),
					AntiRepetitionPromptEnabled: boolPtr(true),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Plan】\n- Goal: test\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (3 original + 1 guidance), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		if !strings.Contains(string(lastMsg.Content), "历史中的【Review】") {
			t.Fatalf("expected guidance followup prompt, got %s", string(lastMsg.Content))
		}
	})

	t.Run("no injection on LOW without markers", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "low-coder1",
					HighModel:              "low-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "normal response"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		// 不应注入，保持 3 条原始消息
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (no injection), got %d", len(capturedMessages))
		}
	})

	t.Run("no injection on HIGH even with markers", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "medium-coder1",
					HighModel:              "high-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.001 } // → HIGH

		// 历史中有【Review】，但 HIGH 不应注入指导提示
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		// HIGH: system + assistant + user + review = 4（追加在末尾，不追加 guidance）
		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (system + assistant + user + review), got %d", len(capturedMessages))
		}

		// 最后一条是 HIGH Review（role=user，英文内容）
		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		// HIGH Review 是英文，不应包含中文 guidance 内容
		if strings.Contains(string(lastMsg.Content), "历史中的【Review】") {
			t.Fatalf("HIGH should not inject guidance followup, but it did")
		}
	})

	// HIGH + DeepSeek + tool 边界：只追加 Review，不追加 compat
	t.Run("HIGH with DeepSeek tool boundary only injects review", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "deepseek-v4-flash",
					MediumModel:            "deepseek-v4-flash",
					HighModel:              "deepseek-v4-flash",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.001 } // → HIGH

		// tool 边界：末尾是 tool 消息，tool_call_id 非 DeepSeek 风格
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "hello"},
				{"role": "assistant", "tool_calls": [{"id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]},
				{"role": "tool", "tool_call_id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "content": "done"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != "deepseek-v4-flash" {
			t.Fatalf("expected HIGH model %q, got %q", "deepseek-v4-flash", capturedModel)
		}

		// 4 条原始消息 + 1 条 HIGH Review = 5，不追加 DeepSeek compat
		if len(capturedMessages) != 5 {
			t.Fatalf("expected 5 messages (4 original + 1 review), got %d", len(capturedMessages))
		}

		// 最后一条是 HIGH Review，不应该是 "继续"
		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		if strings.Contains(string(lastMsg.Content), "继续") {
			t.Fatalf("HIGH should not inject DeepSeek compat prompt, but it did")
		}
		if !strings.Contains(string(lastMsg.Content), "【Review】") {
			t.Fatalf("expected HIGH Review prompt to contain '【Review】', got %s", string(lastMsg.Content))
		}
	})

	// DIRECT enabled（默认） + 历史【Review】→ 只注入 DIRECT 提示，抑制 guidance
	t.Run("DIRECT prompt suppresses guidance when enabled with 【Review】 history", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    "low-coder1",
					MediumModel:                 "low-coder1",
					HighModel:                   "low-coder1",
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
					// DirectPromptEnabled 默认 true
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected model %q, got %q", directModel, capturedModel)
		}

		// 3 原始消息 + 1 DIRECT 提示 = 4（不注入 guidance）
		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (3 original + 1 DIRECT prompt), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		// 应是 DIRECT 提示，不是 guidance
		if strings.Contains(string(lastMsg.Content), "历史中的【Review】") {
			t.Fatalf("should NOT inject guidance followup when DIRECT prompt enabled, got %s", string(lastMsg.Content))
		}
		if !strings.Contains(string(lastMsg.Content), "先") {
			t.Fatalf("expected DIRECT first-response prompt, got %s", string(lastMsg.Content))
		}
	})

	// DIRECT disabled + 历史【Review】→ 注入 guidance（保留旧行为）
	t.Run("injects guidance on DIRECT when prompt disabled with 【Review】 history", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                         "low-coder1",
					MediumModel:                      "low-coder1",
					HighModel:                        "low-coder1",
					MediumProbability:           medProb(0.10),
					HighProbability:             highProb(0.01),
					DirectModel:      strPtr(directModel),
					DirectPromptEnabled: boolPtr(false),
					AntiRepetitionPromptEnabled:    boolPtr(true),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected model %q, got %q", directModel, capturedModel)
		}

		// 3 原始消息 + 1 guidance = 4
		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (3 original + 1 guidance), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		if !strings.Contains(string(lastMsg.Content), "历史中的【Review】") {
			t.Fatalf("expected guidance followup prompt, got %s", string(lastMsg.Content))
		}
	})

	// DIRECT enabled（默认） + 历史【Plan】→ 只注入 DIRECT 提示，抑制 guidance
	t.Run("DIRECT prompt suppresses guidance when enabled with 【Plan】 history", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    "low-coder1",
					MediumModel:                 "low-coder1",
					HighModel:                   "low-coder1",
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
					// DirectPromptEnabled 默认 true
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Plan】\n- Goal: test\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected model %q, got %q", directModel, capturedModel)
		}

		// 3 原始消息 + 1 DIRECT 提示 = 4
		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (3 original + 1 DIRECT prompt), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		// 应是 DIRECT 提示，不是 guidance
		if strings.Contains(string(lastMsg.Content), "【Review】和【Plan】") {
			t.Fatalf("should NOT inject guidance followup when DIRECT prompt enabled, got %s", string(lastMsg.Content))
		}
		if !strings.Contains(string(lastMsg.Content), "先") {
			t.Fatalf("expected DIRECT first-response prompt, got %s", string(lastMsg.Content))
		}
	})

	// DIRECT enabled + 无历史标记 → 注入 DIRECT 提示
	t.Run("inject DIRECT prompt when enabled with no history markers", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    "low-coder1",
					MediumModel:                 "low-coder1",
					HighModel:                   "low-coder1",
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		// 无历史标记
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected model %q, got %q", directModel, capturedModel)
		}

		// 2 原始消息 + 1 DIRECT 提示 = 3
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (2 original + 1 DIRECT prompt), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		if !strings.Contains(string(lastMsg.Content), "先") {
			t.Fatalf("expected DIRECT first-response prompt, got %s", string(lastMsg.Content))
		}
	})
}

// TestDirectImagePromptInjection 测试 DIRECT 档含图片时的合并提示注入：
// 图片理解段落与 DIRECT 首轮提示合并为一条 user 消息，并使用包裹标注。
func TestDirectImagePromptInjection(t *testing.T) {
	// DIRECT 命中且最新消息包含图片（OpenAI image_url 格式）
	t.Run("direct with image injects single merged prompt", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    "low-coder1",
					MediumModel:                 "low-coder1",
					HighModel:                   "low-coder1",
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": [
					{"type": "text", "text": "what does this error mean?"},
					{"type": "image_url", "image_url": {"url": "https://example.com/error.png"}}
				]}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected model %q, got %q", directModel, capturedModel)
		}

		// 2 原始消息 + 1 条合并提示 = 3（只注入一条）
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (2 original + 1 merged prompt), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		var content string
		if err := json.Unmarshal(lastMsg.Content, &content); err != nil {
			t.Fatalf("decode last message content: %v", err)
		}
		// 合并提示同时包含图片理解段落与 DIRECT 首轮内容，且带包裹标注
		if !strings.Contains(content, "OCR") {
			t.Fatalf("expected merged prompt to contain OCR section, got %s", content)
		}
		if !strings.Contains(content, "<smart-coder-switch-instruction>") {
			t.Fatalf("expected merged prompt to use instruction wrapper, got %s", content)
		}
		if !strings.Contains(content, "BUILD") {
			t.Fatalf("expected merged prompt to keep DIRECT phase content, got %s", content)
		}

		// trace 记录图片提示注入
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	// DIRECT 命中但无图片 → 只注入普通 DIRECT 提示，不包含图片理解段落
	t.Run("direct without image injects plain direct prompt", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    "low-coder1",
					MediumModel:                 "low-coder1",
					HighModel:                   "low-coder1",
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "please fix this bug"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (2 original + 1 DIRECT prompt), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		var content string
		if err := json.Unmarshal(lastMsg.Content, &content); err != nil {
			t.Fatalf("decode last message content: %v", err)
		}
		if strings.Contains(content, "OCR") {
			t.Fatalf("expected no OCR section without image, got %s", content)
		}
		if !strings.Contains(content, "<smart-coder-switch-instruction>") {
			t.Fatalf("expected DIRECT prompt to use instruction wrapper, got %s", content)
		}
	})

	// DIRECT 命中但 direct-prompt-enabled: false → 不注入任何提示
	t.Run("direct with image and prompt disabled skips injection", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                            "low-coder1",
					MediumModel:                         "low-coder1",
					HighModel:                           "low-coder1",
					MediumProbability:              medProb(0.10),
					HighProbability:                highProb(0.01),
					DirectModel:         strPtr(directModel),
					DirectPromptEnabled: boolPtr(false),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": [
					{"type": "text", "text": "what does this error mean?"},
					{"type": "image_url", "image_url": {"url": "https://example.com/error.png"}}
				]}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		// 提示关闭时不注入任何内容
		if len(capturedMessages) != 2 {
			t.Fatalf("expected 2 messages (no injection), got %d", len(capturedMessages))
		}
	})

	// DIRECT 命中且 direct-prompt-enabled 默认启用，但
	// image-prompt-enabled: false → 注入普通 DIRECT 提示，不含图片理解段落
	t.Run("direct with image and image prompt disabled injects plain direct prompt", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    "low-coder1",
					MediumModel:                 "low-coder1",
					HighModel:                   "low-coder1",
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
					ImagePromptEnabled:     boolPtr(false),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": [
					{"type": "text", "text": "what does this error mean?"},
					{"type": "image_url", "image_url": {"url": "https://example.com/error.png"}}
				]}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected model %q, got %q", directModel, capturedModel)
		}

		// 只注入普通 DIRECT 提示：2 原始 + 1 = 3
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (2 original + 1 DIRECT prompt), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		var content string
		if err := json.Unmarshal(lastMsg.Content, &content); err != nil {
			t.Fatalf("decode last message content: %v", err)
		}
		// 关闭图片理解后不注入 OCR 段落
		if strings.Contains(content, "OCR") {
			t.Fatalf("expected no OCR section when image prompt disabled, got %s", content)
		}
		// 仍注入普通 DIRECT 首轮提示（带包裹标注）
		if !strings.Contains(content, "<smart-coder-switch-instruction>") {
			t.Fatalf("expected plain DIRECT prompt to use instruction wrapper, got %s", content)
		}
		if !strings.Contains(content, "BUILD") {
			t.Fatalf("expected plain DIRECT prompt to keep phase content, got %s", content)
		}
	})

	// 非 DIRECT（LOW）且最新消息含图片 → 不注入图片提示
	t.Run("non-direct with image does not inject image prompt", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "low-coder1",
					HighModel:              "low-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
					// DirectModel 未配置 → 不走 DIRECT
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		// 让概率路由命中 LOW
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": [
					{"type": "text", "text": "describe this"},
					{"type": "image_url", "image_url": {"url": "https://example.com/error.png"}}
				]}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != "low-coder1" {
			t.Fatalf("expected LOW model, got %q", capturedModel)
		}

		// LOW 档不注入图片理解提示
		for i, msg := range capturedMessages {
			if msg.Role == "user" && strings.Contains(string(msg.Content), "OCR") {
				t.Fatalf("should NOT inject image prompt for non-DIRECT, but found at message %d", i)
			}
		}
	})
}

// TestGuidanceTakesPrecedenceOverDeepSeekCompat 测试组合规则：
// 当 LOW/MEDIUM 同时满足 DeepSeek 工具边界兼容和指导防复述时，只注入指导提示。
func TestGuidanceTakesPrecedenceOverDeepSeekCompat(t *testing.T) {
	t.Run("guidance takes precedence when both conditions match", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                         "deepseek-v4-flash",
					MediumModel:                      "deepseek-v4-flash",
					HighModel:                        "deepseek-v4-flash",
					MediumProbability:           medProb(0.10),
					HighProbability:             highProb(0.01),
					AntiRepetitionPromptEnabled: boolPtr(true),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 } // → LOW

		// 同时满足两个条件：
		// - DeepSeek 模型 + 工具边界（non-DeepSeek tool_call_id）
		// - 最新一条 assistant 消息 text 含【Review】（content 与 tool_calls 并存）
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "hello"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n", "tool_calls": [{"id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]},
				{"role": "tool", "tool_call_id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "content": "done"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		// 4 原始消息 + 1 指导提示 = 5（不应有第二条 DeepSeek 兼容提示）
		if len(capturedMessages) != 5 {
			t.Fatalf("expected 5 messages (4 original + 1 guidance), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}

		lastContent := string(lastMsg.Content)
		// 应包含指导提示内容，而不是 DeepSeek 兼容提示
		if !strings.Contains(lastContent, "历史中的【Review】") {
			t.Fatalf("expected guidance followup prompt, got %s", lastContent)
		}
		// 确认不是 DeepSeek 兼容提示
		if strings.Contains(lastContent, "基于已有工具结果") {
			t.Fatalf("should NOT contain DeepSeek compat prompt when guidance takes precedence, got %s", lastContent)
		}
	})

	t.Run("deepseek compat still applies when no guidance markers", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "deepseek-v4-flash",
					MediumModel:            "deepseek-v4-flash",
					HighModel:              "deepseek-v4-flash",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 } // → LOW

		// 只有 DeepSeek 工具边界条件，没有 guidance markers
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "hello"},
				{"role": "assistant", "tool_calls": [{"id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]},
				{"role": "tool", "tool_call_id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "content": "done"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		// 新逻辑：补充 reasoning_content，不追加 user 消息
		// 消息数应保持 4 条不变
		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (no appended message), got %d", len(capturedMessages))
		}

		// 验证 assistant 消息的 reasoning_content 已被补充
		assistantMsg := capturedMessages[2]
		if assistantMsg.Role != "assistant" {
			t.Fatalf("expected assistant message at index 2, got role %q", assistantMsg.Role)
		}
		if assistantMsg.ReasoningContent == nil {
			t.Fatal("expected assistant message to have reasoning_content")
		}
		if !strings.Contains(
			string(assistantMsg.ReasoningContent),
			"继续处理",
		) {
			t.Fatalf(
				"expected reasoning_content to contain placeholder, got %s",
				string(assistantMsg.ReasoningContent),
			)
		}
	})
}

// TestHighRoundsTriggersHighReview 测试：
// 当 high-rounds 配置且 assistant 消息数能整除时，应路由到 HIGH 模型并注入 review 提示。
func TestHighRoundsTriggersHighReview(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               "low-coder1",
				MediumModel:            "med-coder1",
				HighModel:              "high-coder1",
				MediumProbability: medProb(0.10),
				HighProbability:   highProb(0.90), // 高概率，但轮次优先
				HighRounds:        new(10),
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	handler := NewHandler(cfg, upstream, recorder, nil, nil)
	handler.randomFunc = func() float64 { return 0.99 } // 概率走 LOW，但轮次应覆盖

	// 构造包含 10 条 assistant 消息的请求
	var messages []openai.Message
	messages = append(messages, openai.Message{Role: "system", Content: json.RawMessage(`"you are a coder"`)})
	for range 10 {
		messages = append(messages, openai.Message{Role: "user", Content: json.RawMessage(`"task"`)})
		messages = append(messages, openai.Message{Role: "assistant", Content: json.RawMessage(`"response"`)})
	}
	messages = append(messages, openai.Message{Role: "user", Content: json.RawMessage(`"next task"`)})

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "coder1",
		"messages": messages,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	if capturedModel != "high-coder1" {
		t.Fatalf("expected HIGH model %q, got %q", "high-coder1", capturedModel)
	}

	// HIGH 注入 Review：10 条 assistant 消息触发
	lastMsg := capturedMessages[len(capturedMessages)-1]
	if lastMsg.Role != "user" {
		t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
	}
	if !strings.Contains(string(lastMsg.Content), "Review") {
		t.Fatalf("expected HIGH review prompt, got %s", string(lastMsg.Content))
	}
}

// TestMediumRoundsTriggersMedium 测试：
// 当 medium-rounds 配置且 assistant 消息数能整除时，应路由到 MEDIUM 模型且不注入 review 提示。
func TestMediumRoundsTriggersMedium(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               "low-coder1",
				MediumModel:            "med-coder1",
				HighModel:              "high-coder1",
				MediumProbability: medProb(0.10),
				HighProbability:   highProb(0.01),
				MediumRounds:      new(5),
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	handler := NewHandler(cfg, upstream, recorder, nil, nil)
	handler.randomFunc = func() float64 { return 0.99 } // 概率走 LOW

	// 构造包含 5 条 assistant 消息的请求
	var messages []openai.Message
	messages = append(messages, openai.Message{Role: "system", Content: json.RawMessage(`"you are a coder"`)})
	for range 5 {
		messages = append(messages, openai.Message{Role: "user", Content: json.RawMessage(`"task"`)})
		messages = append(messages, openai.Message{Role: "assistant", Content: json.RawMessage(`"response"`)})
	}
	messages = append(messages, openai.Message{Role: "user", Content: json.RawMessage(`"next task"`)})

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "coder1",
		"messages": messages,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	if capturedModel != "med-coder1" {
		t.Fatalf("expected MEDIUM model %q, got %q", "med-coder1", capturedModel)
	}

	// MEDIUM 不应注入 review 提示
	for i, msg := range capturedMessages {
		if msg.Role == "user" && strings.Contains(string(msg.Content), "Review") {
			t.Fatalf("MEDIUM should not inject review prompt, but found at message %d", i)
		}
	}
}

// TestDirectPriorityOverRounds 测试：
// 即使轮次命中，DIRECT 仍优先。
func TestDirectPriorityOverRounds(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	directModel := "gpt-5.6-terra"
	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "med-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0.10),
				HighProbability:        highProb(0.01),
				HighRounds:             new(2),
				DirectModel: strPtr(directModel),
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	handler := NewHandler(cfg, upstream, recorder, nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	// 2 条 assistant 消息 + 最后一条是 user → DIRECT 优先于 HIGH 轮次
	body := []byte(`{
		"model": "coder1",
		"messages": [
			{"role": "system", "content": "you are a coder"},
			{"role": "user", "content": "task 1"},
			{"role": "assistant", "content": "done 1"},
			{"role": "user", "content": "task 2"},
			{"role": "assistant", "content": "done 2"},
			{"role": "user", "content": "task 3"}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	if capturedModel != directModel {
		t.Fatalf("expected DIRECT model %q, got %q", directModel, capturedModel)
	}
}
func newCapturingUpstream(
	t *testing.T,
	capturedModel *string,
	capturedMessages *[]openai.Message,
) (*Upstream, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openai.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode upstream request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*capturedModel = req.Model
		*capturedMessages = req.Messages

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion"}`))
	}))

	upstream, err := NewUpstream(server.URL+"/", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return upstream, server
}

func TestDeepSeekToolBoundaryCompat(t *testing.T) {
	// Test case 1: DeepSeek + last=tool + non-DeepSeek ID → should inject
	t.Run("inject for deepseek + tool tail + non-deepseek id", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "deepseek-v4-flash",
					MediumModel:            "deepseek-v4-flash",
					HighModel:              "deepseek-v4-flash",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "write a function"},
				{"role": "assistant", "tool_calls": [{"id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]},
				{"role": "tool", "tool_call_id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "content": "done"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != "deepseek-v4-flash" {
			t.Fatalf(
				"expected model %q, got %q",
				"deepseek-v4-flash",
				capturedModel,
			)
		}

		// 新逻辑：补充 reasoning_content，不追加 user 消息
		// 消息数应保持 4 条不变
		if len(capturedMessages) != 4 {
			t.Fatalf(
				"expected 4 messages (no appended message), got %d",
				len(capturedMessages),
			)
		}

		// 验证 assistant 消息的 reasoning_content 已被补充
		assistantMsg := capturedMessages[2]
		if assistantMsg.Role != "assistant" {
			t.Fatalf(
				"expected assistant message at index 2, got role %q",
				assistantMsg.Role,
			)
		}
		if assistantMsg.ReasoningContent == nil {
			t.Fatal("expected assistant message to have reasoning_content")
		}
		if !strings.Contains(
			string(assistantMsg.ReasoningContent),
			"继续处理",
		) {
			t.Fatalf(
				"expected reasoning_content to contain placeholder, got %s",
				string(assistantMsg.ReasoningContent),
			)
		}
	})

	// Test case 2: DeepSeek + last=assistant.tool_calls → should NOT inject
	// (assistant.tool_calls 尾部未经直连验证，暂不处理)
	t.Run(
		"skip injection for assistant tool_calls tail",
		func(t *testing.T) {
			var capturedModel string
			var capturedMessages []openai.Message

			upstream, testServer := newCapturingUpstream(
				t,
				&capturedModel,
				&capturedMessages,
			)
			defer testServer.Close()

			cfg := &config.Config{
				Models: map[string]config.ModelProfile{
					"coder1": {
						LowModel:               "deepseek-v4-flash",
						MediumModel:            "deepseek-v4-flash",
						HighModel:              "deepseek-v4-flash",
						MediumProbability: medProb(0.10),
						HighProbability:   highProb(0.01),
					},
				},
			}

			recorder := newTestRecorder(t, cfg)

			handler := NewHandler(cfg, upstream, recorder, nil, nil)
			handler.randomFunc = func() float64 { return 0.99 }

			// 最后一条是 assistant 且含 tool_calls（无 tool 结果跟随）
			body := []byte(`{
				"model": "coder1",
				"messages": [
					{"role": "system", "content": "you are a coder"},
					{"role": "user", "content": "write a function"},
					{"role": "assistant", "tool_calls": [{"id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]}
				]
			}`)

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				bytes.NewReader(body),
			)
			rec := httptest.NewRecorder()
			handler.ChatCompletions(rec, req)

			if capturedModel != "deepseek-v4-flash" {
				t.Fatalf(
					"expected model %q, got %q",
					"deepseek-v4-flash",
					capturedModel,
				)
			}

			// 不应注入，保持 3 条原始消息
			if len(capturedMessages) != 3 {
				t.Fatalf(
					"expected 3 messages (no injection), got %d",
					len(capturedMessages),
				)
			}
		},
	)

	// Test case 3: DeepSeek + tool tail + call_00_* ID + no reasoning_content → should inject
	// 新逻辑：检查 reasoning_content 缺失，不再检查 ID 风格。
	// call_00_* 虽是 DeepSeek 风格 ID，但 assistant 缺少 reasoning_content 仍需注入。
	t.Run(
		"inject for deepseek + deepseek-style id without reasoning_content",
		func(t *testing.T) {
			var capturedModel string
			var capturedMessages []openai.Message

			upstream, testServer := newCapturingUpstream(
				t,
				&capturedModel,
				&capturedMessages,
			)
			defer testServer.Close()

			cfg := &config.Config{
				Models: map[string]config.ModelProfile{
					"coder1": {
						LowModel:               "deepseek-v4-flash",
						MediumModel:            "deepseek-v4-flash",
						HighModel:              "deepseek-v4-flash",
						MediumProbability: medProb(0.10),
						HighProbability:   highProb(0.01),
					},
				},
			}

			recorder := newTestRecorder(t, cfg)

			handler := NewHandler(cfg, upstream, recorder, nil, nil)
			handler.randomFunc = func() float64 { return 0.99 }

			// assistant 虽然是 call_00_* ID，但缺少 reasoning_content → 应注入
			body := []byte(`{
				"model": "coder1",
				"messages": [
					{"role": "system", "content": "you are a coder"},
					{"role": "user", "content": "write a function"},
					{"role": "assistant", "tool_calls": [{"id": "call_00_ET_EjUfMBjoYGuFXEIHfq3J6898", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]},
					{"role": "tool", "tool_call_id": "call_00_ET_EjUfMBjoYGuFXEIHfq3J6898", "content": "done"}
				]
			}`)

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				bytes.NewReader(body),
			)
			rec := httptest.NewRecorder()
			handler.ChatCompletions(rec, req)

			if capturedModel != "deepseek-v4-flash" {
				t.Fatalf(
					"expected model %q, got %q",
					"deepseek-v4-flash",
					capturedModel,
				)
			}

			// 新逻辑：补充 reasoning_content，不追加 user 消息
			// 消息数应保持 4 条不变
			if len(capturedMessages) != 4 {
				t.Fatalf(
					"expected 4 messages (no appended message), got %d",
					len(capturedMessages),
				)
			}

			// 验证 assistant 消息的 reasoning_content 已被补充
			assistantMsg := capturedMessages[2]
			if assistantMsg.Role != "assistant" {
				t.Fatalf(
					"expected assistant message at index 2, got role %q",
					assistantMsg.Role,
				)
			}
			if assistantMsg.ReasoningContent == nil {
				t.Fatal("expected assistant message to have reasoning_content")
			}
			if !strings.Contains(
				string(assistantMsg.ReasoningContent),
				"继续处理",
			) {
				t.Fatalf(
					"expected reasoning_content to contain placeholder, got %s",
					string(assistantMsg.ReasoningContent),
				)
			}
		},
	)

	// Test case 4: 非 DeepSeek 模型 → should NOT inject
	t.Run(
		"skip injection for non-deepseek model",
		func(t *testing.T) {
			var capturedModel string
			var capturedMessages []openai.Message

			upstream, testServer := newCapturingUpstream(
				t,
				&capturedModel,
				&capturedMessages,
			)
			defer testServer.Close()

			cfg := &config.Config{
				Models: map[string]config.ModelProfile{
					"coder1": {
						LowModel:               "gpt-5.6-terra",
						MediumModel:            "gpt-5.6-terra",
						HighModel:              "gpt-5.6-terra",
						MediumProbability: medProb(0.10),
						HighProbability:   highProb(0.01),
					},
				},
			}

			recorder := newTestRecorder(t, cfg)

			handler := NewHandler(cfg, upstream, recorder, nil, nil)
			handler.randomFunc = func() float64 { return 0.99 }

			body := []byte(`{
				"model": "coder1",
				"messages": [
					{"role": "system", "content": "you are a coder"},
					{"role": "user", "content": "write a function"},
					{"role": "assistant", "tool_calls": [{"id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]},
					{"role": "tool", "tool_call_id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "content": "done"}
				]
			}`)

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				bytes.NewReader(body),
			)
			rec := httptest.NewRecorder()
			handler.ChatCompletions(rec, req)

			// 非 DeepSeek 模型，不应注入
			if len(capturedMessages) != 4 {
				t.Fatalf(
					"expected 4 messages (no injection), got %d",
					len(capturedMessages),
				)
			}
		},
	)

	// Test case 5: user tail → should NOT inject
	t.Run(
		"skip injection for user tail",
		func(t *testing.T) {
			var capturedModel string
			var capturedMessages []openai.Message

			upstream, testServer := newCapturingUpstream(
				t,
				&capturedModel,
				&capturedMessages,
			)
			defer testServer.Close()

			cfg := &config.Config{
				Models: map[string]config.ModelProfile{
					"coder1": {
						LowModel:               "deepseek-v4-flash",
						MediumModel:            "deepseek-v4-flash",
						HighModel:              "deepseek-v4-flash",
						MediumProbability: medProb(0.10),
						HighProbability:   highProb(0.01),
					},
				},
			}

			recorder := newTestRecorder(t, cfg)

			handler := NewHandler(cfg, upstream, recorder, nil, nil)
			handler.randomFunc = func() float64 { return 0.99 }

			body := []byte(`{
				"model": "coder1",
				"messages": [
					{"role": "system", "content": "you are a coder"},
					{"role": "user", "content": "hello"}
				]
			}`)

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				bytes.NewReader(body),
			)
			rec := httptest.NewRecorder()
			handler.ChatCompletions(rec, req)

			// user tail，不应注入
			if len(capturedMessages) != 2 {
				t.Fatalf(
					"expected 2 messages (no injection), got %d",
					len(capturedMessages),
				)
			}
		},
	)

	// Test case 7: DeepSeek + tail tool + last ID is call_00_ but segment has non-deepseek IDs → should inject
	t.Run(
		"inject when last id is deepseek style but segment has non-deepseek ids",
		func(t *testing.T) {
			var capturedModel string
			var capturedMessages []openai.Message

			upstream, testServer := newCapturingUpstream(
				t,
				&capturedModel,
				&capturedMessages,
			)
			defer testServer.Close()

			cfg := &config.Config{
				Models: map[string]config.ModelProfile{
					"coder1": {
						LowModel:               "deepseek-v4-flash",
						MediumModel:            "deepseek-v4-flash",
						HighModel:              "deepseek-v4-flash",
						MediumProbability: medProb(0.10),
						HighProbability:   highProb(0.01),
					},
				},
			}

			recorder := newTestRecorder(t, cfg)

			handler := NewHandler(cfg, upstream, recorder, nil, nil)
			handler.randomFunc = func() float64 { return 0.99 }

			// 模拟 v1 漏判场景：
			// user → assistant(tool_calls: call_qDSu...) → tool(call_qDSu...) → assistant(tool_calls: call_00_Chwe...) → tool(call_00_Chwe...)
			body := []byte(`{
				"model": "coder1",
				"messages": [
					{"role": "system", "content": "you are a coder"},
					{"role": "user", "content": "hello"},
					{"role": "assistant", "tool_calls": [{"id": "call_qDSu8To9vY51dTA870igOqBa", "type": "function", "function": {"name": "read", "arguments": "{}"}}]},
					{"role": "tool", "tool_call_id": "call_qDSu8To9vY51dTA870igOqBa", "content": "file content"},
					{"role": "assistant", "tool_calls": [{"id": "call_00_ChweYsQsI24whCWB18BO3827", "type": "function", "function": {"name": "edit", "arguments": "{}"}}]},
					{"role": "tool", "tool_call_id": "call_00_ChweYsQsI24whCWB18BO3827", "content": "edited"}
				]
			}`)

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				bytes.NewReader(body),
			)
			rec := httptest.NewRecorder()
			handler.ChatCompletions(rec, req)

			if capturedModel != "deepseek-v4-flash" {
				t.Fatalf(
					"expected model %q, got %q",
					"deepseek-v4-flash",
					capturedModel,
				)
			}

			// 新逻辑：补充 reasoning_content，不追加 user 消息
			// 消息数应保持 6 条不变
			if len(capturedMessages) != 6 {
				t.Fatalf(
					"expected 6 messages (no appended message), got %d",
					len(capturedMessages),
				)
			}

			// 验证两个 assistant 消息的 reasoning_content 都已被补充
			for i, msg := range capturedMessages {
				if msg.Role != "assistant" {
					continue
				}
				if msg.ReasoningContent == nil {
					t.Fatalf(
						"expected assistant message at index %d to have reasoning_content",
						i,
					)
				}
				if !strings.Contains(
					string(msg.ReasoningContent),
					"继续处理",
				) {
					t.Fatalf(
						"expected assistant message at index %d reasoning_content to contain placeholder, got %s",
						i,
						string(msg.ReasoningContent),
					)
				}
			}
		},
	)

	// Test case 8: DeepSeek + tool tail + assistant with reasoning_content → should NOT inject
	t.Run(
		"skip injection when assistant has reasoning_content",
		func(t *testing.T) {
			var capturedModel string
			var capturedMessages []openai.Message

			upstream, testServer := newCapturingUpstream(
				t,
				&capturedModel,
				&capturedMessages,
			)
			defer testServer.Close()

			cfg := &config.Config{
				Models: map[string]config.ModelProfile{
					"coder1": {
						LowModel:               "deepseek-v4-flash",
						MediumModel:            "deepseek-v4-flash",
						HighModel:              "deepseek-v4-flash",
						MediumProbability: medProb(0.10),
						HighProbability:   highProb(0.01),
					},
				},
			}

			recorder := newTestRecorder(t, cfg)

			handler := NewHandler(cfg, upstream, recorder, nil, nil)
			handler.randomFunc = func() float64 { return 0.99 }

			// assistant 包含 reasoning_content
			body := []byte(`{
				"model": "coder1",
				"messages": [
					{"role": "system", "content": "you are a coder"},
					{"role": "user", "content": "hello"},
					{"role": "assistant", "content": "I will call tool", "reasoning_content": "User wants test", "tool_calls": [{"id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "type": "function", "function": {"name": "greet", "arguments": "{}"}}]},
					{"role": "tool", "tool_call_id": "call_KZQBjvAeiexzeLrMBoMOvgWW", "content": "hello"}
				]
			}`)

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				bytes.NewReader(body),
			)
			rec := httptest.NewRecorder()
			handler.ChatCompletions(rec, req)

			if capturedModel != "deepseek-v4-flash" {
				t.Fatalf(
					"expected model %q, got %q",
					"deepseek-v4-flash",
					capturedModel,
				)
			}

			// assistant 有 reasoning_content，不应注入
			if len(capturedMessages) != 4 {
				t.Fatalf(
					"expected 4 messages (no injection), got %d",
					len(capturedMessages),
				)
			}
		},
	)

	// Test case 9: DeepSeek + tool tail + assistant with reasoning_content=null → should fix
	t.Run(
		"fix reasoning_content when it is null",
		func(t *testing.T) {
			var capturedModel string
			var capturedMessages []openai.Message

			upstream, testServer := newCapturingUpstream(
				t,
				&capturedModel,
				&capturedMessages,
			)
			defer testServer.Close()

			cfg := &config.Config{
				Models: map[string]config.ModelProfile{
					"coder1": {
						LowModel:               "deepseek-v4-flash",
						MediumModel:            "deepseek-v4-flash",
						HighModel:              "deepseek-v4-flash",
						MediumProbability: medProb(0.10),
						HighProbability:   highProb(0.01),
					},
				},
			}

			recorder := newTestRecorder(t, cfg)

			handler := NewHandler(cfg, upstream, recorder, nil, nil)
			handler.randomFunc = func() float64 { return 0.99 }

			// assistant 有 reasoning_content=null（GPT 返回的格式）
			body := []byte(`{
				"model": "coder1",
				"messages": [
					{"role": "system", "content": "you are a coder"},
					{"role": "user", "content": "hello"},
					{"role": "assistant", "content": null, "reasoning_content": null, "tool_calls": [{"id": "call_WVRdVgI0gxWcCBYvz5SF7Mdp", "type": "function", "function": {"name": "greet", "arguments": "{}"}}]},
					{"role": "tool", "tool_call_id": "call_WVRdVgI0gxWcCBYvz5SF7Mdp", "content": "ok"}
				]
			}`)

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				bytes.NewReader(body),
			)
			rec := httptest.NewRecorder()
			handler.ChatCompletions(rec, req)

			if capturedModel != "deepseek-v4-flash" {
				t.Fatalf(
					"expected model %q, got %q",
					"deepseek-v4-flash",
					capturedModel,
				)
			}

			// 新逻辑：补充 reasoning_content，不追加 user 消息
			// 消息数应保持 4 条不变
			if len(capturedMessages) != 4 {
				t.Fatalf(
					"expected 4 messages (no appended message), got %d",
					len(capturedMessages),
				)
			}

			// 验证 assistant 消息的 reasoning_content 已被补充（从 null 改为占位文本）
			assistantMsg := capturedMessages[2]
			if assistantMsg.Role != "assistant" {
				t.Fatalf(
					"expected assistant message at index 2, got role %q",
					assistantMsg.Role,
				)
			}
			if assistantMsg.ReasoningContent == nil {
				t.Fatal("expected assistant message to have reasoning_content")
			}
			if !strings.Contains(
				string(assistantMsg.ReasoningContent),
				"继续处理",
			) {
				t.Fatalf(
					"expected reasoning_content to contain placeholder, got %s",
					string(assistantMsg.ReasoningContent),
				)
			}
			// 确认不再是 null
			if string(assistantMsg.ReasoningContent) == "null" {
				t.Fatal("reasoning_content should not be null after fix")
			}
		},
	)
}

// TestAntiRepetitionPromptEnabledSwitch 测试 anti-repetition-prompt-enabled 开关行为。
func TestAntiRepetitionPromptEnabledSwitch(t *testing.T) {
	t.Run("LOW with markers and anti-repetition disabled skips injection", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                         "low-coder1",
					MediumModel:                      "low-coder1",
					HighModel:                        "low-coder1",
					MediumProbability:           medProb(0.10),
					HighProbability:             highProb(0.01),
					AntiRepetitionPromptEnabled: boolPtr(false),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 } // → LOW

		// 历史中包含【Review】标记
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != "low-coder1" {
			t.Fatalf("expected model %q, got %q", "low-coder1", capturedModel)
		}

		// anti-repetition 关闭：不应注入，保持 3 条原始消息
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (anti-repetition disabled), got %d", len(capturedMessages))
		}
	})

	t.Run("LOW with markers and anti-repetition enabled injects guidance", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                         "low-coder1",
					MediumModel:                      "low-coder1",
					HighModel:                        "low-coder1",
					MediumProbability:           medProb(0.10),
					HighProbability:             highProb(0.01),
					AntiRepetitionPromptEnabled: boolPtr(true),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 } // → LOW

		// 历史中包含【Review】标记
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != "low-coder1" {
			t.Fatalf("expected model %q, got %q", "low-coder1", capturedModel)
		}

		// anti-repetition 启用：应注入 guidance，3 原始 + 1 指导 = 4
		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (3 original + 1 guidance), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		if !strings.Contains(string(lastMsg.Content), "历史中的【Review】") {
			t.Fatalf("expected guidance followup prompt, got %s", string(lastMsg.Content))
		}
	})

	t.Run("LOW with markers and nil (default) skips guidance", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "low-coder1",
					HighModel:              "low-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
					// AntiRepetitionPromptEnabled 未配置（nil）→ 默认 false。
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 } // → LOW

		// 历史中包含【Plan】标记。
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Plan】\n- Goal: test\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != "low-coder1" {
			t.Fatalf("expected model %q, got %q", "low-coder1", capturedModel)
		}

		// nil 默认禁用：不注入 guidance，保留 3 条原始消息。
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 original messages, got %d", len(capturedMessages))
		}
	})

	t.Run("DIRECT with markers and anti-repetition disabled skips guidance", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                            "low-coder1",
					MediumModel:                         "low-coder1",
					HighModel:                           "low-coder1",
					MediumProbability:              medProb(0.10),
					HighProbability:                highProb(0.01),
					DirectModel:         strPtr(directModel),
					DirectPromptEnabled: boolPtr(false),
					AntiRepetitionPromptEnabled:    boolPtr(false),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != directModel {
			t.Fatalf("expected DIRECT model %q, got %q", directModel, capturedModel)
		}

		// DIRECT 提示关闭 + anti-repetition 关闭：不注入任何内容，保持 3 条原始消息
		if len(capturedMessages) != 3 {
			t.Fatalf("expected 3 messages (no injection), got %d", len(capturedMessages))
		}
	})
}

// TestGuidanceFollowupOnlyChecksLatestAssistant 验证防复述注入只检查
// 最新一条 assistant 消息的 text 是否包含【Review】/【Plan】标记，
// 而不是扫描全部历史 assistant 消息。
func TestGuidanceFollowupOnlyChecksLatestAssistant(t *testing.T) {
	t.Run("no injection when only older assistant has marker", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "low-coder1",
					HighModel:              "low-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 } // → LOW

		// 较旧的 assistant 含【Review】，但最新 assistant 不含标记
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": "【Review】\n结论: continue\n"},
				{"role": "user", "content": "continue please"},
				{"role": "assistant", "content": "done"},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		// 最新 assistant 不含标记：不应注入，保持 5 条原始消息
		if len(capturedMessages) != 5 {
			t.Fatalf("expected 5 messages (no injection), got %d", len(capturedMessages))
		}
	})

	t.Run("inject when latest assistant array content text has marker", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                         "low-coder1",
					MediumModel:                      "low-coder1",
					HighModel:                        "low-coder1",
					MediumProbability:           medProb(0.10),
					HighProbability:             highProb(0.01),
					AntiRepetitionPromptEnabled: boolPtr(true),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 } // → LOW

		// 最新 assistant content 为数组，text part 含【Plan】
		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "assistant", "content": [{"type": "text", "text": "【Plan】\n- Goal: test\n"}]},
				{"role": "user", "content": "do it"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		// 最新 assistant 含标记：应注入 guidance，3 原始 + 1 指导 = 4
		if len(capturedMessages) != 4 {
			t.Fatalf("expected 4 messages (3 original + 1 guidance), got %d", len(capturedMessages))
		}

		lastMsg := capturedMessages[len(capturedMessages)-1]
		if lastMsg.Role != "user" {
			t.Fatalf("expected last message role 'user', got %q", lastMsg.Role)
		}
		if !strings.Contains(string(lastMsg.Content), "历史中的【Review】") {
			t.Fatalf("expected guidance followup prompt, got %s", string(lastMsg.Content))
		}
	})
}

// TestUpstreamResultLogger 验证上游转发完成后 resultLogger 回写状态码与错误摘要：
// 上游返回非 2xx 时捕获状态码 + OpenAI 风格错误消息。
func TestUpstreamResultLogger(t *testing.T) {
	t.Run("non-2xx captures status and error message", func(t *testing.T) {
		// 上游返回 429 + OpenAI 风格错误体
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
		}))
		defer server.Close()

		upstream, err := NewUpstream(server.URL+"/", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}

		cfg := &config.Config{
			Trace: config.TraceConfig{
				Directory:   t.TempDir(),
				MaxRecords:  100,
				MaxBodySize: 20 * 1024 * 1024,
			},
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "medium-coder1",
					HighModel:              "high-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		var gotResult DecisionResult
		handler := NewHandler(cfg, upstream, recorder, nil, nil, func(result DecisionResult) {
			gotResult = result
		})
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "write a function"}
			]
		}`)

		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if gotResult.RequestID == "" {
			t.Fatal("expected result logger to be called with request id")
		}
		if gotResult.StatusCode != http.StatusTooManyRequests {
			t.Errorf("status code = %d, want %d", gotResult.StatusCode, http.StatusTooManyRequests)
		}
		if gotResult.ErrorMessage != "rate limit exceeded" {
			t.Errorf("error message = %q, want %q", gotResult.ErrorMessage, "rate limit exceeded")
		}
	})

	t.Run("2xx success has empty error message", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion"}`))
		}))
		defer server.Close()

		upstream, err := NewUpstream(server.URL+"/", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "medium-coder1",
					HighModel:              "high-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		var gotResult DecisionResult
		handler := NewHandler(cfg, upstream, recorder, nil, nil, func(result DecisionResult) {
			gotResult = result
		})
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "write a function"}
			]
		}`)

		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if gotResult.StatusCode != http.StatusOK {
			t.Errorf("status code = %d, want %d", gotResult.StatusCode, http.StatusOK)
		}
		if gotResult.ErrorMessage != "" {
			t.Errorf("error message = %q, want empty", gotResult.ErrorMessage)
		}
	})
}

// TestSSEStreamingResultLogger 验证流式 SSE 场景下 resultLogger 的及时回写：
// 上游保持连接持续推送事件时，状态码应在 WriteHeader 后立即回写，
// 而不必等待整个流结束（否则长流场景状态码会一直缺失，前端显示"未知"）。
func TestSSEStreamingResultLogger(t *testing.T) {
	t.Run("streaming SSE writes result before stream ends", func(t *testing.T) {
		// 上游返回 text/event-stream，推送一个事件后保持连接 2 秒不关闭，
		// 模拟真实长 SSE 流（上游持续输出、短时间内不结束）。
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = w.Write([]byte("data: {\"id\":\"1\"}\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}))
		defer server.Close()

		upstream, err := NewUpstream(server.URL+"/", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}

		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               "low-coder1",
					MediumModel:            "medium-coder1",
					HighModel:              "high-coder1",
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		resultCh := make(chan DecisionResult, 1)
		handler := NewHandler(cfg, upstream, recorder, nil, nil, func(result DecisionResult) {
			select {
			case resultCh <- result:
			default:
			}
		})
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"stream": true,
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": "write a function"}
			]
		}`)

		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			defer close(done)
			handler.ChatCompletions(rec, req)
		}()

		// 流尚未结束（上游保持连接 2 秒），此时状态码应已被回写。
		// 先等流自然结束再断言，避免失败路径下 handler goroutine 泄漏。
		var got DecisionResult
		select {
		case got = <-resultCh:
		case <-time.After(500 * time.Millisecond):
		}

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("handler did not return after stream ended")
		}

		if got.StatusCode != http.StatusOK {
			t.Fatalf("result logger not called while SSE stream open (got zero value): want status code %d, got %d; dashboard shows 'unknown'", http.StatusOK, got.StatusCode)
		}
	})
}

// messageHasImagePart 判断消息 content 数组是否包含 image_url part。
func messageHasImagePart(msg openai.Message) bool {
	var parts []openai.ContentPart
	if err := json.Unmarshal(msg.Content, &parts); err != nil {
		return false
	}
	for _, part := range parts {
		if part.Type == "image_url" {
			return true
		}
	}
	return false
}

// TestImagePartsStrippedForNonMultimodalModel 测试历史消息中的 image_url content part
// 在路由到不支持多模态的模型（如 LOW/DeepSeek）时会被过滤，避免上游接口解析失败。
func TestImagePartsStrippedForNonMultimodalModel(t *testing.T) {
	t.Run("low after direct image keeps history image stripped", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		lowModel := "deepseek-v4-flash"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    lowModel,
					MediumModel:                 lowModel,
					HighModel:                   lowModel,
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		// 最后一条 user 为"继续"→ 续接消息不走 DIRECT，概率路由 random=0.99 → LOW
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "system", "content": "you are a coder"},
				{"role": "user", "content": [
					{"type": "text", "text": "what does this error mean?"},
					{"type": "image_url", "image_url": {"url": "https://example.com/error.png"}}
				]},
				{"role": "assistant", "content": "the error is a null pointer at line 12"},
				{"role": "user", "content": "继续"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != lowModel {
			t.Fatalf("expected model %q (LOW), got %q", lowModel, capturedModel)
		}

		// 历史图片消息的 image_url part 必须被过滤，避免上游解析失败
		for i, msg := range capturedMessages {
			if messageHasImagePart(msg) {
				t.Fatalf("message %d still contains image_url part after strip: %s", i, string(msg.Content))
			}
		}

		// 过滤后第一条 user 消息应保留 text part（或占位文本）
		userMsg := capturedMessages[1]
		var parts []openai.ContentPart
		if err := json.Unmarshal(userMsg.Content, &parts); err == nil {
			for _, part := range parts {
				if part.Type != "text" {
					t.Fatalf("unexpected part type %q after strip", part.Type)
				}
			}
		}
	})

	t.Run("image stripped when no direct model configured", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		lowModel := "deepseek-v4-flash"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:               lowModel,
					MediumModel:            lowModel,
					HighModel:              lowModel,
					MediumProbability: medProb(0.10),
					HighProbability:   highProb(0.01),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "user", "content": [
					{"type": "image_url", "image_url": {"url": "https://example.com/screenshot.png"}}
				]}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != lowModel {
			t.Fatalf("expected model %q (LOW), got %q", lowModel, capturedModel)
		}

		for i, msg := range capturedMessages {
			if messageHasImagePart(msg) {
				t.Fatalf("message %d still contains image_url part after strip: %s", i, string(msg.Content))
			}
		}
	})
	t.Run("image kept when image prompt disabled", func(t *testing.T) {
		var capturedModel string
		var capturedMessages []openai.Message

		upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
		defer testServer.Close()

		directModel := "gpt-5.6-terra"
		lowModel := "deepseek-v4-flash"
		cfg := &config.Config{
			Models: map[string]config.ModelProfile{
				"coder1": {
					LowModel:                    lowModel,
					MediumModel:                 lowModel,
					HighModel:                   lowModel,
					MediumProbability:      medProb(0.10),
					HighProbability:        highProb(0.01),
					DirectModel: strPtr(directModel),
					// 图片理解提示注入关闭时，图片未经前序模型转写为文字，
					// 必须保留 image_url part 原样转发，避免图片信息丢失。
					ImagePromptEnabled: boolPtr(false),
				},
			},
		}

		recorder := newTestRecorder(t, cfg)

		handler := NewHandler(cfg, upstream, recorder, nil, nil)
		// 最后一条 user 为"继续"→ 续接消息不走 DIRECT，概率路由 random=0.99 → LOW
		handler.randomFunc = func() float64 { return 0.99 }

		body := []byte(`{
			"model": "coder1",
			"messages": [
				{"role": "user", "content": [
					{"type": "text", "text": "what does this error mean?"},
					{"type": "image_url", "image_url": {"url": "https://example.com/error.png"}}
				]},
				{"role": "assistant", "content": "the error is a null pointer at line 12"},
				{"role": "user", "content": "继续"}
			]
		}`)

		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/chat/completions",
			bytes.NewReader(body),
		)
		rec := httptest.NewRecorder()
		handler.ChatCompletions(rec, req)

		if capturedModel != lowModel {
			t.Fatalf("expected model %q (LOW), got %q", lowModel, capturedModel)
		}

		// 图片理解提示注入关闭 → 不过滤，历史图片 part 必须保留
		if !messageHasImagePart(capturedMessages[0]) {
			t.Fatalf("expected image_url part kept when image prompt disabled, got %s", string(capturedMessages[0].Content))
		}
	})
}

// TestImagePartsKeptForDirectModel 测试 DIRECT 档（direct-model）命中时
// 图片消息保留 image_url part，由支持多模态的模型处理。
func TestImagePartsKeptForDirectModel(t *testing.T) {
	var capturedModel string
	var capturedMessages []openai.Message

	upstream, testServer := newCapturingUpstream(t, &capturedModel, &capturedMessages)
	defer testServer.Close()

	directModel := "gpt-5.6-terra"
	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "deepseek-v4-flash",
				MediumModel:                 "deepseek-v4-flash",
				HighModel:                   "deepseek-v4-flash",
				MediumProbability:      medProb(0.10),
				HighProbability:        highProb(0.01),
				DirectModel: strPtr(directModel),
			},
		},
	}

	recorder := newTestRecorder(t, cfg)

	handler := NewHandler(cfg, upstream, recorder, nil, nil)

	body := []byte(`{
		"model": "coder1",
		"messages": [
			{"role": "system", "content": "you are a coder"},
			{"role": "user", "content": [
				{"type": "text", "text": "what does this error mean?"},
				{"type": "image_url", "image_url": {"url": "https://example.com/error.png"}}
			]}
		]
	}`)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		bytes.NewReader(body),
	)
	rec := httptest.NewRecorder()
	handler.ChatCompletions(rec, req)

	if capturedModel != directModel {
		t.Fatalf("expected model %q (DIRECT), got %q", directModel, capturedModel)
	}

	// DIRECT 模型支持多模态，图片 part 必须保留
	if !messageHasImagePart(capturedMessages[1]) {
		t.Fatalf("expected image_url part kept for DIRECT model, got %s", string(capturedMessages[1].Content))
	}
}
