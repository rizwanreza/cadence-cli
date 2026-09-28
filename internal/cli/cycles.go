package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/rizwanreza/cadence-cli/internal/api"
	"github.com/spf13/cobra"
)

func newCyclesCmd(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cycles",
		Aliases: []string{"cycle"},
		Short:   "List, plan, activate, review and restart 12-week cycles",
		Long: `List your 12-week cycles (12-week years), or manage one with a subcommand.

A cycle moves draft → scheduled → active → ended. Plans are drafted, previewed
with --dry-run, then activated explicitly. Goal definitions lock on activation.`,
		Example: `  cadence cycles
  cadence cycles show --id 42
  cadence cycles next --id 42 --start after`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			cycles, err := c.Cycles(a.ctx())
			if err != nil {
				return err
			}
			if cycles == nil {
				cycles = []api.Cycle{}
			}
			if a.jsonOut {
				// Pass the server's full payload through for agents.
				var raw []map[string]any
				if err := c.RequestJSON(a.ctx(), http.MethodGet, "/twelve_week_years", nil, &raw); err == nil && raw != nil {
					return a.printJSON(raw)
				}
				return a.printJSON(cycles)
			}
			if len(cycles) == 0 {
				a.println(emptyStyle.Render("No cycles yet. Plan one with `cadence cycles create` or at cadenceweek.com."))
				return nil
			}
			rows := make([]table.Row, 0, len(cycles))
			for _, cy := range cycles {
				week := ""
				if cy.CurrentWeekNumber > 0 && cy.Status == "active" {
					week = fmt.Sprintf("%d/%d", cy.CurrentWeekNumber, cy.TotalWeeks)
				}
				rows = append(rows, table.Row{strconv.Itoa(cy.ID), cy.Name, cy.StartDate, cy.EndDate, cy.Status, week, cy.ReviewState})
			}
			a.println(renderTable([]table.Column{
				{Title: "ID", Width: 5}, {Title: "Name", Width: 22}, {Title: "Start", Width: 12}, {Title: "End", Width: 12},
				{Title: "Status", Width: 10}, {Title: "Week", Width: 6}, {Title: "Review", Width: 14},
			}, rows))
			return nil
		},
	}
	cmd.AddCommand(
		newCycleShowCmd(a, "show"),
		newCycleShowCmd(a, "export"),
		newCycleActivateCmd(a),
		newCyclePlanCmd(a, "create"),
		newCyclePlanCmd(a, "import"),
		newCyclePlanCmd(a, "update"),
		newCycleNotesCmd(a),
		newCycleReviewCmd(a),
		newCycleNextCmd(a),
		newCycleUnscheduleCmd(a),
		newFreshStartCmd(a),
	)
	return cmd
}

// cycleIDOrDefault returns id, or the cycle a command should act on by default:
// the closing cycle (awaiting review) when preferClosing, else the active one.
func (a *App) cycleIDOrDefault(c *api.Client, id int, preferClosing bool) (int, error) {
	if id > 0 {
		return id, nil
	}
	me, err := c.Me(a.ctx())
	if err != nil {
		return 0, err
	}
	if preferClosing && me.ClosingCycle != nil {
		return me.ClosingCycle.ID, nil
	}
	if me.ActiveCycle != nil {
		return me.ActiveCycle.ID, nil
	}
	if me.ClosingCycle != nil {
		return me.ClosingCycle.ID, nil
	}
	return 0, &UsageError{Msg: "no active cycle today; pass --id (see `cadence cycles`)"}
}

func newCycleShowCmd(a *App, command string) *cobra.Command {
	var id int
	cmd := &cobra.Command{Use: command, Args: noArgs}
	if command == "show" {
		cmd.Short = "Show a cycle and its goals (drafts included)"
		cmd.Example = "  cadence cycles show --id 42"
	} else {
		cmd.Short = "Print a cycle's plan document (JSON) for editing and re-import"
		cmd.Example = "  cadence cycles export --id 42 > plan.json"
	}
	cmd.Flags().IntVar(&id, "id", 0, "Cycle id (default: the cycle active today)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := a.authedClient()
		if err != nil {
			return err
		}
		if id, err = a.cycleIDOrDefault(c, id, false); err != nil {
			return err
		}
		plan, err := c.Cycle(a.ctx(), id)
		if err != nil {
			return err
		}
		if command == "export" {
			return a.printJSON(exportDocument(plan))
		}
		if a.jsonOut {
			return a.printJSON(plan)
		}
		fmt.Fprint(a.Out, renderPlan(plan))
		return nil
	}
	return cmd
}

func newCycleActivateCmd(a *App) *cobra.Command {
	var id int
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "activate",
		Short: "Activate (or schedule) a draft cycle — confirm with the user first",
		Long: `Activate a draft. If it starts today or earlier it begins now; if it starts
within the next three weeks it is scheduled. Overlapping cycles are rejected.
A fresh-start draft replaces the running cycle from today. Goal definitions
lock on activation. Always confirm with the user before running this.`,
		Example: "  cadence cycles activate --id 42",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlags(cmd, "id"); err != nil {
				return err
			}
			if dryRun {
				return usageErrorf("activate does not support --dry-run; inspect the draft with `cadence cycles show --id %d`", id)
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			var plan map[string]any
			if err := c.RequestJSON(a.ctx(), http.MethodPost, fmt.Sprintf("/twelve_week_years/%d/activation", id), nil, &plan); err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(plan)
			}
			fmt.Fprint(a.Out, renderPlan(plan))
			return nil
		},
	}
	cmd.Flags().IntVar(&id, "id", 0, "Draft cycle id (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Not supported; use `cycles show`")
	_ = cmd.Flags().MarkHidden("dry-run")
	return cmd
}

func newCyclePlanCmd(a *App, command string) *cobra.Command {
	var id int
	var key, name, start, notes, file string
	var dryRun bool
	cmd := &cobra.Command{Use: command, Args: noArgs}
	switch command {
	case "create":
		cmd.Short = "Create an empty draft cycle"
		cmd.Example = `  cadence cycles create --key fall-2026 --name "Fall 2026" --start 2026-10-05 --dry-run`
	case "import":
		cmd.Short = "Create or replace a draft from a plan document (JSON)"
		cmd.Long = `Create a draft from a plan document (schema_version 1), or replace a draft's
plan with --id. Retrying the same document with the same plan_key is safe and
returns the same ids. Preview first with --dry-run. --key, --name, --start and
--notes override the document's values.`
		cmd.Example = `  cadence cycles import --file plan.json --dry-run
  cadence cycles import --file plan.json --json
  cadence cycles import --id 42 --file plan.json`
	case "update":
		cmd.Short = "Rename or move a draft cycle"
		cmd.Example = `  cadence cycles update --id 42 --name "Autumn goals" --start 2026-10-06`
	}
	f := cmd.Flags()
	f.IntVar(&id, "id", 0, "Draft cycle id")
	f.StringVar(&key, "key", "", "Stable plan key (makes retries safe)")
	f.StringVar(&name, "name", "", "Cycle name")
	f.StringVar(&start, "start", "", "Start date, YYYY-MM-DD")
	f.StringVar(&notes, "notes", "", "Cycle notes")
	f.StringVar(&file, "file", "", "Plan JSON file, or - for stdin")
	f.BoolVar(&dryRun, "dry-run", false, "Validate and preview without saving")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validDate("--start", start); err != nil {
			return err
		}
		c, err := a.authedClient()
		if err != nil {
			return err
		}
		var plan map[string]any
		path := "/twelve_week_years"
		if id > 0 {
			path += "/" + strconv.Itoa(id)
		}
		switch command {
		case "import":
			if err := requireFlags(cmd, "file"); err != nil {
				return err
			}
			var data []byte
			if file == "-" {
				data, err = io.ReadAll(a.In)
			} else {
				data, err = os.ReadFile(file)
			}
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &plan); err != nil {
				return usageErrorf("invalid plan JSON: %v", err)
			}
			if plan == nil {
				return usageErrorf("plan must be a JSON object")
			}
		case "update":
			if err := requireFlags(cmd, "id"); err != nil {
				return err
			}
			if err := c.RequestJSON(a.ctx(), http.MethodGet, path, nil, &plan); err != nil {
				return err
			}
		default:
			if id != 0 {
				return usageErrorf("create does not accept --id; use `cadence cycles update`")
			}
			if err := requireFlags(cmd, "key", "name", "start"); err != nil {
				return err
			}
			plan = map[string]any{"schema_version": 1, "goals": []any{}}
		}
		set := map[string]string{"key": "plan_key", "name": "name", "start": "start_date", "notes": "notes"}
		values := map[string]string{"key": key, "name": name, "start": start, "notes": notes}
		for flag, field := range set {
			if cmd.Flags().Changed(flag) {
				plan[field] = values[flag]
			}
		}
		method := http.MethodPost
		if id > 0 {
			method = http.MethodPatch
		}
		var response map[string]any
		if err := c.RequestJSON(a.ctx(), method, path, map[string]any{"plan": exportDocument(plan), "dry_run": dryRun}, &response); err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(response)
		}
		fmt.Fprint(a.Out, renderPlan(response))
		return nil
	}
	return cmd
}

func newCycleNotesCmd(a *App) *cobra.Command {
	var id int
	var notes string
	cmd := &cobra.Command{
		Use:   "notes",
		Short: "Show or replace a cycle's notes (works on active cycles too)",
		Example: `  cadence cycles notes
  cadence cycles notes --id 42 --notes "Travel weeks 5-6: protein only"`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			if id, err = a.cycleIDOrDefault(c, id, false); err != nil {
				return err
			}
			var result map[string]any
			if cmd.Flags().Changed("notes") {
				result, err = c.UpdateCycleNotes(a.ctx(), id, notes)
			} else {
				result, err = c.Cycle(a.ctx(), id)
			}
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(map[string]any{"id": id, "notes": result["notes"]})
			}
			text := describe(result["notes"])
			if cmd.Flags().Changed("notes") {
				a.printf("Saved notes for cycle %d.\n", id)
				return nil
			}
			if text == "" {
				a.println(emptyStyle.Render("No notes yet. Add some with --notes."))
				return nil
			}
			a.println(text)
			return nil
		},
	}
	cmd.Flags().IntVar(&id, "id", 0, "Cycle id (default: the cycle active today)")
	cmd.Flags().StringVar(&notes, "notes", "", "New notes (replaces the old ones; empty clears)")
	return cmd
}

// reflectionFlags maps flags to the six end-of-cycle reflection fields.
var reflectionFlags = []struct{ flag, field, question string }{
	{"what-drove-results", "what_drove_results", "What specifically made your strongest weeks work?"},
	{"what-limited-execution", "what_limited_execution", "What constraint or pattern most often reduced execution?"},
	{"what-needs-redesign", "what_needs_redesign", "Which goal needs a better design, not just more discipline?"},
	{"what-to-carry-forward", "what_to_carry_forward", "What should stay exactly the same next cycle because it clearly worked?"},
	{"next-cycle-adjustment", "next_cycle_adjustment", "What single change would most improve the next 12 weeks?"},
	{"closing-notes", "closing_notes", "What else should your future self remember from this cycle?"},
}

func newCycleReviewCmd(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Show, save or dismiss the end-of-cycle review",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	show := newReviewCmd(a)
	show.Use = "show"
	show.GroupID = ""
	show.Short = "Show the end-of-cycle review (numbers, learnings, prompts, your answers)"

	var setID int
	values := make([]string, len(reflectionFlags))
	set := &cobra.Command{
		Use:   "set",
		Short: "Save answers to the six end-of-cycle reflection questions",
		Long: `Save answers to the end-of-cycle reflection (any subset; others are kept).
Flags are named after the API's reflection fields (underscores work too):

  --what-drove-results      What specifically made your strongest weeks work?
  --what-limited-execution  What constraint or pattern most often reduced execution?
  --what-needs-redesign     Which goal needs a better design, not just more discipline?
  --what-to-carry-forward   What should stay exactly the same next cycle because it clearly worked?
  --next-cycle-adjustment   What single change would most improve the next 12 weeks?
  --closing-notes           What else should your future self remember from this cycle?

Saving a review closes the cycle out (it is no longer awaiting review).
Defaults to the cycle awaiting review.`,
		Example: `  cadence cycles review set --what-drove-results "Morning blocks before email" \
    --next-cycle-adjustment "Plan on Sundays"`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fields := map[string]string{}
			for i, rf := range reflectionFlags {
				if cmd.Flags().Changed(rf.flag) {
					fields[rf.field] = values[i]
				}
			}
			if len(fields) == 0 {
				return &UsageError{Msg: "nothing to save: pass at least one answer flag", Hint: "Run `cadence cycles review set --help` for the questions."}
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			id, err := a.cycleIDOrDefault(c, setID, true)
			if err != nil {
				return err
			}
			result, err := c.UpdateReview(a.ctx(), id, fields)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(result)
			}
			a.printf("Saved %d reflection answer(s) for cycle %d.\n", len(fields), id)
			return nil
		},
	}
	set.Flags().IntVar(&setID, "id", 0, "Cycle id (default: the cycle awaiting review)")
	for i, rf := range reflectionFlags {
		set.Flags().StringVar(&values[i], rf.flag, "", rf.question)
	}

	var dismissID int
	dismiss := &cobra.Command{
		Use:   "dismiss",
		Short: "Skip the end-of-cycle review — confirm with the user first",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			id, err := a.cycleIDOrDefault(c, dismissID, true)
			if err != nil {
				return err
			}
			result, err := c.DismissReview(a.ctx(), id)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(result)
			}
			a.printf("Dismissed the review for cycle %d.\n", id)
			return nil
		},
	}
	dismiss.Flags().IntVar(&dismissID, "id", 0, "Cycle id (default: the cycle awaiting review)")
	cmd.AddCommand(show, set, dismiss)
	return cmd
}

// startOptions maps CLI spellings to the API's next-cycle start keys.
var startOptions = map[string]string{
	"after": "after", "right-after": "after", "right_after": "after",
	"today":      "today",
	"after-13th": "after_13th", "after_13th": "after_13th", "13th-week": "after_13th", "thirteenth_week": "after_13th",
}

func newCycleNextCmd(a *App) *cobra.Command {
	var id int
	var start, goals string
	var blank bool
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Draft the next cycle, carrying goals forward",
		Long: `Draft the cycle that follows --id (default: the active or closing cycle),
copying its active goals (or only --goals) into a new draft. Nothing is
activated: review the draft, then run ` + "`cadence cycles activate`" + `.

--start (default: the first option available):
  after       right after this cycle ends (no gap; only before that date passes)
  today       start today (only once this cycle has ended)
  after-13th  take a 13th week to finish, reflect and plan first

If a draft for the next cycle already exists it is returned instead; if the
next cycle is already scheduled the command fails with exit code 5.`,
		Example: `  cadence cycles next --start after
  cadence cycles next --id 42 --start after-13th --goals reading,protein`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := ""
			if start != "" {
				var ok bool
				if key, ok = startOptions[strings.ToLower(start)]; !ok {
					return usageErrorf("--start must be after, today or after-13th (got %q)", start)
				}
			}
			if blank && goals != "" {
				return usageErrorf("--blank and --goals can't be combined")
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			if id, err = a.cycleIDOrDefault(c, id, true); err != nil {
				return err
			}
			goalIDs, err := a.resolveGoalIDs(c, id, goals)
			if err != nil {
				return err
			}
			if blank {
				goalIDs = []int{}
			}
			result, raw, err := c.NextCycle(a.ctx(), id, key, goalIDs)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(raw)
			}
			title := "Next cycle drafted"
			if result.CarryForward == nil {
				title = "A draft for the next cycle already exists"
			}
			a.printDraftResult(title, result)
			return nil
		},
	}
	cmd.Flags().IntVar(&id, "id", 0, "Cycle to follow (default: the active or closing cycle)")
	cmd.Flags().StringVar(&start, "start", "", "after, today or after-13th (default: the first option available)")
	cmd.Flags().StringVar(&goals, "goals", "", "Only carry these goals (ids or slugs, comma-separated; default all active)")
	cmd.Flags().BoolVar(&blank, "blank", false, "Start the draft with no goals")
	return cmd
}

// resolveGoalIDs turns "12,reading" into goal ids of cycleID (nil = default).
func (a *App) resolveGoalIDs(c *api.Client, cycleID int, list string) ([]int, error) {
	list = strings.TrimSpace(list)
	if list == "" {
		return nil, nil
	}
	// Always resolve against the cycle's goals so a typo'd id fails loudly
	// instead of silently carrying nothing.
	goals, err := c.Goals(a.ctx(), "", cycleID)
	if err != nil {
		return nil, err
	}
	ids := []int{}
	for _, raw := range strings.Split(list, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		g, err := matchGoal(goals, raw)
		if err != nil {
			return nil, err
		}
		ids = append(ids, g.ID)
	}
	return ids, nil
}

func (a *App) printDraftResult(title string, result api.NextCycleResult) {
	cy := result.Cycle
	a.println(accentStyle.Render(title))
	a.printf("%s [%s] — %s to %s (cycle %d)\n", cy.Name, cy.Status, cy.StartDate, cy.EndDate, cy.ID)
	for i, g := range result.Goals {
		a.printf("%d. %s — %s; %s\n", i+1, g.Name, goalTarget(g), weeklyTarget(g))
	}
	if cf := result.CarryForward; cf != nil {
		for _, bad := range cf.Invalid {
			a.printf("Not carried: %s (%s)\n", bad.Name, strings.Join(bad.Errors, "; "))
		}
	}
	a.println()
	a.printf("Nothing is active yet. Review with `cadence cycles show --id %d`, edit with `cadence goals ... --cycle %d`,\nthen activate (after the user confirms) with `cadence cycles activate --id %d`.\n", cy.ID, cy.ID, cy.ID)
}

func newCycleUnscheduleCmd(a *App) *cobra.Command {
	var id int
	cmd := &cobra.Command{
		Use:     "unschedule",
		Short:   "Return a scheduled (not yet started) cycle to a draft — confirm first",
		Example: "  cadence cycles unschedule --id 43",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlags(cmd, "id"); err != nil {
				return err
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			result, err := c.Unschedule(a.ctx(), id)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(result)
			}
			a.printf("Cycle %d is a draft again. Edit it, then `cadence cycles activate --id %d` when ready.\n", id, id)
			return nil
		},
	}
	cmd.Flags().IntVar(&id, "id", 0, "Scheduled cycle id (required)")
	return cmd
}

func newFreshStartCmd(a *App) *cobra.Command {
	var goals string
	cmd := &cobra.Command{
		Use:   "fresh-start",
		Short: "Draft a new 12 weeks from today to replace a lapsed cycle",
		Long: `Draft a fresh 12 weeks starting today, carrying the running cycle's goals (or
only --goals). Nothing changes until the draft is activated with
` + "`cadence cycles activate --id <draft>`" + `, which ends the old cycle yesterday and
moves today's check-ins across. Confirm with the user before activating.`,
		Example: `  cadence cycles fresh-start
  cadence cycles fresh-start --goals workout,reading
  cadence cycles fresh-start dismiss`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			var goalIDs []int
			if strings.TrimSpace(goals) != "" {
				id, err := a.cycleIDOrDefault(c, 0, false)
				if err != nil {
					return err
				}
				if goalIDs, err = a.resolveGoalIDs(c, id, goals); err != nil {
					return err
				}
			}
			result, raw, err := c.FreshStart(a.ctx(), goalIDs)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(raw)
			}
			a.printDraftResult("Fresh start drafted", result)
			return nil
		},
	}
	cmd.Flags().StringVar(&goals, "goals", "", "Only carry these goals (ids or slugs, comma-separated; default all active)")
	cmd.AddCommand(&cobra.Command{
		Use:   "dismiss",
		Short: `Hide the fresh-start offer until the next check-in ("Not now")`,
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			if err := c.DismissFreshStart(a.ctx()); err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(map[string]any{"dismissed": true})
			}
			a.println("Fresh-start offer dismissed until your next check-in.")
			return nil
		},
	})
	return cmd
}
