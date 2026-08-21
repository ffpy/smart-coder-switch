package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/protocol/openai"
	"smart-coder-switch/internal/routing"
	"smart-coder-switch/internal/trace"
)

// Responses 处理 OpenAI Responses API 请求。
//
// 与 ChatCompletions 保持相同职责：归一化、路由、提示注入、图片剥离和透明转发，
// 但协议入口改为 input 数组，且不做 Chat/Responses 格式转换。
func (h *Handler) Responses(
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

	model, input, err := responsesExtractModelAndInput(rawBody)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			err.Error(),
		)
		return
	}

	profile, ok := h.cfg.Models[model]
	if !ok {
		resetRequestBody(r, rawBody)
		h.upstream.ServeHTTP(w, r)
		return
	}

	inputItems, err := responsesParseInputItems(input)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			err.Error(),
		)
		return
	}

	normalizedMessages, err := normalizeResponsesMessages(inputItems)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			err.Error(),
		)
		return
	}

	var routeDecision routing.Decision
	var randomValue float64
	continuationSkipped := false
	assistantCount := routing.CountAssistantMessages(normalizedMessages)

	lastInputText := lastResponsesItemText(inputItems)
	lastInputIsUser := isLastResponsesItemUserInput(inputItems)

	if profile.DirectModel != nil &&
		lastInputIsUser &&
		routing.IsContinuationMessageFromText(lastInputText) {
		continuationSkipped = true
		randomValue = h.randomFunc()
		routeDecision = routing.Decide(profile, randomValue, assistantCount)
	} else if profile.DirectModel != nil &&
		lastInputIsUser &&
		routing.IsLatestUserInputMessageFromText(
			lastInputText,
			h.cfg.IgnoredUserInputPrefixes,
		) {
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
		rewrittenBody, err = openai.ResponsesAppendUserInputItem(
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

	if routeDecision.Tier == routing.TierDirect && profile.IsDirectPromptEnabled() {
		var prompt routing.PromptResult
		if hasResponsesItemImages(inputItems) && profile.IsImagePromptEnabled() {
			prompt = routing.BuildDirectPromptWithImage()
			imagePromptInjected = true
		} else {
			prompt = routing.BuildDirectPrompt()
		}
		rewrittenBody, err = openai.ResponsesAppendUserInputItem(
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

	guidanceInjected := false
	guidanceMarkerKinds := ""
	guidanceHistoryDetected := false

	shouldCheckGuidance := routeDecision.Tier == routing.TierLow ||
		routeDecision.Tier == routing.TierMedium ||
		routeDecision.Tier == routing.TierDirect

	if shouldCheckGuidance {
		hasMarkers, kinds := detectGuidanceMarkers(
			normalizedMessages,
		)
		guidanceMarkerKinds = kinds
		guidanceHistoryDetected = hasMarkers

		if hasMarkers && !promptInjected && profile.IsAntiRepetitionPromptEnabled() {
			rewrittenBody, err = openai.ResponsesAppendUserInputItem(
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

	imagePartsStripped := false
	if profile.IsImagePromptEnabled() &&
		!isMultimodalModel(selectedModel, profile) {
		strippedBody, modified, err := responsesStripInputImages(
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

	trcDecision := traceDecisionFromResponses(
		model,
		selectedTier,
		selectedModel,
		routeReason,
		rawBody,
		randomValue,
		assistantCount,
		profile,
		promptInjected,
		promptKind,
		imagePromptInjected,
		imagePartsStripped,
		continuationSkipped,
		guidanceHistoryDetected,
		guidanceInjected,
		guidanceMarkerKinds,
	)

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

	if h.decisionLogger != nil {
		h.decisionLogger(DecisionLog{
			RequestID:      recordName,
			LogicalModel:   model,
			SelectedTier:   selectedTier,
			SelectedModel:  selectedModel,
			AssistantCount: int64(assistantCount),
			Reason:         routeReason,
			TraceDir:       recordName,
		})
	}

	slog.Info(
		"route responses input",
		"logical_model", model,
		"selected_tier", selectedTier,
		"selected_model", selectedModel,
		"route_reason", routeReason,
		"random_value", randomValue,
		"prompt_injected", promptInjected,
		"prompt_injection_kind", promptKind,
		"image_prompt_injected", imagePromptInjected,
		"image_parts_stripped", imagePartsStripped,
		"continuation_message", lastInputIsUser && routing.IsContinuationMessageFromText(lastInputText),
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
			model,
			success,
		)
	}
}

func responsesExtractModelAndInput(rawBody []byte) (string, json.RawMessage, error) {
	var req struct {
		Model string          `json:"model"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(rawBody, &req); err != nil {
		return "", nil, err
	}
	if req.Model == "" {
		return "", nil, errors.New("model is required")
	}
	return req.Model, req.Input, nil
}

// responsesStripInputImages 从 Responses 请求中去掉纯 input_image 条目。
// 当整条 input 只剩图片时，注入占位 user 文本避免空 input。
func responsesStripInputImages(rawBody []byte) ([]byte, bool, error) {
	var request map[string]json.RawMessage

	if err := json.Unmarshal(rawBody, &request); err != nil {
		return nil, false, err
	}

	rawInput, ok := request["input"]
	if !ok {
		return nil, false, nil
	}

	items, err := responsesParseInputItems(rawInput)
	if err != nil {
		return nil, false, err
	}

	modified := false
	filtered := make([]json.RawMessage, 0, len(items))

	for _, raw := range items {
		var kind responsesInputItemType
		if err := json.Unmarshal(raw, &kind); err != nil {
			filtered = append(filtered, raw)
			continue
		}

		itemKind := responsesItemKind(kind)
		if itemKind == "input_image" {
			modified = true
			continue
		}

		filtered = append(filtered, raw)
	}

	if !modified {
		return rawBody, false, nil
	}

	if len(filtered) == 0 {
		placeholder, err := json.Marshal(map[string]string{
			"role":    "user",
			"content": responsesImagePlaceholder,
		})
		if err != nil {
			return nil, false, err
		}
		filtered = append(filtered, placeholder)
	}

	encodedInput, err := json.Marshal(filtered)
	if err != nil {
		return nil, false, err
	}

	request["input"] = encodedInput

	result, err := json.Marshal(request)
	if err != nil {
		return nil, false, err
	}

	return result, true, nil
}

const responsesImagePlaceholder = "[图片内容已由前序支持多模态的模型转写处理]"

func traceDecisionFromResponses(
	logicalModel string,
	selectedTier string,
	selectedModel string,
	routeReason string,
	rawBody []byte,
	randomValue float64,
	assistantCount int,
	profile config.ModelProfile,
	promptInjected bool,
	promptKind string,
	imagePromptInjected bool,
	imagePartsStripped bool,
	continuationSkipped bool,
	guidanceHistoryDetected bool,
	guidanceFollowupInjected bool,
	guidanceMarkerKinds string,
) trace.Decision {
	return trace.Decision{
		LogicalModel:             logicalModel,
		SelectedTier:             selectedTier,
		SelectedModel:            selectedModel,
		RouteReason:              routeReason,
		BodySize:                 len(rawBody),
		MediumProbability:        *profile.MediumProbability,
		HighProbability:          *profile.HighProbability,
		RandomValue:              randomValue,
		AssistantCount:           assistantCount,
		HighRounds:               profile.HighRoundsValue(),
		MediumRounds:             profile.MediumRoundsValue(),
		PromptInjected:           promptInjected,
		PromptInjectionKind:      promptKind,
		ImagePromptInjected:      imagePromptInjected,
		ImagePartsStripped:       imagePartsStripped,
		ContinuationSkipped:      continuationSkipped,
		GuidanceHistoryDetected:  guidanceHistoryDetected,
		GuidanceFollowupInjected: guidanceFollowupInjected,
		GuidanceMarkerKinds:      guidanceMarkerKinds,
	}
}
