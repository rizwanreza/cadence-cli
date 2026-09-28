package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rizwanreza/cadence-cli/internal/api"
	"github.com/rizwanreza/cadence-cli/internal/config"
	"github.com/rizwanreza/cadence-cli/internal/skill"
	"github.com/rizwanreza/cadence-cli/internal/update"
	"github.com/rizwanreza/cadence-cli/internal/version"
	"github.com/rizwanreza/cadence-cli/skills"
	"github.com/spf13/cobra"
)

// InstallScriptURL is what `cadence update` pipes to bash.
const InstallScriptURL = "https://cadenceweek.com/install-cli"

func newVersionCmd(a *App) *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print the CLI version",
		Args:        noArgs,
		Annotations: map[string]string{annotationNoChecks: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.jsonOut {
				return a.printJSON(map[string]string{
					"version": version.Version, "commit": version.Commit, "date": version.Date,
					"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
				})
			}
			a.printf("cadence %s (commit %s, built %s, %s/%s)\n", version.Version, version.Commit, version.Date, runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}

func homeDir() (string, error) {
	return os.UserHomeDir()
}

// refreshSkills silently brings managed skill installs up to this version.
func (a *App) refreshSkills() {
	if version.IsDev() {
		return
	}
	home, err := homeDir()
	if err != nil {
		return
	}
	skill.RefreshManaged(home, skills.Cadence, version.Version)
}

func newSkillCmd(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the Cadence skill for Claude Code, Codex and other agents",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	var agent string
	var force bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Write the agent skill (refreshed automatically after upgrades)",
		Long: `Write the Cadence agent skill (SKILL.md) where agents look for skills:

  ~/.agents/skills/cadence   read by Codex and other agents (always, unless --agent claude)
  ~/.claude/skills/cadence   Claude Code (when ~/.claude exists, or --agent claude|all)

Each copy gets a .managed-by-cadence-cli marker. Managed copies are refreshed
automatically the first time a newer cadence runs. An existing skill without
the marker is never overwritten unless you pass --force.`,
		Example: `  cadence skill install
  cadence skill install --agent claude
  cadence skill install --agent all --force`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := homeDir()
			if err != nil {
				return err
			}
			targets, err := skill.Targets(agent, home)
			if err != nil {
				return usageErrorf("%v", err)
			}
			var results []skill.Result
			var failures []string
			for _, t := range targets {
				res, err := skill.Install(t, skills.Cadence, version.Version, force)
				if err != nil {
					failures = append(failures, err.Error())
					continue
				}
				results = append(results, res)
			}
			if a.jsonOut {
				if results == nil {
					results = []skill.Result{}
				}
				if err := a.printJSON(map[string]any{"version": version.Version, "results": results, "errors": nonNil(failures)}); err != nil {
					return err
				}
				if len(failures) > 0 {
					return &exitError{code: ExitConflict, err: errors.New(strings.Join(failures, "; "))}
				}
				return nil
			}
			for _, r := range results {
				a.printf("%s %s\n", strings.ToUpper(r.Action[:1])+r.Action[1:], r.Path)
			}
			if len(failures) > 0 {
				return &exitError{code: ExitConflict, err: errors.New(strings.Join(failures, "; "))}
			}
			a.println("Agents will pick up the skill in new sessions. Try: \"log today's workout in Cadence\".")
			return nil
		},
	}
	install.Flags().StringVar(&agent, "agent", "", "claude, codex or all (default: ~/.agents plus Claude Code if installed)")
	install.Flags().BoolVar(&force, "force", false, "Replace a skill that wasn't installed by the CLI")

	var uninstallAgent string
	var uninstallForce bool
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the skill copies this CLI installed",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := homeDir()
			if err != nil {
				return err
			}
			targets := skill.AllTargets(home)
			if uninstallAgent != "" {
				if targets, err = skill.Targets(uninstallAgent, home); err != nil {
					return usageErrorf("%v", err)
				}
			}
			removed := []string{}
			for _, t := range targets {
				ok, err := skill.Uninstall(t, uninstallForce)
				if err != nil {
					return &exitError{code: ExitConflict, err: err}
				}
				if ok {
					removed = append(removed, t.Dir)
				}
			}
			if a.jsonOut {
				return a.printJSON(map[string]any{"removed": removed})
			}
			if len(removed) == 0 {
				a.println("No installed skill found.")
			}
			for _, dir := range removed {
				a.printf("Removed %s\n", dir)
			}
			return nil
		},
	}
	uninstall.Flags().StringVar(&uninstallAgent, "agent", "", "claude, codex or all (default all)")
	uninstall.Flags().BoolVar(&uninstallForce, "force", false, "Also remove a skill the CLI didn't install")

	show := &cobra.Command{
		Use:   "show",
		Short: "Print the embedded SKILL.md",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.jsonOut {
				return a.printJSON(map[string]string{"version": version.Version, "content": string(skills.Cadence)})
			}
			_, err := a.Out.Write(skills.Cadence)
			return err
		},
	}
	cmd.AddCommand(install, uninstall, show)
	return cmd
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// --- update notice -------------------------------------------------------

func (a *App) startUpdateNotice() {
	if a.skipCheck || a.jsonOut || version.IsDev() || update.Disabled() || a.Updates == nil {
		return
	}
	if a.StderrIsTTY == nil || !a.StderrIsTTY() {
		return
	}
	a.notice = make(chan string, 1)
	if latest, fresh := a.Updates.Cached(); fresh {
		a.notice <- noticeFor(latest)
		return
	}
	a.Updates.UserAgent = version.UserAgent()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		latest, err := a.Updates.Fetch(ctx)
		if err != nil {
			a.notice <- ""
			return
		}
		a.notice <- noticeFor(latest)
	}()
}

func noticeFor(latest string) string {
	if !update.Newer(latest, version.Version) {
		return ""
	}
	return fmt.Sprintf("A new cadence is available: %s → %s. Run `cadence update`.", version.Version, latest)
}

func (a *App) finishUpdateNotice() {
	if a.notice == nil {
		return
	}
	select {
	case msg := <-a.notice:
		if msg != "" {
			fmt.Fprintln(a.Err, mutedStyle.Render(msg))
		}
	case <-time.After(400 * time.Millisecond):
	}
}

// --- update ----------------------------------------------------------------

// installMethod guesses how the running binary was installed.
func installMethod(path string) string {
	p := filepath.ToSlash(path)
	switch {
	case strings.Contains(p, "/Cellar/") || strings.Contains(p, "/Caskroom/") || strings.Contains(p, "/homebrew/") || strings.Contains(p, "/linuxbrew/"):
		return "brew"
	}
	goBins := []string{os.Getenv("GOBIN")}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		for _, dir := range filepath.SplitList(gopath) {
			goBins = append(goBins, filepath.Join(dir, "bin"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		goBins = append(goBins, filepath.Join(home, "go", "bin"))
	}
	for _, dir := range goBins {
		if dir != "" && filepath.Dir(path) == filepath.Clean(dir) {
			return "go"
		}
	}
	return "installer"
}

func newUpdateCmd(a *App) *cobra.Command {
	var yes, check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update cadence to the latest release",
		Long: `Update to the latest release. Homebrew installs are told to run
` + "`brew upgrade rizwanreza/tap/cadence`" + `; go install users get the go command;
everything else re-runs the installer (` + InstallScriptURL + `) into the
same directory, after confirmation (or --yes).`,
		Example: `  cadence update --check
  cadence update --yes`,
		Args:        noArgs,
		Annotations: map[string]string{annotationNoChecks: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, err := a.Executable()
			if err != nil {
				return err
			}
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			method := installMethod(exe)
			a.Updates.UserAgent = version.UserAgent()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			latest, latestErr := a.Updates.Fetch(ctx)
			available := latestErr == nil && (version.IsDev() || update.Newer(latest, version.Version))

			command := ""
			switch method {
			case "brew":
				command = "brew upgrade rizwanreza/tap/cadence"
			case "go":
				command = "go install github.com/rizwanreza/cadence-cli/cmd/cadence@latest"
			default:
				command = "curl -fsSL " + InstallScriptURL + " | bash"
			}
			result := map[string]any{
				"current": version.Version, "latest": latest, "update_available": available,
				"method": method, "command": command, "binary": exe, "ran": false,
			}
			if latestErr != nil {
				result["latest_error"] = latestErr.Error()
			}
			report := func() error {
				if a.jsonOut {
					return a.printJSON(result)
				}
				return nil
			}
			if !a.jsonOut {
				if latestErr != nil {
					a.printf("Couldn't check the latest release (%v).\n", latestErr)
				} else if !available {
					a.printf("cadence %s is the latest release.\n", version.Version)
				} else {
					a.printf("cadence %s is available (you have %s).\n", latest, version.Version)
				}
			}
			if check || (latestErr == nil && !available) {
				return report()
			}
			if method != "installer" || runtime.GOOS == "windows" {
				if runtime.GOOS == "windows" && method == "installer" {
					result["command"] = "Download the latest zip from https://github.com/" + update.Repo + "/releases/latest"
				}
				if !a.jsonOut {
					a.printf("Update with:\n  %s\n", result["command"])
				}
				return report()
			}
			if !yes {
				if a.jsonOut || a.StdinIsTTY == nil || !a.StdinIsTTY() {
					if !a.jsonOut {
						a.printf("Re-run with --yes to install, or run:\n  %s\n", command)
					}
					return report()
				}
				if !a.confirm(fmt.Sprintf("Re-run the installer into %s?", filepath.Dir(exe)), true) {
					a.println("Cancelled.")
					return nil
				}
			}
			if err := a.RunInstaller(filepath.Dir(exe)); err != nil {
				return fmt.Errorf("the installer failed: %w", err)
			}
			result["ran"] = true
			return report()
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Don't ask before re-running the installer")
	cmd.Flags().BoolVar(&check, "check", false, "Only report whether an update is available")
	return cmd
}

func runInstaller(binDir string) error {
	cmd := exec.Command("bash", "-c", "set -o pipefail; curl -fsSL "+InstallScriptURL+" | bash")
	cmd.Env = append(os.Environ(), "CADENCE_BIN_DIR="+binDir, "CADENCE_SKIP_SETUP=1")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// --- doctor ----------------------------------------------------------------

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok | warn | fail
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func newDoctorCmd(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the install, login, server compatibility and agent skill",
		Long: `Check that everything is healthy:

  version  this binary vs the latest release
  path     the cadence on your PATH is this binary (catches stale copies)
  config   the saved login file and its permissions
  auth     the host is reachable and the token is valid (GET /api/v1/me)
  server   the server supports this CLI version (min_version)
  skill    the agent skill is installed and current

Exits 1 when any check fails.`,
		Args:        noArgs,
		Annotations: map[string]string{annotationNoChecks: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := a.runDoctor()
			failed := false
			for _, c := range checks {
				if c.Status == "fail" {
					failed = true
				}
			}
			if a.jsonOut {
				if err := a.printJSON(map[string]any{"ok": !failed, "version": version.Version, "checks": checks}); err != nil {
					return err
				}
			} else {
				for _, c := range checks {
					mark := map[string]string{"ok": "✓", "warn": "!", "fail": "✗"}[c.Status]
					a.printf("%s %-8s %s\n", mark, c.Name, c.Message)
					if c.Hint != "" {
						a.printf("  %s\n", mutedStyle.Render(c.Hint))
					}
				}
			}
			if failed {
				return errSilent
			}
			return nil
		},
	}
}

func (a *App) runDoctor() []doctorCheck {
	var checks []doctorCheck
	add := func(name, status, message, hint string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Hint: hint})
	}

	// version
	a.Updates.UserAgent = version.UserAgent()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	latest, err := a.Updates.Fetch(ctx)
	cancel()
	switch {
	case version.IsDev():
		add("version", "warn", "development build ("+version.Version+")", "Release builds come from https://github.com/"+update.Repo+"/releases")
	case err != nil:
		add("version", "warn", fmt.Sprintf("cadence %s; couldn't check the latest release (%v)", version.Version, err), "")
	case update.Newer(latest, version.Version):
		add("version", "warn", fmt.Sprintf("cadence %s; %s is available", version.Version, latest), "Run `cadence update`.")
	default:
		add("version", "ok", "cadence "+version.Version+" (latest)", "")
	}

	// path
	if exe, err := a.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		onPath, lookErr := exec.LookPath("cadence")
		if lookErr != nil {
			add("path", "warn", "cadence is not on your PATH (running "+exe+")", "Add "+filepath.Dir(exe)+" to PATH, or re-run the installer.")
		} else if resolved, _ := filepath.EvalSymlinks(onPath); resolved != exe {
			add("path", "warn", "`cadence` on PATH is "+onPath+", not this binary ("+exe+")", "An older copy may shadow this one. Remove it or reorder PATH.")
		} else {
			add("path", "ok", onPath, "")
		}
	}

	// config
	cfg, cfgErr := a.config()
	path, _ := config.Path()
	switch {
	case cfgErr != nil:
		add("config", "fail", cfgErr.Error(), "Fix or delete the file, then `cadence login`.")
	default:
		if info, err := os.Stat(path); err == nil {
			if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
				add("config", "warn", fmt.Sprintf("%s is readable by others (%v)", path, info.Mode().Perm()), "chmod 600 "+path)
			} else {
				add("config", "ok", path, "")
			}
		} else {
			add("config", "ok", "no saved login ("+path+")", "")
		}
	}

	// auth + server
	host, hostErr := config.ResolveHost(a.urlFlag, cfg)
	token, source := config.ResolveToken(a.tokenFlag, cfg)
	switch {
	case hostErr != nil:
		add("auth", "fail", hostErr.Error(), "")
	case token == "":
		add("auth", "fail", "not signed in to "+host, "Run `cadence login`.")
	default:
		c, err := a.clientFor(host, token)
		if err != nil {
			add("auth", "fail", err.Error(), "")
			break
		}
		me, err := c.Me(a.ctx())
		var netErr *api.NetworkError
		switch {
		case errors.As(err, &netErr):
			add("auth", "fail", err.Error(), "Check your connection, CADENCE_URL, or raise CADENCE_TIMEOUT.")
		case api.IsStatus(err, 401):
			add("auth", "fail", fmt.Sprintf("%s rejected the token (from %s)", host, source), "Run `cadence login` with a new token from "+host+"/settings#cli.")
		case api.IsStatus(err, 404):
			add("auth", "fail", host+" doesn't serve /api/v1/me", "Is --url/CADENCE_URL pointing at a Cadence server?")
		case err != nil:
			add("auth", "fail", err.Error(), "")
		default:
			add("auth", "ok", fmt.Sprintf("signed in to %s as %s (%s)", host, me.User.Email, me.User.TimeZone), "")
			min := me.CLI.MinVersion
			switch {
			case min == "":
				add("server", "ok", "no minimum CLI version", "")
			case version.IsDev():
				add("server", "warn", "server requires cadence ≥ "+min+"; this is a development build", "")
			case update.Compare(version.Version, min) < 0:
				add("server", "fail", fmt.Sprintf("server requires cadence ≥ %s; this is %s", min, version.Version), "Run `cadence update`.")
			default:
				add("server", "ok", "compatible (server requires ≥ "+min+")", "")
			}
		}
	}

	// skill
	if home, err := homeDir(); err == nil {
		var installed []string
		status, message, hint := "ok", "", ""
		for _, t := range skill.AllTargets(home) {
			st := skill.Inspect(t, skills.Cadence, version.Version)
			if !st.Installed {
				continue
			}
			installed = append(installed, t.Dir)
			switch {
			case !st.Managed:
				status, hint = "warn", "A hand-written skill is at "+t.Dir+"; `cadence skill install --force` replaces it."
			case !st.Current:
				status, hint = "warn", "Run `cadence skill install` to refresh "+t.Dir+"."
			}
		}
		if len(installed) == 0 {
			add("skill", "warn", "agent skill not installed", "Run `cadence skill install`.")
		} else {
			message = "installed at " + strings.Join(installed, ", ")
			if status != "ok" {
				message += " (outdated or unmanaged)"
			}
			add("skill", status, message, hint)
		}
	}
	return checks
}
