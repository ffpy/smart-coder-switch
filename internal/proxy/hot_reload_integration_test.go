package proxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"smart-coder-switch/internal/admin"
	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/proxy"
)

func TestHTTPConfigReloadChangesSelectedModel(
	t *testing.T,
) {
	var mutex sync.Mutex
	var receivedModels []string
	var receivedAuthorizations []string

	upstreamServer := httptest.NewServer(
		http.HandlerFunc(
			func(
				w http.ResponseWriter,
				r *http.Request,
			) {
				if r.URL.Path !=
					"/v1/chat/completions" {

					t.Errorf(
						"unexpected upstream path %s",
						r.URL.Path,
					)
				}

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

					http.Error(
						w,
						err.Error(),
						http.StatusBadRequest,
					)

					return
				}

				mutex.Lock()

				receivedModels = append(
					receivedModels,
					request.Model,
				)

				receivedAuthorizations = append(
					receivedAuthorizations,
					r.Header.Get("Authorization"),
				)

				mutex.Unlock()

				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_ = json.NewEncoder(w).Encode(
					map[string]any{
						"id":      "test-response",
						"object":  "chat.completion",
						"choices": []any{},
					},
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

	reloadedConfig := hotReloadTestConfig(
		upstreamServer.URL+"/",
		"medium-v2",
		t.TempDir(),
	)

	nextConfig := reloadedConfig

	manager, err := config.NewManager(
		"config.yaml",
		initialConfig,
		func(string) (*config.Config, error) {
			return nextConfig, nil
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

	assertReceivedModels(
		t,
		receivedModels,
		[]string{
			"medium-v1",
		},
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

	if reloadResponse.Code != http.StatusOK {
		t.Fatalf(
			"expected reload status 200, got %d body=%s",
			reloadResponse.Code,
			reloadResponse.Body.String(),
		)
	}

	if manager.Snapshot().Version != 2 {
		t.Fatalf(
			"expected config version 2, got %d",
			manager.Snapshot().Version,
		)
	}

	assertChatRequest(
		t,
		mux,
		http.StatusOK,
	)

	mutex.Lock()
	defer mutex.Unlock()

	assertReceivedModels(
		t,
		receivedModels,
		[]string{
			"medium-v1",
			"medium-v2",
		},
	)

	if len(receivedAuthorizations) != 2 {
		t.Fatalf(
			"expected 2 authorization headers, got %d",
			len(receivedAuthorizations),
		)
	}

	for index, authorization := range receivedAuthorizations {

		if authorization != "Bearer test-key" {
			t.Fatalf(
				"request %d expected authorization forwarded, got %q",
				index,
				authorization,
			)
		}
	}
}

func TestHTTPFailedReloadKeepsPreviousModel(
	t *testing.T,
) {
	var mutex sync.Mutex
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
						"decode request: %v",
						err,
					)

					return
				}

				mutex.Lock()

				receivedModels = append(
					receivedModels,
					request.Model,
				)

				mutex.Unlock()

				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = io.WriteString(
					w,
					`{"choices":[]}`,
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

	manager, err := config.NewManager(
		"config.yaml",
		initialConfig,
		func(string) (*config.Config, error) {
			return nil, io.ErrUnexpectedEOF
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
			"expected reload status 500, got %d",
			reloadResponse.Code,
		)
	}

	if manager.Snapshot().Version != 1 {
		t.Fatalf(
			"expected config version 1, got %d",
			manager.Snapshot().Version,
		)
	}

	assertChatRequest(
		t,
		mux,
		http.StatusOK,
	)

	mutex.Lock()
	defer mutex.Unlock()

	assertReceivedModels(
		t,
		receivedModels,
		[]string{
			"medium-v1",
			"medium-v1",
		},
	)
}

func assertChatRequest(
	t *testing.T,
	handler http.Handler,
	expectedStatus int,
) {
	t.Helper()

	body := `{
		"model": "coder1",
		"messages": [
			{
				"role": "user",
				"content": "hello"
			}
		],
		"stream": false
	}`

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		io.NopCloser(
			http.NoBody,
		),
	)

	request.Body = io.NopCloser(
		newStringReader(body),
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	request.Header.Set(
		"Authorization",
		"Bearer test-key",
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != expectedStatus {
		t.Fatalf(
			"expected status %d, got %d body=%s",
			expectedStatus,
			response.Code,
			response.Body.String(),
		)
	}
}

func assertReceivedModels(
	t *testing.T,
	actual []string,
	expected []string,
) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Fatalf(
			"expected models %v, got %v",
			expected,
			actual,
		)
	}

	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf(
				"expected models %v, got %v",
				expected,
				actual,
			)
		}
	}
}

func hotReloadTestConfig(
	baseURL string,
	mediumModel string,
	traceDirectory string,
) *config.Config {
	med := 0.0
	high := 0.0
	return &config.Config{
		Listen: config.ListenConfig{
			Address: "127.0.0.1:18082",
		},

		Upstream: config.UpstreamConfig{
			BaseURL: baseURL,
		},

		Models: map[string]config.ModelProfile{
			"coder1": {
				LowModel:               mediumModel,
				MediumModel:            mediumModel,
				HighModel:              "strong-model",
				MediumProbability: &med,
				HighProbability:   &high,
			},
		},

		Trace: config.TraceConfig{
			MaxRecords:  10,
			MaxBodySize: 1024 * 1024,
			Directory:   traceDirectory,
		},
	}
}

type stringReader struct {
	value string
	index int
}

func newStringReader(
	value string,
) *stringReader {
	return &stringReader{
		value: value,
	}
}

func (r *stringReader) Read(
	buffer []byte,
) (int, error) {
	if r.index >= len(r.value) {
		return 0, io.EOF
	}

	count := copy(
		buffer,
		r.value[r.index:],
	)

	r.index += count

	return count, nil
}
