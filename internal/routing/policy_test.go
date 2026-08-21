package routing

import (
	"encoding/json"
	"testing"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/protocol/openai"
)

func textMessage(role, content string) openai.Message {
	raw, err := json.Marshal(content)
	if err != nil {
		panic(err)
	}
	return openai.Message{Role: role, Content: raw}
}

func TestIsLatestUserInputMessage_EmptyMessages(t *testing.T) {
	if IsLatestUserInputMessage(nil, nil) {
		t.Fatal("expected false for nil messages")
	}
	if IsLatestUserInputMessage([]openai.Message{}, nil) {
		t.Fatal("expected false for empty messages")
	}
}

func TestIsLatestUserInputMessage_LastRoleNotUser(t *testing.T) {
	msgs := []openai.Message{
		textMessage("system", "you are a coder"),
		textMessage("assistant", "hello"),
		textMessage("tool", "result"),
	}
	if IsLatestUserInputMessage(msgs, nil) {
		t.Fatal("expected false when last message role is not user")
	}
}

func TestIsLatestUserInputMessage_NormalUserMessage(t *testing.T) {
	msgs := []openai.Message{
		textMessage("system", "you are a coder"),
		textMessage("user", "write a function"),
	}
	if !IsLatestUserInputMessage(msgs, nil) {
		t.Fatal("expected true for normal user message")
	}
}

func TestIsLatestUserInputMessage_DefaultPrefixes(t *testing.T) {
	// nil 表示使用内置默认前缀。
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "<system-reminder>",
			content: "<system-reminder>\n# Plan Mode\n...",
			want:    false,
		},
		{
			name:    "[Compressed conversation section]",
			content: "[Compressed conversation section]\n## Session\n...",
			want:    false,
		},
		{
			name:    "html fragment",
			content: "<div>hello</div>",
			want:    true,
		},
		{
			name:    "markdown link",
			content: "[click here](http://example.com)",
			want:    true,
		},
		{
			name:    "normal text",
			content: "write a function",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := []openai.Message{textMessage("user", tt.content)}
			got := IsLatestUserInputMessage(msgs, nil)
			if got != tt.want {
				t.Fatalf("got %v, want %v for content %q", got, tt.want, tt.content)
			}
		})
	}
}

func TestIsLatestUserInputMessage_CustomPrefixes(t *testing.T) {
	customPrefixes := []string{
		"<system-remark>",
		"[Custom injected]",
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "<system-remark> match",
			content: "<system-remark>\nsomething",
			want:    false,
		},
		{
			name:    "[Custom injected] match",
			content: "[Custom injected] block\ncontent",
			want:    false,
		},
		{
			name:    "<system-reminder> does not match custom prefixes",
			content: "<system-reminder>\n# Plan Mode",
			want:    true,
		},
		{
			name:    "normal message",
			content: "write a function",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := []openai.Message{textMessage("user", tt.content)}
			got := IsLatestUserInputMessage(msgs, customPrefixes)
			if got != tt.want {
				t.Fatalf("got %v, want %v for content %q", got, tt.want, tt.content)
			}
		})
	}
}

func TestIsLatestUserInputMessage_NonStringContent(t *testing.T) {
	// 多模态 content（JSON 数组）应视为真实输入。
	msgs := []openai.Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"describe this image"}]`)},
	}
	if !IsLatestUserInputMessage(msgs, nil) {
		t.Fatal("expected true for non-string content")
	}
}

func TestIsLatestUserInputMessage_EmptyConfiguredPrefixes(t *testing.T) {
	// 显式空列表表示不忽略任何前缀。
	msgs := []openai.Message{
		textMessage("user", "<system-reminder>\n..."),
	}
	if !IsLatestUserInputMessage(msgs, []string{}) {
		t.Fatal("expected true when configured prefixes list is empty")
	}
}
func TestCountAssistantMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []openai.Message
		want     int
	}{
		{"nil", nil, 0},
		{"empty", []openai.Message{}, 0},
		{"no assistant", []openai.Message{
			textMessage("user", "hi"),
			textMessage("system", "sys"),
		}, 0},
		{"mixed", []openai.Message{
			textMessage("user", "hi"),
			textMessage("assistant", "ok"),
			textMessage("user", "next"),
			textMessage("assistant", "done"),
			textMessage("assistant", "extra"),
		}, 3},
		{"all assistant", []openai.Message{
			textMessage("assistant", "a"),
			textMessage("assistant", "b"),
		}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountAssistantMessages(tt.messages)
			if got != tt.want {
				t.Fatalf("CountAssistantMessages = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDecide_HighRounds(t *testing.T) {
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "med-model",
		HighModel:         "high-model",
		MediumProbability: new(0.10),
		HighProbability:   new(0.01),
		HighRounds:        new(10),
	}

	d := Decide(profile, 0.5, 10)
	if d.Tier != TierHigh || d.Model != "high-model" || d.Reason != "rounds_high" {
		t.Fatalf("expected HIGH via rounds, got tier=%s model=%s reason=%s", d.Tier, d.Model, d.Reason)
	}

	// assistantCount=5 不整除 10，走概率
	d = Decide(profile, 0.5, 5)
	if d.Tier != TierLow {
		t.Fatalf("expected LOW (probability fallback), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}

func TestDecide_MediumRounds(t *testing.T) {
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "med-model",
		HighModel:         "high-model",
		MediumProbability: new(0.10),
		HighProbability:   new(0.01),
		MediumRounds:      new(5),
	}

	d := Decide(profile, 0.5, 5)
	if d.Tier != TierMedium || d.Model != "med-model" || d.Reason != "rounds_medium" {
		t.Fatalf("expected MEDIUM via rounds, got tier=%s model=%s reason=%s", d.Tier, d.Model, d.Reason)
	}

	// assistantCount=3 不整除 5，走概率
	d = Decide(profile, 0.5, 3)
	if d.Tier != TierLow {
		t.Fatalf("expected LOW (probability fallback), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}

func TestDecide_BothRounds_HighPriority(t *testing.T) {
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "med-model",
		HighModel:         "high-model",
		MediumProbability: new(0.10),
		HighProbability:   new(0.01),
		HighRounds:        new(10),
		MediumRounds:      new(5),
	}

	// assistantCount=10 同时被 10 和 5 整除，HIGH 优先
	d := Decide(profile, 0.5, 10)
	if d.Tier != TierHigh || d.Reason != "rounds_high" {
		t.Fatalf("expected HIGH (priority), got tier=%s reason=%s", d.Tier, d.Reason)
	}

	// assistantCount=5 只被 5 整除，不被 10 整除 → MEDIUM
	d = Decide(profile, 0.5, 5)
	if d.Tier != TierMedium || d.Reason != "rounds_medium" {
		t.Fatalf("expected MEDIUM (rounds), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}

func TestDecide_RoundsNotConfigured(t *testing.T) {
	// 两个 rounds 都未配置，完全走概率
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "med-model",
		HighModel:         "high-model",
		MediumProbability: new(0.10),
		HighProbability:   new(0.01),
	}

	d := Decide(profile, 0.5, 10)
	if d.Tier != TierLow || d.Reason != "probability_low" {
		t.Fatalf("expected LOW (probability), got tier=%s reason=%s", d.Tier, d.Reason)
	}

	d = Decide(profile, 0.005, 10)
	if d.Tier != TierHigh || d.Reason != "probability_high" {
		t.Fatalf("expected HIGH (probability), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}

func TestDecide_RoundsZeroValue(t *testing.T) {
	// rounds 配置为 0，视为未启用
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "med-model",
		HighModel:         "high-model",
		MediumProbability: new(0.10),
		HighProbability:   new(0.01),
		HighRounds:        new(0),
		MediumRounds:      new(0),
	}

	d := Decide(profile, 0.5, 10)
	if d.Tier != TierLow || d.Reason != "probability_low" {
		t.Fatalf("expected LOW (probability fallback), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}

func TestDecide_AssistantCountZero(t *testing.T) {
	// assistantCount=0，不会触发任何轮次判定
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "med-model",
		HighModel:         "high-model",
		MediumProbability: new(0.10),
		HighProbability:   new(0.01),
		HighRounds:        new(10),
		MediumRounds:      new(5),
	}

	d := Decide(profile, 0.5, 0)
	if d.Tier != TierLow || d.Reason != "probability_low" {
		t.Fatalf("expected LOW (probability), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}

func TestDecide_EmptyHighModel_ProbabilityFallsThroughToMedium(t *testing.T) {
	// High 模型为空：即使概率命中 HIGH 区间也应跳过 HIGH，
	// 但 Medium 概率区间仍然有效。
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "med-model",
		HighModel:         "",
		MediumProbability: new(0.50),
		HighProbability:   new(0.30),
	}

	// randomValue=0.10 原本落在 HIGH 区间，现在应回落到 MEDIUM
	d := Decide(profile, 0.10, 0)
	if d.Tier != TierMedium || d.Model != "med-model" || d.Reason != "probability_medium" {
		t.Fatalf("expected MEDIUM via probability, got tier=%s model=%s reason=%s", d.Tier, d.Model, d.Reason)
	}
}

func TestDecide_EmptyMediumModel_Probability(t *testing.T) {
	// Medium 模型为空：MEDIUM 概率区间被禁用，HIGH 仍生效。
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "",
		HighModel:         "high-model",
		MediumProbability: new(0.30),
		HighProbability:   new(0.50),
	}

	// 命中 HIGH 区间
	d := Decide(profile, 0.10, 0)
	if d.Tier != TierHigh || d.Model != "high-model" || d.Reason != "probability_high" {
		t.Fatalf("expected HIGH via probability, got tier=%s model=%s reason=%s", d.Tier, d.Model, d.Reason)
	}

	// 未命中 HIGH 区间，MEDIUM 已禁用 → LOW
	d = Decide(profile, 0.60, 0)
	if d.Tier != TierLow || d.Reason != "probability_low" {
		t.Fatalf("expected LOW (medium disabled), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}

func TestDecide_EmptyHighModel_RoundsSkipped(t *testing.T) {
	// High 模型为空：即使 assistantCount 整除 high-rounds 也不触发 HIGH，
	// 回落到 LOW。
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "med-model",
		HighModel:         "",
		MediumProbability: new(0.10),
		HighProbability:   new(0.10),
		HighRounds:        new(5),
	}

	d := Decide(profile, 0.90, 5)
	if d.Tier != TierLow || d.Reason != "probability_low" {
		t.Fatalf("expected LOW (high rounds disabled), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}

func TestDecide_EmptyMediumModel_RoundsSkipped(t *testing.T) {
	// Medium 模型为空：即使 assistantCount 整除 medium-rounds 也不触发 MEDIUM，
	// 未命中概率区间时回落到 LOW。
	profile := config.ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "",
		HighModel:         "high-model",
		MediumProbability: new(0.30),
		HighProbability:   new(0.10),
		MediumRounds:      new(5),
	}

	d := Decide(profile, 0.5, 5)
	if d.Tier != TierLow || d.Reason != "probability_low" {
		t.Fatalf("expected LOW (medium rounds disabled), got tier=%s reason=%s", d.Tier, d.Reason)
	}
}
