package proxy

import (
	"bytes"
	"encoding/json"
	"strings"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/protocol/openai"
)

// guidanceFollowupPromptContent 是追加到 LOW/MEDIUM 请求末尾的用户消息内容，
// 用于提示弱模型直接执行历史中强模型留下的工作指导，避免机械复述。
// 若当前可通过工具推进任务，模型必须在本轮直接调用工具，不得输出确认性空回复。
// 实际注入时会由 handler 调用 routing.WrapInstruction 包裹。
const guidanceFollowupPromptContent = `历史中的【Review】和【Plan】是前序强模型留下的工作指导。请直接遵循其结论继续执行，不要复述、改写或重新输出这些标题和模板。若当前可通过工具推进任务，必须在本轮直接调用合适的工具；不要先输出\u201c明白\u201d\u201c收到\u201d\u201c直接执行\u201d等确认性文字。仅在没有可执行工具操作，或确实需要用户补充关键信息时，才输出简短说明或提问。`

// isDeepSeekModel 判断 selectedModel 是否为 DeepSeek 系模型。
func isDeepSeekModel(model string) bool {
	return strings.Contains(
		strings.ToLower(model),
		"deepseek",
	)
}

// isMultimodalModel 判断选中模型是否支持多模态输入（如图片）。
// 当模型支持多模态时，保留历史消息中的 image_url content part；
// 否则过滤掉，避免上游接口解析失败（如 DeepSeek 只接受 text 变体）。
//
// 判定规则：
//   - 仅 direct-model（DIRECT 档模型）视为支持多模态
//   - 其余档位模型一律视为不支持
//
// 如需某档模型保留图片能力，可将该档模型名配置为与 direct-model 相同。
func isMultimodalModel(
	selectedModel string,
	profile config.ModelProfile,
) bool {
	if profile.DirectModel == nil {
		return false
	}

	return selectedModel == *profile.DirectModel
}

// detectToolBoundary 检查请求末尾是否处于工具调用边界，
// 并在当前连续工具调用段中扫描是否存在缺少 reasoning_content 的 assistant 消息。
//
// DeepSeek 系模型在思考模式下要求历史 assistant 消息必须包含 reasoning_content。
// 当上一轮路由到其他模型时，assistant 消息不会有此字段，导致 DeepSeek 返回 400。
// 通过追加 user 消息将边界从 tool 转为 user，可绕过该校验。
//
// 返回 (shouldInject, missingReasoningToolCallID)：
//   - 最后一条消息 role=tool 且有 tool_call_id，且从最近一个 user
//     之后存在至少一个含 tool_calls 但缺少 reasoning_content 的 assistant
//     → (true, 该 assistant 的第一个 tool_call ID)
//   - 否则 → (false, "")
func detectToolBoundary(
	messages []openai.Message,
) (bool, string) {
	if len(messages) == 0 {
		return false, ""
	}

	last := messages[len(messages)-1]

	// 仅当最后一条是 tool 结果且有有效的 tool_call_id 时才触发兼容注入。
	// assistant.tool_calls 尾部未经直连验证，暂不做处理。
	if last.Role != "tool" || last.ToolCallID == "" {
		return false, ""
	}

	// 从消息尾部向前扫描，直到遇到最近一个 user 为止。
	// 在当前连续工具调用段中，检查是否有 assistant 消息
	// 包含 tool_calls 但缺少 reasoning_content。
	var missingReasoningToolCallID string

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]

		if msg.Role == "user" {
			break
		}

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// assistant 有 tool_calls 但 reasoning_content 缺失或为 null → 需要注入
			// GPT 模型返回 null，DeepSeek 也拒绝 null，需要补完整字段
			if len(msg.ReasoningContent) == 0 || bytes.Equal(msg.ReasoningContent, []byte("null")) {
				if missingReasoningToolCallID == "" {
					missingReasoningToolCallID = msg.ToolCalls[0].ID
				}
			}
		}
	}

	if missingReasoningToolCallID != "" {
		return true, missingReasoningToolCallID
	}

	return false, ""
}

// detectGuidanceMarkers 检查最新一条 assistant 消息的 text 内容是否包含
// 【Review】或【Plan】标记。返回是否发现标记以及发现的标记种类字符串。
//
// 只检查最新一条 assistant 消息，而不是扫描全部历史：
// 防复述注入的目标是引导模型直接执行前序强模型留下的工作指导，
// 而工作指导只会出现在最近一次 assistant 回复中；更早的标记
// 已被执行或已被后续消息覆盖，不应再触发注入。
func detectGuidanceMarkers(messages []openai.Message) (bool, string) {
	hasReview := false
	hasPlan := false

	// 从后向前查找最新一条 assistant 消息
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" {
			continue
		}

		content := extractTextContent(messages[i].Content)

		if strings.Contains(content, "【Review】") {
			hasReview = true
		}
		if strings.Contains(content, "【Plan】") {
			hasPlan = true
		}

		break
	}

	if !hasReview && !hasPlan {
		return false, ""
	}

	var kinds string
	switch {
	case hasReview && hasPlan:
		kinds = "review,plan"
	case hasReview:
		kinds = "review"
	case hasPlan:
		kinds = "plan"
	}

	return true, kinds
}

// extractTextContent 提取消息 content 的文本内容。
// content 为字符串时直接返回；为数组时拼接所有 type=text part 的 text 字段，
// 用于在多模态消息中定位标记关键字。
func extractTextContent(raw json.RawMessage) string {
	if len(raw) == 0 || !json.Valid(raw) {
		return ""
	}

	// 字符串 content
	var plain string

	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain
	}

	// 数组 content：拼接 text part
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}

	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}

	var sb strings.Builder

	for _, p := range parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}

	return sb.String()
}
