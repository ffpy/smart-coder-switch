package config

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var ErrNilRuntimeConfig = errors.New(
	"runtime config must not be nil",
)

type RuntimeConfig struct {
	Config  *Config
	Version uint64
}

type RuntimeConfigLoader func(
	path string,
) (*Config, error)

type RuntimeConfigValidator func(
	cfg *Config,
) error

type Manager struct {
	path      string
	loader    RuntimeConfigLoader
	validator RuntimeConfigValidator

	reloadMu sync.Mutex
	current  atomic.Pointer[RuntimeConfig]
}

func NewManager(
	path string,
	initial *Config,
	loader RuntimeConfigLoader,
) (*Manager, error) {
	if initial == nil {
		return nil, ErrNilRuntimeConfig
	}

	if loader == nil {
		return nil, errors.New(
			"runtime config loader must not be nil",
		)
	}

	manager := &Manager{
		path:   path,
		loader: loader,
	}

	manager.current.Store(
		&RuntimeConfig{
			Config:  initial,
			Version: 1,
		},
	)

	return manager, nil
}

func (m *Manager) SetValidator(
	validator RuntimeConfigValidator,
) error {
	if validator == nil {
		return errors.New(
			"runtime config validator must not be nil",
		)
	}

	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	m.validator = validator

	return nil
}

func (m *Manager) Snapshot() *RuntimeConfig {
	return m.current.Load()
}

func (m *Manager) Reload() (
	*RuntimeConfig,
	error,
) {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	current := m.Snapshot()

	candidate, err := m.loader(m.path)
	if err != nil {
		return current, fmt.Errorf(
			"reload config: %w",
			err,
		)
	}

	if candidate == nil {
		return current, fmt.Errorf(
			"reload config: %w",
			ErrNilRuntimeConfig,
		)
	}

	if m.validator != nil {
		if err := m.validator(candidate); err != nil {
			return current, fmt.Errorf(
				"validate runtime config: %w",
				err,
			)
		}
	}

	next := &RuntimeConfig{
		Config:  candidate,
		Version: current.Version + 1,
	}

	m.current.Store(next)

	return next, nil
}

// SaveAndReload persists the given config to the file path, then reloads it.
// The config is validated before saving. On validation failure, nothing is written.
func (m *Manager) SaveAndReload(cfg *Config) (*RuntimeConfig, error) {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	if cfg == nil {
		return m.current.Load(), fmt.Errorf("config must not be nil")
	}

	if err := cfg.Validate(); err != nil {
		return m.current.Load(), fmt.Errorf("validate config: %w", err)
	}

	if err := Save(m.path, cfg); err != nil {
		return m.current.Load(), fmt.Errorf("save config: %w", err)
	}

	// Reload from the file we just wrote
	current := m.current.Load()

	candidate, err := m.loader(m.path)
	if err != nil {
		return current, fmt.Errorf("reload after save: %w", err)
	}

	if candidate == nil {
		return current, fmt.Errorf("reload after save: %w", ErrNilRuntimeConfig)
	}

	if m.validator != nil {
		if err := m.validator(candidate); err != nil {
			return current, fmt.Errorf("validate runtime config: %w", err)
		}
	}

	next := &RuntimeConfig{
		Config:  candidate,
		Version: current.Version + 1,
	}

	m.current.Store(next)

	return next, nil
}
