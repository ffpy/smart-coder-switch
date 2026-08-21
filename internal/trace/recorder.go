package trace

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"smart-coder-switch/internal/config"
)

type Decision struct {
	RequestID     string `json:"requestId"`
	Time          string `json:"time"`
	LogicalModel  string `json:"logicalModel"`
	SelectedTier  string `json:"selectedTier"`
	SelectedModel string `json:"selectedModel"`
	RouteReason   string `json:"routeReason"`
	BodySize      int    `json:"bodySize"`

	MediumProbability float64 `json:"mediumProbability"`
	HighProbability   float64 `json:"highProbability"`
	RandomValue       float64 `json:"randomValue"`
	AssistantCount    int     `json:"assistantCount"`
	HighRounds        int     `json:"highRounds"`
	MediumRounds      int     `json:"mediumRounds"`

	PromptInjected      bool   `json:"promptInjected"`
	PromptInjectionKind string `json:"promptInjectionKind"`
	ImagePromptInjected bool   `json:"imagePromptInjected,omitempty"`
	ImagePartsStripped  bool   `json:"imagePartsStripped,omitempty"`

	CompatInjected         bool   `json:"compatInjected,omitempty"`
	CompatKind             string `json:"compatKind,omitempty"`
	CompatToolCallIDPrefix string `json:"compatToolCallIdPrefix,omitempty"`

	ContinuationSkipped      bool   `json:"continuationSkipped,omitempty"`
	GuidanceHistoryDetected  bool   `json:"guidanceHistoryDetected,omitempty"`
	GuidanceFollowupInjected bool   `json:"guidanceFollowupInjected,omitempty"`
	GuidanceMarkerKinds      string `json:"guidanceMarkerKinds,omitempty"`
}

type Recorder struct {
	cfg config.TraceConfig

	mu  sync.Mutex
	seq atomic.Uint64
}

func NewRecorder(
	cfg config.TraceConfig,
) (*Recorder, error) {
	recorder := &Recorder{
		cfg: cfg,
	}

	if err := os.MkdirAll(
		cfg.Directory,
		0o755,
	); err != nil {
		return nil, fmt.Errorf(
			"create trace directory: %w",
			err,
		)
	}

	return recorder, nil
}

func (r *Recorder) Record(
	rawBody []byte,
	decision Decision,
	headers http.Header,
) (string, error) {
	if int64(len(rawBody)) > r.cfg.MaxBodySize {
		return "", fmt.Errorf(
			"request body exceeds trace max body size: size=%d max=%d",
			len(rawBody),
			r.cfg.MaxBodySize,
		)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	sequence := r.seq.Add(1)

	requestID := fmt.Sprintf(
		"req_%d_%06d",
		now.UnixNano(),
		sequence,
	)

	recordName := newRecordDirectoryName()

	recordDir := filepath.Join(
		r.cfg.Directory,
		recordName,
	)

	if err := os.MkdirAll(
		recordDir,
		0o755,
	); err != nil {
		return recordName, fmt.Errorf(
			"create trace record directory: %w",
			err,
		)
	}

	if err := os.WriteFile(
		filepath.Join(recordDir, "request.json"),
		rawBody,
		0o600,
	); err != nil {
		return recordName, fmt.Errorf(
			"write trace request: %w",
			err,
		)
	}

	if headers != nil {
		sanitized := sanitizeHeaders(headers)
		headersBody, err := json.MarshalIndent(sanitized, "", "  ")
		if err != nil {
			return recordName, fmt.Errorf(
				"encode trace headers: %w",
				err,
			)
		}
		if err := os.WriteFile(
			filepath.Join(recordDir, "headers.json"),
			headersBody,
			0o600,
		); err != nil {
			return recordName, fmt.Errorf(
				"write trace headers: %w",
				err,
			)
		}
	}

	decision.RequestID = requestID
	decision.Time = now.Format(time.RFC3339Nano)

	decisionBody, err := json.MarshalIndent(
		decision,
		"",
		"  ",
	)
	if err != nil {
		return recordName, fmt.Errorf(
			"encode trace decision: %w",
			err,
		)
	}

	if err := os.WriteFile(
		filepath.Join(recordDir, "decision.json"),
		decisionBody,
		0o600,
	); err != nil {
		return recordName, fmt.Errorf(
			"write trace decision: %w",
			err,
		)
	}

	if err := r.rotate(); err != nil {
		return recordName, fmt.Errorf(
			"rotate trace records: %w",
			err,
		)
	}

	return recordName, nil
}

func (r *Recorder) rotate() error {
	entries, err := os.ReadDir(r.cfg.Directory)
	if err != nil {
		return err
	}

	directories := make(
		[]os.DirEntry,
		0,
		len(entries),
	)

	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(
				directories,
				entry,
			)
		}
	}

	removeCount :=
		len(directories) - r.cfg.MaxRecords

	if removeCount <= 0 {
		return nil
	}

	for i := 0; i < removeCount; i++ {
		path := filepath.Join(
			r.cfg.Directory,
			directories[i].Name(),
		)

		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}

	return nil
}

// sensitiveHeaders 列出了需要脱敏的请求头（小写匹配）。
var sensitiveHeaders = map[string]bool{
	"authorization": true,
	"cookie":        true,
}

// sanitizeHeaders 复制 headers 并对敏感头的值进行脱敏。
// 脱敏规则：保留前 8 个字符，其余替换为 "***"。
func sanitizeHeaders(h http.Header) http.Header {
	sanitized := h.Clone()
	for key, values := range sanitized {
		if sensitiveHeaders[strings.ToLower(key)] {
			for i, v := range values {
				if len(v) > 8 {
					sanitized[key][i] = v[:8] + "***"
				} else {
					sanitized[key][i] = "***"
				}
			}
		}
	}
	return sanitized
}
