package routing

import (
	"encoding/json"
	"math/rand"
	"strings"
	"time"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/protocol/openai"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Tier string

const (
	TierLow    Tier = "LOW"
	TierMedium Tier = "MEDIUM"
	TierHigh   Tier = "HIGH"
	TierDirect Tier = "DIRECT"
)

type Decision struct {
	Tier   Tier
	Model  string
	Reason string
}

func Decide(
	profile config.ModelProfile,
	randomValue float64,
	assistantCount int,
) Decision {
	highRounds := profile.HighRoundsValue()
	if highRounds > 0 && assistantCount > 0 && assistantCount%highRounds == 0 && profile.HighModel != "" {
		return Decision{
			Tier:   TierHigh,
			Model:  profile.HighModel,
			Reason: "rounds_high",
		}
	}

	mediumRounds := profile.MediumRoundsValue()
	if mediumRounds > 0 && assistantCount > 0 && assistantCount%mediumRounds == 0 && profile.MediumModel != "" {
		return Decision{
			Tier:   TierMedium,
			Model:  profile.MediumModel,
			Reason: "rounds_medium",
		}
	}

	highProb := *profile.HighProbability
	mediumProb := *profile.MediumProbability

	if profile.HighModel == "" {
		highProb = 0
	}
	if profile.MediumModel == "" {
		mediumProb = 0
	}

	if highProb > 0 && randomValue < highProb {
		return Decision{
			Tier:   TierHigh,
			Model:  profile.HighModel,
			Reason: "probability_high",
		}
	}

	if mediumProb > 0 && randomValue < highProb+mediumProb {
		return Decision{
			Tier:   TierMedium,
			Model:  profile.MediumModel,
			Reason: "probability_medium",
		}
	}

	return Decision{
		Tier:   TierLow,
		Model:  profile.LowModel,
		Reason: "probability_low",
	}
}

// CountAssistantMessages 统计 messages 中 role=assistant 的消息数量。
func CountAssistantMessages(messages []openai.Message) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == "assistant" {
			count++
		}
	}
	return count
}

func GenerateRandomValue() float64 {
	return rand.Float64()
}

// IsContinuationMessage 判断最后一条 user 消息是否为预定义的续接短语。
// 续接消息不应触发 direct-model 的 DIRECT 路由。
func IsContinuationMessage(messages []openai.Message) bool {
	if len(messages) == 0 {
		return false
	}

	last := messages[len(messages)-1]
	if last.Role != "user" {
		return false
	}

	var content string
	if last.Content != nil {
		if err := json.Unmarshal(last.Content, &content); err != nil {
			return false
		}
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}

	normalized := strings.ToLower(content)

	continuationPhrases := map[string]bool{
		"继续":       true,
		"继续吧":      true,
		"继续处理":     true,
		"go on":    true,
		"continue": true,
		"ok":       true,
		"好的":       true,
		"嗯":        true,
	}

	return continuationPhrases[normalized]
}

// ContainsImage 判断 messages 的最后一条 user 消息是否包含图片内容。
//
// 检测逻辑：将 content 解析为 []openai.ContentPart 数组，
// 遍历各 part，任一部分的 type 为 "image_url" 即返回 true。
// 解析失败（如纯字符串 content）或最后一条非 user 时返回 false。
func ContainsImage(messages []openai.Message) bool {
	if len(messages) == 0 {
		return false
	}

	last := messages[len(messages)-1]
	if last.Role != "user" {
		return false
	}

	if last.Content == nil {
		return false
	}

	var parts []openai.ContentPart
	if err := json.Unmarshal(last.Content, &parts); err != nil {
		return false
	}

	for _, part := range parts {
		if part.Type == "image_url" {
			return true
		}
	}

	return false
}

// defaultIgnoredPrefixes 是 IsLatestUserInputMessage 在调用方未提供时的默认前缀列表。
var defaultIgnoredPrefixes = []string{
	"<system-reminder>",
	"[Compressed conversation section]",
}

// IsContinuationMessageFromText 从已提取的文本判断是否为续接短语。
func IsContinuationMessageFromText(text string) bool {
	content := strings.TrimSpace(text)
	if content == "" {
		return false
	}

	normalized := strings.ToLower(content)

	continuationPhrases := map[string]bool{
		"继续":       true,
		"继续吧":      true,
		"继续处理":     true,
		"go on":    true,
		"continue": true,
		"ok":       true,
		"好的":       true,
		"嗯":        true,
	}

	return continuationPhrases[normalized]
}

// IsLatestUserInputMessageFromText 从已提取的文本判断最后一条是否属于人类用户的真实输入。
func IsLatestUserInputMessageFromText(text string, ignoredPrefixes []string) bool {
	content := strings.TrimSpace(text)
	if len(content) == 0 {
		return true
	}

	prefixes := ignoredPrefixes
	if prefixes == nil {
		prefixes = defaultIgnoredPrefixes
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(content, prefix) {
			return false
		}
	}

	return true
}

// IsLatestUserInputMessage 判断 messages 的最后一条是否属于人类用户的真实输入。
//
// 当最后一条消息 role != "user"，或内容去除首尾空白后以 ignoredPrefixes
// 中任一前缀开头时返回 false。不符合上述条件时返回 true。
//
// ignoredPrefixes 为 nil 时使用 defaultIgnoredPrefixes。
// 空列表表示不做任何前缀过滤。
func IsLatestUserInputMessage(messages []openai.Message, ignoredPrefixes []string) bool {
	if len(messages) == 0 {
		return false
	}

	last := messages[len(messages)-1]
	if last.Role != "user" {
		return false
	}

	// Content 可能为 nil 或非字符串（例如多模态 content parts）
	var content string
	if last.Content != nil {
		if err := json.Unmarshal(last.Content, &content); err != nil {
			// 非纯字符串内容视为真实输入
			return true
		}
	}

	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return true
	}

	// 使用调用方传入的前缀列表（或默认值）
	prefixes := ignoredPrefixes
	if prefixes == nil {
		prefixes = defaultIgnoredPrefixes
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(content, prefix) {
			return false
		}
	}

	return true
}
