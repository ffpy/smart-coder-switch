package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"smart-coder-switch/internal/admin"
	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/proxy"
)

func TestHTTPReloadRejectsListenAddressChange(
	t *testing.T,
) {
	var receivedModels []string

	upstreamServer := httptest.NewServer(
		http.HandlerFunc(
			func(
				w http.ResponseWriter,
				r *http.Request,
			) {
				var request struct {
					Model string `json:"model"`
				}

				if err := json.NewDecoder(
					r.Body,
				).Decode(&request); err != nil {

					t.Errorf(
						"decode upstream request: %v",
						err,
					)

					return
				}

				receivedModels = append(
					receivedModels,
					request.Model,
				)

				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = w.Write(
					[]byte(`{"choices":[]}`),
				)
			},
		),
	)
	defer upstreamServer.Close()

	initialConfig := hotReloadTestConfig(
		upstreamServer.URL+"/",
		"medium-v1",
		t.TempDir(),
	)

	initialConfig.Listen.Address =
		"127.0.0.1:18082"

	reloadedConfig := hotReloadTestConfig(
		upstreamServer.URL+"/",
		"medium-v2",
		t.TempDir(),
	)

	reloadedConfig.Listen.Address =
		"127.0.0.1:19000"

	manager, err := config.NewManager(
		"config.yaml",
		initialConfig,
		func(string) (*config.Config, error) {
			return reloadedConfig, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	proxyHandler, err :=
		proxy.NewManagedHandler(manager, nil, nil)

	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.Handle(
		"/v1/chat/completions",
		proxyHandler,
	)

	mux.Handle(
		"/admin/config/",
		admin.NewHandler(manager, nil),
	)

	assertChatRequest(
		t,
		mux,
		http.StatusOK,
	)

	reloadRequest := httptest.NewRequest(
		http.MethodPost,
		"/admin/config/reload",
		nil,
	)

	reloadResponse :=
		httptest.NewRecorder()

	mux.ServeHTTP(
		reloadResponse,
		reloadRequest,
	)

	if reloadResponse.Code !=
		http.StatusInternalServerError {

		t.Fatalf(
			"expected reload status 500, got %d body=%s",
			reloadResponse.Code,
			reloadResponse.Body.String(),
		)
	}

	if !strings.Contains(
		reloadResponse.Body.String(),
		"listen.address cannot be changed by reload",
	) {
		t.Fatalf(
			"expected immutable listen address error, got %s",
			reloadResponse.Body.String(),
		)
	}

	if manager.Snapshot().Version != 1 {
		t.Fatalf(
			"expected version 1, got %d",
			manager.Snapshot().Version,
		)
	}

	if manager.Snapshot().
		Config.
		Listen.
		Address != "127.0.0.1:18082" {

		t.Fatalf(
			"expected old listen address preserved, got %q",
			manager.Snapshot().
				Config.
				Listen.
				Address,
		)
	}

	assertChatRequest(
		t,
		mux,
		http.StatusOK,
	)

	assertReceivedModels(
		t,
		receivedModels,
		[]string{
			"medium-v1",
			"medium-v1",
		},
	)
}
