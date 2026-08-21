package config

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateRuntimeConfig(
	cfg *Config,
) error {
	if cfg == nil {
		return ErrNilRuntimeConfig
	}

	if strings.TrimSpace(cfg.Listen.Address) == "" {
		return errors.New("listen.address must not be empty")
	}

	if strings.TrimSpace(cfg.Upstream.BaseURL) == "" {
		return errors.New("upstream.base-url must not be empty")
	}

	if len(cfg.Models) == 0 {
		return errors.New("models must not be empty")
	}

	for logicalModel, profile := range cfg.Models {
		if strings.TrimSpace(logicalModel) == "" {
			return errors.New("logical model name must not be empty")
		}

		prefix := fmt.Sprintf("models.%s", logicalModel)

		if strings.TrimSpace(profile.LowModel) == "" {
			return fmt.Errorf("%s.low-model must not be empty", prefix)
		}
		if strings.TrimSpace(profile.MediumModel) == "" {
			return fmt.Errorf("%s.medium-model must not be empty", prefix)
		}
		if strings.TrimSpace(profile.HighModel) == "" {
			return fmt.Errorf("%s.high-model must not be empty", prefix)
		}

		if profile.MediumProbability == nil {
			return fmt.Errorf("%s.medium-probability must not be empty", prefix)
		}
		if profile.HighProbability == nil {
			return fmt.Errorf("%s.high-probability must not be empty", prefix)
		}

		med := *profile.MediumProbability
		high := *profile.HighProbability

		if med < 0 || med > 1 {
			return fmt.Errorf("%s.medium-probability must be in [0, 1]", prefix)
		}
		if high < 0 || high > 1 {
			return fmt.Errorf("%s.high-probability must be in [0, 1]", prefix)
		}
		if med+high > 1 {
			return fmt.Errorf("%s.medium-probability + high-probability must not exceed 1", prefix)
		}

		if profile.DirectModel != nil &&
			strings.TrimSpace(*profile.DirectModel) == "" {
			return fmt.Errorf("%s.direct-model must not be empty", prefix)
		}
	}

	if cfg.Trace.MaxRecords <= 0 {
		return errors.New("trace.max-records must be greater than 0")
	}
	if cfg.Trace.MaxBodySize <= 0 {
		return errors.New("trace.max-body-size must be greater than 0")
	}
	if strings.TrimSpace(cfg.Trace.Directory) == "" {
		return errors.New("trace.directory must not be empty")
	}

	return nil
}
