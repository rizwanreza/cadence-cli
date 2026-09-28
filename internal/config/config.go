// Package config reads and writes ~/.config/cadence/config.json and resolves
// the host, token and request timeout from flags, environment and that file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultHost is where the CLI points when nothing else is configured.
const DefaultHost = "https://cadenceweek.com"

// DefaultTimeout applies to every API request unless CADENCE_TIMEOUT is set.
const DefaultTimeout = 10 * time.Second

// Config is persisted to <config dir>/config.json with mode 0600.
type Config struct {
	Host  string `json:"host,omitempty"`
	Token string `json:"token,omitempty"`
	// Email and TimeZone are remembered from the last successful login so
	// `auth status` can say who you are even when offline.
	Email    string `json:"email,omitempty"`
	TimeZone string `json:"time_zone,omitempty"`
}

// Dir is $XDG_CONFIG_HOME/cadence, else ~/.config/cadence.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cadence"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cadence"), nil
}

// Path is the config file location.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load returns the saved config, or an empty one when none exists yet.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("could not parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config with owner-only permissions.
func Save(cfg Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	// WriteFile keeps the mode of an existing file; tighten it regardless.
	return os.Chmod(path, 0o600)
}

// ResolveHost picks the base URL from, in order: the --url flag, CADENCE_URL,
// the saved config, then the production default.
func ResolveHost(flagURL string, cfg Config) (string, error) {
	return NormalizeBaseURL(FirstNonEmpty(flagURL, os.Getenv("CADENCE_URL"), cfg.Host, DefaultHost))
}

// TokenSource names where ResolveToken found the token.
type TokenSource string

const (
	TokenFromFlag   TokenSource = "flag"
	TokenFromEnv    TokenSource = "env"
	TokenFromConfig TokenSource = "config"
	TokenNone       TokenSource = "none"
)

// ResolveToken picks the API token from, in order: the --token flag,
// CADENCE_TOKEN, then the saved config.
func ResolveToken(flagToken string, cfg Config) (string, TokenSource) {
	if flagToken != "" {
		return flagToken, TokenFromFlag
	}
	if env := os.Getenv("CADENCE_TOKEN"); env != "" {
		return env, TokenFromEnv
	}
	if cfg.Token != "" {
		return cfg.Token, TokenFromConfig
	}
	return "", TokenNone
}

// Timeout reads CADENCE_TIMEOUT as a Go duration ("30s", "1m") or a plain
// number of seconds ("30"), defaulting to 10s.
func Timeout() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv("CADENCE_TIMEOUT"))
	if raw == "" {
		return DefaultTimeout, nil
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		if seconds <= 0 {
			return 0, fmt.Errorf("CADENCE_TIMEOUT must be positive, got %q", raw)
		}
		return time.Duration(seconds * float64(time.Second)), nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("CADENCE_TIMEOUT must be a duration like 30s or a number of seconds, got %q", raw)
	}
	return d, nil
}

// NormalizeBaseURL validates a base URL and strips any trailing slash.
func NormalizeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("base URL must include scheme and host, got %q", raw)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// TokenPrefix returns the non-secret leading part of a token for display
// (the server stores the same 12 characters).
func TokenPrefix(token string) string {
	if len(token) <= 12 {
		if len(token) <= 4 {
			return strings.Repeat("*", len(token))
		}
		return token[:4] + "…"
	}
	return token[:12] + "…"
}

// FirstNonEmpty returns the first non-empty string.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
