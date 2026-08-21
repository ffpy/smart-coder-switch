package proxy

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpstreamErrorHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	oldDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	upstream, err := NewUpstream("http://127.0.0.1:19999/", 1)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	upstream.ServeHTTP(rec, req)

	// 应返回 502 Bad Gateway
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway, got %d", rec.Code)
	}

	// 应通过 slog 输出错误日志
	output := buf.String()
	t.Logf("captured log output:\n%s", output)

	if !strings.Contains(output, "upstream request failed") {
		t.Fatal("expected slog error about upstream failure")
	}
}

func TestUpstreamErrorHandlerWithTraceDir(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	oldDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	upstream, err := NewUpstream("http://127.0.0.1:19999/", 1)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(ContextWithTraceDir(req.Context(), "test-trace-001"))
	rec := httptest.NewRecorder()
	upstream.ServeHTTP(rec, req)

	output := buf.String()
	t.Logf("captured log output:\n%s", output)

	if !strings.Contains(output, "test-trace-001") {
		t.Fatal("expected trace_dir in error log output")
	}
	if !strings.Contains(output, "error_kind") {
		t.Fatal("expected error_kind in error log output")
	}
}

func TestUpstreamErrorHandlerClientCancel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	oldDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	upstream, err := NewUpstream("http://127.0.0.1:19999/", 1)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	upstream.ServeHTTP(rec, req)

	output := buf.String()
	t.Logf("captured log output:\n%s", output)

	if !strings.Contains(output, "client_cancel") {
		t.Fatal("expected error_kind=client_cancel for canceled client context")
	}
}

func TestUpstreamLogsNon2xxResponseBodySummary(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	oldDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid tool role order"}}`))
	}))
	defer upstreamServer.Close()

	upstream, err := NewUpstream(upstreamServer.URL+"/", 0)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(ContextWithTraceDir(req.Context(), "trace-400-body"))
	rec := httptest.NewRecorder()
	upstream.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":{"message":"invalid tool role order"}}` {
		t.Fatalf("expected original response body preserved, got %q", got)
	}

	output := buf.String()
	t.Logf("captured log output:\n%s", output)

	if !strings.Contains(output, "upstream non-2xx response") {
		t.Fatal("expected non-2xx upstream response log")
	}
	if !strings.Contains(output, "trace-400-body") {
		t.Fatal("expected trace_dir in non-2xx response log")
	}
	if !strings.Contains(output, "invalid tool role order") {
		t.Fatal("expected response body preview in non-2xx response log")
	}
}
