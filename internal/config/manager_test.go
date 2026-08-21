package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ptrFloat64(v float64) *float64 { return &v }

func TestNewManagerRejectsNilLoader(
	t *testing.T,
) {
	_, err := NewManager(
		"config.yaml",
		&Config{},
		nil,
	)

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestManagerSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// Write an initial config file so Reload can read it
	initial := &Config{
		Listen:   ListenConfig{Address: ":18082"},
		Upstream: UpstreamConfig{BaseURL: "http://upstream:8080", Timeout: 30},
		Models: map[string]ModelProfile{
			"coder1": {
				LowModel:          "low-model",
				MediumModel:       "medium-model",
				HighModel:         "high-model",
				MediumProbability: ptrFloat64(0.1),
				HighProbability:   ptrFloat64(0.01),
			},
		},
		Log:   LogConfig{Level: "info"},
		Trace: TraceConfig{MaxRecords: 100, MaxBodySize: 20 * 1024 * 1024, Directory: "./log/traces"},
	}

	// Save initial config to file
	if err := Save(configPath, initial); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	// Create manager loading from the file
	manager, err := NewManager(configPath, initial, Load)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if manager.Snapshot().Version != 1 {
		t.Fatalf("expected initial version 1, got %d", manager.Snapshot().Version)
	}

	// Modify the config
	updated := initial.Clone()
	updated.Upstream.BaseURL = "http://new-upstream:9090"

	// SaveAndReload should persist and bump version
	snapshot, err := manager.SaveAndReload(updated)
	if err != nil {
		t.Fatalf("save and reload: %v", err)
	}

	if snapshot.Version != 2 {
		t.Fatalf("expected version 2 after save, got %d", snapshot.Version)
	}

	if snapshot.Config.Upstream.BaseURL != "http://new-upstream:9090" {
		t.Fatalf("expected new upstream URL, got %s", snapshot.Config.Upstream.BaseURL)
	}

	// Verify file was actually written
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(data), "new-upstream") {
		t.Fatalf("config file not persisted, content: %s", string(data))
	}
}

func TestManagerSaveAndReloadInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	initial := &Config{
		Listen:   ListenConfig{Address: ":18082"},
		Upstream: UpstreamConfig{BaseURL: "http://upstream:8080", Timeout: 30},
		Models: map[string]ModelProfile{
			"coder1": {
				LowModel:          "low-model",
				MediumModel:       "medium-model",
				HighModel:         "high-model",
				MediumProbability: ptrFloat64(0.1),
				HighProbability:   ptrFloat64(0.01),
			},
		},
		Log:   LogConfig{Level: "info"},
		Trace: TraceConfig{MaxRecords: 100, MaxBodySize: 20 * 1024 * 1024, Directory: "./log/traces"},
	}

	if err := Save(configPath, initial); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	manager, err := NewManager(configPath, initial, Load)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Invalid config: empty upstream URL
	bad := initial.Clone()
	bad.Upstream.BaseURL = ""

	_, err = manager.SaveAndReload(bad)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}

	// Version should not have changed
	if manager.Snapshot().Version != 1 {
		t.Fatalf("expected version unchanged at 1, got %d", manager.Snapshot().Version)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	cfg := &Config{
		Listen:   ListenConfig{Address: ":9999"},
		Upstream: UpstreamConfig{BaseURL: "http://test:8080", Timeout: 60},
		Models: map[string]ModelProfile{
			"test-model": {
				LowModel:          "gpt-4o-mini",
				MediumModel:       "gpt-4o",
				HighModel:         "gpt-4o",
				MediumProbability: ptrFloat64(0.15),
				HighProbability:   ptrFloat64(0.05),
			},
		},
		Log:   LogConfig{Level: "debug"},
		Trace: TraceConfig{MaxRecords: 500, MaxBodySize: 1024, Directory: "./traces"},
	}

	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Listen.Address != ":9999" {
		t.Errorf("listen.address = %s, want :9999", loaded.Listen.Address)
	}
	if loaded.Upstream.BaseURL != "http://test:8080" {
		t.Errorf("upstream.base-url = %s, want http://test:8080", loaded.Upstream.BaseURL)
	}
	if loaded.Upstream.Timeout != 60 {
		t.Errorf("upstream.timeout = %d, want 60", loaded.Upstream.Timeout)
	}
	if loaded.Log.Level != "debug" {
		t.Errorf("log.level = %s, want debug", loaded.Log.Level)
	}
	profile, ok := loaded.Models["test-model"]
	if !ok {
		t.Fatalf("expected model test-model")
	}
	if profile.LowModel != "gpt-4o-mini" {
		t.Errorf("low-model = %s, want gpt-4o-mini", profile.LowModel)
	}
	if profile.MediumModel != "gpt-4o" {
		t.Errorf("medium-model = %s, want gpt-4o", profile.MediumModel)
	}
	if profile.HighModel != "gpt-4o" {
		t.Errorf("high-model = %s, want gpt-4o", profile.HighModel)
	}
}
