package config

import (
	"os"
	"path/filepath"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// TestIsAntiRepetitionPromptEnabled_NilDefaultsFalse 测试未配置时默认返回 false。
func TestIsAntiRepetitionPromptEnabled_NilDefaultsFalse(t *testing.T) {
	profile := ModelProfile{
		LowModel:                     "low-model",
		MediumModel:                  "medium-model",
		HighModel:                    "high-model",
		MediumProbability:            floatPtr(0.10),
		HighProbability:              floatPtr(0.01),
		AntiRepetitionPromptEnabled: nil,
	}

	if profile.IsAntiRepetitionPromptEnabled() {
		t.Fatalf("expected IsAntiRepetitionPromptEnabled to return false when nil, got true")
	}
}

// TestIsAntiRepetitionPromptEnabled_ExplicitTrue 测试显式设置 true 时返回 true。
func TestIsAntiRepetitionPromptEnabled_ExplicitTrue(t *testing.T) {
	profile := ModelProfile{
		LowModel:                     "low-model",
		MediumModel:                  "medium-model",
		HighModel:                    "high-model",
		MediumProbability:            floatPtr(0.10),
		HighProbability:              floatPtr(0.01),
		AntiRepetitionPromptEnabled: boolPtr(true),
	}

	if !profile.IsAntiRepetitionPromptEnabled() {
		t.Fatalf("expected IsAntiRepetitionPromptEnabled to return true when explicitly set to true, got false")
	}
}

// TestIsAntiRepetitionPromptEnabled_ExplicitFalse 测试显式设置 false 时返回 false。
func TestIsAntiRepetitionPromptEnabled_ExplicitFalse(t *testing.T) {
	profile := ModelProfile{
		LowModel:                     "low-model",
		MediumModel:                  "medium-model",
		HighModel:                    "high-model",
		MediumProbability:            floatPtr(0.10),
		HighProbability:              floatPtr(0.01),
		AntiRepetitionPromptEnabled: boolPtr(false),
	}

	if profile.IsAntiRepetitionPromptEnabled() {
		t.Fatalf("expected IsAntiRepetitionPromptEnabled to return false when explicitly set to false, got true")
	}
}

func floatPtr(v float64) *float64 { return &v }

// TestIsImagePromptEnabled_NilDefaultsTrue 测试图片理解提示开关未配置时默认返回 true。
func TestIsImagePromptEnabled_NilDefaultsTrue(t *testing.T) {
	profile := ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "medium-model",
		HighModel:         "high-model",
		MediumProbability: floatPtr(0.10),
		HighProbability:   floatPtr(0.01),
		ImagePromptEnabled: nil,
	}

	if !profile.IsImagePromptEnabled() {
		t.Fatalf("expected IsImagePromptEnabled to return true when nil, got false")
	}
}

// TestIsImagePromptEnabled_ExplicitTrue 测试显式设置 true 时返回 true。
func TestIsImagePromptEnabled_ExplicitTrue(t *testing.T) {
	profile := ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "medium-model",
		HighModel:         "high-model",
		MediumProbability: floatPtr(0.10),
		HighProbability:   floatPtr(0.01),
		ImagePromptEnabled: boolPtr(true),
	}

	if !profile.IsImagePromptEnabled() {
		t.Fatalf("expected IsImagePromptEnabled to return true when explicitly set to true, got false")
	}
}

// TestIsImagePromptEnabled_ExplicitFalse 测试显式设置 false 时返回 false。
func TestIsImagePromptEnabled_ExplicitFalse(t *testing.T) {
	profile := ModelProfile{
		LowModel:          "low-model",
		MediumModel:       "medium-model",
		HighModel:         "high-model",
		MediumProbability: floatPtr(0.10),
		HighProbability:   floatPtr(0.01),
		ImagePromptEnabled: boolPtr(false),
	}

	if profile.IsImagePromptEnabled() {
		t.Fatalf("expected IsImagePromptEnabled to return false when explicitly set to false, got true")
	}
}

func TestInitLoggerCreatesParentDirectory(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "nested", "logs", "app.log")

	if err := InitLogger(&Config{Log: LogConfig{File: logPath}}); err != nil {
		t.Fatalf("InitLogger returned error: %v", err)
	}

	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected log file to be created: %v", err)
	}
}
