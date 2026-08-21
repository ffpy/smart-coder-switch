package config

import (
	"errors"
	"testing"
)

func TestManagerValidatorRejectsReload(
	t *testing.T,
) {
	initial := &Config{}
	candidate := &Config{}

	manager, err := NewManager(
		"config.yaml",
		initial,
		func(string) (*Config, error) {
			return candidate, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	validationError :=
		errors.New("runtime build failed")

	if err := manager.SetValidator(
		func(cfg *Config) error {
			if cfg != candidate {
				t.Fatal(
					"validator received unexpected config",
				)
			}

			return validationError
		},
	); err != nil {
		t.Fatal(err)
	}

	before := manager.Snapshot()

	result, err := manager.Reload()
	if err == nil {
		t.Fatal(
			"expected validation error",
		)
	}

	if !errors.Is(err, validationError) {
		t.Fatalf(
			"expected validation error, got %v",
			err,
		)
	}

	if result != before {
		t.Fatal(
			"expected previous snapshot returned",
		)
	}

	if manager.Snapshot() != before {
		t.Fatal(
			"expected previous snapshot preserved",
		)
	}

	if manager.Snapshot().Version != 1 {
		t.Fatalf(
			"expected version 1, got %d",
			manager.Snapshot().Version,
		)
	}
}

func TestManagerValidatorAllowsReload(
	t *testing.T,
) {
	initial := &Config{}
	candidate := &Config{}
	validated := false

	manager, err := NewManager(
		"config.yaml",
		initial,
		func(string) (*Config, error) {
			return candidate, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.SetValidator(
		func(cfg *Config) error {
			validated = true

			if cfg != candidate {
				t.Fatal(
					"validator received unexpected config",
				)
			}

			return nil
		},
	); err != nil {
		t.Fatal(err)
	}

	result, err := manager.Reload()
	if err != nil {
		t.Fatal(err)
	}

	if !validated {
		t.Fatal(
			"expected validator called",
		)
	}

	if result.Config != candidate {
		t.Fatal(
			"expected candidate config",
		)
	}

	if result.Version != 2 {
		t.Fatalf(
			"expected version 2, got %d",
			result.Version,
		)
	}
}

func TestManagerRejectsNilValidator(
	t *testing.T,
) {
	manager, err := NewManager(
		"config.yaml",
		&Config{},
		func(string) (*Config, error) {
			return &Config{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.SetValidator(nil); err == nil {
		t.Fatal(
			"expected nil validator error",
		)
	}
}
