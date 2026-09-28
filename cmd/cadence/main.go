// Command cadence is the Cadence CLI: run your 12-Week Year from the
// terminal, or let your agent do it.
package main

import (
	"os"

	"github.com/rizwanreza/cadence-cli/internal/cli"
	versionpkg "github.com/rizwanreza/cadence-cli/internal/version"
)

// Stamped at release by GoReleaser:
//
//	-X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}}
var (
	version = ""
	commit  = ""
	date    = ""
)

func main() {
	setVersion()
	os.Exit(cli.Main(os.Args[1:]))
}

func setVersion() {
	versionpkg.Set(version, commit, date)
}
