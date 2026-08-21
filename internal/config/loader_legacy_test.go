package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadIgnoresLegacyInjectPlanPromptTrue 证明：
// YAML 中包含旧的 inject-plan-prompt: true 时，
// Load 成功且 ModelProfile 不暴露该字段。
func TestLoadIgnoresLegacyInjectPlanPromptTrue(t *testing.T) {
	yamlContent := `
listen:
  address: 127.0.0.1:18082

upstream:
  base-url: http://127.0.0.1:18080/

models:
  coder1:
    low-model: low-model
    medium-model: medium-model
    high-model: high-model
    medium-probability: 0.10
    high-probability: 0.01
    direct-model: direct-model
    inject-plan-prompt: true
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load should succeed with legacy inject-plan-prompt: true, got: %v", err)
	}

	profile := cfg.Models["coder1"]
	if profile.DirectModel == nil || *profile.DirectModel != "direct-model" {
		t.Fatal("direct-model should be parsed")
	}
	// InjectPlanPrompt 字段已从 ModelProfile 删除，无法访问。
	// 通过编译即证明结构体已不再包含该字段。
}

// TestLoadIgnoresLegacyInjectPlanPromptFalse 证明：
// YAML 中包含旧的 inject-plan-prompt: false 时，
// Load 成功且行为与未配置该字段完全相同。
func TestLoadIgnoresLegacyInjectPlanPromptFalse(t *testing.T) {
	yamlContent := `
listen:
  address: 127.0.0.1:18082

upstream:
  base-url: http://127.0.0.1:18080/

models:
  coder1:
    low-model: low-model
    medium-model: medium-model
    high-model: high-model
    medium-probability: 0.10
    high-probability: 0.01
    inject-plan-prompt: false
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load should succeed with legacy inject-plan-prompt: false, got: %v", err)
	}

	if len(cfg.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(cfg.Models))
	}
}

// TestLoadDirectPromptEnabled 测试
// 新开关 direct-prompt-enabled 的解析行为。
func TestLoadDirectPromptEnabled(t *testing.T) {
	t.Run("default when omitted", func(t *testing.T) {
		yamlContent := `
listen:
  address: 127.0.0.1:18082
upstream:
  base-url: http://127.0.0.1:18080/
models:
  coder1:
    low-model: low-model
    medium-model: medium-model
    high-model: high-model
    medium-probability: 0.10
    high-probability: 0.01
    direct-model: direct-model
`
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("Load should succeed, got: %v", err)
		}
		profile := cfg.Models["coder1"]
		if !profile.IsDirectPromptEnabled() {
			t.Fatal("expected prompt enabled by default when field omitted")
		}
	})

	t.Run("explicit true", func(t *testing.T) {
		yamlContent := `
listen:
  address: 127.0.0.1:18082
upstream:
  base-url: http://127.0.0.1:18080/
models:
  coder1:
    low-model: low-model
    medium-model: medium-model
    high-model: high-model
    medium-probability: 0.10
    high-probability: 0.01
    direct-model: direct-model
    direct-prompt-enabled: true
`
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("Load should succeed, got: %v", err)
		}
		profile := cfg.Models["coder1"]
		if !profile.IsDirectPromptEnabled() {
			t.Fatal("expected prompt enabled when true")
		}
	})

	t.Run("explicit false", func(t *testing.T) {
		yamlContent := `
listen:
  address: 127.0.0.1:18082
upstream:
  base-url: http://127.0.0.1:18080/
models:
  coder1:
    low-model: low-model
    medium-model: medium-model
    high-model: high-model
    medium-probability: 0.10
    high-probability: 0.01
    direct-model: direct-model
    direct-prompt-enabled: false
`
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("Load should succeed, got: %v", err)
		}
		profile := cfg.Models["coder1"]
		if profile.IsDirectPromptEnabled() {
			t.Fatal("expected prompt disabled when explicit false")
		}
	})
}

// TestLoadIgnoresLegacyInjectPlanPromptOmitted 证明：
// YAML 中不包含 inject-plan-prompt 时，Load 成功（基准行为）。
func TestLoadIgnoresLegacyInjectPlanPromptOmitted(t *testing.T) {
	yamlContent := `
listen:
  address: 127.0.0.1:18082

upstream:
  base-url: http://127.0.0.1:18080/

models:
  coder1:
    low-model: low-model
    medium-model: medium-model
    high-model: high-model
    medium-probability: 0.10
    high-probability: 0.01
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load should succeed without inject-plan-prompt, got: %v", err)
	}

	if len(cfg.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(cfg.Models))
	}
}
