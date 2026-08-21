package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := Config{
		Log: LogConfig{
			Level: "info",
		},
		Trace: TraceConfig{
			MaxRecords:  100,
			MaxBodySize: 20 * 1024 * 1024,
			Directory:   "./log/traces",
		},
		SQLite: SQLiteConfig{
			Path:       "./data/decisions.db",
			MaxRecords: 1000,
		},
		IgnoredUserInputPrefixes: []string{
			"<system-reminder>",
			"[Compressed conversation section]",
		},
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Listen.Address == "" {
		return fmt.Errorf("listen.address is required")
	}

	if c.Log.Level == "" {
		c.Log.Level = "info"
	}

	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf(
			"log.level must be one of: debug, info, warn, error",
		)
	}

	if c.Upstream.BaseURL == "" {
		return fmt.Errorf("upstream.base-url is required")
	}

	if c.Upstream.Timeout < 0 {
		return fmt.Errorf("upstream.timeout must be >= 0")
	}

	if len(c.Models) == 0 {
		return fmt.Errorf("models is required")
	}

	for name, profile := range c.Models {
		if profile.LowModel == "" {
			return fmt.Errorf(
				"models.%s.low-model is required",
				name,
			)
		}

		if profile.MediumModel == "" {
			return fmt.Errorf(
				"models.%s.medium-model is required",
				name,
			)
		}

		if profile.HighModel == "" {
			return fmt.Errorf(
				"models.%s.high-model is required",
				name,
			)
		}

		if profile.MediumProbability == nil {
			return fmt.Errorf(
				"models.%s.medium-probability is required",
				name,
			)
		}

		if profile.HighProbability == nil {
			return fmt.Errorf(
				"models.%s.high-probability is required",
				name,
			)
		}

		if *profile.MediumProbability < 0 ||
			*profile.MediumProbability > 1 {
			return fmt.Errorf(
				"models.%s.medium-probability must be in [0, 1]",
				name,
			)
		}

		if *profile.HighProbability < 0 ||
			*profile.HighProbability > 1 {
			return fmt.Errorf(
				"models.%s.high-probability must be in [0, 1]",
				name,
			)
		}

		if *profile.MediumProbability+*profile.HighProbability > 1 {
			return fmt.Errorf(
				"models.%s.medium-probability + high-probability must not exceed 1",
				name,
			)
		}

		if profile.DirectModel != nil &&
			*profile.DirectModel == "" {
			return fmt.Errorf(
				"models.%s.direct-model must not be empty",
				name,
			)
		}
	}

	if c.Trace.MaxRecords <= 0 {
		return fmt.Errorf(
			"trace.max-records must be greater than 0",
		)
	}

	if c.Trace.MaxBodySize <= 0 {
		return fmt.Errorf(
			"trace.max-body-size must be greater than 0",
		)
	}

	if c.Trace.Directory == "" {
		return fmt.Errorf(
			"trace.directory is required",
		)
	}

	return nil
}

func (c *Config) Clone() *Config {
	clone := *c
	clone.IgnoredUserInputPrefixes = make([]string, len(c.IgnoredUserInputPrefixes))
	copy(clone.IgnoredUserInputPrefixes, c.IgnoredUserInputPrefixes)
	return &clone
}

// Save writes the config to a YAML file at the given path.
func Save(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}
