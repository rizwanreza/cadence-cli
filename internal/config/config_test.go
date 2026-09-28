package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveHostPrecedence(t *testing.T) {
	t.Setenv("CADENCE_URL", "")
	if host, _ := ResolveHost("", Config{}); host != DefaultHost {
		t.Fatalf("expected default host %q, got %q", DefaultHost, host)
	}
	if host, _ := ResolveHost("", Config{Host: "https://saved.example.com"}); host != "https://saved.example.com" {
		t.Fatalf("expected saved host, got %q", host)
	}
	t.Setenv("CADENCE_URL", "https://env.example.com")
	if host, _ := ResolveHost("", Config{Host: "https://saved.example.com"}); host != "https://env.example.com" {
		t.Fatalf("expected env host to win over config, got %q", host)
	}
	if host, _ := ResolveHost("http://localhost:3000/", Config{Host: "https://saved.example.com"}); host != "http://localhost:3000" {
		t.Fatalf("expected flag host to win (trailing slash trimmed), got %q", host)
	}
	if _, err := ResolveHost("localhost:3000", Config{}); err == nil {
		t.Fatal("expected a host without scheme to be rejected")
	}
}

func TestResolveTokenPrecedence(t *testing.T) {
	t.Setenv("CADENCE_TOKEN", "")
	if got, src := ResolveToken("", Config{Token: "config-token"}); got != "config-token" || src != TokenFromConfig {
		t.Fatalf("expected config token, got %q (%s)", got, src)
	}
	t.Setenv("CADENCE_TOKEN", "env-token")
	if got, src := ResolveToken("", Config{Token: "config-token"}); got != "env-token" || src != TokenFromEnv {
		t.Fatalf("expected env token to win over config, got %q", got)
	}
	if got, src := ResolveToken("flag-token", Config{Token: "config-token"}); got != "flag-token" || src != TokenFromFlag {
		t.Fatalf("expected flag token to win, got %q", got)
	}
}

func TestSaveWritesOwnerOnlyFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "cadence", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(Config{Token: "cad_x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %v", info.Mode().Perm())
	}
	cfg, err := Load()
	if err != nil || cfg.Token != "cad_x" {
		t.Fatalf("round trip failed: %+v %v", cfg, err)
	}
}

func TestTimeout(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want time.Duration
		ok   bool
	}{
		{"", DefaultTimeout, true},
		{"30", 30 * time.Second, true},
		{"1.5", 1500 * time.Millisecond, true},
		{"2m", 2 * time.Minute, true},
		{"0", 0, false},
		{"soon", 0, false},
	} {
		t.Setenv("CADENCE_TIMEOUT", tc.raw)
		got, err := Timeout()
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Fatalf("Timeout(%q) = %v, %v", tc.raw, got, err)
		}
	}
}

func TestTokenPrefixNeverShowsWholeToken(t *testing.T) {
	if got := TokenPrefix("cad_Ab3dEf7hIjKlMnOpQr"); got != "cad_Ab3dEf7h…" {
		t.Fatalf("got %q", got)
	}
	if got := TokenPrefix("abc"); got != "***" {
		t.Fatalf("got %q", got)
	}
}
