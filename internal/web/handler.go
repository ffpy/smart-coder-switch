package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"smart-coder-switch/internal/config"
)

// traceDirNamePattern 匹配 trace 记录目录名，
// 例如 20260803-105317.252075064-001861。
var traceDirNamePattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}\.[0-9]{9}-[0-9]{6}$`)

// TraceDetail 返回单个 trace 记录的路由决策信息与最后一条消息。
// 原始请求通过独立接口 GET /admin/traces/{trace_dir}/request 按需获取。
type TraceDetail struct {
	TraceDir    string          `json:"trace_dir"`
	Decision    json.RawMessage `json:"decision"`
	LastMessage json.RawMessage `json:"last_message,omitempty"`
	Headers     json.RawMessage `json:"headers,omitempty"`
}

// Handler serves the web control panel API.
type Handler struct {
	store   *Store
	manager *config.Manager
}

// NewHandler creates a Handler that reads decision logs from store
// and reads/writes config through manager.
func NewHandler(store *Store, manager *config.Manager) *Handler {
	return &Handler{
		store:   store,
		manager: manager,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/admin/decisions":
		h.handleDecisions(w, r)
	case "/admin/decisions/distribution":
		h.handleDistribution(w, r)
	case "/admin/config/form":
		h.handleConfigForm(w, r)
	case "/admin/stats/models":
		h.handleStatsModels(w, r)
	case "/admin/stats/models/reset":
		h.handleStatsReset(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/admin/traces/") {
			h.handleTrace(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

// GET /admin/traces/{trace_dir} — 返回单个 trace 记录的路由决策信息
// GET /admin/traces/{trace_dir}/request — 返回该记录的原始请求
func (h *Handler) handleTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/admin/traces/")
	// 原始请求子路径：/admin/traces/{name}/request
	if strings.HasSuffix(name, "/request") {
		h.handleTraceRequest(w, r, strings.TrimSuffix(name, "/request"))
		return
	}

	if !traceDirNamePattern.MatchString(name) {
		http.Error(w, "invalid trace directory name", http.StatusBadRequest)
		return
	}

	baseDir := h.manager.Snapshot().Config.Trace.Directory
	if baseDir == "" {
		http.NotFound(w, r)
		return
	}

	recordDir := filepath.Join(baseDir, name)

	// 防御：确保解析后的路径仍位于 trace 根目录内
	cleanBase := filepath.Clean(baseDir)
	cleanRecord := filepath.Clean(recordDir)
	if cleanRecord != filepath.Join(cleanBase, filepath.Clean(name)) {
		http.Error(w, "invalid trace directory path", http.StatusBadRequest)
		return
	}

	decisionData, err := os.ReadFile(filepath.Join(recordDir, "decision.json"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	lastMsg, err := readLastMessage(filepath.Join(recordDir, "request.json"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	detail := TraceDetail{
		TraceDir: name,
		Decision: json.RawMessage(decisionData),
	}
	if lastMsg != nil {
		detail.LastMessage = json.RawMessage(lastMsg)
	}

	headersData, err := os.ReadFile(filepath.Join(recordDir, "headers.json"))
	if err == nil && len(headersData) > 0 {
		detail.Headers = json.RawMessage(headersData)
	}

	writeJSON(w, http.StatusOK, detail)
}

// GET /admin/traces/{trace_dir}/request — 下载原始请求 JSON
func (h *Handler) handleTraceRequest(w http.ResponseWriter, r *http.Request, name string) {
	if !traceDirNamePattern.MatchString(name) {
		http.Error(w, "invalid trace directory name", http.StatusBadRequest)
		return
	}

	baseDir := h.manager.Snapshot().Config.Trace.Directory
	if baseDir == "" {
		http.NotFound(w, r)
		return
	}

	recordDir := filepath.Join(baseDir, name)

	// 防御：确保解析后的路径仍位于 trace 根目录内
	cleanBase := filepath.Clean(baseDir)
	cleanRecord := filepath.Clean(recordDir)
	if cleanRecord != filepath.Join(cleanBase, filepath.Clean(name)) {
		http.Error(w, "invalid trace directory path", http.StatusBadRequest)
		return
	}

	requestData, err := os.ReadFile(filepath.Join(recordDir, "request.json"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var formatted bytes.Buffer
	if err := json.Indent(&formatted, requestData, "", "  "); err != nil {
		http.Error(w, "invalid trace request JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="trace-request-%s.json"`, name))
	w.WriteHeader(http.StatusOK)
	_, _ = formatted.WriteTo(w)
}

// readLastMessage 从 request.json 中读取并返回最后一条消息的 JSON 字节
func readLastMessage(requestPath string) ([]byte, error) {
	data, err := os.ReadFile(requestPath)
	if err != nil {
		return nil, err
	}

	var req struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	if len(req.Messages) == 0 {
		return nil, nil
	}

	return req.Messages[len(req.Messages)-1], nil
}

// GET /admin/decisions?logical_model=&tier=&query=&limit=&before=
func (h *Handler) handleDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()

	query := DecisionQuery{
		LogicalModel: q.Get("logical_model"),
		Tier:         q.Get("tier"),
		Query:        q.Get("query"),
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			query.Limit = v
		}
	}

	if beforeStr := q.Get("before"); beforeStr != "" {
		if v, err := strconv.ParseInt(beforeStr, 10, 64); err == nil {
			t := time.Unix(v, 0).UTC()
			query.Before = &t
		}
	}

	result, err := h.store.QueryDecisions(r.Context(), query)
	if err != nil {
		http.Error(w, fmt.Sprintf("query decisions: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// GET /admin/decisions/distribution?logical_model=&minutes=
func (h *Handler) handleDistribution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()

	query := DistributionQuery{
		LogicalModel: q.Get("logical_model"),
	}

	if minutesStr := q.Get("minutes"); minutesStr != "" {
		if v, err := strconv.Atoi(minutesStr); err == nil {
			query.Minutes = v
		}
	}

	result, err := h.store.QueryDistribution(r.Context(), query)
	if err != nil {
		http.Error(w, fmt.Sprintf("query distribution: %v", err), http.StatusInternalServerError)
		return
	}

	// 若配置了 direct-model，确保 DIRECT 档始终出现在分布中（未命中时显示 0）
	if query.LogicalModel != "" {
		snapshot := h.manager.Snapshot()
		if profile, ok := snapshot.Config.Models[query.LogicalModel]; ok && profile.DirectModel != nil {
			found := false
			for _, tier := range result.Tiers {
				if tier.Name == "DIRECT" {
					found = true
					break
				}
			}
			if !found {
				result.Tiers = append(result.Tiers, DistributionTier{Name: "DIRECT", Count: 0, Ratio: 0})
			}
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// GET /admin/config/form — 返回当前配置的结构化 JSON
// PUT /admin/config/form — 从 JSON 表单保存配置并触发热重载
func (h *Handler) handleConfigForm(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleConfigFormGet(w, r)
	case http.MethodPut:
		h.handleConfigFormPut(w, r)
	default:
		http.Error(w, "", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleConfigFormGet(w http.ResponseWriter, r *http.Request) {
	snapshot := h.manager.Snapshot()
	writeJSON(w, http.StatusOK, snapshot.Config)
}

func (h *Handler) handleConfigFormPut(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, fmt.Sprintf("decode config: %v", err), http.StatusBadRequest)
		return
	}

	snapshot, err := h.manager.SaveAndReload(&cfg)
	if err != nil {
		http.Error(w, fmt.Sprintf("save config: %v", err), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, snapshot.Config)
}

// GET /admin/stats/models — 从持久化决策记录聚合统计
func (h *Handler) handleStatsModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	snap, err := h.store.QueryStats(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("query stats: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// POST /admin/stats/models/reset — 清空决策记录（统计随之归零）
func (h *Handler) handleStatsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	if err := h.store.ClearStats(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("clear stats: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": true})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
