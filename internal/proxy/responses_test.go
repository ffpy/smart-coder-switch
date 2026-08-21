package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/protocol/openai"
	"smart-coder-switch/internal/trace"
)

func responsesUpstreamRequestCapture(t *testing.T, capturedModel *string, capturedInput *json.RawMessage) (*httptest.Server, *Upstream) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var req struct {
			Model string          `json:"model"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		*capturedModel = req.Model
		*capturedInput = req.Input

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_test","object":"response","model":"gpt-5.6-luna","output":[]}`)
	}))

	upstream, err := NewUpstream(server.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return server, upstream
}

func newResponsesTestRecorder(t *testing.T, cfg *config.Config) *trace.Recorder {
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

	rec, err := trace.NewRecorder(cfg.Trace)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func responsesBody(model string, input any) []byte {
	var inputRaw json.RawMessage
	switch v := input.(type) {
	case string:
		b, _ := json.Marshal(v)
		inputRaw = b
	default:
		b, _ := json.Marshal(v)
		inputRaw = b
	}
	body := map[string]any{
		"model": model,
		"input": json.RawMessage(inputRaw),
	}
	b, _ := json.Marshal(body)
	return b
}

func TestResponses_DirectRouteInjectsPrompt(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

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

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := responsesBody("coder1", []map[string]any{
		{"role": "user", "content": "write a function"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	if capturedModel != directModel {
		t.Fatalf("expected model %q, got %q", directModel, capturedModel)
	}

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(items))
	}

	lastItem := items[len(items)-1]
	if lastItem["role"] != "user" {
		t.Fatalf("expected last input role=user, got %v", lastItem["role"])
	}
	if s, _ := lastItem["content"].(string); !strings.Contains(s, "先") {
		t.Fatalf("expected DIRECT prompt contain '先', got %q", s)
	}
}

func TestResponses_LowModelRoutesByProbability(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

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

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := responsesBody("coder1", []map[string]any{
		{"role": "system", "content": "you are a coder"},
		{"role": "user", "content": "write a function"},
		{"role": "assistant", "content": "here is code"},
		{"role": "tool", "content": "ok"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	if capturedModel != "low-coder1" {
		t.Fatalf("expected model %q, got %q", "low-coder1", capturedModel)
	}

	var items []openai.Message
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 input items, got %d", len(items))
	}
}

func TestResponses_IgnoredPrefixSkipsDirect(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

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

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := responsesBody("coder1", []map[string]any{
		{"role": "system", "content": "policy"},
		{"role": "user", "content": "<system-reminder>\nplan mode"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	if capturedModel != "low-coder1" {
		t.Fatalf("expected model %q, got %q", "low-coder1", capturedModel)
	}

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(items))
	}
}

func TestResponses_HighRouteAppendsGuidance(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0),
				HighProbability:        highProb(1.0),
				DirectModel: strPtr("direct-model"),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.0 }

	body := responsesBody("coder1", []map[string]any{
		{"role": "user", "content": "go on"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	if capturedModel != "high-coder1" {
		t.Fatalf("expected model %q, got %q", "high-coder1", capturedModel)
	}

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(items))
	}

	lastItem := items[len(items)-1]
	if s, _ := lastItem["content"].(string); !strings.Contains(s, "Review") {
		t.Fatalf("expected HIGH review prompt, got %q", s)
	}
}

func TestResponses_StringInputNormalizedAndInjected(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

	directModel := "gpt-5.6-terra"
	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0),
				HighProbability:        highProb(0),
				DirectModel: strPtr(directModel),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := responsesBody("coder1", "write once")

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	if capturedModel != directModel {
		t.Fatalf("expected model %q, got %q", directModel, capturedModel)
	}

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(items))
	}

	// 输入为字符串时，Responses 格式将原字符串包装为 type:text item，
	// 其后追加 role:user 的 DIRECT 提示。原始文本保存在 type:text 条目的 text 字段中。
	if items[0]["type"] != "text" {
		t.Fatalf("expected first item type=text, got %v", items[0]["type"])
	}
	if s, _ := items[0]["text"].(string); s != "write once" {
		t.Fatalf("expected original text preserved, got %q", s)
	}
}

func TestResponses_ImagePromptInjected(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0),
				HighProbability:        highProb(0),
				DirectModel: strPtr("vision-model"),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := responsesBody("coder1", []map[string]any{
		{"type": "input_image", "image_url": "https://example.com/img.png"},
		{"type": "input_text", "text": "what is this?"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	if capturedModel != "vision-model" {
		t.Fatalf("expected model %q, got %q", "vision-model", capturedModel)
	}

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(items))
	}

	lastItem := items[len(items)-1]
	if s, _ := lastItem["content"].(string); !strings.Contains(s, "图片理解") {
		t.Fatalf("expected image prompt, got %q", s)
	}
}

func TestResponses_UnknownModelForwardsTransparently(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0),
				HighProbability:        highProb(0),
				DirectModel: strPtr("vision-model"),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := responsesBody("unknown-model", []map[string]any{
		{"role": "user", "content": "keep body as-is"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	if capturedModel != "unknown-model" {
		t.Fatalf("expected model %q, got %q", "unknown-model", capturedModel)
	}

	var input string
	if err := json.Unmarshal(capturedInput, &input); err == nil {
		if input != "keep body as-is" {
			t.Fatalf("expected original input forwarded, got %q", input)
		}
		return
	}

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 input item, got %d", len(items))
	}
	if s, _ := items[0]["content"].(string); s != "keep body as-is" {
		t.Fatalf("expected original input forwarded, got %q", s)
	}
}

func TestResponses_StripImageItems(t *testing.T) {
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, new(string), &capturedInput)
	defer server.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               "low-coder1",
				MediumModel:            "medium-coder1",
				HighModel:              "high-coder1",
				MediumProbability: medProb(0),
				HighProbability:   highProb(0),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := responsesBody("coder1", []map[string]any{
		{"type": "input_image", "image_url": "https://example.com/a.png"},
		{"type": "input_image", "image_url": "https://example.com/b.png"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 input item after strip, got %d", len(items))
	}
	if s, _ := items[0]["content"].(string); !strings.Contains(s, "图片内容已由前序支持多模态的模型转写处理") {
		t.Fatalf("expected placeholder text, got %q", s)
	}
}

func TestResponses_EmptyModelReturnsBadRequest(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               "low-coder1",
				MediumModel:            "medium-coder1",
				HighModel:              "high-coder1",
				MediumProbability: medProb(0),
				HighProbability:   highProb(0),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.0 }

	body := []byte(`{"model":"","input":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestResponses_InvalidInputReturnsBadRequest(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               "low-coder1",
				MediumModel:            "medium-coder1",
				HighModel:              "high-coder1",
				MediumProbability: medProb(0),
				HighProbability:   highProb(0),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.0 }

	body := []byte(`{"model":"coder1","input":"not-json"`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	// 目前实现中 input 字符串合法，仅空 model 和非法 JSON 会 400。
	// 此处确保非法 JSON 能返回 400。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestResponses_FunctionCallAndOutputNormalized(t *testing.T) {
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, new(string), &capturedInput)
	defer server.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               "low-coder1",
				MediumModel:            "medium-coder1",
				HighModel:              "high-coder1",
				MediumProbability: medProb(0),
				HighProbability:   highProb(0),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.99 }

	body := responsesBody("coder1", []map[string]any{
		{"role": "user", "content": "do something"},
		{"type": "function_call", "call_id": "call_1", "name": "search", "arguments": "{}"},
		{"type": "function_call_output", "call_id": "call_1", "output": "done"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(items))
	}
	// Responses 格式的 function_call 保留原始 type 字段，不转为 role:assistant。
	if items[1]["type"] != "function_call" {
		t.Fatalf("expected function_call type preserved, got %v", items[1]["type"])
	}
	if items[1]["name"] != "search" {
		t.Fatalf("expected function_call name preserved, got %v", items[1]["name"])
	}
	if items[2]["type"] != "function_call_output" {
		t.Fatalf("expected function_call_output type preserved, got %v", items[2]["type"])
	}
	if items[2]["call_id"] != "call_1" {
		t.Fatalf("expected function_call_output call_id preserved, got %v", items[2]["call_id"])
	}
}

func TestResponses_AppendPreservesExistingInput(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               "low-coder1",
				MediumModel:            "medium-coder1",
				HighModel:              "high-coder1",
				MediumProbability: medProb(0),
				HighProbability:   highProb(1.0),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.0 }

	body := responsesBody("coder1", []map[string]any{
		{"role": "user", "content": "do A"},
		{"role": "user", "content": "do B"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	var items []map[string]any
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(items))
	}
	if s, _ := items[0]["content"].(string); s != "do A" {
		t.Fatalf("expected original item A preserved, got %q", s)
	}
	if s, _ := items[1]["content"].(string); s != "do B" {
		t.Fatalf("expected original item B preserved, got %q", s)
	}
	if !strings.Contains(items[2]["content"].(string), "Review") {
		t.Fatalf("expected review prompt appended, got %q", items[2]["content"])
	}
}

func TestResponses_ContinuationSkipsDirect(t *testing.T) {
	var capturedModel string
	var capturedInput json.RawMessage

	server, upstream := responsesUpstreamRequestCapture(t, &capturedModel, &capturedInput)
	defer server.Close()

	directModel := "gpt-5.6-terra"
	cfg := &config.Config{
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:                    "low-coder1",
				MediumModel:                 "medium-coder1",
				HighModel:                   "high-coder1",
				MediumProbability:      medProb(0.20),
				HighProbability:        highProb(0.00),
				DirectModel: strPtr(directModel),
			},
		},
	}

	handler := NewHandler(cfg, upstream, newResponsesTestRecorder(t, cfg), nil, nil)
	handler.randomFunc = func() float64 { return 0.0 }

	body := responsesBody("coder1", []map[string]any{
		{"role": "assistant", "content": "working"},
		{"role": "user", "content": "继续"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.Responses(rec, req)

	// "继续" 属于续接消息，不走 DIRECT；在当前随机值下应走 MEDIUM。
	if capturedModel != "medium-coder1" {
		t.Fatalf("expected model %q, got %q", "medium-coder1", capturedModel)
	}

	var items []openai.Message
	if err := json.Unmarshal(capturedInput, &items); err != nil {
		t.Fatalf("parse input: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(items))
	}
}
