// Package cli implements the cadence command tree.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/rizwanreza/cadence-cli/internal/api"
	"github.com/rizwanreza/cadence-cli/internal/config"
	"github.com/rizwanreza/cadence-cli/internal/update"
	"github.com/rizwanreza/cadence-cli/internal/version"
	"golang.org/x/term"
)

// App holds everything a command needs: streams, global flags, and seams
// that tests replace (HTTP transport, TTY detection, browser, clock).
type App struct {
	Out io.Writer
	Err io.Writer
	In  io.Reader

	// Global flags.
	urlFlag   string
	tokenFlag string
	jsonOut   bool

	// Transport overrides the HTTP transport (tests).
	Transport http.RoundTripper
	// StdinIsTTY / StderrIsTTY report whether a human is attached.
	StdinIsTTY  func() bool
	StderrIsTTY func() bool
	// OpenBrowser opens a URL in the user's browser.
	OpenBrowser func(string) error
	// ReadSecret reads a line without echo (hidden token input).
	ReadSecret func() (string, error)
	// Updates checks GitHub for the latest release.
	Updates *update.Checker
	// Executable returns the path of the running binary.
	Executable func() (string, error)
	// RunInstaller runs the install script (cadence update).
	RunInstaller func(binDir string) error

	cfg       *config.Config
	skipCheck bool
	notice    chan string
}

// NewApp returns an App wired to the real process environment.
func NewApp() *App {
	return &App{
		Out:         os.Stdout,
		Err:         os.Stderr,
		In:          os.Stdin,
		StdinIsTTY:  func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		StderrIsTTY: func() bool { return term.IsTerminal(int(os.Stderr.Fd())) },
		OpenBrowser: openBrowser,
		ReadSecret: func() (string, error) {
			data, err := term.ReadPassword(int(os.Stdin.Fd()))
			return string(data), err
		},
		Updates:      update.NewChecker(),
		Executable:   os.Executable,
		RunInstaller: runInstaller,
	}
}

// Main runs the CLI with process args and returns the exit code.
func Main(args []string) int {
	return NewApp().Run(args)
}

// Run executes args and returns the exit code. Errors are printed once here:
// as a human line, or as a JSON object on stderr when --json is set.
func (a *App) Run(args []string) int {
	root := NewRootCmd(a)
	args = NormalizeArgs(root, args)
	root.SetArgs(args)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	root.SetIn(a.In)

	err := root.Execute()
	code := ExitCode(err)
	if err != nil && !errors.Is(err, errSilent) {
		a.printError(err, code, jsonRequested(args) || a.jsonOut)
	}
	a.finishUpdateNotice()
	return code
}

// jsonRequested catches --json when cobra failed before binding flags.
func jsonRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	return false
}

func (a *App) printError(err error, code int, asJSON bool) {
	message := err.Error()
	if asJSON {
		body := map[string]any{"code": errorCode(err), "message": message, "details": errorDetails(err)}
		var apiErr *api.Error
		if errors.As(err, &apiErr) {
			body["status"] = apiErr.Status
		}
		_ = json.NewEncoder(a.Err).Encode(map[string]any{"error": body, "errors": []string{message}, "exit_code": code})
		return
	}
	fmt.Fprintln(a.Err, "Error: "+message)
	var usage *UsageError
	if errors.As(err, &usage) && usage.Hint != "" {
		fmt.Fprintln(a.Err, usage.Hint)
	}
}

// config returns the saved config, loading it once.
func (a *App) config() (config.Config, error) {
	if a.cfg != nil {
		return *a.cfg, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, err
	}
	a.cfg = &cfg
	return cfg, nil
}

// client builds an authenticated API client.
func (a *App) client() (*api.Client, error) {
	cfg, err := a.config()
	if err != nil {
		return nil, err
	}
	host, err := config.ResolveHost(a.urlFlag, cfg)
	if err != nil {
		return nil, usageErrorf("%v", err)
	}
	token, _ := config.ResolveToken(a.tokenFlag, cfg)
	return a.clientFor(host, token)
}

func (a *App) clientFor(host, token string) (*api.Client, error) {
	timeout, err := config.Timeout()
	if err != nil {
		return nil, usageErrorf("%v", err)
	}
	c := api.New(host, token, version.UserAgent(), timeout)
	if a.Transport != nil {
		c.HTTP.Transport = a.Transport
	}
	return c, nil
}

// authedClient is client() but fails fast (exit 3) when no token is set.
func (a *App) authedClient() (*api.Client, error) {
	c, err := a.client()
	if err != nil {
		return nil, err
	}
	if c.Token == "" {
		return nil, &api.Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "not signed in: run `cadence login` (or set CADENCE_TOKEN)"}
	}
	return c, nil
}

func (a *App) ctx() context.Context { return context.Background() }

// printJSON writes v as indented JSON to stdout.
func (a *App) printJSON(v any) error {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.Out, string(payload))
	return err
}

func (a *App) printf(format string, args ...any) {
	fmt.Fprintf(a.Out, format, args...)
}

func (a *App) println(args ...any) {
	fmt.Fprintln(a.Out, args...)
}

// confirm asks a yes/no question on stderr; non-interactive sessions get def.
func (a *App) confirm(question string, def bool) bool {
	if a.StdinIsTTY == nil || !a.StdinIsTTY() {
		return def
	}
	suffix := " [y/N] "
	if def {
		suffix = " [Y/n] "
	}
	fmt.Fprint(a.Err, question+suffix)
	line, _ := readLine(a.In)
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def
	case "y", "yes":
		return true
	default:
		return false
	}
}

// readLine reads one line byte by byte so it never over-reads a shared stdin.
func readLine(r io.Reader) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return strings.TrimRight(sb.String(), "\r"), nil
			}
			sb.WriteByte(buf[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return sb.String(), nil
			}
			return sb.String(), err
		}
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// validDate checks an optional YYYY-MM-DD flag.
func validDate(flag, value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return usageErrorf("invalid %s: %s (expected YYYY-MM-DD)", flag, value)
	}
	return nil
}
