// Package version holds the build metadata stamped in by GoReleaser.
//
// cmd/cadence declares main.version, main.commit and main.date (the ldflags
// targets) and copies them here at startup, so every package reads one source.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"golang.org/x/mod/module"
)

var (
	// Version is the semver of this build without a leading "v" ("dev" for
	// untagged local builds).
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// Date is the build timestamp (RFC 3339).
	Date = "unknown"
)

// Set records the ldflags values from main, falling back to the module
// version that `go install ...@vX.Y.Z` embeds when no ldflags were given.
func Set(version, commit, date string) {
	if version != "" {
		Version = strings.TrimPrefix(version, "v")
	}
	if commit != "" {
		Commit = commit
	}
	if date != "" {
		Date = date
	}
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			// `go install ...@v1.2.3` embeds the tag. Local builds embed a
			// pseudo-version (v0.0.0-2026...-abcdef+dirty): keep "dev" for
			// those, but record the commit.
			if v := info.Main.Version; v != "" && v != "(devel)" && !module.IsPseudoVersion(v) && !strings.Contains(v, "+dirty") {
				Version = strings.TrimPrefix(v, "v")
			}
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" && Commit == "none" {
					Commit = s.Value
				}
				if s.Key == "vcs.time" && Date == "unknown" {
					Date = s.Value
				}
			}
		}
	}
}

// IsDev reports whether this is an unreleased local build.
func IsDev() bool {
	return Version == "dev" || Version == ""
}

// UserAgent is sent on every API request: cadence-cli/<version> (<os>/<arch>).
func UserAgent() string {
	return fmt.Sprintf("cadence-cli/%s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH)
}
