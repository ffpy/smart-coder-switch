package proxy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/proxy"
)

type capturedStreamRequest struct {
	Model         string
	Stream        bool
	Authorization string
}

func TestSSEStreamingSurvivesConfigReload(
	t *testing.T,
) {
	var mutex sync.Mutex
	var captured []capturedStreamRequest

	upstreamServer := httptest.NewServer(
		http.HandlerFunc(
			func(
				w http.ResponseWriter,
				r *http.Request,
			) {
				var request struct {
					Model  string `json:"model"`
					Stream bool   `json:"stream"`
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
				captured = append(
					captured,
					capturedStreamRequest{
						Model:         request.Model,
						Stream:        request.Stream,
						Authorization: r.Header.Get("Authorization"),
					},
				)
				mutex.Unlock()

				w.Header().Set(
					"Content-Type",
					"text/event-stream",
				)

				w.Header().Set(
					"Cache-Control",
					"no-cache",
				)

				flusher, ok :=
					w.(http.Flusher)

				if !ok {
					t.Error(
						"upstream response writer does not support flushing",
					)

					return
				}

				writeSSEEvent(
					t,
					w,
					flusher,
					fmt.Sprintf(
						`{"id":"chunk-1","model":%q,"choices":[{"delta":{"content":"hello"}}]}`,
						request.Model,
					),
				)

				writeSSEEvent(
					t,
					w,
					flusher,
					fmt.Sprintf(
						`{"id":"chunk-2","model":%q,"choices":[{"delta":{"content":" world"}}]}`,
						request.Model,
					),
				)

				writeSSEEvent(
					t,
					w,
					flusher,
					"[DONE]",
				)
			},
		),
	)
	defer upstreamServer.Close()

	initialConfig := hotReloadTestConfig(
		upstreamServer.URL+"/",
		"medium-stream-v1",
		t.TempDir(),
	)

	reloadedConfig := hotReloadTestConfig(
		upstreamServer.URL+"/",
		"medium-stream-v2",
		t.TempDir(),
	)

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

	handler, err := proxy.NewManagedHandler(
		manager,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	firstResponse := sendStreamingRequest(
		t,
		handler,
	)

	assertStreamingResponse(
		t,
		firstResponse,
		"medium-stream-v1",
	)

	reloadedSnapshot, err :=
		manager.Reload()

	if err != nil {
		t.Fatal(err)
	}

	if reloadedSnapshot.Version != 2 {
		t.Fatalf(
			"expected version 2, got %d",
			reloadedSnapshot.Version,
		)
	}

	secondResponse := sendStreamingRequest(
		t,
		handler,
	)

	assertStreamingResponse(
		t,
		secondResponse,
		"medium-stream-v2",
	)

	mutex.Lock()
	defer mutex.Unlock()

	if len(captured) != 2 {
		t.Fatalf(
			"expected 2 upstream requests, got %d",
			len(captured),
		)
	}

	expectedModels := []string{
		"medium-stream-v1",
		"medium-stream-v2",
	}

	for index, expectedModel := range expectedModels {
		actual := captured[index]

		if actual.Model != expectedModel {
			t.Fatalf(
				"request %d expected model %q, got %q",
				index,
				expectedModel,
				actual.Model,
			)
		}

		if !actual.Stream {
			t.Fatalf(
				"request %d expected stream=true",
				index,
			)
		}

		if actual.Authorization !=
			"Bearer stream-test-key" {
			t.Fatalf(
				"request %d expected Authorization forwarded, got %q",
				index,
				actual.Authorization,
			)
		}
	}
}

func writeSSEEvent(
	t *testing.T,
	w http.ResponseWriter,
	flusher http.Flusher,
	data string,
) {
	t.Helper()

	if _, err := fmt.Fprintf(
		w,
		"data: %s\n\n",
		data,
	); err != nil {
		t.Errorf(
			"write SSE event: %v",
			err,
		)

		return
	}

	flusher.Flush()
}

func sendStreamingRequest(
	t *testing.T,
	handler http.Handler,
) *httptest.ResponseRecorder {
	t.Helper()

	body := `{
		"model": "coder1",
		"messages": [
			{
				"role": "user",
				"content": "stream a response"
			}
		],
		"stream": true
	}`

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(body),
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	request.Header.Set(
		"Authorization",
		"Bearer stream-test-key",
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	return response
}

func assertStreamingResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
	expectedModel string,
) {
	t.Helper()

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d body=%s",
			response.Code,
			response.Body.String(),
		)
	}

	contentType := response.Header().
		Get("Content-Type")

	if !strings.HasPrefix(
		contentType,
		"text/event-stream",
	) {
		t.Fatalf(
			"expected text/event-stream, got %q",
			contentType,
		)
	}

	if !response.Flushed {
		t.Fatal(
			"expected streaming response to be flushed",
		)
	}

	body := response.Body.String()

	expectedEvents := []string{
		fmt.Sprintf(
			`data: {"id":"chunk-1","model":%q,"choices":[{"delta":{"content":"hello"}}]}`,
			expectedModel,
		),
		fmt.Sprintf(
			`data: {"id":"chunk-2","model":%q,"choices":[{"delta":{"content":" world"}}]}`,
			expectedModel,
		),
		"data: [DONE]",
	}

	previousIndex := -1

	for _, expectedEvent := range expectedEvents {
		index := strings.Index(
			body,
			expectedEvent,
		)

		if index < 0 {
			t.Fatalf(
				"expected SSE event %q in body:\n%s",
				expectedEvent,
				body,
			)
		}

		if index <= previousIndex {
			t.Fatalf(
				"SSE events are out of order:\n%s",
				body,
			)
		}

		previousIndex = index
	}

	if strings.Count(
		body,
		"data:",
	) != 3 {
		t.Fatalf(
			"expected exactly 3 SSE events, got body:\n%s",
			body,
		)
	}
}
