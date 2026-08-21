package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/stats"
	"smart-coder-switch/internal/trace"
)

var ErrNilConfigManager = errors.New(
	"config manager must not be nil",
)

type handlerFactory func(
	cfg *config.Config,
) (http.Handler, error)

type managedDelegate struct {
	version uint64
	handler http.Handler
}

type preparedDelegate struct {
	cfg     *config.Config
	handler http.Handler
}

type ManagedHandler struct {
	manager       *config.Manager
	factory       handlerFactory
	listenAddress string

	refreshMu sync.Mutex
	current   atomic.Pointer[managedDelegate]

	preparedMu sync.Mutex
	prepared   *preparedDelegate
}

func NewManagedHandler(
	manager *config.Manager,
	counter *stats.Counter,
	decisionLogger DecisionLogFunc,
	resultLoggers ...DecisionResultFunc,
) (*ManagedHandler, error) {
	if manager == nil {
		return nil, ErrNilConfigManager
	}

	var resultLogger DecisionResultFunc
	if len(resultLoggers) > 0 {
		resultLogger = resultLoggers[0]
	}

	factory := func(
		cfg *config.Config,
	) (http.Handler, error) {
		return buildProxyHandler(cfg, counter, decisionLogger, resultLogger)
	}

	handler, err := newManagedHandler(
		manager,
		factory,
	)
	if err != nil {
		return nil, err
	}

	if err := manager.SetValidator(
		handler.prepareRuntimeConfig,
	); err != nil {
		return nil, fmt.Errorf(
			"set runtime config validator: %w",
			err,
		)
	}

	return handler, nil
}

func buildProxyHandler(
	cfg *config.Config,
	counter *stats.Counter,
	decisionLogger DecisionLogFunc,
	resultLogger DecisionResultFunc,
) (http.Handler, error) {
	if err := config.ValidateRuntimeConfig(
		cfg,
	); err != nil {
		return nil, fmt.Errorf(
			"validate config: %w",
			err,
		)
	}

	upstream, err := NewUpstream(
		cfg.Upstream.BaseURL,
		time.Duration(cfg.Upstream.Timeout)*time.Second,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create upstream: %w",
			err,
		)
	}

	recorder, err := trace.NewRecorder(
		cfg.Trace,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create trace recorder: %w",
			err,
		)
	}

	handler := NewHandler(
		cfg,
		upstream,
		recorder,
		counter,
		decisionLogger,
		resultLogger,
	)

	return http.HandlerFunc(
		handler.routeByPath,
	), nil
}

func newManagedHandler(
	manager *config.Manager,
	factory handlerFactory,
) (*ManagedHandler, error) {
	if manager == nil {
		return nil, ErrNilConfigManager
	}

	if factory == nil {
		return nil, errors.New(
			"handler factory must not be nil",
		)
	}

	snapshot := manager.Snapshot()

	if snapshot == nil ||
		snapshot.Config == nil {
		return nil, errors.New(
			"config snapshot must not be nil",
		)
	}

	handler := &ManagedHandler{
		manager:       manager,
		factory:       factory,
		listenAddress: snapshot.Config.Listen.Address,
	}

	delegate, err := handler.buildDelegate(
		snapshot,
	)
	if err != nil {
		return nil, err
	}

	handler.current.Store(delegate)

	return handler, nil
}

func (h *ManagedHandler) prepareRuntimeConfig(
	cfg *config.Config,
) error {
	if cfg == nil {
		return config.ErrNilRuntimeConfig
	}

	if cfg.Listen.Address != h.listenAddress {
		return fmt.Errorf(
			"listen.address cannot be changed by reload: running=%q candidate=%q",
			h.listenAddress,
			cfg.Listen.Address,
		)
	}

	handler, err := h.factory(cfg)
	if err != nil {
		return err
	}

	if handler == nil {
		return errors.New(
			"prepared proxy handler must not be nil",
		)
	}

	h.preparedMu.Lock()
	h.prepared = &preparedDelegate{
		cfg:     cfg,
		handler: handler,
	}
	h.preparedMu.Unlock()

	return nil
}

func (h *ManagedHandler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	snapshot := h.manager.Snapshot()

	if snapshot == nil ||
		snapshot.Config == nil {
		http.Error(
			w,
			"config snapshot unavailable",
			http.StatusInternalServerError,
		)

		return
	}

	delegate, err := h.delegateFor(snapshot)
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)

		return
	}

	delegate.handler.ServeHTTP(w, r)
}

func (h *ManagedHandler) delegateFor(
	snapshot *config.RuntimeConfig,
) (*managedDelegate, error) {
	current := h.current.Load()

	if current != nil &&
		current.version == snapshot.Version {
		return current, nil
	}

	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()

	current = h.current.Load()

	if current != nil &&
		current.version == snapshot.Version {
		return current, nil
	}

	next, err := h.buildDelegate(snapshot)
	if err != nil {
		return nil, err
	}

	if current == nil ||
		snapshot.Version >= current.version {
		h.current.Store(next)
	}

	return next, nil
}

func (h *ManagedHandler) buildDelegate(
	snapshot *config.RuntimeConfig,
) (*managedDelegate, error) {
	handler, prepared :=
		h.takePreparedHandler(snapshot.Config)

	if !prepared {
		var err error

		handler, err = h.factory(
			snapshot.Config,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"build proxy handler for config version %d: %w",
				snapshot.Version,
				err,
			)
		}
	}

	if handler == nil {
		return nil, fmt.Errorf(
			"build proxy handler for config version %d: nil handler",
			snapshot.Version,
		)
	}

	return &managedDelegate{
		version: snapshot.Version,
		handler: handler,
	}, nil
}

func (h *ManagedHandler) takePreparedHandler(
	cfg *config.Config,
) (http.Handler, bool) {
	h.preparedMu.Lock()
	defer h.preparedMu.Unlock()

	if h.prepared == nil ||
		h.prepared.cfg != cfg {
		return nil, false
	}

	handler := h.prepared.handler
	h.prepared = nil

	return handler, true
}
