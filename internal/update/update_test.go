package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"1.1.0", "1.0.0", true},
		{"v1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.0-rc.1", true},
		{"0.9.0", "1.0.0", false},
		{"garbage", "1.0.0", false},
		{"1.0.0", "dev", false},
	} {
		if got := Newer(tc.latest, tc.current); got != tc.want {
			t.Fatalf("Newer(%q, %q) = %v", tc.latest, tc.current, got)
		}
	}
}

func TestFetchCachesForADay(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer server.Close()
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	c := &Checker{URL: server.URL, HTTP: server.Client(), Now: func() time.Time { return now }}
	if _, fresh := c.Cached(); fresh {
		t.Fatal("empty cache should not be fresh")
	}
	latest, err := c.Fetch(context.Background())
	if err != nil || latest != "1.2.3" {
		t.Fatalf("Fetch = %q, %v", latest, err)
	}
	if got, fresh := c.Cached(); !fresh || got != "1.2.3" {
		t.Fatalf("Cached = %q, %v", got, fresh)
	}
	now = now.Add(25 * time.Hour)
	if _, fresh := c.Cached(); fresh {
		t.Fatal("cache should expire after a day")
	}
	if hits != 1 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestDisabledInCI(t *testing.T) {
	for _, name := range []string{"CI", "CADENCE_NO_UPDATE_CHECK", "GITHUB_ACTIONS", "BUILDKITE", "CIRCLECI", "GITLAB_CI", "JENKINS_URL", "TF_BUILD"} {
		t.Setenv(name, "")
	}
	if Disabled() {
		t.Fatal("should be enabled with no CI env")
	}
	t.Setenv("CI", "true")
	if !Disabled() {
		t.Fatal("should be disabled in CI")
	}
}
