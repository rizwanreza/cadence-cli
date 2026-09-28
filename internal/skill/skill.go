// Package skill installs the embedded Cadence agent skill into the directories
// agents read (~/.agents/skills, ~/.claude/skills) and keeps managed copies
// current after upgrades.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rizwanreza/cadence-cli/internal/update"
)

// Name is the skill's directory name.
const Name = "cadence"

// MarkerFile marks a skill directory as written by this CLI. It holds the
// version that wrote it.
const MarkerFile = ".managed-by-cadence-cli"

// ErrUnmanaged means a skill exists that this CLI did not write.
var ErrUnmanaged = errors.New("a cadence skill that wasn't installed by the CLI already exists")

// Target is one place an agent looks for skills.
type Target struct {
	Agent string `json:"agent"` // "agents" (shared: Codex and others) or "claude"
	Dir   string `json:"dir"`   // .../skills/cadence
}

// SkillPath is the SKILL.md path inside the target.
func (t Target) SkillPath() string { return filepath.Join(t.Dir, "SKILL.md") }

func agentsTarget(home string) Target {
	return Target{Agent: "agents", Dir: filepath.Join(home, ".agents", "skills", Name)}
}

func claudeTarget(home string) Target {
	return Target{Agent: "claude", Dir: filepath.Join(home, ".claude", "skills", Name)}
}

// AllTargets lists every location the CLI manages, installed or not.
func AllTargets(home string) []Target {
	return []Target{agentsTarget(home), claudeTarget(home)}
}

// Targets resolves --agent: "" (auto: ~/.agents plus Claude Code when
// ~/.claude exists), "all", "claude", or "codex" (Codex reads ~/.agents).
func Targets(agent, home string) ([]Target, error) {
	switch strings.ToLower(agent) {
	case "", "auto":
		targets := []Target{agentsTarget(home)}
		if info, err := os.Stat(filepath.Join(home, ".claude")); err == nil && info.IsDir() {
			targets = append(targets, claudeTarget(home))
		}
		return targets, nil
	case "all":
		return AllTargets(home), nil
	case "claude":
		return []Target{claudeTarget(home)}, nil
	case "codex", "agents":
		return []Target{agentsTarget(home)}, nil
	}
	return nil, fmt.Errorf("unknown agent %q (use claude, codex or all)", agent)
}

// Status describes what is installed at a target.
type Status struct {
	Target
	Installed bool   `json:"installed"`
	Managed   bool   `json:"managed"`
	Version   string `json:"version,omitempty"`
	Current   bool   `json:"current"`
}

// Inspect reports the state of a target against content/version.
func Inspect(t Target, content []byte, version string) Status {
	st := Status{Target: t}
	existing, err := os.ReadFile(t.SkillPath())
	if err != nil {
		return st
	}
	st.Installed = true
	marker, err := os.ReadFile(filepath.Join(t.Dir, MarkerFile))
	if err == nil {
		st.Managed = true
		st.Version = strings.TrimSpace(string(marker))
	}
	st.Current = bytes.Equal(existing, content)
	return st
}

// Result is what Install did.
type Result struct {
	Target
	Action string `json:"action"` // installed | updated | unchanged
	Path   string `json:"path"`
}

// Install writes SKILL.md and the marker. An existing skill without the
// marker is left alone unless force is set.
func Install(t Target, content []byte, version string, force bool) (Result, error) {
	res := Result{Target: t, Path: t.SkillPath()}
	st := Inspect(t, content, version)
	if st.Installed && !st.Managed && !force {
		return res, fmt.Errorf("%w at %s (re-run with --force to replace it)", ErrUnmanaged, t.Dir)
	}
	switch {
	case !st.Installed:
		res.Action = "installed"
	case st.Current && st.Version == version:
		res.Action = "unchanged"
		return res, nil
	default:
		res.Action = "updated"
	}
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return res, err
	}
	if err := writeAtomic(t.SkillPath(), content); err != nil {
		return res, err
	}
	if err := writeAtomic(filepath.Join(t.Dir, MarkerFile), []byte(version+"\n")); err != nil {
		return res, err
	}
	return res, nil
}

// Uninstall removes a managed skill (never an unmanaged one unless force).
func Uninstall(t Target, force bool) (bool, error) {
	st := Inspect(t, nil, "")
	if !st.Installed {
		return false, nil
	}
	if !st.Managed && !force {
		return false, fmt.Errorf("%w at %s (re-run with --force to remove it)", ErrUnmanaged, t.Dir)
	}
	return true, os.RemoveAll(t.Dir)
}

// NeedsRefresh reports whether a managed install predates version.
func NeedsRefresh(st Status, version string) bool {
	if !st.Installed || !st.Managed || !update.Valid(version) {
		return false
	}
	if !update.Valid(st.Version) {
		return true
	}
	return update.Compare(st.Version, version) < 0
}

// RefreshManaged silently rewrites managed installs older than version.
// It returns the targets it refreshed.
func RefreshManaged(home string, content []byte, version string) []Target {
	var refreshed []Target
	for _, t := range AllTargets(home) {
		st := Inspect(t, content, version)
		if !NeedsRefresh(st, version) {
			continue
		}
		if _, err := Install(t, content, version, false); err == nil {
			refreshed = append(refreshed, t)
		}
	}
	return refreshed
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
