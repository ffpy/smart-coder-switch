package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"smart-coder-switch/internal/config"
)

func TestManagedHandlerUsesReloadedConfig(
	t *testing.T,
) {
	initial := &config.Config{}
	next := &config.Config{}

	manager, err := config.NewManager(
		"config.yaml",
		initial,
		func(string) (*config.Config, error) {
			return next, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	handler, err := newManagedHandler(
		manager,
		func(
			cfg *config.Config,
		) (http.Handler, error) {
			body := ""

			switch cfg {
			case initial:
				body = "initial"

			case next:
				body = "next"

			default:
				t.Fatal("unexpected config")
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

	assertManagedHandlerBody(
		t,
		handler,
		"initial",
	)

	if _, err := manager.Reload(); err != nil {
		t.Fatal(err)
	}

	assertManagedHandlerBody(
		t,
		handler,
		"next",
	)
}

func TestManagedHandlerKeepsRequestSnapshotDuringReload(
	t *testing.T,
) {
	initial := &config.Config{}
	next := &config.Config{}

	manager, err := config.NewManager(
		"config.yaml",
		initial,
		func(string) (*config.Config, error) {
			return next, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	handler, err := newManagedHandler(
		manager,
		func(
			cfg *config.Config,
		) (http.Handler, error) {
			switch cfg {
			case initial:
				return http.HandlerFunc(
					func(
						w http.ResponseWriter,
						r *http.Request,
					) {
						if _, err :=
							manager.Reload(); err != nil {

							t.Errorf(
								"reload failed: %v",
								err,
							)
						}

						_, _ = io.WriteString(
							w,
							"initial",
						)
					},
				), nil

			case next:
				return http.HandlerFunc(
					func(
						w http.ResponseWriter,
						r *http.Request,
					) {
						_, _ = io.WriteString(
							w,
							"next",
						)
					},
				), nil

			default:
				t.Fatal("unexpected config")
				return nil, nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	assertManagedHandlerBody(
		t,
		handler,
		"initial",
	)

	if manager.Snapshot().Version != 2 {
		t.Fatalf(
			"expected reload to version 2, got %d",
			manager.Snapshot().Version,
		)
	}

	assertManagedHandlerBody(
		t,
		handler,
		"next",
	)
}

func TestNewManagedHandlerRejectsNilManager(
	t *testing.T,
) {
	_, err := NewManagedHandler(nil, nil, nil)

	if err == nil {
		t.Fatal("expected error")
	}
}

func assertManagedHandlerBody(
	t *testing.T,
	handler http.Handler,
	expected string,
) {
	t.Helper()

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Body.String() != expected {
		t.Fatalf(
			"expected body %q, got %q",
			expected,
			response.Body.String(),
		)
	}
}
