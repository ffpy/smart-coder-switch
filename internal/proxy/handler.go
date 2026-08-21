package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/protocol/openai"
	"smart-coder-switch/internal/routing"
	"smart-coder-switch/internal/stats"
	"smart-coder-switch/internal/trace"
)

type DecisionLog struct {
	RequestID      string
	LogicalModel   string
	SelectedTier   string
	SelectedModel  string
	AssistantCount int64
	Reason         string
	TraceDir       string
}

type DecisionLogFunc func(log DecisionLog)

// DecisionResult 上游转发完成后回写的请求结果摘要。
type DecisionResult struct {
	RequestID    string
	StatusCode   int
	ErrorMessage string
}

type DecisionResultFunc func(result DecisionResult)

type Handler struct {
	cfg            *config.Config
	upstream       *Upstream
	recorder       *trace.Recorder
	randomFunc     func() float64
	counter        *stats.Counter
	decisionLogger DecisionLogFunc
	resultLogger   DecisionResultFunc
}

func NewHandler(
	cfg *config.Config,
	upstream *Upstream,
	recorder *trace.Recorder,
	counter *stats.Counter,
	decisionLogger DecisionLogFunc,
	resultLoggers ...DecisionResultFunc,
) *Handler {
	var resultLogger DecisionResultFunc
	if len(resultLoggers) > 0 {
		resultLogger = resultLoggers[0]
	}
	return &Handler{
		cfg:            cfg,
		upstream:       upstream,
		recorder:       recorder,
		randomFunc:     routing.GenerateRandomValue,
		counter:        counter,
		decisionLogger: decisionLogger,
		resultLogger:   resultLogger,
	}
}

// routeByPath 将已热重载的 Handler 分派到对应 OpenAI 协议端点。
func (h *Handler) routeByPath(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/chat/completions":
		h.ChatCompletions(w, r)
	case "/v1/responses":
		h.Responses(w, r)
	default:
		http.NotFound(w, r)
	}
}

func NewHandlerWithRandom(
	cfg *config.Config,
	upstream *Upstream,
	recorder *trace.Recorder,
	randomFunc func() float64,
	counter *stats.Counter,
	decisionLogger DecisionLogFunc,
	resultLoggers ...DecisionResultFunc,
) *Handler {
	var resultLogger DecisionResultFunc
	if len(resultLoggers) > 0 {
		resultLogger = resultLoggers[0]
	}
	return &Handler{
		cfg:            cfg,
		upstream:       upstream,
		recorder:       recorder,
		randomFunc:     randomFunc,
		counter:        counter,
		decisionLogger: decisionLogger,
		resultLogger:   resultLogger,
	}
}

func (h *Handler) ChatCompletions(
	w http.ResponseWriter,
	r *http.Request,
) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"read request body failed",
		)
		return
	}

	req, err := openai.ParseRequest(rawBody)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			err.Error(),
		)
		return
	}

	profile, ok := h.cfg.Models[req.Model]
	if !ok {
		resetRequestBody(r, rawBody)
		h.upstream.ServeHTTP(w, r)
		return
	}

	var routeDecision routing.Decision
	var randomValue float64
	continuationSkipped := false
	assistantCount := routing.CountAssistantMessages(req.Messages)

	if profile.DirectModel != nil &&
		routing.IsContinuationMessage(req.Messages) {
		// 续接消息不走 DIRECT，走概率路由
		continuationSkipped = true
		randomValue = h.randomFunc()
		routeDecision = routing.Decide(profile, randomValue, assistantCount)
	} else if profile.DirectModel != nil &&
		routing.IsLatestUserInputMessage(req.Messages, h.cfg.IgnoredUserInputPrefixes) {
		routeDecision = routing.Decision{
			Tier:   routing.TierDirect,
			Model:  *profile.DirectModel,
			Reason: "direct_route",
		}
	} else {
		randomValue = h.randomFunc()
		routeDecision = routing.Decide(profile, randomValue, assistantCount)
	}

	selectedTier := string(routeDecision.Tier)
	selectedModel := routeDecision.Model
	routeReason := routeDecision.Reason

	rewrittenBody, err := openai.RewriteModel(
		rawBody,
		selectedModel,
	)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			err.Error(),
		)
		return
	}

	promptInjected := false
	promptKind := ""
	imagePromptInjected := false

	if routeDecision.Tier == routing.TierHigh {
		prompt := routing.BuildHighTierPrompt()
		rewrittenBody, err = openai.AppendUserMessage(
			rewrittenBody,
			prompt.Content,
		)
		if err != nil {
			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}
		promptInjected = true
		promptKind = prompt.Kind
	}

	// DIRECT 档默认注入首轮说明提示，要求强模型先自然语言说明，再允许工具调用。
	// 若配置显式关闭 direct-prompt-enabled，则保留旧 DIRECT 语义（只改模型）。
	// 当最新用户消息包含图片且图片理解开关启用时，将图片理解段落与首轮说明提示
	// 合并为单条注入，不额外追加消息；开关关闭时退回普通 DIRECT 首轮提示。
	if routeDecision.Tier == routing.TierDirect && profile.IsDirectPromptEnabled() {
		var prompt routing.PromptResult
		if routing.ContainsImage(req.Messages) && profile.IsImagePromptEnabled() {
			// 合并图片理解段落与 DIRECT 首轮提示，只注入一条 user 消息
			prompt = routing.BuildDirectPromptWithImage()
			imagePromptInjected = true
		} else {
			prompt = routing.BuildDirectPrompt()
		}
		rewrittenBody, err = openai.AppendUserMessage(
			rewrittenBody,
			prompt.Content,
		)
		if err != nil {
			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}
		promptInjected = true
		promptKind = prompt.Kind
	}

	// HIGH 档追加 Review 提示到消息末尾。
	// DIRECT 档默认注入首轮说明提示；关闭时或 LOW/MEDIUM 档在历史含【Review】或【Plan】时追加防复述提示。
	// DeepSeek 兼容仅在没有其他注入时追加，确保每次请求最多追加一条临时 user 消息。
	guidanceInjected := false
	guidanceMarkerKinds := ""
	guidanceHistoryDetected := false

	shouldCheckGuidance := routeDecision.Tier == routing.TierLow ||
		routeDecision.Tier == routing.TierMedium ||
		routeDecision.Tier == routing.TierDirect

	if shouldCheckGuidance {

		hasMarkers, kinds := detectGuidanceMarkers(
			req.Messages,
		)
		guidanceMarkerKinds = kinds
		guidanceHistoryDetected = hasMarkers

		// DIRECT 启用首轮提示时，不再追加 guidance，避免与“先说明、再按需执行”冲突。
		// 仅当 DIRECT 提示未注入（显式关闭或 LOW/MEDIUM）且有历史标记时才追加 guidance。
		// anti-repetition-prompt-enabled: false 时完全跳过防复述注入。
		if hasMarkers && !promptInjected && profile.IsAntiRepetitionPromptEnabled() {
			rewrittenBody, err = openai.AppendUserMessage(
				rewrittenBody,
				routing.WrapInstruction(guidanceFollowupPromptContent),
			)
			if err != nil {
				writeError(
					w,
					http.StatusBadRequest,
					err.Error(),
				)
				return
			}
			guidanceInjected = true
		}
	}

	// DeepSeek 工具调用边界兼容补丁。
	// 当目标模型是 DeepSeek 系、请求末尾处于 tool-call 边界、
	// 且历史 assistant.tool_calls 消息缺少 reasoning_content 时，
	// 补充固定的占位 reasoning_content 字段，而不是追加 user 消息。
	//
	// DeepSeek 要求历史 assistant.tool_calls 消息必须包含非 null 的 reasoning_content。
	// 当上一轮路由到其他模型（如 GPT）时，assistant 消息可能缺失该字段或值为 null。
	//
	// 若已注入指导防复述提示（guidanceInjected），则跳过本条，
	// 避免同一请求进行多次修改。
	compatInjected := false
	compatKind := ""
	compatToolCallIDPrefix := ""

	if !promptInjected && !guidanceInjected && isDeepSeekModel(selectedModel) {
		if shouldFix, toolCallID := detectToolBoundary(
			req.Messages,
		); shouldFix && toolCallID != "" {
			// 使用固定占位文本补充 reasoning_content
			fixedBody, modified, err := openai.FixMissingReasoningContent(
				rewrittenBody,
				"继续处理当前工具调用并基于工具结果完成任务。",
			)
			if err != nil {
				writeError(
					w,
					http.StatusBadRequest,
					err.Error(),
				)
				return
			}

			if modified {
				rewrittenBody = fixedBody
				compatInjected = true
				compatKind = "reasoning_content_fixed"

				// 记录第一个缺失 reasoning_content 的 tool_call ID
				compatToolCallIDPrefix = toolCallID
				if len(toolCallID) > 40 {
					compatToolCallIDPrefix = toolCallID[:40]
				}
			}
		}
	}

	// 非多模态模型不保留图片：过滤历史消息中的 image_url content part，
	// 避免上游接口解析 image_url 变体失败（如 DeepSeek 只接受 text 变体）。
	// 默认仅 direct-model（DIRECT 模型）视为支持多模态输入；
	// 其余档位模型一律过滤。如需某档模型保留图片能力，可将该档模型名
	// 配置为与 direct-model 相同。
	//
	// 过滤仅在图片理解提示注入开启（image-prompt-enabled 默认 true）时执行：
	// 开启时 DIRECT 会先把图片转写为结构化文字，过滤历史 image_url 不会丢信息；
	// 关闭时图片未经转写，保留原样转发，避免过滤导致图片信息丢失。
	imagePartsStripped := false
	if profile.IsImagePromptEnabled() &&
		!isMultimodalModel(selectedModel, profile) {
		strippedBody, modified, err := openai.StripImageParts(
			rewrittenBody,
		)
		if err != nil {
			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return
		}

		if modified {
			rewrittenBody = strippedBody
			imagePartsStripped = true
		}
	}

	trcDecision := trace.Decision{
		LogicalModel:        req.Model,
		SelectedTier:        selectedTier,
		SelectedModel:       selectedModel,
		RouteReason:         routeReason,
		BodySize:            len(rawBody),
		MediumProbability:   *profile.MediumProbability,
		HighProbability:     *profile.HighProbability,
		RandomValue:         randomValue,
		AssistantCount:      assistantCount,
		HighRounds:          profile.HighRoundsValue(),
		MediumRounds:        profile.MediumRoundsValue(),
		PromptInjected:      promptInjected,
		PromptInjectionKind: promptKind,
		ImagePromptInjected: imagePromptInjected,
		ImagePartsStripped:  imagePartsStripped,

		CompatInjected:           compatInjected,
		CompatKind:               compatKind,
		CompatToolCallIDPrefix:   compatToolCallIDPrefix,
		ContinuationSkipped:      continuationSkipped,
		GuidanceHistoryDetected:  guidanceHistoryDetected,
		GuidanceFollowupInjected: guidanceInjected,
		GuidanceMarkerKinds:      guidanceMarkerKinds,
	}

	recordName, err := h.recorder.Record(
		rawBody,
		trcDecision,
		r.Header,
	)
	if err != nil {
		slog.Error(
			"record request trace failed",
			"error", err,
			"trace_dir", recordName,
		)
	}

	// 记录决策到 SQLite（用于 Web 控制台查询）
	if h.decisionLogger != nil {
		h.decisionLogger(DecisionLog{
			RequestID:      recordName,
			LogicalModel:   req.Model,
			SelectedTier:   selectedTier,
			SelectedModel:  selectedModel,
			AssistantCount: int64(assistantCount),
			Reason:         routeReason,
			TraceDir:       recordName,
		})
	}

	slog.Info(
		"route logical model",
		"logical_model", req.Model,
		"selected_tier", selectedTier,
		"selected_model", selectedModel,
		"route_reason", routeReason,
		"random_value", randomValue,
		"prompt_injected", promptInjected,
		"prompt_injection_kind", promptKind,
		"image_prompt_injected", imagePromptInjected,
		"image_parts_stripped", imagePartsStripped,
		"compat_injected", compatInjected,
		"compat_kind", compatKind,
		"compat_tool_call_id_prefix", compatToolCallIDPrefix,
		"continuation_message", routing.IsContinuationMessage(req.Messages),
		"direct_skipped_for_continuation", continuationSkipped,
		"guidance_history_detected", guidanceHistoryDetected,
		"guidance_followup_injected", guidanceInjected,
		"guidance_marker_kinds", guidanceMarkerKinds,
		"body_size", len(rawBody),
		"medium_probability", *profile.MediumProbability,
		"high_probability", *profile.HighProbability,
		"assistant_count", assistantCount,
		"high_rounds", profile.HighRoundsValue(),
		"medium_rounds", profile.MediumRoundsValue(),
		"trace_dir", recordName,
	)

	resetRequestBody(r, rewrittenBody)

	// 将 trace 目录名注入 request context，供 ErrorHandler 输出关联日志
	r = r.WithContext(
		ContextWithTraceDir(r.Context(), recordName),
	)

	recorder := &statusCodeRecorder{
		ResponseWriter: w,
		notify: func(code int, body []byte) {
			if h.resultLogger == nil {
				return
			}
			h.resultLogger(DecisionResult{
				RequestID:    recordName,
				StatusCode:   code,
				ErrorMessage: extractErrorPreview(body),
			})
		},
	}

	h.upstream.ServeHTTP(recorder, r)

	slog.Info(
		"upstream response",
		"selected_model", selectedModel,
		"status_code", recorder.statusCode,
		"trace_dir", recordName,
	)

	// 回写上游请求结果（状态码 + 非 2xx 错误摘要），供 Web 控制台排查。
	// 成功响应已在 WriteHeader 时由 notify 及时回写（SSE 长流不等流结束）；
	// 此处最终回写覆盖错误响应（错误体需等转发返回才完整）并作为幂等兜底。
	if h.resultLogger != nil {
		h.resultLogger(DecisionResult{
			RequestID:    recordName,
			StatusCode:   recorder.statusCode,
			ErrorMessage: extractErrorPreview(recorder.body),
		})
	}

	if h.counter != nil {
		success := recorder.statusCode >= 200 &&
			recorder.statusCode < 300

		h.counter.Record(
			selectedModel,
			req.Model,
			success,
		)
	}
}

// errorBodyLimit 限制从上游非 2xx 响应中捕获的错误体大小。
const errorBodyLimit = 4096

// extractErrorPreview 从捕获的上游响应体中提取可读错误摘要。
// 优先解析 OpenAI 风格的 {"error":{"message":"..."}} 结构，其次取原文（截断）。
func extractErrorPreview(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}

	preview := strings.TrimSpace(string(body))
	if len(preview) > errorBodyLimit {
		preview = preview[:errorBodyLimit]
	}
	return preview
}

func resetRequestBody(
	r *http.Request,
	body []byte,
) {
	r.Body = io.NopCloser(
		bytes.NewReader(body),
	)

	r.ContentLength = int64(len(body))

	r.Header.Set(
		"Content-Length",
		strconv.Itoa(len(body)),
	)

	// 设置 GetBody 允许 http.Transport 在重试时重新创建请求体。
	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(
			bytes.NewReader(bodyCopy),
		), nil
	}
}

func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(
		map[string]any{
			"error": map[string]string{
				"message": message,
			},
		},
	)
}

// statusCodeRecorder 包装 http.ResponseWriter，捕获第一个写出的状态码。
//
// 热重载或 SSE 流式响应不会影响状态码捕获。
// 只捕获首次 WriteHeader 调用的状态码，后续调用不影响记录的值。
// 非 2xx 响应会额外捕获响应体前 errorBodyLimit 字节，用于提取错误摘要。
//
// notify 在成功（2xx/3xx）响应确定状态码的瞬间回调一次，用于及时回写结果：
// SSE 长流场景下流可能长时间不结束，若等到上游转发返回（copyBuffer 读到
// body EOF）再回写，状态码会一直缺失（数据库 status_code 保持 0）。
type statusCodeRecorder struct {
	http.ResponseWriter

	statusCode int
	once       int32
	body       []byte
	notify     func(statusCode int, body []byte)
}

func (w *statusCodeRecorder) WriteHeader(code int) {
	if atomic.CompareAndSwapInt32(&w.once, 0, 1) {
		w.statusCode = code
		// 成功/重定向响应立即通知；错误响应等首次 Write 捕获 body 后由最终回写处理。
		if code < 400 && w.notify != nil {
			w.notify(code, w.body)
		}
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCodeRecorder) Write(p []byte) (int, error) {
	if atomic.CompareAndSwapInt32(&w.once, 0, 1) {
		w.statusCode = http.StatusOK
		if w.notify != nil {
			w.notify(w.statusCode, w.body)
		}
	}

	// 仅对失败响应（>=400）捕获响应体，用于提取错误摘要。
	// 成功与 SSE 流式响应不缓冲，避免内存与延迟开销。
	if w.statusCode >= 400 {
		remaining := errorBodyLimit - len(w.body)
		if remaining > 0 {
			if len(p) > remaining {
				w.body = append(w.body, p[:remaining]...)
			} else {
				w.body = append(w.body, p...)
			}
		}
	}

	return w.ResponseWriter.Write(p)
}

func (w *statusCodeRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
