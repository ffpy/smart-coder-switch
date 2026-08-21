package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"smart-coder-switch/internal/admin"
	"smart-coder-switch/internal/buildinfo"
	"smart-coder-switch/internal/config"
	"smart-coder-switch/internal/proxy"
	"smart-coder-switch/internal/stats"
	"smart-coder-switch/internal/web"

	_ "modernc.org/sqlite"
)

const (
	defaultConfigPath = "config.yaml"
	shutdownTimeout   = 10 * time.Second
)

type commandOptions struct {
	ConfigPath  string
	ShowVersion bool
}

func main() {
	options, positional, err := parseOptions(
		os.Args[1:],
	)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printHelp()

			return
		}

		slog.Error(
			"parse command arguments failed",
			"error",
			err,
		)
		os.Exit(2)
	}

	if options.ShowVersion {
		fmt.Println(buildinfo.String())
		return
	}

	if len(positional) > 0 {
		os.Exit(dispatchSubcommand(
			positional,
			options.ConfigPath,
		))
	}

	if err := run(
		options.ConfigPath,
	); err != nil {
		slog.Error(
			"smart coder switch stopped with error",
			"error",
			err,
		)
		os.Exit(1)
	}
}

func dispatchSubcommand(
	positional []string,
	configPath string,
) int {
	switch positional[0] {
	case "reload":
		return runReload(configPath)

	case "stats":
		return runStats(
			positional[1:],
			configPath,
		)

	case "version":
		return runConfVersion(configPath)

	case "config":
		return runConfig(configPath)

	default:
		fmt.Fprintf(
			os.Stderr,
			"error: unknown subcommand: %s\n",
			positional[0],
		)

		return 2
	}
}

func parseOptions(
	args []string,
) (commandOptions, []string, error) {
	flags := flag.NewFlagSet(
		"smart-coder-switch",
		flag.ContinueOnError,
	)

	flags.SetOutput(io.Discard)

	configPath := flags.String(
		"config",
		defaultConfigPath,
		"path to config file",
	)

	showVersion := flags.Bool(
		"version",
		false,
		"print version information",
	)

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return commandOptions{}, nil, err
		}

		return commandOptions{}, nil, err
	}

	if *configPath == "" {
		return commandOptions{}, nil, errors.New(
			"config path must not be empty",
		)
	}

	return commandOptions{
		ConfigPath:  *configPath,
		ShowVersion: *showVersion,
	}, flags.Args(), nil
}

func printHelp() {
	fmt.Println(`Usage: smart-coder-switch [options] [subcommand]

Options:
  -config <path>   config file path (default: config.yaml)
  -version         print build version
  -help            print this help

Subcommands:
  reload           hot-reload configuration
  config           show current configuration
  stats            show model call statistics
  stats reset      reset model call statistics
  version          show config version

Without a subcommand, starts the HTTP proxy server.`)
}

func run(
	configPath string,
) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf(
			"load config: %w",
			err,
		)
	}

	if err := config.InitLogger(cfg); err != nil {
		return fmt.Errorf(
			"init logger: %w",
			err,
		)
	}

	slog.Info(
		"logger initialized",
		"file", cfg.Log.File,
		"level", cfg.Log.Level,
	)

	configManager, err := config.NewManager(
		configPath,
		cfg,
		config.Load,
	)
	if err != nil {
		return fmt.Errorf(
			"create config manager: %w",
			err,
		)
	}

	counter := stats.NewCounter()

	// Initialize SQLite database for decision logs
	dbPath := cfg.SQLite.Path
	if dbPath == "" {
		dbPath = "./data/decisions.db"
	}

	// Ensure the data directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer db.Close()

	maxRecords := cfg.SQLite.MaxRecords
	if maxRecords <= 0 {
		maxRecords = 1000
	}

	store, err := web.NewStore(db, maxRecords)
	if err != nil {
		return fmt.Errorf("create decision store: %w", err)
	}

	// Create decision logger callback for proxy handler
	decisionLogger := func(log proxy.DecisionLog) {
		record := web.DecisionRecord{
			RequestID:      log.RequestID,
			Timestamp:      time.Now(),
			LogicalModel:   log.LogicalModel,
			SelectedTier:   log.SelectedTier,
			SelectedModel:  log.SelectedModel,
			AssistantCount: log.AssistantCount,
			Reason:         log.Reason,
			TraceDir:       log.TraceDir,
			RequestTimeMs:  0, // Not tracked yet
		}
		if err := store.Insert(context.Background(), record); err != nil {
			slog.Error("insert decision log", "error", err)
		}
	}

	// Create result logger callback: 上游转发完成后回写状态码与错误摘要
	resultLogger := func(result proxy.DecisionResult) {
		if err := store.UpdateResult(
			context.Background(),
			result.RequestID,
			result.StatusCode,
			result.ErrorMessage,
		); err != nil {
			slog.Error("update decision result", "error", err)
		}
	}

	proxyHandler, err :=
		proxy.NewManagedHandler(
			configManager,
			counter,
			decisionLogger,
			resultLogger,
		)

	if err != nil {
		return fmt.Errorf(
			"create managed proxy handler: %w",
			err,
		)
	}

	adminHandler := admin.NewHandler(
		configManager,
		counter,
	)

	webHandler := web.NewHandler(
		store,
		configManager,
	)

	mux := http.NewServeMux()

	// Root redirect to /web/
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/web/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Frontend SPA (must be registered first for /web/* prefix matching)
	mux.Handle("/web/", web.FrontendHandler())

	mux.HandleFunc(
		"/health",
		handleHealth,
	)

	mux.Handle(
		"/admin/config/form",
		webHandler,
	)

	mux.Handle(
		"/admin/decisions",
		webHandler,
	)

	mux.Handle(
		"/admin/decisions/distribution",
		webHandler,
	)

	mux.Handle(
		"/admin/traces/",
		webHandler,
	)

	mux.Handle(
		"/admin/stats/models",
		webHandler,
	)

	mux.Handle(
		"/admin/stats/models/reset",
		webHandler,
	)

	mux.Handle(
		"/admin/",
		adminHandler,
	)

	mux.Handle(
		"/v1/chat/completions",
		proxyHandler,
	)

	mux.Handle(
		"/v1/responses",
		proxyHandler,
	)

	server := &http.Server{
		Addr:              cfg.Listen.Address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serverError := make(
		chan error,
		1,
	)

	go func() {
		slog.Info(
			"smart coder switch started",
			"address",
			cfg.Listen.Address,
			"config_path",
			configPath,
			"config_version",
			configManager.Snapshot().Version,
			"version",
			buildinfo.Version,
			"commit",
			buildinfo.Commit,
		)

		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if errors.Is(
			err,
			http.ErrServerClosed,
		) {
			return nil
		}

		return fmt.Errorf(
			"serve HTTP: %w",
			err,
		)

	case <-ctx.Done():
		slog.Info(
			"shutdown requested",
		)

		shutdownContext, cancel :=
			context.WithTimeout(
				context.Background(),
				shutdownTimeout,
			)

		defer cancel()

		if err := server.Shutdown(
			shutdownContext,
		); err != nil {
			_ = server.Close()

			return fmt.Errorf(
				"shutdown HTTP server: %w",
				err,
			)
		}

		err := <-serverError

		if err != nil &&
			!errors.Is(
				err,
				http.ErrServerClosed,
			) {
			return fmt.Errorf(
				"serve HTTP during shutdown: %w",
				err,
			)
		}

		slog.Info(
			"smart coder switch stopped",
		)

		return nil
	}
}

func handleHealth(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		w.WriteHeader(
			http.StatusMethodNotAllowed,
		)

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	_ = json.NewEncoder(w).Encode(
		map[string]string{
			"status":  "ok",
			"version": buildinfo.Version,
			"commit":  buildinfo.Commit,
		},
	)
}
