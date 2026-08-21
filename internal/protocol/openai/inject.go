package openai

import (
	"encoding/json"
	"fmt"
)

// AppendUserMessage 在 messages 末尾追加一条用户消息。
func AppendUserMessage(
	rawBody []byte,
	content string,
) ([]byte, error) {
	var request map[string]json.RawMessage

	if err := json.Unmarshal(rawBody, &request); err != nil {
		return nil, fmt.Errorf(
			"parse request for append user message: %w",
			err,
		)
	}

	rawMessages, ok := request["messages"]
	if !ok {
		return nil, fmt.Errorf(
			"messages is required for append user message",
		)
	}

	var messages []json.RawMessage

	if err := json.Unmarshal(
		rawMessages,
		&messages,
	); err != nil {
		return nil, fmt.Errorf(
			"parse messages for append user message: %w",
			err,
		)
	}

	userMessage, err := json.Marshal(
		map[string]string{
			"role":    "user",
			"content": content,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"encode user message: %w",
			err,
		)
	}

	messages = append(messages, userMessage)

	encodedMessages, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf(
			"encode messages: %w",
			err,
		)
	}

	request["messages"] = encodedMessages

	result, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf(
			"encode request: %w",
			err,
		)
	}

	return result, nil
}

// FixMissingReasoningContent 检查并修复最近一段工具调用中缺失或为 null 的 reasoning_content 字段。
// DeepSeek 系模型要求历史 assistant.tool_calls 消息必须包含非 null 的 reasoning_content。
// 当上一轮路由到其他模型（如 GPT）时，assistant 消息可能缺失该字段或值为 null。
// 为避免占位文本污染更早的历史（若遍历全部消息，历史中每条 tool_calls 消息都会被
// 补上相同的占位文本，导致模型上下文堆积大量无意义文本），仅扫描最近一个 user
// 消息之后的连续工具调用段，与 proxy.detectToolBoundary 的判定范围保持一致。
// 返回修改后的请求体和是否进行了修改。
func FixMissingReasoningContent(
	rawBody []byte,
	placeholder string,
) ([]byte, bool, error) {
	var request map[string]json.RawMessage

	if err := json.Unmarshal(rawBody, &request); err != nil {
		return nil, false, fmt.Errorf(
			"parse request for reasoning_content fix: %w",
			err,
		)
	}

	rawMessages, ok := request["messages"]
	if !ok {
		return nil, false, fmt.Errorf(
			"messages is required for reasoning_content fix",
		)
	}

	var messages []json.RawMessage

	if err := json.Unmarshal(
		rawMessages,
		&messages,
	); err != nil {
		return nil, false, fmt.Errorf(
			"parse messages for reasoning_content fix: %w",
			err,
		)
	}

	modified := false

	// 定位最近一个 user 消息之后的起始位置，仅修复该连续工具调用段内的消息。
	// 历史更早的工具调用段已被后续 user 消息分隔，不再补充占位文本，
	// 避免占位内容在完整历史中堆积。
	startIdx := 0

	for i := len(messages) - 1; i >= 0; i-- {
		var head struct {
			Role string `json:"role"`
		}

		if err := json.Unmarshal(messages[i], &head); err != nil {
			continue
		}

		if head.Role == "user" {
			startIdx = i + 1
			break
		}
	}

	for i := startIdx; i < len(messages); i++ {
		rawMsg := messages[i]

		var msg struct {
			Role             string          `json:"role"`
			ReasoningContent json.RawMessage `json:"reasoning_content"`
			ToolCalls        []interface{}   `json:"tool_calls"`
		}

		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			continue
		}

		// 只处理有 tool_calls 的 assistant 消息
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		// 检查 reasoning_content 是否缺失或为 null
		needsFix := len(msg.ReasoningContent) == 0 ||
			string(msg.ReasoningContent) == "null"

		if !needsFix {
			continue
		}

		// 重新构造消息，添加 reasoning_content
		var fullMsg map[string]interface{}
		if err := json.Unmarshal(rawMsg, &fullMsg); err != nil {
			continue
		}

		fullMsg["reasoning_content"] = placeholder

		fixedMsg, err := json.Marshal(fullMsg)
		if err != nil {
			continue
		}

		messages[i] = fixedMsg
		modified = true
	}

	if !modified {
		return rawBody, false, nil
	}

	encodedMessages, err := json.Marshal(messages)
	if err != nil {
		return nil, false, fmt.Errorf(
			"encode messages: %w",
			err,
		)
	}

	request["messages"] = encodedMessages

	result, err := json.Marshal(request)
	if err != nil {
		return nil, false, fmt.Errorf(
			"encode request: %w",
			err,
		)
	}

	return result, true, nil
}
