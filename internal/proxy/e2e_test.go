package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/protocol/openai"
	"smart-coder-switch/internal/stats"
	"smart-coder-switch/internal/trace"
)

const (
	e2eUpstreamURL  = "https://one-api.ffpy.site/"
	e2eModel        = "gpt-5.6-luna"
	e2eLogicalModel = "coder1"
)

func e2eEnabled() bool {
	return os.Getenv("TEST_E2E") == "1"
}

func e2eToken() string {
	return os.Getenv("E2E_API_TOKEN")
}

func e2eSkip(t *testing.T) {
	t.Helper()
	if !e2eEnabled() {
		t.Skip("E2E tests disabled (set TEST_E2E=1 and E2E_API_TOKEN to enable)")
	}
	if e2eToken() == "" {
		t.Skip("E2E tests disabled (E2E_API_TOKEN not set)")
	}
}

func e2eConfig() *config.Config {
	med := 0.1
	high := 0.01
	return &config.Config{
		Listen: config.ListenConfig{Address: "127.0.0.1:0"},
		Upstream: config.UpstreamConfig{
			BaseURL: e2eUpstreamURL,
		},
		Models: map[string]config.ModelProfile{
			e2eLogicalModel: {
				LowModel:               e2eModel,
				MediumModel:            e2eModel,
				HighModel:              e2eModel,
				MediumProbability: &med,
				HighProbability:   &high,
			},
		},
	}
}

func e2eTraceRecorder(t *testing.T, cfg *config.Config) *trace.Recorder {
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

func e2eRequest(t *testing.T, body map[string]any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e2eToken())
	return req
}

func e2eNewHandler(t *testing.T, cfg *config.Config) (*Handler, *httptest.Server) {
	t.Helper()
	rec := e2eTraceRecorder(t, cfg)
	upstream, err := NewUpstream(cfg.Upstream.BaseURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	counter := stats.NewCounter()
	h := NewHandler(cfg, upstream, rec, counter, nil, nil)
	return h, nil
}

// --- Non-stream E2E ---

func TestE2E_Responses_NonStream(t *testing.T) {
	e2eSkip(t)

	cfg := e2eConfig()
	h, _ := e2eNewHandler(t, cfg)

	body := map[string]any{
		"model": e2eLogicalModel,
		"input": "What is 2+2?",
	}
	req := e2eRequest(t, body)
	w := httptest.NewRecorder()
	h.Responses(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Response should have standard Responses format fields
	if result["object"] != "response" {
		t.Errorf("object = %q, want %q", result["object"], "response")
	}
	if result["model"] != e2eModel {
		t.Errorf("model = %q, want %q", result["model"], e2eModel)
	}

	t.Logf("Non-stream E2E: PASS (id=%v)", result["id"])
}

// --- Stream E2E ---

// flushResponseWriter wraps httptest.ResponseRecorder to implement http.Flusher,
// which is required for SSE streaming through the reverse proxy.
type flushResponseWriter struct {
	*httptest.ResponseRecorder
}

func (fw *flushResponseWriter) Flush() {}

func TestE2E_Responses_Stream(t *testing.T) {
	e2eSkip(t)

	cfg := e2eConfig()
	h, _ := e2eNewHandler(t, cfg)

	body := map[string]any{
		"model":  e2eLogicalModel,
		"input":  "Say hello in one sentence",
		"stream": true,
	}
	req := e2eRequest(t, body)
	w := httptest.NewRecorder()
	fw := &flushResponseWriter{w}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Responses(fw, req)
	}()

	select {
	case <-done:
	case <-time.After(90 * time.Second):
		t.Fatal("streaming E2E timed out after 90s")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", contentType)
	}

	respBody, _ := io.ReadAll(resp.Body)
	bodyStr := string(respBody)

	// Check SSE events are forwarded
	if !strings.Contains(bodyStr, "data:") {
		t.Error("response missing SSE data events")
	}

	t.Logf("Stream E2E: PASS (body length=%d)", len(bodyStr))
}

// --- Route injection E2E ---

func TestE2E_Responses_RouteInjection(t *testing.T) {
	e2eSkip(t)

	var capturedInput json.RawMessage

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model string          `json:"model"`
			Input json.RawMessage `json:"input"`
		}
		json.Unmarshal(raw, &req)
		capturedInput = req.Input

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_e2e_inject","object":"response","model":"`+e2eModel+`","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer upstream.Close()

	cfg := e2eConfig()
	cfg.Upstream.BaseURL = upstream.URL
	directModel := e2eModel
	profile := cfg.Models[e2eLogicalModel]
	profile.DirectModel = &directModel
	cfg.Models[e2eLogicalModel] = profile
	rec := e2eTraceRecorder(t, cfg)
	upstreamH, _ := NewUpstream(upstream.URL, 0)
	counter := stats.NewCounter()
	h := NewHandler(cfg, upstreamH, rec, counter, nil, nil)

	// First message: new task → should get DIRECT injection (first user message)
	body := map[string]any{
		"model": e2eLogicalModel,
		"input": "Create a REST API endpoint",
	}
	req := e2eRequest(t, body)
	w := httptest.NewRecorder()
	h.Responses(w, req)

	if w.Result().StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("status %d: %s", w.Result().StatusCode, respBody)
	}

	// Parse input array
	var inputArr []json.RawMessage
	json.Unmarshal(capturedInput, &inputArr)

	// Should have at least 2 input items: original + injected prompt
	if len(inputArr) < 2 {
		t.Fatalf("expected >=2 input items for DIRECT injection, got %d: %s", len(inputArr), capturedInput)
	}

	// Last item should be the injected user message with prompt
	var lastItem map[string]any
	json.Unmarshal(inputArr[len(inputArr)-1], &lastItem)
	if lastItem["role"] != "user" {
		t.Errorf("last input role = %q, want %q", lastItem["role"], "user")
	}
	content, _ := lastItem["content"].(string)
	if !strings.Contains(content, "请先") && !strings.Contains(content, "不要") {
		t.Errorf("injected content doesn't look like first-turn prompt: %.80s", content)
	}

	t.Log("Route injection E2E: PASS")
}

// --- Chat endpoint unchanged E2E ---

func TestE2E_ChatCompletions_Unchanged(t *testing.T) {
	e2eSkip(t)

	cfg := e2eConfig()
	h, _ := e2eNewHandler(t, cfg)

	body := map[string]any{
		"model": e2eLogicalModel,
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e2eToken())

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Result().StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("status %d: %s", w.Result().StatusCode, respBody)
	}

	respBody, _ := io.ReadAll(w.Result().Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)
	t.Logf("Chat Completions unchanged E2E: PASS (id=%v)", result["id"])
}

// --- Empty input E2E ---

func TestE2E_Responses_EmptyInput(t *testing.T) {
	e2eSkip(t)

	cfg := e2eConfig()
	h, _ := e2eNewHandler(t, cfg)

	body := map[string]any{
		"model": e2eLogicalModel,
		"input": []any{},
	}
	req := e2eRequest(t, body)
	w := httptest.NewRecorder()
	h.Responses(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Result().StatusCode)
	}

	t.Log("Empty input E2E: PASS")
}

// --- Input array preservation E2E ---

func TestE2E_Responses_InputArrayPreserved(t *testing.T) {
	e2eSkip(t)

	var capturedInput json.RawMessage

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model string          `json:"model"`
			Input json.RawMessage `json:"input"`
		}
		json.Unmarshal(raw, &req)
		capturedInput = req.Input

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_e2e_arr","object":"response","model":"`+e2eModel+`","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer upstream.Close()

	cfg := e2eConfig()
	cfg.Upstream.BaseURL = upstream.URL
	rec := e2eTraceRecorder(t, cfg)
	upstreamH, _ := NewUpstream(upstream.URL, 0)
	counter := stats.NewCounter()
	h := NewHandler(cfg, upstreamH, rec, counter, nil, nil)

	inputItems := []map[string]any{
		{"type": "message", "role": "user", "content": "What is Python?"},
		{"type": "message", "role": "assistant", "content": "Python is a programming language."},
		{"type": "message", "role": "user", "content": "Tell me more"},
	}
	body := map[string]any{
		"model": e2eLogicalModel,
		"input": inputItems,
	}
	req := e2eRequest(t, body)
	w := httptest.NewRecorder()
	h.Responses(w, req)

	if w.Result().StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("status %d: %s", w.Result().StatusCode, respBody)
	}

	// Parse input array
	var inputArr []json.RawMessage
	json.Unmarshal(capturedInput, &inputArr)

	// The 3 original items should be present
	if len(inputArr) < 3 {
		t.Fatalf("expected >=3 input items, got %d: %s", len(inputArr), capturedInput)
	}

	// Check first item preserved
	var first map[string]any
	json.Unmarshal(inputArr[0], &first)
	if first["role"] != "user" || first["content"] != "What is Python?" {
		t.Errorf("input[0] = %v, want role=user, content='What is Python?'", first)
	}

	// Check second item preserved
	var second map[string]any
	json.Unmarshal(inputArr[1], &second)
	if second["role"] != "assistant" {
		t.Errorf("input[1].role = %q, want %q", second["role"], "assistant")
	}

	// "Tell me more" is NOT a continuation phrase → should trigger DIRECT → injected prompt appended
	// So we should see 4 items: 3 original + 1 injected
	if len(inputArr) == 4 {
		var last map[string]any
		json.Unmarshal(inputArr[3], &last)
		if last["role"] != "user" {
			t.Errorf("injected item role = %q, want %q", last["role"], "user")
		}
		t.Log("Input array preserved with DIRECT injection: PASS")
	} else if len(inputArr) == 3 {
		t.Log("Input array preserved without injection (LOW route): PASS")
	} else {
		t.Errorf("unexpected input count: %d", len(inputArr))
	}
}

// --- Image in input E2E ---

func TestE2E_Responses_ImageInInput(t *testing.T) {
	e2eSkip(t)

	var capturedInput json.RawMessage

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model string          `json:"model"`
			Input json.RawMessage `json:"input"`
		}
		json.Unmarshal(raw, &req)
		capturedInput = req.Input

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_e2e_img","object":"response","model":"`+e2eModel+`","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I see an image"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer upstream.Close()

	cfg := e2eConfig()
	cfg.Upstream.BaseURL = upstream.URL
	rec := e2eTraceRecorder(t, cfg)
	upstreamH, _ := NewUpstream(upstream.URL, 0)
	counter := stats.NewCounter()
	h := NewHandler(cfg, upstreamH, rec, counter, nil, nil)

	body := map[string]any{
		"model": e2eLogicalModel,
		"input": []any{
			map[string]any{"type": "input_image", "image_url": "https://example.com/img.png"},
			map[string]any{"type": "input_text", "text": "What do you see?"},
		},
	}
	req := e2eRequest(t, body)
	w := httptest.NewRecorder()
	h.Responses(w, req)

	if w.Result().StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("status %d: %s", w.Result().StatusCode, respBody)
	}

	// Parse input array
	var inputArr []map[string]any
	json.Unmarshal(capturedInput, &inputArr)

	// Image should be stripped, replaced with placeholder
	hasImage := false
	for _, item := range inputArr {
		if item["type"] == "input_image" {
			hasImage = true
		}
	}
	if hasImage {
		t.Error("input_image should have been stripped for non-multimodal model")
	}

	t.Log("Image in input E2E: PASS")
}

// --- Unknown model transparent forward E2E ---

func TestE2E_Responses_UnknownModel(t *testing.T) {
	e2eSkip(t)

	var capturedModel string
	var capturedInput json.RawMessage

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model string          `json:"model"`
			Input json.RawMessage `json:"input"`
		}
		json.Unmarshal(raw, &req)
		capturedModel = req.Model
		capturedInput = req.Input

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_e2e_unknown","object":"response","model":"unknown-model","output":[],"usage":{"input_tokens":1,"output_tokens":0}}`)
	}))
	defer upstream.Close()

	cfg := e2eConfig()
	cfg.Upstream.BaseURL = upstream.URL
	rec := e2eTraceRecorder(t, cfg)
	upstreamH, _ := NewUpstream(upstream.URL, 0)
	counter := stats.NewCounter()
	h := NewHandler(cfg, upstreamH, rec, counter, nil, nil)

	body := map[string]any{
		"model": "totally-unknown-model",
		"input": "test",
	}
	req := e2eRequest(t, body)
	w := httptest.NewRecorder()
	h.Responses(w, req)

	if w.Result().StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("status %d: %s", w.Result().StatusCode, respBody)
	}

	// Unknown model: transparent forward, model unchanged
	if capturedModel != "totally-unknown-model" {
		t.Errorf("model = %q, want %q (transparent forward)", capturedModel, "totally-unknown-model")
	}

	// Input should be preserved as-is (string not converted since no routing needed)
	var inputStr string
	if err := json.Unmarshal(capturedInput, &inputStr); err != nil {
		// Might be already a JSON string
		t.Logf("input = %s (raw)", capturedInput)
	} else {
		if inputStr != "test" {
			t.Errorf("input = %q, want %q", inputStr, "test")
		}
	}

	t.Log("Unknown model E2E: PASS")
}

// Helper: verify openai.ParseRequest still works for Chat
func TestE2E_ChatParseRequest(t *testing.T) {
	e2eSkip(t)

	body := `{"model":"coder1","messages":[{"role":"user","content":"hi"}]}`
	req, err := openai.ParseRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "coder1" {
		t.Errorf("model = %q, want %q", req.Model, "coder1")
	}
	if len(req.Messages) != 1 {
		t.Errorf("messages len = %d, want 1", len(req.Messages))
	}
	t.Log("Chat ParseRequest: PASS")
}
