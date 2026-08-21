package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"smart-coder-switch/internal/config"
)

func runReload(configPath string) int {
	return runAdminRequest(
		configPath,
		http.MethodPost,
		"/config/reload",
	)
}

func runStats(args []string, configPath string) int {
	if len(args) == 0 {
		return runAdminRequest(
			configPath,
			http.MethodGet,
			"/stats/models",
		)
	}

	if len(args) == 1 && args[0] == "reset" {
		return runStatsReset(configPath)
	}

	fmt.Fprintln(
		os.Stderr,
		"error: usage: smart-coder-switch stats [reset]",
	)

	return 2
}

func runStatsReset(configPath string) int {
	return runAdminRequest(
		configPath,
		http.MethodPost,
		"/stats/models/reset",
	)
}

func runConfVersion(configPath string) int {
	return runAdminRequest(
		configPath,
		http.MethodGet,
		"/config/version",
	)
}

func runConfig(configPath string) int {
	return runAdminRequest(
		configPath,
		http.MethodGet,
		"/config",
	)
}

func runAdminRequest(
	configPath string,
	method string,
	path string,
) int {
	adminURL, err := resolveAdminURL(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)

		return 1
	}

	request, err := http.NewRequest(
		method,
		adminURL+path,
		http.NoBody,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create request: %v\n", err)

		return 1
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)

		return 1
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read response: %v\n", err)

		return 1
	}

	if len(body) > 0 {
		_, _ = os.Stdout.Write(body)
		if body[len(body)-1] != '\n' {
			_, _ = fmt.Fprintln(os.Stdout)
		}
	}

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintf(
			os.Stderr,
			"error: admin request failed: %s\n",
			response.Status,
		)

		return 1
	}

	return 0
}

// resolveAdminURL loads the config file and returns
// the base admin URL based on listen.address.
func resolveAdminURL(
	configPath string,
) (string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", fmt.Errorf(
			"load config: %w",
			err,
		)
	}

	if cfg.Listen.Address == "" {
		return "", fmt.Errorf(
			"listen.address is empty",
		)
	}

	return fmt.Sprintf(
		"http://%s/admin",
		cfg.Listen.Address,
	), nil
}
