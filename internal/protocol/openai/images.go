package openai

import (
	"encoding/json"
	"fmt"
)

// imageRemovedPlaceholder 是 content 数组中 image_url part 全部被移除后
// 使用的占位文本。避免空 content 数组导致上游校验失败，同时提示模型
// 图片内容已由前序支持多模态的模型转写处理。
const imageRemovedPlaceholder = "[图片内容已由前序支持多模态的模型转写处理，详见上文 assistant 消息；本条不再包含图片数据]"

// StripImageParts 从请求的 messages 中移除所有 image_url 类型的 content part。
// 当选中模型不支持多模态输入时使用，避免上游接口因无法解析 image_url 变体
// （如 DeepSeek 只接受 text 变体）而返回 400 错误。
//
// 处理规则：
//   - content 为字符串：保持不变（无图片）
//   - content 为数组：移除 type == "image_url" 的 part
//   - 移除后数组仍有剩余 part：保留为数组
//   - 移除后数组为空：替换为占位文本字符串
//
// 消息的其余字段（role、tool_calls、reasoning_content 等）原样保留。
// 返回修改后的请求体与是否发生了修改。
func StripImageParts(rawBody []byte) ([]byte, bool, error) {
	var request map[string]json.RawMessage

	if err := json.Unmarshal(rawBody, &request); err != nil {
		return nil, false, fmt.Errorf(
			"parse request for image strip: %w",
			err,
		)
	}

	rawMessages, ok := request["messages"]
	if !ok {
		return nil, false, fmt.Errorf(
			"messages is required for image strip",
		)
	}

	var messages []json.RawMessage

	if err := json.Unmarshal(
		rawMessages,
		&messages,
	); err != nil {
		return nil, false, fmt.Errorf(
			"parse messages for image strip: %w",
			err,
		)
	}

	modified := false

	for i, rawMsg := range messages {
		var msg struct {
			Content json.RawMessage `json:"content"`
		}

		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			continue
		}

		// content 缺失或非法时跳过；字符串 content 无图片也跳过。
		if len(msg.Content) == 0 || !json.Valid(msg.Content) {
			continue
		}

		var parts []json.RawMessage

		if err := json.Unmarshal(
			msg.Content,
			&parts,
		); err != nil {
			// 非数组（如纯字符串）→ 无图片，跳过
			continue
		}

		filtered := make(
			[]json.RawMessage,
			0,
			len(parts),
		)
		removed := false

		for _, part := range parts {
			var meta struct {
				Type string `json:"type"`
			}

			if err := json.Unmarshal(
				part,
				&meta,
			); err == nil && meta.Type == "image_url" {
				removed = true
				continue
			}

			filtered = append(filtered, part)
		}

		if !removed {
			continue
		}

		// 重新构造消息，仅替换 content 字段
		var fullMsg map[string]interface{}

		if err := json.Unmarshal(rawMsg, &fullMsg); err != nil {
			continue
		}

		if len(filtered) == 0 {
			// 全部是图片 part → 使用占位文本，避免空 content
			fullMsg["content"] = imageRemovedPlaceholder
		} else {
			fullMsg["content"] = filtered
		}

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
			"encode messages for image strip: %w",
			err,
		)
	}

	request["messages"] = encodedMessages

	result, err := json.Marshal(request)
	if err != nil {
		return nil, false, fmt.Errorf(
			"encode request for image strip: %w",
			err,
		)
	}

	return result, true, nil
}
