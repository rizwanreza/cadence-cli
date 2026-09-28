package cli

import (
	"fmt"
	"strings"

	"github.com/rizwanreza/cadence-cli/internal/api"
	"github.com/spf13/cobra"
)

// newReviewCmd is `cadence review` (also `cadence cycles review show`).
func newReviewCmd(a *App) *cobra.Command {
	var id int
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Show a cycle's end-of-cycle review",
		Long: `Show the end-of-cycle review for a cycle: execution, best and worst weeks,
per-goal trends, learnings, the six reflection prompts and any saved answers.
Defaults to the cycle awaiting review, else the active cycle.`,
		Example: `  cadence review
  cadence review --id 3 --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			if id, err = a.cycleIDOrDefault(c, id, true); err != nil {
				return err
			}
			if a.jsonOut {
				raw, err := c.ReviewRaw(a.ctx(), id)
				if err != nil {
					return err
				}
				return a.printJSON(raw)
			}
			review, err := c.Review(a.ctx(), id)
			if err != nil {
				return err
			}
			fmt.Fprint(a.Out, renderReviewSummary(review))
			return nil
		},
	}
	cmd.Flags().IntVar(&id, "id", 0, "Cycle id (default: the cycle awaiting review, else the active one)")
	return cmd
}

func newScoreCmd(a *App) *cobra.Command {
	var cycleID int
	var week, asOf string
	cmd := &cobra.Command{
		Use:   "score",
		Short: "Show the week's execution score, pace, risk, leverage and streaks",
		Long: `Show the server's weekly scorecard: execution against the full week target
(capped per goal), pace so far, the tier, plus what's at risk today, the
highest-leverage action and current streaks. Defaults to this week of the
active cycle.`,
		Example: `  cadence score
  cadence score --week 2026-09-21 --json
  cadence score --cycle 42 --week 2026-10-05 --as-of 2026-10-08`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validDate("--week", week); err != nil {
				return err
			}
			if err := validDate("--as-of", asOf); err != nil {
				return err
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			if cycleID, err = a.cycleIDOrDefault(c, cycleID, false); err != nil {
				return err
			}
			result, err := c.Scorecard(a.ctx(), cycleID, week, asOf)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(result)
			}
			a.printf("Weekly execution: %v%% (%v / %v points)\n", result["execution_percentage"], result["total_points"], result["max_possible_points"])
			a.printf("Pace: %v%%; tier: %v\n", result["pace_percentage"], result["rating"])
			a.printf("Cycle %d, %v to %v; snapshot %v\n", cycleID, result["period_start_date"], result["period_end_date"], result["as_of_date"])
			for _, key := range []string{"risk", "leverage", "streaks"} {
				if text := describe(result[key]); text != "" {
					a.println(text)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&cycleID, "cycle", 0, "Cycle id (default: the cycle active today)")
	cmd.Flags().StringVar(&week, "week", "", "Any day in the week to score, YYYY-MM-DD (default this week)")
	cmd.Flags().StringVar(&asOf, "as-of", "", "Snapshot date, YYYY-MM-DD (default today)")
	return cmd
}

func newInsightCmd(a *App) *cobra.Command {
	var week string
	cmd := &cobra.Command{
		Use:   "insight",
		Short: "Show this week's coaching: assessment, guidance, risk and leverage",
		Long: `Show the coaching insight for a week (the dashboard's Progress card):
an assessment, guidance, the main risk and the highest-leverage move.
"source" says whether it came from the coaching model or the built-in
summary; "stale" means a fresher one is being generated — ask again shortly.`,
		Example: `  cadence insight
  cadence insight --week 2026-09-21 --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validDate("--week", week); err != nil {
				return err
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			insight, err := c.ProgressInsight(a.ctx(), week)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(insight)
			}
			a.println(accentStyle.Render("Coaching for the week of " + insight.WeekStart))
			for _, part := range []struct {
				label string
				value *string
			}{{"Assessment", insight.Assessment}, {"Guidance", insight.Guidance}, {"Risk", insight.Risk}, {"Leverage", insight.Leverage}} {
				if text := strings.TrimSpace(deref(part.value)); text != "" {
					a.printf("%s: %s\n", part.label, text)
				}
			}
			note := "source: " + insight.Source
			if insight.Stale {
				note += " (refreshing — run again in a minute for the latest)"
			}
			a.println(mutedStyle.Render(note))
			return nil
		},
	}
	cmd.Flags().StringVar(&week, "week", "", "Any day in the week, YYYY-MM-DD (default this week)")
	return cmd
}

// weeklyReviewFields maps flags to the four weekly review prompts.
var weeklyReviewFields = []struct{ flag, field, label, prompt string }{
	{"win", "biggest_win", "Biggest win", "What moved the needle?"},
	{"derail", "derail_root_cause", "Derail cause", "What got in the way?"},
	{"change", "one_change_next_week", "One change", "The single adjustment for next week"},
	{"constraint", "next_week_constraint", "Constraint", "Capacity limits next week?"},
}

func newWeekCmd(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "week",
		Short: "Weekly review: show or save the four prompts",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	review := &cobra.Command{
		Use:   "review",
		Short: "Show or save a week's review (win, derail, change, constraint)",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var showWeek string
	show := &cobra.Command{
		Use:   "show",
		Short: "Show a week's review and last week's commitment",
		Example: `  cadence week review show
  cadence week review show --week 2026-09-21 --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validWeek(showWeek); err != nil {
				return err
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			result, err := c.WeeklyReview(a.ctx(), weekOrCurrent(showWeek))
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(result)
			}
			a.renderWeeklyReview(result)
			return nil
		},
	}
	show.Flags().StringVar(&showWeek, "week", "", "Any day in the week, YYYY-MM-DD, or current (default)")

	var setWeek string
	values := make([]string, len(weeklyReviewFields))
	set := &cobra.Command{
		Use:   "set",
		Short: "Save any of the four weekly review answers",
		Long: `Save a week's review. Pass any subset; answers you leave out are kept.

  --win         Biggest win: what moved the needle?
  --derail      Derail cause: what got in the way?
  --change      One change: the single adjustment for next week
  --constraint  Constraint: capacity limits next week?

Next week's review shows --change back as "last week's commitment".`,
		Example: `  cadence week review set --win "Four deep-work blocks" --derail "Late nights" \
    --change "Phone out of the bedroom" --constraint "Travel Thu-Fri"
  cadence week review set --week 2026-09-21 --change "Plan Sunday night"`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validWeek(setWeek); err != nil {
				return err
			}
			fields := map[string]string{}
			for i, f := range weeklyReviewFields {
				if cmd.Flags().Changed(f.flag) {
					fields[f.field] = values[i]
				}
			}
			if len(fields) == 0 {
				return &UsageError{Msg: "nothing to save: pass --win, --derail, --change and/or --constraint", Hint: "Run `cadence week review set --help` for usage."}
			}
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			result, err := c.SaveWeeklyReview(a.ctx(), weekOrCurrent(setWeek), fields)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(result)
			}
			a.printf("Saved the review for the week of %s.\n", result.WeekStart)
			a.renderWeeklyReview(result)
			return nil
		},
	}
	set.Flags().StringVar(&setWeek, "week", "", "Any day in the week, YYYY-MM-DD, or current (default)")
	for i, f := range weeklyReviewFields {
		set.Flags().StringVar(&values[i], f.flag, "", f.label+": "+f.prompt)
	}
	review.AddCommand(show, set)
	cmd.AddCommand(review)
	return cmd
}

func validWeek(week string) error {
	if week == "" || week == "current" {
		return nil
	}
	return validDate("--week", week)
}

func weekOrCurrent(week string) string {
	if week == "" {
		return "current"
	}
	return week
}

func (a *App) renderWeeklyReview(result api.WeeklyReviewResponse) {
	a.println(accentStyle.Render("Weekly review — week of " + result.WeekStart))
	if result.PreviousCommitment != nil && strings.TrimSpace(*result.PreviousCommitment) != "" {
		a.printf("Last week's commitment: %s\n", *result.PreviousCommitment)
	}
	r := result.WeeklyReview
	if r == nil {
		a.println(emptyStyle.Render("Not reviewed yet. Save one with `cadence week review set`."))
		return
	}
	for _, pair := range []struct {
		label string
		value *string
	}{{"Biggest win", r.BiggestWin}, {"Derail cause", r.DerailRootCause}, {"One change", r.OneChangeNextWeek}, {"Constraint", r.NextWeekConstraint}} {
		value := strings.TrimSpace(deref(pair.value))
		if value == "" {
			value = mutedStyle.Render("—")
		}
		a.printf("%s: %s\n", pair.label, value)
	}
}
