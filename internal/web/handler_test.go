package web_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/web"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestHandler(t *testing.T) (*web.Handler, *web.Store, *config.Manager) {
	t.Helper()
	db := newTestDB(t)
	store, err := web.NewStore(db, 100)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tmp := t.TempDir()
	path := tmp + "/config.yaml"
	cfg := &config.Config{
		Listen:   config.ListenConfig{Address: "0.0.0.0:18082"},
		Upstream: config.UpstreamConfig{BaseURL: "https://example.com/"},
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:          "gpt-4o-mini",
				MediumModel:       "gpt-4o",
				HighModel:         "gpt-4o",
				MediumProbability: ptrFloat64(0.1),
				HighProbability:   ptrFloat64(0.01),
			},
		},
		Log:   config.LogConfig{Level: "info"},
		Trace: config.TraceConfig{Directory: tmp + "/traces"},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}
	manager, err := config.NewManager(path, cfg, config.Load)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	handler := web.NewHandler(store, manager)
	return handler, store, manager
}

func ptrFloat64(f float64) *float64 { return &f }

func newTestHandlerWithTraceDir(t *testing.T, traceDir string) (*web.Handler, *web.Store, *config.Manager) {
	t.Helper()
	db := newTestDB(t)
	store, err := web.NewStore(db, 100)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tmp := t.TempDir()
	path := tmp + "/config.yaml"
	cfg := &config.Config{
		Listen:   config.ListenConfig{Address: "0.0.0.0:18082"},
		Upstream: config.UpstreamConfig{BaseURL: "https://example.com/"},
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:          "gpt-4o-mini",
				MediumModel:       "gpt-4o",
				HighModel:         "gpt-4o",
				MediumProbability: ptrFloat64(0.1),
				HighProbability:   ptrFloat64(0.01),
			},
		},
		Log:   config.LogConfig{Level: "info"},
		Trace: config.TraceConfig{Directory: traceDir},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}
	manager, err := config.NewManager(path, cfg, config.Load)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	handler := web.NewHandler(store, manager)
	return handler, store, manager
}

func writeTraceRecord(t *testing.T, traceDir, recordName, decisionJSON, requestJSON string) {
	t.Helper()
	recordPath := filepath.Join(traceDir, recordName)
	if err := os.MkdirAll(recordPath, 0o755); err != nil {
		t.Fatalf("mkdir trace record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(recordPath, "decision.json"), []byte(decisionJSON), 0o644); err != nil {
		t.Fatalf("write decision.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(recordPath, "request.json"), []byte(requestJSON), 0o644); err != nil {
		t.Fatalf("write request.json: %v", err)
	}
}

func TestHandler_GetTrace(t *testing.T) {
	traceDir := t.TempDir()
	recordName := "20260803-105317.252075064-001861"
	writeTraceRecord(t, traceDir, recordName,
		`{"logicalModel":"coder1","selectedTier":"LOW","randomValue":0.321}`,
		`{"model":"coder1","messages":[{"role":"user","content":"hi"}]}`,
	)

	handler, _, _ := newTestHandlerWithTraceDir(t, traceDir)

	req := httptest.NewRequest(http.MethodGet, "/admin/traces/"+recordName, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var detail web.TraceDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.TraceDir != recordName {
		t.Errorf("expected trace_dir %s, got %s", recordName, detail.TraceDir)
	}

	var decision map[string]any
	if err := json.Unmarshal(detail.Decision, &decision); err != nil {
		t.Fatalf("decision not valid json: %v", err)
	}
	if decision["selectedTier"] != "LOW" {
		t.Errorf("expected selectedTier LOW, got %v", decision["selectedTier"])
	}

	// 详情接口返回最后一条消息，供前端默认展示；原始请求需通过独立接口按需获取
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, exists := raw["request"]; exists {
		t.Errorf("expected detail response without request field, got %s", w.Body.String())
	}
	var lastMsg map[string]any
	if err := json.Unmarshal(raw["last_message"], &lastMsg); err != nil {
		t.Fatalf("last_message not valid json: %v", err)
	}
	if lastMsg["role"] != "user" {
		t.Errorf("expected last message role user, got %v", lastMsg["role"])
	}
}

func TestHandler_GetTraceRequest(t *testing.T) {
	traceDir := t.TempDir()
	recordName := "20260803-105317.252075064-001861"
	writeTraceRecord(t, traceDir, recordName,
		`{"logicalModel":"coder1","selectedTier":"LOW"}`,
		`{"model":"coder1","messages":[{"role":"user","content":"hi"}],"stream":false}`,
	)

	handler, _, _ := newTestHandlerWithTraceDir(t, traceDir)

	req := httptest.NewRequest(http.MethodGet, "/admin/traces/"+recordName+"/request", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="trace-request-`+recordName+`.json"` {
		t.Fatalf("expected attachment disposition, got %q", got)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("\n  \"model\": \"coder1\"")) {
		t.Fatalf("expected formatted JSON body, got %q", w.Body.String())
	}
	var request map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &request); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	messages, ok := request["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected 1 message, got %v", request["messages"])
	}
	if msg, ok := messages[0].(map[string]any); !ok || msg["role"] != "user" {
		t.Errorf("expected user message, got %v", messages[0])
	}
}

func TestHandler_GetTraceRequest_NotFound(t *testing.T) {
	traceDir := t.TempDir()
	handler, _, _ := newTestHandlerWithTraceDir(t, traceDir)

	req := httptest.NewRequest(http.MethodGet, "/admin/traces/20260803-105317.252075064-999999/request", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestHandler_GetTraceRequest_MethodNotAllowed(t *testing.T) {
	traceDir := t.TempDir()
	recordName := "20260803-105317.252075064-001861"
	writeTraceRecord(t, traceDir, recordName, `{}`, `{"messages":[]}`)
	handler, _, _ := newTestHandlerWithTraceDir(t, traceDir)

	req := httptest.NewRequest(http.MethodPost, "/admin/traces/"+recordName+"/request", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandler_GetTrace_NotFound(t *testing.T) {
	traceDir := t.TempDir()
	handler, _, _ := newTestHandlerWithTraceDir(t, traceDir)

	req := httptest.NewRequest(http.MethodGet, "/admin/traces/20260803-105317.252075064-999999", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestHandler_GetTrace_InvalidName(t *testing.T) {
	handler, _, _ := newTestHandlerWithTraceDir(t, t.TempDir())

	invalid := []string{
		"../outside",
		"..%2Foutside",
		"a/b/c",
		"20260803",
		"20260803-105317.252075064-001861/../../etc",
	}
	for _, name := range invalid {
		req := httptest.NewRequest(http.MethodGet, "/admin/traces/"+name, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
			t.Errorf("name %q: expected 4xx, got %d", name, w.Code)
		}
	}
}

func TestHandler_GetTrace_MethodNotAllowed(t *testing.T) {
	handler, _, _ := newTestHandlerWithTraceDir(t, t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/admin/traces/20260803-105317.252075064-001861", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func insertTestRecord(t *testing.T, store *web.Store, reqID string, tier string) {
	t.Helper()
	_ = store.Insert(context.Background(), web.DecisionRecord{
		RequestID:     reqID,
		Timestamp:     time.Now().UTC(),
		LogicalModel:  "coder1",
		SelectedTier:  tier,
		SelectedModel: "model-" + tier,
		Reason:        "test",
	})
}

func TestHandler_GetDecisions(t *testing.T) {
	handler, store, _ := newTestHandler(t)

	for i := 0; i < 5; i++ {
		insertTestRecord(t, store, "req-"+string(rune('0'+i)), "LOW")
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/decisions?logical_model=coder1&limit=3", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var result web.DecisionResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(result.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(result.Items))
	}
}

func TestHandler_GetDecisions_TierFilter(t *testing.T) {
	handler, store, _ := newTestHandler(t)

	insertTestRecord(t, store, "req-1", "LOW")
	insertTestRecord(t, store, "req-2", "HIGH")

	req := httptest.NewRequest(http.MethodGet, "/admin/decisions?tier=HIGH", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var result web.DecisionResult
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Items) != 1 {
		t.Errorf("expected 1 HIGH item, got %d", len(result.Items))
	}
	if result.Items[0].SelectedTier != "HIGH" {
		t.Errorf("expected tier HIGH, got %s", result.Items[0].SelectedTier)
	}
}

func TestHandler_GetDistribution(t *testing.T) {
	handler, store, _ := newTestHandler(t)

	insertTestRecord(t, store, "req-1", "LOW")
	insertTestRecord(t, store, "req-2", "HIGH")

	req := httptest.NewRequest(http.MethodGet, "/admin/decisions/distribution?logical_model=coder1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result web.DistributionResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected total 2, got %d", result.Total)
	}
	if len(result.Tiers) != 2 {
		t.Errorf("expected 2 tiers, got %d", len(result.Tiers))
	}
}

func TestHandler_GetDistribution_DirectTierAlwaysVisible(t *testing.T) {
	// 配置了 direct-model 时，DIRECT 档应始终出现在分布中（未命中时显示 0）
	db := newTestDB(t)
	store, err := web.NewStore(db, 100)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tmp := t.TempDir()
	cfg := &config.Config{
		Listen:   config.ListenConfig{Address: "0.0.0.0:18082"},
		Upstream: config.UpstreamConfig{BaseURL: "https://example.com/"},
		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:          "gpt-4o-mini",
				MediumModel:       "gpt-4o",
				HighModel:         "gpt-4o",
				MediumProbability: ptrFloat64(0.1),
				HighProbability:   ptrFloat64(0.01),
				DirectModel:       new("gpt-4o"),
			},
		},
		Log:   config.LogConfig{Level: "info"},
		Trace: config.TraceConfig{Directory: tmp + "/traces"},
	}
	path := tmp + "/config.yaml"
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	manager, err := config.NewManager(path, cfg, config.Load)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	handler := web.NewHandler(store, manager)

	// 只插入 LOW 和 MEDIUM 记录，不插入 DIRECT
	insertTestRecord(t, store, "req-1", "LOW")
	insertTestRecord(t, store, "req-2", "MEDIUM")

	req := httptest.NewRequest(http.MethodGet, "/admin/decisions/distribution?logical_model=coder1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result web.DistributionResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}

	// DIRECT 应出现在分布中，count 为 0
	tierMap := map[string]int64{}
	for _, tier := range result.Tiers {
		tierMap[tier.Name] = tier.Count
	}
	if tierMap["DIRECT"] != 0 {
		t.Errorf("DIRECT count = %d, want 0 (configured but no DIRECT requests)", tierMap["DIRECT"])
	}
	if tierMap["LOW"] != 1 {
		t.Errorf("LOW count = %d, want 1", tierMap["LOW"])
	}
	if tierMap["MEDIUM"] != 1 {
		t.Errorf("MEDIUM count = %d, want 1", tierMap["MEDIUM"])
	}
}
