package config

import (
	"strings"
	"testing"
)

func medProb(v float64) *float64 { return &v }
func highProb(v float64) *float64 { return &v }
func strPtr(s string) *string { return &s }

func TestValidateRuntimeConfigAcceptsValidConfig(t *testing.T) {
	cfg := validRuntimeConfig()
	if err := ValidateRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRuntimeConfigRejectsNilConfig(t *testing.T) {
	err := ValidateRuntimeConfig(nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "runtime config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRuntimeConfigRejectsMissingMediumProbability(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.MediumProbability = nil
	cfg.Models["coder1"] = p
	err := ValidateRuntimeConfig(cfg)
	assertValidationErrorContains(t, err, "medium-probability")
}

func TestValidateRuntimeConfigRejectsMissingHighProbability(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.HighProbability = nil
	cfg.Models["coder1"] = p
	err := ValidateRuntimeConfig(cfg)
	assertValidationErrorContains(t, err, "high-probability")
}

func TestValidateRuntimeConfigRejectsMediumProbabilityOutOfRange(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.MediumProbability = medProb(1.5)
	cfg.Models["coder1"] = p
	err := ValidateRuntimeConfig(cfg)
	assertValidationErrorContains(t, err, "must be in [0, 1]")
}

func TestValidateRuntimeConfigRejectsHighProbabilityOutOfRange(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.HighProbability = highProb(-0.1)
	cfg.Models["coder1"] = p
	err := ValidateRuntimeConfig(cfg)
	assertValidationErrorContains(t, err, "must be in [0, 1]")
}

func TestValidateRuntimeConfigRejectsSumExceedsOne(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.MediumProbability = medProb(0.6)
	p.HighProbability = highProb(0.5)
	cfg.Models["coder1"] = p
	err := ValidateRuntimeConfig(cfg)
	assertValidationErrorContains(t, err, "must not exceed 1")
}

func TestValidateRuntimeConfigRejectsMissingModelField(t *testing.T) {
	cfg := validRuntimeConfig()
	profile := cfg.Models["coder1"]
	profile.HighModel = ""
	cfg.Models["coder1"] = profile
	err := ValidateRuntimeConfig(cfg)
	assertValidationErrorContains(t, err, "models.coder1.high-model")
}

func TestValidateRuntimeConfigRejectsEmptyModelName(t *testing.T) {
	cfg := validRuntimeConfig()
	cfg.Models[""] = ModelProfile{
		LowModel:          "low",
		MediumModel:       "medium",
		HighModel:         "high",
		MediumProbability: medProb(0.10),
		HighProbability:   highProb(0.01),
	}
	err := ValidateRuntimeConfig(cfg)
	assertValidationErrorContains(t, err, "logical model name")
}

func TestValidateRuntimeConfigAcceptsValidDirectModel(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.DirectModel = strPtr("gpt-5.6-terra")
	cfg.Models["coder1"] = p
	if err := ValidateRuntimeConfig(cfg); err != nil {
		t.Fatalf("expected no error for valid direct-model, got: %v", err)
	}
}

func TestValidateRuntimeConfigAcceptsNilDirectModel(t *testing.T) {
	cfg := validRuntimeConfig()
	// DirectModel is nil by default, should be accepted
	if err := ValidateRuntimeConfig(cfg); err != nil {
		t.Fatalf("expected no error for nil direct-model, got: %v", err)
	}
}

func TestValidateRuntimeConfigRejectsEmptyDirectModel(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.DirectModel = strPtr("")
	cfg.Models["coder1"] = p
	err := ValidateRuntimeConfig(cfg)
	assertValidationErrorContains(t, err, "direct-model")
}

func TestValidateRuntimeConfigAcceptsZeroProbabilities(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.MediumProbability = medProb(0)
	p.HighProbability = highProb(0)
	cfg.Models["coder1"] = p
	if err := ValidateRuntimeConfig(cfg); err != nil {
		t.Fatalf("expected no error for zero probabilities, got: %v", err)
	}
}

func TestValidateRuntimeConfigAcceptsOneTotalProbability(t *testing.T) {
	cfg := validRuntimeConfig()
	p := cfg.Models["coder1"]
	p.MediumProbability = medProb(0.3)
	p.HighProbability = highProb(0.7)
	cfg.Models["coder1"] = p
	if err := ValidateRuntimeConfig(cfg); err != nil {
		t.Fatalf("expected no error for sum=1, got: %v", err)
	}
}

func assertValidationErrorContains(t *testing.T, err error, expected string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error containing %q", expected)
	}
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected error containing %q, got %v", expected, err)
	}
}

func validRuntimeConfig() *Config {
	return &Config{
		Listen: ListenConfig{
			Address: "127.0.0.1:18082",
		},
		Upstream: UpstreamConfig{
			BaseURL: "http://127.0.0.1:18080/",
		},
		Models: map[string]ModelProfile{
			"coder1": {
				LowModel:          "low-coder1",
				MediumModel:       "medium-coder1",
				HighModel:         "high-coder1",
				MediumProbability: medProb(0.10),
				HighProbability:   highProb(0.01),
			},
		},
		Trace: TraceConfig{
			MaxRecords:  100,
			MaxBodySize: 20 * 1024 * 1024,
			Directory:   "./log/traces",
		},
	}
}
