package proxy

import "smart-coder-switch/internal/config"

func ValidateRuntimeConfig(
	cfg *config.Config,
) error {
	_, err := buildProxyHandler(cfg, nil, nil, nil)
	return err
}
