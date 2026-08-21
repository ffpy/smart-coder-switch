package admin

import (
	"encoding/json"
	"net/http"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/stats"
	"gopkg.in/yaml.v3"
)

type Handler struct {
	manager *config.Manager
	counter *stats.Counter
}

type versionResponse struct {
	Version uint64 `json:"version"`
}

type reloadResponse struct {
	Version  uint64 `json:"version"`
	Reloaded bool   `json:"reloaded"`
	Error    string `json:"error,omitempty"`
}

type statsResetResponse struct {
	Reset bool `json:"reset"`
}

func NewHandler(
	manager *config.Manager,
	counter *stats.Counter,
) *Handler {
	return &Handler{
		manager: manager,
		counter: counter,
	}
}

func (h *Handler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	switch r.URL.Path {
	case "/admin/config":
		h.handleConfig(w, r)

	case "/admin/config/version":
		h.handleVersion(w, r)

	case "/admin/config/reload":
		h.handleReload(w, r)

	case "/admin/stats/models":
		h.handleStatsModels(w, r)

	case "/admin/stats/models/reset":
		h.handleStatsReset(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleVersion(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	snapshot := h.manager.Snapshot()

	writeJSON(
		w,
		http.StatusOK,
		versionResponse{
			Version: snapshot.Version,
		},
	)
}

func (h *Handler) handleConfig(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	snapshot := h.manager.Snapshot()

	raw, err := yaml.Marshal(snapshot.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(raw)
}

func (h *Handler) handleReload(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.WriteHeader(
			http.StatusMethodNotAllowed,
		)

		return
	}

	snapshot, err := h.manager.Reload()
	if err != nil {
		writeJSON(
			w,
			http.StatusInternalServerError,
			reloadResponse{
				Version:  snapshot.Version,
				Reloaded: false,
				Error:    err.Error(),
			},
		)

		return
	}

	writeJSON(
		w,
		http.StatusOK,
		reloadResponse{
			Version:  snapshot.Version,
			Reloaded: true,
		},
	)
}

func (h *Handler) handleStatsModels(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		w.WriteHeader(
			http.StatusMethodNotAllowed,
		)

		return
	}

	var snap stats.Snapshot

	if h.counter != nil {
		snap = h.counter.Snapshot()
	}

	writeJSON(
		w,
		http.StatusOK,
		snap,
	)
}

func (h *Handler) handleStatsReset(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.WriteHeader(
			http.StatusMethodNotAllowed,
		)

		return
	}

	if h.counter != nil {
		h.counter.Reset()
	}

	writeJSON(
		w,
		http.StatusOK,
		statsResetResponse{
			Reset: true,
		},
	)
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}
