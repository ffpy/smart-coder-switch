package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRootRedirect(t *testing.T) {
	// Build a mux the same way as run() to verify
	// that GET / redirects to /web/
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/web/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if location != "/web/" {
		t.Fatalf("expected redirect to /web/, got %q", location)
	}
}

func TestParseOptionsDefault(t *testing.T) {
	options, positional, err := parseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}

	if options.ConfigPath != "config.yaml" {
		t.Fatalf(
			"expected config.yaml, got %q",
			options.ConfigPath,
		)
	}

	if len(positional) != 0 {
		t.Fatalf(
			"expected no positional args, got %v",
			positional,
		)
	}
}

func TestParseOptionsExplicitConfig(t *testing.T) {
	options, _, err := parseOptions(
		[]string{
			"-config",
			"/etc/smart-coder-switch/config.yaml",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if options.ConfigPath !=
		"/etc/smart-coder-switch/config.yaml" {
		t.Fatalf(
			"unexpected config path %q",
			options.ConfigPath,
		)
	}
}

func TestParseOptionsEqualsSyntax(t *testing.T) {
	options, _, err := parseOptions(
		[]string{
			"-config=custom.yaml",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if options.ConfigPath != "custom.yaml" {
		t.Fatalf(
			"expected custom.yaml, got %q",
			options.ConfigPath,
		)
	}
}

func TestParseOptionsRejectsEmptyConfig(t *testing.T) {
	_, _, err := parseOptions(
		[]string{
			"-config=",
		},
	)

	if err == nil {
		t.Fatal(
			"expected empty config path error",
		)
	}
}

func TestParseOptionsPreservesPositionalArgs(t *testing.T) {
	options, positional, err := parseOptions(
		[]string{
			"-config",
			"test.yaml",
			"reload",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if options.ConfigPath != "test.yaml" {
		t.Fatalf(
			"expected test.yaml, got %q",
			options.ConfigPath,
		)
	}

	if len(positional) != 1 ||
		positional[0] != "reload" {
		t.Fatalf(
			"expected [reload], got %v",
			positional,
		)
	}
}

func TestParseOptionsVersion(t *testing.T) {
	options, _, err := parseOptions(
		[]string{
			"-version",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !options.ShowVersion {
		t.Fatal(
			"expected ShowVersion=true",
		)
	}

	if options.ConfigPath != "config.yaml" {
		t.Fatalf(
			"expected default config path, got %q",
			options.ConfigPath,
		)
	}
}

func TestParseOptionsVersionWithConfig(t *testing.T) {
	options, _, err := parseOptions(
		[]string{
			"-version",
			"-config",
			"custom.yaml",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !options.ShowVersion {
		t.Fatal(
			"expected ShowVersion=true",
		)
	}

	if options.ConfigPath != "custom.yaml" {
		t.Fatalf(
			"expected custom.yaml, got %q",
			options.ConfigPath,
		)
	}
}

func TestDispatchSubcommandUnknown(t *testing.T) {
	code := dispatchSubcommand(
		[]string{"unknown"},
		"config.yaml",
	)

	if code != 2 {
		t.Fatalf(
			"expected exit code 2, got %d",
			code,
		)
	}
}

func TestDispatchSubcommandStatsReset(
	t *testing.T,
) {
	// With a non-existent config file,
	// stats reset should fail to load config.
	code := dispatchSubcommand(
		[]string{"stats", "reset"},
		"/nonexistent/config.yaml",
	)

	// Should exit non-zero because config load fails.
	if code == 0 {
		t.Fatal(
			"expected non-zero exit for missing config",
		)
	}
}

func TestDispatchSubcommandStatsNoArgs(
	t *testing.T,
) {
	code := dispatchSubcommand(
		[]string{"stats"},
		"/nonexistent/config.yaml",
	)

	if code == 0 {
		t.Fatal(
			"expected non-zero exit for missing config",
		)
	}
}

func TestDispatchSubcommandStatsInvalidSubArg(
	t *testing.T,
) {
	code := dispatchSubcommand(
		[]string{"stats", "unknown"},
		"config.yaml",
	)

	if code != 2 {
		t.Fatalf(
			"expected exit code 2 for invalid stats sub-arg, got %d",
			code,
		)
	}
}

func TestDispatchSubcommandReload(
	t *testing.T,
) {
	code := dispatchSubcommand(
		[]string{"reload"},
		"/nonexistent/config.yaml",
	)

	if code == 0 {
		t.Fatal(
			"expected non-zero exit for missing config",
		)
	}
}

func TestDispatchSubcommandVersion(
	t *testing.T,
) {
	code := dispatchSubcommand(
		[]string{"version"},
		"/nonexistent/config.yaml",
	)

	if code == 0 {
		t.Fatal(
			"expected non-zero exit for missing config",
		)
	}
}
