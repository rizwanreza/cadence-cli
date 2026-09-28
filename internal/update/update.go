// Package update finds the latest cadence-cli release on GitHub (cached for a
// day) and compares versions.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rizwanreza/cadence-cli/internal/config"
	"golang.org/x/mod/semver"
)

// Repo is the GitHub repository releases come from.
const Repo = "rizwanreza/cadence-cli"

// DefaultURL is the GitHub API endpoint for the latest release.
const DefaultURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// CheckInterval is how often the automatic notice may hit the network.
const CheckInterval = 24 * time.Hour

// Checker looks up the latest release.
type Checker struct {
	URL       string
	HTTP      *http.Client
	Now       func() time.Time
	UserAgent string
}

// NewChecker returns a checker for the real GitHub API. CADENCE_RELEASES_URL
// overrides the endpoint (tests).
func NewChecker() *Checker {
	url := DefaultURL
	if override := os.Getenv("CADENCE_RELEASES_URL"); override != "" {
		url = override
	}
	return &Checker{URL: url, HTTP: &http.Client{Timeout: 3 * time.Second}, Now: time.Now}
}

type cache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest_version"`
}

func cachePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update-check.json"), nil
}

// Cached returns the cached latest version and whether it is still fresh.
func (c *Checker) Cached() (string, bool) {
	path, err := cachePath()
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var entry cache
	if json.Unmarshal(data, &entry) != nil {
		return "", false
	}
	return entry.Latest, c.Now().Sub(entry.CheckedAt) < CheckInterval
}

// Fetch asks GitHub for the latest release and refreshes the cache.
func (c *Checker) Fetch(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %d for the latest release", resp.StatusCode)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	if latest == "" {
		return "", fmt.Errorf("the latest release has no tag")
	}
	c.store(latest)
	return latest, nil
}

func (c *Checker) store(latest string) {
	path, err := cachePath()
	if err != nil {
		return
	}
	data, _ := json.Marshal(cache{CheckedAt: c.Now(), Latest: latest})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// Newer reports whether latest is a higher semver than current.
func Newer(latest, current string) bool {
	l, c := canonical(latest), canonical(current)
	if l == "" || c == "" {
		return false
	}
	return semver.Compare(l, c) > 0
}

// Compare returns -1, 0 or 1 like semver.Compare; invalid versions sort low.
func Compare(a, b string) int {
	return semver.Compare(canonical(a), canonical(b))
}

// Core strips any prerelease/build suffix: 1.0.0-rc.1 → 1.0.0. A release
// candidate of X satisfies a server minimum of X.
func Core(v string) string {
	c := canonical(v)
	if c == "" {
		return v
	}
	return strings.TrimPrefix(semver.Canonical(strings.SplitN(strings.SplitN(c, "-", 2)[0], "+", 2)[0]), "v")
}

// Valid reports whether v parses as semver (with or without "v").
func Valid(v string) bool { return canonical(v) != "" }

func canonical(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	return v
}

// Disabled reports whether automatic update checks are off for this process:
// in CI, or when CADENCE_NO_UPDATE_CHECK is set.
func Disabled() bool {
	for _, name := range []string{"CI", "CADENCE_NO_UPDATE_CHECK", "GITHUB_ACTIONS", "BUILDKITE", "CIRCLECI", "GITLAB_CI", "JENKINS_URL", "TF_BUILD"} {
		if v := os.Getenv(name); v != "" && v != "0" && !strings.EqualFold(v, "false") {
			return true
		}
	}
	return false
}
