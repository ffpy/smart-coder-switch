package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type Upstream struct {
	proxy *httputil.ReverseProxy
}

// contextKey 用于通过 context 传递请求关联值。
type contextKey string

const traceDirKey contextKey = "trace_dir"

const non2xxBodyPreviewLimit = 4096

// ContextWithTraceDir 将 trace 目录名注入 context。
func ContextWithTraceDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, traceDirKey, dir)
}

// TraceDirFromContext 从 context 中取出 trace 目录名。
func TraceDirFromContext(ctx context.Context) string {
	if dir, ok := ctx.Value(traceDirKey).(string); ok {
		return dir
	}
	return ""
}

func NewUpstream(
	baseURL string,
	timeout time.Duration,
) (*Upstream, error) {
	target, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if timeout > 0 {
		transport.ResponseHeaderTimeout = timeout
	}

	reverseProxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.SetXForwarded()
		},

		ModifyResponse: logNon2xxResponse,

		FlushInterval: -1,

		Transport: transport,

		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			attrs := []any{
				"method", r.Method,
				"url", r.URL.String(),
				"error", err,
			}

			// 关联 trace 目录
			if traceDir := TraceDirFromContext(r.Context()); traceDir != "" {
				attrs = append(attrs, "trace_dir", traceDir)
			}

			// 记录入站请求 context 状态
			if reqCtxErr := r.Context().Err(); reqCtxErr != nil {
				attrs = append(attrs, "request_context_error", reqCtxErr.Error())
			}

			// 分类错误来源
			var errKind string
			switch {
			case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
				errKind = "client_cancel"
			case errors.Is(err, context.Canceled):
				errKind = "upstream_cancel"
			case errors.Is(err, context.DeadlineExceeded):
				errKind = "upstream_timeout"
			default:
				errKind = "upstream_error"
			}
			attrs = append(attrs, "error_kind", errKind)

			slog.Error("upstream request failed", attrs...)

			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		},
	}

	return &Upstream{
		proxy: reverseProxy,
	}, nil
}

func logNon2xxResponse(resp *http.Response) error {
	if resp.StatusCode < 400 {
		return nil
	}

	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))

	attrs := []any{
		"status_code", resp.StatusCode,
		"method", resp.Request.Method,
		"url", resp.Request.URL.String(),
		"content_type", resp.Header.Get("Content-Type"),
		"body_size", len(body),
	}

	if traceDir := TraceDirFromContext(resp.Request.Context()); traceDir != "" {
		attrs = append(attrs, "trace_dir", traceDir)
	}

	if readErr != nil {
		attrs = append(attrs, "body_read_error", readErr.Error())
	}
	if closeErr != nil {
		attrs = append(attrs, "body_close_error", closeErr.Error())
	}

	bodyPreview := body
	bodyTruncated := false
	if len(bodyPreview) > non2xxBodyPreviewLimit {
		bodyPreview = bodyPreview[:non2xxBodyPreviewLimit]
		bodyTruncated = true
	}

	attrs = append(
		attrs,
		"body_truncated", bodyTruncated,
		"body_preview", string(bodyPreview),
	)

	slog.Warn("upstream non-2xx response", attrs...)

	return nil
}

func (u *Upstream) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	u.proxy.ServeHTTP(w, r)
}
