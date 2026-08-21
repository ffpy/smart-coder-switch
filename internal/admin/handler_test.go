package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/stats"
)

func TestConfigVersion(
	t *testing.T,
) {
	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, nil)

	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/config/version",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			response.Code,
		)
	}

	var body versionResponse

	if err := json.NewDecoder(
		response.Body,
	).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body.Version != 1 {
		t.Fatalf(
			"expected version 1, got %d",
			body.Version,
		)
	}
}

func TestConfigReload(
	t *testing.T,
) {
	next := &config.Config{}

	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return next, nil
		},
	)

	handler := NewHandler(manager, nil)

	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/config/reload",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d body=%s",
			response.Code,
			response.Body.String(),
		)
	}

	var body reloadResponse

	if err := json.NewDecoder(
		response.Body,
	).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if !body.Reloaded {
		t.Fatal("expected reloaded=true")
	}

	if body.Version != 2 {
		t.Fatalf(
			"expected version 2, got %d",
			body.Version,
		)
	}

	if manager.Snapshot().Config != next {
		t.Fatal("expected new config")
	}
}

func TestConfigReloadFailureKeepsVersion(
	t *testing.T,
) {
	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return nil, errors.New("invalid config")
		},
	)

	handler := NewHandler(manager, nil)

	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/config/reload",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code !=
		http.StatusInternalServerError {

		t.Fatalf(
			"expected status 500, got %d",
			response.Code,
		)
	}

	var body reloadResponse

	if err := json.NewDecoder(
		response.Body,
	).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body.Reloaded {
		t.Fatal("expected reloaded=false")
	}

	if body.Version != 1 {
		t.Fatalf(
			"expected version 1, got %d",
			body.Version,
		)
	}

	if manager.Snapshot().Version != 1 {
		t.Fatal(
			"expected manager version unchanged",
		)
	}
}

func TestConfigAdminMethodNotAllowed(
	t *testing.T,
) {
	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, nil)

	tests := []struct {
		method string
		path   string
	}{
		{
			method: http.MethodPost,
			path:   "/admin/config/version",
		},
		{
			method: http.MethodGet,
			path:   "/admin/config/reload",
		},
	}

	for _, test := range tests {
		request := httptest.NewRequest(
			test.method,
			test.path,
			nil,
		)

		response := httptest.NewRecorder()

		handler.ServeHTTP(
			response,
			request,
		)

		if response.Code !=
			http.StatusMethodNotAllowed {

			t.Fatalf(
				"%s %s expected 405, got %d",
				test.method,
				test.path,
				response.Code,
			)
		}
	}
}

func newTestManager(
	t *testing.T,
	loader config.RuntimeConfigLoader,
) *config.Manager {
	t.Helper()

	manager, err := config.NewManager(
		"config.yaml",
		&config.Config{},
		loader,
	)
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

func TestStatsModelsEmpty(t *testing.T) {
	counter := stats.NewCounter()
	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, counter)

	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/stats/models",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			response.Code,
		)
	}

	var body stats.Snapshot

	if err := json.NewDecoder(
		response.Body,
	).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body.Total != 0 {
		t.Fatalf(
			"expected total 0, got %d",
			body.Total,
		)
	}

	if body.Success != 0 {
		t.Fatalf(
			"expected success 0, got %d",
			body.Success,
		)
	}

	if body.Failure != 0 {
		t.Fatalf(
			"expected failure 0, got %d",
			body.Failure,
		)
	}

	if len(body.Models) != 0 {
		t.Fatalf(
			"expected 0 models, got %d",
			len(body.Models),
		)
	}

	if len(body.LogicalModels) != 0 {
		t.Fatalf(
			"expected 0 logical models, got %d",
			len(body.LogicalModels),
		)
	}
}

func TestStatsModelsWithData(t *testing.T) {
	counter := stats.NewCounter()
	counter.Record("model-a", "model-a", true)
	counter.Record("model-a", "model-a", false)
	counter.Record("model-b", "model-b", true)

	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, counter)

	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/stats/models",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			response.Code,
		)
	}

	var body stats.Snapshot

	if err := json.NewDecoder(
		response.Body,
	).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body.Total != 3 {
		t.Fatalf(
			"expected total 3, got %d",
			body.Total,
		)
	}

	if body.Success != 2 {
		t.Fatalf(
			"expected success 2, got %d",
			body.Success,
		)
	}

	if body.Failure != 1 {
		t.Fatalf(
			"expected failure 1, got %d",
			body.Failure,
		)
	}

	if len(body.Models) != 2 {
		t.Fatalf(
			"expected 2 models, got %d",
			len(body.Models),
		)
	}

	if body.Models[0].Model != "model-a" {
		t.Fatalf(
			"expected first model model-a, got %s",
			body.Models[0].Model,
		)
	}

	if body.Models[0].Total != 2 {
		t.Fatalf(
			"expected model-a total 2, got %d",
			body.Models[0].Total,
		)
	}

	if body.Models[1].Model != "model-b" {
		t.Fatalf(
			"expected second model model-b, got %s",
			body.Models[1].Model,
		)
	}

	if len(body.LogicalModels) != 2 {
		t.Fatalf(
			"expected 2 logical models, got %d",
			len(body.LogicalModels),
		)
	}

	if body.LogicalModels[0].Model != "model-a" {
		t.Fatalf(
			"expected first logical model model-a, got %s",
			body.LogicalModels[0].Model,
		)
	}

	if body.LogicalModels[0].Total != 2 {
		t.Fatalf(
			"expected logical model-a total 2, got %d",
			body.LogicalModels[0].Total,
		)
	}
}

func TestStatsModelsMethodNotAllowed(t *testing.T) {
	counter := stats.NewCounter()
	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, counter)

	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/stats/models",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf(
			"expected status 405, got %d",
			response.Code,
		)
	}
}

func TestStatsReset(t *testing.T) {
	counter := stats.NewCounter()
	counter.Record("model-a", "model-a", true)
	counter.Record("model-b", "model-b", false)

	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, counter)

	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/stats/models/reset",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			response.Code,
		)
	}

	var resetBody statsResetResponse

	if err := json.NewDecoder(
		response.Body,
	).Decode(&resetBody); err != nil {
		t.Fatal(err)
	}

	if !resetBody.Reset {
		t.Fatal("expected reset=true")
	}

	snap := counter.Snapshot()

	if snap.Total != 0 {
		t.Fatalf(
			"expected total 0 after reset, got %d",
			snap.Total,
		)
	}
}

func TestStatsResetMethodNotAllowed(t *testing.T) {
	counter := stats.NewCounter()
	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, counter)

	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/stats/models/reset",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf(
			"expected status 405, got %d",
			response.Code,
		)
	}
}

func TestStatsNilCounter(t *testing.T) {
	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, nil)

	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/stats/models",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			response.Code,
		)
	}

	var body stats.Snapshot

	if err := json.NewDecoder(
		response.Body,
	).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body.Total != 0 {
		t.Fatalf(
			"expected total 0, got %d",
			body.Total,
		)
	}
}

func TestStatsResetNilCounter(t *testing.T) {
	manager := newTestManager(
		t,
		func(string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	)

	handler := NewHandler(manager, nil)

	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/stats/models/reset",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			response.Code,
		)
	}

	var resetBody statsResetResponse

	if err := json.NewDecoder(
		response.Body,
	).Decode(&resetBody); err != nil {
		t.Fatal(err)
	}

	if !resetBody.Reset {
		t.Fatal("expected reset=true")
	}
}
