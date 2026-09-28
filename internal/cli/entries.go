package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/charmbracelet/bubbles/table"
	"github.com/rizwanreza/cadence-cli/internal/api"
	"github.com/spf13/cobra"
)

func newTodayCmd(a *App) *cobra.Command {
	var date, slug string
	cmd := &cobra.Command{
		Use:   "today",
		Short: "Show each goal's check-in status for a day",
		Long: `Show each goal's check-in status for a day (default: today in your
account's time zone). --json emits one object per goal with a numeric value
(or null), a derived kind (pass_fail | numeric | count) and the unit.`,
		Example: `  cadence today
  cadence today --date 2026-09-21 --slug protein
  cadence today --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validDate("--date", date); err != nil {
				return err
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			statuses, err := c.Completions(a.ctx(), date)
			if err != nil {
				return err
			}
			if slug != "" {
				statuses = filterStatuses(statuses, slug)
			}
			if a.jsonOut {
				return a.printJSON(todayItems(statuses))
			}
			if len(statuses) == 0 {
				a.println(emptyStyle.Render("No goals found. Is there an active cycle on that date? Try `cadence status`."))
				return nil
			}
			if date == "" {
				date = statuses[0].Date
			}
			a.println(accentStyle.Render("Goal status for " + date))
			a.println()
			a.println(renderStatusTable(statuses))
			done := 0
			for _, s := range statuses {
				if s.Completed {
					done++
				}
			}
			a.println(mutedStyle.Render(fmt.Sprintf("%d / %d goals completed", done, len(statuses))))
			return nil
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "Day to show, YYYY-MM-DD (default today)")
	cmd.Flags().StringVar(&slug, "slug", "", "Only show the goal with this slug or id")
	return cmd
}

func renderStatusTable(statuses []api.GoalStatus) string {
	rows := make([]table.Row, 0, len(statuses))
	for _, s := range statuses {
		rows = append(rows, table.Row{s.Name, kindLabel(s.Kind()), todayProgress(s), todayStatus(s)})
	}
	return renderTable([]table.Column{
		{Title: "Goal", Width: 24}, {Title: "Type", Width: 10}, {Title: "Progress", Width: 16}, {Title: "Status", Width: 14},
	}, rows)
}

func newCompleteCmd(a *App) *cobra.Command {
	var goalID, date, value string
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Check off a yes/no goal (--value 0 unchecks)",
		Long: `Check off a yes/no (checkbox) goal for a day. Numeric goals are rejected
without writing anything: use ` + "`set`" + ` to replace a day's total or ` + "`add`" + ` to
increase it. Safe to retry: completing twice leaves it complete.`,
		Example: `  cadence complete --goal workout
  cadence complete --goal workout --date 2026-09-21
  cadence complete --goal workout --value 0   # uncheck`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlags(cmd, "goal"); err != nil {
				return err
			}
			if err := validDate("--date", date); err != nil {
				return err
			}
			if value != "" && value != "0" && value != "1" {
				return usageErrorf("checkbox --value must be 0 or 1")
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			goals, err := c.Goals(a.ctx(), date, 0)
			if err != nil {
				return err
			}
			g, err := matchGoal(goals, goalID)
			if err != nil {
				return err
			}
			if g.Status().Kind() != "pass_fail" {
				return usageErrorf("complete only supports checkbox goals; no value was written. For %s, use `cadence add --goal %s --value <amount>` to increase the total, or `cadence set --goal %s --value <total>` to replace it", g.Name, g.Slug, g.Slug)
			}
			entry, err := c.CreateEntry(a.ctx(), g.ID, date, value)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(map[string]any{
					"id": entry.ID, "goal_id": entry.GoalID, "goal_name": g.Name, "date": entry.Date,
					"value": entry.Value.Float(), "points": entry.Points.Float(), "completed": entry.Completed,
					"input_kind": g.InputKind, "scoring_mode": g.ScoringMode, "target_value": g.Target, "unit": g.Unit,
				})
			}
			a.println(renderEntrySummary(g, entry))
			return nil
		},
	}
	cmd.Flags().StringVar(&goalID, "goal", "", "Goal id, slug, or exact name (required)")
	cmd.Flags().StringVar(&date, "date", "", "Day, YYYY-MM-DD (default today in your time zone)")
	cmd.Flags().StringVar(&value, "value", "", "1 to check (default), 0 to uncheck")
	return cmd
}

// newNumericCmd builds `set` (replace a day's total) and `add` (increase it).
func newNumericCmd(a *App, operation string) *cobra.Command {
	var goalID, date, rawValue, idempotencyKey string
	cmd := &cobra.Command{Args: noArgs}
	if operation == "set" {
		cmd.Use = "set"
		cmd.Short = "Replace a numeric goal's total for a day"
		cmd.Long = `Replace a numeric goal's total for a day (0 clears it). Idempotent:
repeating the same set leaves the same total.`
		cmd.Example = `  cadence set --goal protein --value 113
  cadence set --goal reading --date 2026-09-21 --value 20`
	} else {
		cmd.Use = "add"
		cmd.Short = "Add to a numeric goal's total for a day"
		cmd.Long = `Add a positive amount to a numeric goal's total for a day. The server applies
it atomically. Each add is a new addition: never blindly retry one. Every add
sends an Idempotency-Key; pass the same --idempotency-key when retrying a
request that timed out and the server will not apply it twice.`
		cmd.Example = `  cadence add --goal protein --value 40
  cadence add --goal deep-work --value 1 --idempotency-key 2026-09-28-deep-work-1`
		cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "Retry-safe key for this addition (default: random per call)")
	}
	cmd.Flags().StringVar(&goalID, "goal", "", "Goal id, slug, or exact name (required)")
	cmd.Flags().StringVar(&date, "date", "", "Day, YYYY-MM-DD (default today in your time zone)")
	cmd.Flags().StringVar(&rawValue, "value", "", "Total for set; positive amount for add (required)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireFlags(cmd, "goal", "value"); err != nil {
			return err
		}
		value, err := strconv.ParseFloat(rawValue, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return usageErrorf("--value must be a finite number")
		}
		if operation == "add" && value <= 0 {
			return usageErrorf("add --value must be greater than zero; use set to correct a total")
		}
		if err := validDate("--date", date); err != nil {
			return err
		}
		c, err := a.authedClient()
		if err != nil {
			return err
		}
		goals, err := c.Goals(a.ctx(), date, 0)
		if err != nil {
			return err
		}
		g, err := matchGoal(goals, goalID)
		if err != nil {
			return err
		}
		if g.Status().Kind() == "pass_fail" {
			return usageErrorf("%s requires a numeric goal; use `cadence complete --goal %s` for checkbox goals", operation, g.Slug)
		}
		key := idempotencyKey
		if operation == "add" && key == "" {
			key = randomKey()
		}
		entry, replayed, err := c.WriteNumericEntryReplay(a.ctx(), g.ID, date, rawValue, operation, key)
		if err != nil {
			var netErr *api.NetworkError
			if operation == "add" && errors.As(err, &netErr) {
				return fmt.Errorf("%w. The addition may have been applied: check `cadence today --date <day>`, or retry with --idempotency-key %s so it can't be applied twice", err, key)
			}
			return err
		}
		if a.jsonOut {
			var previous any
			if entry.PreviousValue != nil {
				previous = entry.PreviousValue.Float()
			}
			out := map[string]any{
				"id": entry.ID, "goal_id": entry.GoalID, "goal_name": g.Name, "date": entry.Date,
				"operation": operation, "previous_value": previous,
				"value": entry.Value.Float(), "points": entry.Points.Float(), "completed": entry.Completed,
				"input_kind": g.InputKind, "scoring_mode": g.ScoringMode, "target_value": g.Target, "unit": g.Unit,
			}
			if operation == "add" {
				out["idempotency_key"] = key
				out["replayed"] = replayed
			}
			return a.printJSON(out)
		}
		before := "no entry"
		if entry.PreviousValue != nil {
			before = formatNumber(entry.PreviousValue.Float())
		}
		if replayed {
			a.printf("Already added earlier with this idempotency key; nothing changed. %s\n", renderEntrySummary(g, entry))
			return nil
		}
		if operation == "add" {
			a.printf("Added %s (%s → %s). %s\n", formatNumber(value), before, formatNumber(entry.Value.Float()), renderEntrySummary(g, entry))
		} else {
			a.printf("Set total (%s → %s). %s\n", before, formatNumber(entry.Value.Float()), renderEntrySummary(g, entry))
		}
		return nil
	}
	return cmd
}

func randomKey() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return "cli-" + hex.EncodeToString(buf)
}

func newHistoryCmd(a *App) *cobra.Command {
	var from, to, goal string
	cmd := &cobra.Command{
		Use:   "history",
		Short: "List logged entries over a date range",
		Long: `List stored entries per day over a date range (default: the last 7 days
ending today; at most 120 days). Days with nothing logged have no row. Use it
to backfill missed check-ins or to analyse a stretch of weeks.`,
		Example: `  cadence history
  cadence history --from 2026-09-01 --to 2026-09-27 --goal protein --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validDate("--from", from); err != nil {
				return err
			}
			if err := validDate("--to", to); err != nil {
				return err
			}
			if from != "" && to != "" && from > to {
				return usageErrorf("--from %s is after --to %s", from, to)
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			result, err := c.Entries(a.ctx(), from, to, goal)
			if err != nil {
				return err
			}
			if result.Entries == nil {
				result.Entries = []api.RangeEntry{}
			}
			if a.jsonOut {
				return a.printJSON(result)
			}
			a.println(accentStyle.Render(fmt.Sprintf("Entries %s to %s", result.From, result.To)))
			if len(result.Entries) == 0 {
				a.println(emptyStyle.Render("Nothing logged in this range."))
				return nil
			}
			rows := make([]table.Row, 0, len(result.Entries))
			for _, e := range result.Entries {
				value := "—"
				if e.Value != nil {
					value = formatNumber(e.Value.Float())
					if e.Unit != nil && *e.Unit != "" {
						value += " " + *e.Unit
					}
				}
				rows = append(rows, table.Row{e.Date, e.GoalName, value})
			}
			a.println(renderTable([]table.Column{{Title: "Date", Width: 12}, {Title: "Goal", Width: 28}, {Title: "Value", Width: 16}}, rows))
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "First day, YYYY-MM-DD (default: 6 days before --to)")
	cmd.Flags().StringVar(&to, "to", "", "Last day, YYYY-MM-DD (default today)")
	cmd.Flags().StringVar(&goal, "goal", "", "Only this goal (id, key or slug)")
	return cmd
}
