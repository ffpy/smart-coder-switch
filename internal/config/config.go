package config

type Config struct {
	Listen                   ListenConfig            `yaml:"listen" json:"listen"`
	Upstream                 UpstreamConfig          `yaml:"upstream" json:"upstream"`
	Models                   map[string]ModelProfile `yaml:"models" json:"models"`
	Log                      LogConfig               `yaml:"log" json:"log"`
	Trace                    TraceConfig             `yaml:"trace" json:"trace"`
	SQLite                   SQLiteConfig            `yaml:"sqlite" json:"sqlite"`
	IgnoredUserInputPrefixes []string                `yaml:"ignored-user-input-prefixes" json:"ignored_user_input_prefixes,omitempty"`
}

type ListenConfig struct {
	Address string `yaml:"address" json:"address"`
}

type UpstreamConfig struct {
	BaseURL string `yaml:"base-url" json:"base_url"`
	Timeout int    `yaml:"timeout" json:"timeout"`
}

type ModelProfile struct {
	LowModel string `yaml:"low-model" json:"low_model"`
	MediumModel string `yaml:"medium-model" json:"medium_model"`
	HighModel string `yaml:"high-model" json:"high_model"`
	MediumProbability *float64 `yaml:"medium-probability" json:"medium_probability"`
	HighProbability *float64 `yaml:"high-probability" json:"high_probability"`
	DirectModel *string `yaml:"direct-model" json:"direct_model,omitempty"`
	DirectPromptEnabled *bool `yaml:"direct-prompt-enabled" json:"direct_prompt_enabled,omitempty"`
	AntiRepetitionPromptEnabled    *bool    `yaml:"anti-repetition-prompt-enabled" json:"anti_repetition_prompt_enabled,omitempty"`
	ImagePromptEnabled             *bool    `yaml:"image-prompt-enabled" json:"image_prompt_enabled,omitempty"`
	HighRounds                     *int     `yaml:"high-rounds" json:"high_rounds,omitempty"`
	MediumRounds                   *int     `yaml:"medium-rounds" json:"medium_rounds,omitempty"`
}

// IsDirectPromptEnabled 返回该 profile 的提示开关是否启用。
// 当字段未配置（nil）时返回 true（默认启用）。
func (p ModelProfile) IsDirectPromptEnabled() bool {
	if p.DirectPromptEnabled == nil {
		return true
	}
	return *p.DirectPromptEnabled
}

// IsAntiRepetitionPromptEnabled 返回该 profile 的防复述注入开关是否启用。
// 当字段未配置（nil）时返回 false（默认禁用）。
func (p ModelProfile) IsAntiRepetitionPromptEnabled() bool {
	if p.AntiRepetitionPromptEnabled == nil {
		return false
	}
	return *p.AntiRepetitionPromptEnabled
}

// IsImagePromptEnabled 返回该 profile 的图片理解提示注入开关是否启用。
// 当字段未配置（nil）时返回 true（默认启用）。
func (p ModelProfile) IsImagePromptEnabled() bool {
	if p.ImagePromptEnabled == nil {
		return true
	}
	return *p.ImagePromptEnabled
}

// HighRoundsValue 返回 HighRounds 配置值。
// 当字段未配置（nil）或值 <= 0 时返回 0（未启用）。
func (p ModelProfile) HighRoundsValue() int {
	if p.HighRounds == nil || *p.HighRounds <= 0 {
		return 0
	}
	return *p.HighRounds
}

// MediumRoundsValue 返回 MediumRounds 配置值。
// 当字段未配置（nil）或值 <= 0 时返回 0（未启用）。
func (p ModelProfile) MediumRoundsValue() int {
	if p.MediumRounds == nil || *p.MediumRounds <= 0 {
		return 0
	}
	return *p.MediumRounds
}

type LogConfig struct {
	File  string `yaml:"file" json:"file"`
	Level string `yaml:"level" json:"level"`
}

type TraceConfig struct {
	MaxRecords  int    `yaml:"max-records" json:"max_records"`
	MaxBodySize int64  `yaml:"max-body-size" json:"max_body_size"`
	Directory   string `yaml:"directory" json:"directory"`
}

type SQLiteConfig struct {
	Path       string `yaml:"path" json:"path"`
	MaxRecords int    `yaml:"max-records" json:"max_records"`
}
