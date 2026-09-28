package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rizwanreza/cadence-cli/internal/update"
)

// recorded is one request the fake server saw.
type recorded struct {
	Method string
	Path   string
	Query  string
	Body   string
	Header http.Header
}

// fake is an httptest Cadence API. Routes are "METHOD /path" (path without
// query); unmatched requests fail the test and return 404.
type fake struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	routes   map[string]http.HandlerFunc
	requests []recorded
	fallback func(*http.Request) string
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{t: t, routes: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.mu.Lock()
		f.requests = append(f.requests, recorded{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body), Header: r.Header.Clone()})
		handler, ok := f.routes[r.Method+" "+r.URL.Path]
		fallback := f.fallback
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case ok:
			handler(w, r)
		case fallback != nil:
			_, _ = w.Write([]byte(fallback(r)))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Not found","details":[]},"errors":["Not found"]}`))
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// serve answers every request with 200 and handler's body (the old mockClient).
func serve(t *testing.T, handler func(*http.Request) string) *fake {
	f := newFake(t)
	f.fallback = handler
	return f
}

func (f *fake) on(method, path string, status int, body string) *fake {
	f.routes[method+" "+path] = func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
	return f
}

func (f *fake) handle(method, path string, h http.HandlerFunc) *fake {
	f.routes[method+" "+path] = h
	return f
}

func (f *fake) calls() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.requests...)
}

func (f *fake) last(method, path string) recorded {
	f.t.Helper()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Method == method && f.requests[i].Path == path {
			return f.requests[i]
		}
	}
	f.t.Fatalf("no %s %s request; saw %v", method, path, f.requests)
	return recorded{}
}

type result struct {
	stdout, stderr string
	code           int
	app            *App
}

// testEnv isolates HOME/config and disables update checks.
func testEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Setenv("CADENCE_URL", "")
	t.Setenv("CADENCE_TOKEN", "")
	t.Setenv("CADENCE_TIMEOUT", "")
	t.Setenv("CI", "1")
	return home
}

func newTestApp(f *fake, stdin string) (*App, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	releases := "http://127.0.0.1:1/unused"
	if f != nil {
		releases = f.srv.URL + "/releases/latest"
	}
	app := &App{
		Out: &out, Err: &errOut, In: strings.NewReader(stdin),
		StdinIsTTY:   func() bool { return false },
		StderrIsTTY:  func() bool { return false },
		OpenBrowser:  func(string) error { return errors.New("no browser in tests") },
		ReadSecret:   func() (string, error) { return "", errors.New("no tty") },
		Updates:      &update.Checker{URL: releases, HTTP: &http.Client{Timeout: 2 * time.Second}, Now: time.Now},
		Executable:   func() (string, error) { return "/opt/cadence/bin/cadence", nil },
		RunInstaller: func(string) error { return errors.New("installer not stubbed") },
	}
	return app, &out, &errOut
}

// run executes the CLI against f with a valid token (unless the test set one).
func run(t *testing.T, f *fake, args ...string) result {
	t.Helper()
	return runWith(t, f, "", args...)
}

func runWith(t *testing.T, f *fake, stdin string, args ...string) result {
	t.Helper()
	if f != nil {
		t.Setenv("CADENCE_URL", f.srv.URL)
	}
	if os.Getenv("CADENCE_TOKEN") == "" && os.Getenv("CADENCE_TEST_NO_TOKEN") == "" {
		t.Setenv("CADENCE_TOKEN", "test-token")
	}
	app, out, errOut := newTestApp(f, stdin)
	code := app.Run(args)
	return result{stdout: out.String(), stderr: errOut.String(), code: code, app: app}
}

func decodeJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, s)
	}
	return v
}

func mustContain(t *testing.T, s string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(s, p) {
			t.Fatalf("missing %q in:\n%s", p, s)
		}
	}
}

const meActive = `{
  "user": {"email": "demo@example.com", "name": "Demo", "time_zone": "America/Chicago"},
  "today": "2026-09-28", "now": "2026-09-28T08:15:00-05:00",
  "active_cycle": {"id": 42, "name": "Fall 2026", "status": "active", "start_date": "2026-09-14", "end_date": "2026-12-06",
    "total_weeks": 12, "current_week_number": 3, "awaiting_review": false, "final_stretch": false, "lapsed": false,
    "restartable": true, "restart_blocker": null, "successor_id": null, "ended_early": false, "notes": null},
  "closing_cycle": null, "successor_cycle": null, "fresh_start_offer": false,
  "token": {"prefix": "cad_Ab3dEf7h", "last_used_at": "2026-09-28T07:00:00-05:00"},
  "cli": {"min_version": "1.0.0"}
}`
