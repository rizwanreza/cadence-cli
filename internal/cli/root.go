package cli

import (
	"fmt"
	"strings"

	"github.com/rizwanreza/cadence-cli/internal/config"
	"github.com/rizwanreza/cadence-cli/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	groupDaily  = "daily"
	groupReview = "review"
	groupCycle  = "cycle"
	groupSetup  = "setup"

	// annotationNoChecks skips the update notice and skill refresh.
	annotationNoChecks = "cadence/no-checks"
)

// NewRootCmd builds the full command tree bound to a.
func NewRootCmd(a *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "cadence",
		Short: "Run your 12-Week Year from the terminal",
		Long: `Cadence CLI — run your 12-Week Year from the terminal, or let your agent do it.

Plan a cycle, check in daily, review each week, and close the cycle with
https://cadenceweek.com. Start with:

  cadence login          save your API token (Settings → CLI Access)
  cadence status         where you are today: cycle, week, check-ins, what's due
  cadence skill install  teach Claude Code, Codex and other agents to use Cadence`,
		Example: `  cadence status
  cadence complete --goal workout
  cadence add --goal protein --value 40
  cadence week review set --win "Shipped the draft" --change "Block mornings"
  cadence today --json`,
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          unknownSubcommand,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return a.preRun(cmd)
		},
	}
	root.SuggestionsMinimumDistance = 2
	root.SetVersionTemplate(fmt.Sprintf("cadence %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date))
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &UsageError{Msg: err.Error(), Hint: fmt.Sprintf("Run `%s --help` for usage.", cmd.CommandPath())}
	})

	// --what_drove_results works like --what-drove-results.
	root.SetGlobalNormalizationFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		return pflag.NormalizedName(strings.ReplaceAll(name, "_", "-"))
	})
	flags := root.PersistentFlags()
	flags.StringVar(&a.urlFlag, "url", "", "Cadence host (default "+config.DefaultHost+"; env CADENCE_URL)")
	flags.StringVar(&a.tokenFlag, "token", "", "API token (overrides CADENCE_TOKEN and the saved login)")
	flags.BoolVar(&a.jsonOut, "json", false, "Emit machine-readable JSON (errors go to stderr as JSON)")

	root.AddGroup(
		&cobra.Group{ID: groupDaily, Title: "Daily check-ins:"},
		&cobra.Group{ID: groupReview, Title: "Weekly rhythm and reviews:"},
		&cobra.Group{ID: groupCycle, Title: "Plan and manage cycles:"},
		&cobra.Group{ID: groupSetup, Title: "Setup and maintenance:"},
	)

	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add(groupDaily,
		newStatusCmd(a),
		newTodayCmd(a),
		newCompleteCmd(a),
		newNumericCmd(a, "set"),
		newNumericCmd(a, "add"),
		newHistoryCmd(a),
	)
	add(groupReview,
		newScoreCmd(a),
		newInsightCmd(a),
		newWeekCmd(a),
		newReviewCmd(a),
	)
	add(groupCycle,
		newCyclesCmd(a),
		newGoalsCmd(a),
	)
	add(groupSetup,
		newLoginCmd(a),
		newLogoutCmd(a),
		newAuthCmd(a),
		newDoctorCmd(a),
		newSkillCmd(a),
		newUpdateCmd(a),
		newVersionCmd(a),
	)
	// The default completion command captures its writer at init time.
	if a.Out != nil {
		root.SetOut(a.Out)
	}
	if a.Err != nil {
		root.SetErr(a.Err)
	}
	root.SetHelpCommandGroupID(groupSetup)
	root.SetCompletionCommandGroupID(groupSetup)
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			c.Annotations = map[string]string{annotationNoChecks: "true"}
		}
	}
	return root
}

// unknownSubcommand rejects stray positional arguments with exit code 2 and
// a "did you mean" hint, for the root and for list-style parent commands.
func unknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	hint := fmt.Sprintf("Run `%s --help` for usage.", cmd.CommandPath())
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		hint = "Did you mean: " + strings.Join(suggestions, ", ") + "?\n" + hint
	}
	if cmd.HasSubCommands() {
		return &UsageError{Msg: fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()), Hint: hint}
	}
	return &UsageError{Msg: fmt.Sprintf("unexpected argument %q for %q", args[0], cmd.CommandPath()), Hint: hint}
}

// noArgs is cobra.NoArgs with our usage exit code.
func noArgs(cmd *cobra.Command, args []string) error {
	return unknownSubcommand(cmd, args)
}

// preRun refreshes managed skills after an upgrade and starts the daily
// update check. Both are silent and never fail the command.
func (a *App) preRun(cmd *cobra.Command) error {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[annotationNoChecks] == "true" || strings.HasPrefix(c.Name(), "__") {
			a.skipCheck = true
			return nil
		}
	}
	a.refreshSkills()
	a.startUpdateNotice()
	return nil
}

// requireFlags fails with a usage error when any named flag is unset.
func requireFlags(cmd *cobra.Command, names ...string) error {
	var missing []string
	for _, name := range names {
		if !cmd.Flags().Changed(name) {
			missing = append(missing, "--"+name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &UsageError{
		Msg:  fmt.Sprintf("%s requires %s", cmd.CommandPath(), strings.Join(missing, " and ")),
		Hint: fmt.Sprintf("Run `%s --help` for usage.", cmd.CommandPath()),
	}
}
