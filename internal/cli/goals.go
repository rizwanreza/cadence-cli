package cli

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/rizwanreza/cadence-cli/internal/api"
	"github.com/spf13/cobra"
)

func newGoalsCmd(a *App) *cobra.Command {
	var cycleID int
	var date string
	cmd := &cobra.Command{
		Use:   "goals",
		Short: "List goals, or add, edit, reorder and archive them",
		Long: `List the goals in the cycle that is active today (or --cycle <id>, including
drafts, or the cycle active on --date). Subcommands edit goal definitions.`,
		Example: `  cadence goals
  cadence goals --cycle 42 --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validDate("--date", date); err != nil {
				return err
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			goals, err := c.Goals(a.ctx(), date, cycleID)
			if err != nil {
				return err
			}
			if goals == nil {
				goals = []api.Goal{}
			}
			if a.jsonOut {
				return a.printJSON(goals)
			}
			if len(goals) == 0 {
				a.println(emptyStyle.Render("No goals found."))
				return nil
			}
			rows := make([]table.Row, 0, len(goals))
			for _, g := range goals {
				rows = append(rows, table.Row{strconv.Itoa(g.ID), g.Slug, g.Name, kindLabel(g.Status().Kind()), weeklyTarget(g), goalTarget(g)})
			}
			a.println(renderTable([]table.Column{
				{Title: "ID", Width: 5}, {Title: "Slug", Width: 16}, {Title: "Name", Width: 28},
				{Title: "Type", Width: 10}, {Title: "Weekly target", Width: 46}, {Title: "Target", Width: 18},
			}, rows))
			return nil
		},
	}
	cmd.Flags().IntVar(&cycleID, "cycle", 0, "Cycle id (drafts included)")
	cmd.Flags().StringVar(&date, "date", "", "Pick the cycle active on this day, YYYY-MM-DD")
	cmd.AddCommand(newGoalEditCmd(a, "add"), newGoalEditCmd(a, "update"), newGoalReorderCmd(a), newGoalArchiveCmd(a))
	return cmd
}

func newGoalEditCmd(a *App, command string) *cobra.Command {
	var cycleID, target, weeklyCap int
	var identifier, key, name, notes, input, scoring, unit string
	cmd := &cobra.Command{Use: command, Args: noArgs}
	if command == "add" {
		cmd.Short = "Add a goal to a draft cycle"
		cmd.Example = `  cadence goals add --cycle 42 --key reading --name "Reading" \
    --input number --scoring threshold --target 20 --unit min --weekly-cap 4`
	} else {
		cmd.Short = "Edit a goal (definitions lock on activation; notes stay editable)"
		cmd.Example = `  cadence goals update --cycle 42 --goal reading --target 20 --weekly-cap 4`
		cmd.Flags().StringVar(&identifier, "goal", "", "Goal id or key (required)")
	}
	f := cmd.Flags()
	f.IntVar(&cycleID, "cycle", 0, "Cycle id (required)")
	f.StringVar(&key, "key", "", "Stable goal key (required for add; cannot change)")
	f.StringVar(&name, "name", "", "Goal name, stored verbatim")
	f.StringVar(&notes, "notes", "", "Goal notes / description")
	f.StringVar(&input, "input", "checkbox", "checkbox or number")
	f.StringVar(&scoring, "scoring", "threshold", "threshold (hit the target) or cumulative (points per unit)")
	f.IntVar(&target, "target", 1, "Target value (a positive integer)")
	f.IntVar(&weeklyCap, "weekly-cap", 7, "Threshold: scored days per week (1-7). Cumulative: weekly point cap")
	f.StringVar(&unit, "unit", "", "Unit label, e.g. min, g, sessions")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		required := []string{"cycle", "key", "name"}
		if command == "update" {
			required = []string{"cycle", "goal"}
		}
		if err := requireFlags(cmd, required...); err != nil {
			return err
		}
		values := map[string]any{"name": name, "description": notes, "input_kind": input, "scoring_mode": scoring,
			"target_value": target, "weekly_cap": weeklyCap, "unit": unit, "key": key}
		flagNames := map[string]string{"name": "name", "description": "notes", "input_kind": "input", "scoring_mode": "scoring",
			"target_value": "target", "weekly_cap": "weekly-cap", "unit": "unit", "key": "key"}
		attributes := map[string]any{}
		for field, value := range values {
			if command == "add" || cmd.Flags().Changed(flagNames[field]) {
				attributes[field] = value
			}
		}
		c, err := a.authedClient()
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/goals?cycle_id=%d", cycleID)
		method := http.MethodPost
		if command == "update" {
			goals, err := c.Goals(a.ctx(), "", cycleID)
			if err != nil {
				return err
			}
			g, err := matchGoal(goals, identifier)
			if err != nil {
				return err
			}
			path = fmt.Sprintf("/goals/%d?cycle_id=%d", g.ID, cycleID)
			method = http.MethodPatch
		}
		var result api.Goal
		if err := c.RequestJSON(a.ctx(), method, path, map[string]any{"goal": attributes}, &result); err != nil {
			return err
		}
		if a.jsonOut {
			return a.printJSON(result)
		}
		a.printf("%s saved: %s, %s, %s (goal %d).\n", result.Name, kindLabel(result.Status().Kind()), goalTarget(result), weeklyTarget(result), result.ID)
		return nil
	}
	return cmd
}

func newGoalReorderCmd(a *App) *cobra.Command {
	var cycleID int
	var order string
	cmd := &cobra.Command{
		Use:     "reorder",
		Short:   "Set the order of every goal in a draft cycle",
		Example: `  cadence goals reorder --cycle 42 --order 101,103,102`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlags(cmd, "cycle", "order"); err != nil {
				return err
			}
			var ids []int
			for _, raw := range strings.Split(order, ",") {
				id, err := strconv.Atoi(strings.TrimSpace(raw))
				if err != nil || id <= 0 {
					return usageErrorf("--order must contain positive goal ids separated by commas")
				}
				ids = append(ids, id)
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			var goals []api.Goal
			path := fmt.Sprintf("/twelve_week_years/%d/goal_order", cycleID)
			if err := c.RequestJSON(a.ctx(), http.MethodPatch, path, map[string]any{"order": ids}, &goals); err != nil {
				return err
			}
			if a.jsonOut {
				if goals == nil {
					goals = []api.Goal{}
				}
				return a.printJSON(goals)
			}
			a.println("Goal order saved.")
			return nil
		},
	}
	cmd.Flags().IntVar(&cycleID, "cycle", 0, "Draft cycle id (required)")
	cmd.Flags().StringVar(&order, "order", "", "Every goal id once, comma-separated, in the new order (required)")
	return cmd
}

func newGoalArchiveCmd(a *App) *cobra.Command {
	var cycleID int
	var identifier string
	cmd := &cobra.Command{
		Use:   "archive",
		Short: "Archive a goal (it stops counting; history is kept)",
		Long: `Archive a goal so it no longer appears or counts toward scores. Its logged
history is kept. Confirm with the user before running this.`,
		Example: `  cadence goals archive --goal cold-shower
  cadence goals archive --cycle 42 --goal reading`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlags(cmd, "goal"); err != nil {
				return err
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			goals, err := c.Goals(a.ctx(), "", cycleID)
			if err != nil {
				return err
			}
			g, err := matchGoal(goals, identifier)
			if err != nil {
				return err
			}
			if err := c.ArchiveGoal(a.ctx(), g.ID, cycleID); err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(map[string]any{"archived": true, "goal_id": g.ID, "goal_name": g.Name, "slug": g.Slug})
			}
			a.printf("Archived %s (goal %d).\n", g.Name, g.ID)
			return nil
		},
	}
	cmd.Flags().IntVar(&cycleID, "cycle", 0, "Cycle id (default: the cycle active today)")
	cmd.Flags().StringVar(&identifier, "goal", "", "Goal id, slug, or exact name (required)")
	return cmd
}
