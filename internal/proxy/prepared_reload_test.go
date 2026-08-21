package proxy

import (
	"io"
	"net/http"
	"testing"

	"smart-coder-switch/internal/config"
)

func TestManagedHandlerReusesPreparedReloadHandler(
	t *testing.T,
) {
	initial := &config.Config{}
	reloaded := &config.Config{}

	manager, err := config.NewManager(
		"config.yaml",
		initial,
		func(string) (*config.Config, error) {
			return reloaded, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	buildCount := 0

	handler, err := newManagedHandler(
		manager,
		func(
			cfg *config.Config,
		) (http.Handler, error) {
			buildCount++

			body := "unknown"

			switch cfg {
			case initial:
				body = "initial"

			case reloaded:
				body = "reloaded"
			}

			return http.HandlerFunc(
				func(
					w http.ResponseWriter,
					r *http.Request,
				) {
					_, _ = io.WriteString(
						w,
						body,
					)
				},
			), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if buildCount != 1 {
		t.Fatalf(
			"expected initial build count 1, got %d",
			buildCount,
		)
	}

	if err := manager.SetValidator(
		handler.prepareRuntimeConfig,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, err := manager.Reload()
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Version != 2 {
		t.Fatalf(
			"expected version 2, got %d",
			snapshot.Version,
		)
	}

	if buildCount != 2 {
		t.Fatalf(
			"expected validation to build once, got build count %d",
			buildCount,
		)
	}

	assertManagedHandlerBody(
		t,
		handler,
		"reloaded",
	)

	if buildCount != 2 {
		t.Fatalf(
			"expected prepared handler reused, got build count %d",
			buildCount,
		)
	}
}

func TestPreparedHandlerConsumedOnlyOnce(
	t *testing.T,
) {
	initial := &config.Config{}
	reloaded := &config.Config{}

	manager, err := config.NewManager(
		"config.yaml",
		initial,
		func(string) (*config.Config, error) {
			return reloaded, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	buildCount := 0

	handler, err := newManagedHandler(
		manager,
		func(
			cfg *config.Config,
		) (http.Handler, error) {
			buildCount++

			return http.HandlerFunc(
				func(
					w http.ResponseWriter,
					r *http.Request,
				) {
					_, _ = io.WriteString(
						w,
						"ok",
					)
				},
			), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.SetValidator(
		handler.prepareRuntimeConfig,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Reload(); err != nil {
		t.Fatal(err)
	}

	assertManagedHandlerBody(
		t,
		handler,
		"ok",
	)

	assertManagedHandlerBody(
		t,
		handler,
		"ok",
	)

	if buildCount != 2 {
		t.Fatalf(
			"expected exactly 2 total builds, got %d",
			buildCount,
		)
	}
}
