package cli

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rizwanreza/cadence-cli/internal/api"
	"github.com/spf13/cobra"
)

// statusFlags are the booleans an agent branches on (see the skill's
// decision tree). Keep names stable.
type statusFlags struct {
	NoActiveCycle      bool `json:"no_active_cycle"`
	AwaitingReview     bool `json:"awaiting_review"`
	FinalStretch       bool `json:"final_stretch"`
	Lapsed             bool `json:"lapsed"`
	FreshStartOffer    bool `json:"fresh_start_offer"`
	SuccessorDraft     bool `json:"successor_draft"`
	SuccessorScheduled bool `json:"successor_scheduled"`
	WeeklyReviewDue    bool `json:"weekly_review_due"`
	CheckinsPending    bool `json:"checkins_pending"`
}

type statusWeek struct {
	WeekStart          string  `json:"week_start"`
	ReviewSaved        bool    `json:"review_saved"`
	PreviousCommitment *string `json:"previous_commitment"`
	// DueReviewWeek is the week whose review is due (last week on Monday).
	DueReviewWeek string `json:"due_review_week,omitempty"`
}

type statusSuggestion struct {
	Action  string `json:"action"`
	Command string `json:"command"`
	Reason  string `json:"reason"`
}

type statusReport struct {
	Today          string             `json:"today"`
	Weekday        string             `json:"weekday"`
	Now            string             `json:"now,omitempty"`
	User           api.User           `json:"user"`
	ActiveCycle    *api.Cycle         `json:"active_cycle"`
	ClosingCycle   *api.Cycle         `json:"closing_cycle"`
	SuccessorCycle *api.Cycle         `json:"successor_cycle"`
	Week           *statusWeek        `json:"week"`
	Checkins       []todayItem        `json:"checkins"`
	CheckinsDone   int                `json:"checkins_done"`
	CheckinsTotal  int                `json:"checkins_total"`
	Flags          statusFlags        `json:"flags"`
	Suggestions    []statusSuggestion `json:"suggestions"`
}

func newStatusCmd(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Where you are today: cycle, week, check-ins, and what's due",
		Long: `Orientation for humans and agents. Shows today's date in your account's time
zone, the active cycle and week N of M, today's check-ins, last week's
commitment, and flags for what needs attention:

  awaiting_review     a cycle ended and its review is waiting
  final_stretch       the last days of the active cycle: plan the next one
  lapsed              no check-ins for a week: offer a fresh start
  fresh_start_offer   the server would show the "start fresh" banner
  successor_draft     a next-cycle draft exists but is not scheduled
  weekly_review_due   Sunday/Monday and the week's review is not saved
  checkins_pending    goals still unchecked today

Agents: run ` + "`cadence status --json`" + ` first, every session.`,
		Example: `  cadence status
  cadence status --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.authedClient()
			if err != nil {
				return err
			}
			report, err := a.buildStatus(c)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(report)
			}
			a.renderStatus(report)
			return nil
		},
	}
}

func (a *App) buildStatus(c *api.Client) (statusReport, error) {
	me, err := c.Me(a.ctx())
	if err != nil {
		return statusReport{}, err
	}
	report := statusReport{
		Today: me.Today, Now: me.Now, User: me.User,
		ActiveCycle: me.ActiveCycle, ClosingCycle: me.ClosingCycle, SuccessorCycle: me.SuccessorCycle,
		Checkins: []todayItem{}, Suggestions: []statusSuggestion{},
	}
	today, _ := time.Parse("2006-01-02", me.Today)
	report.Weekday = today.Weekday().String()

	flags := &report.Flags
	flags.NoActiveCycle = me.ActiveCycle == nil
	flags.FreshStartOffer = me.FreshStartOffer
	if me.ClosingCycle != nil && me.ClosingCycle.AwaitingReview {
		flags.AwaitingReview = true
	}
	if cy := me.ActiveCycle; cy != nil {
		flags.FinalStretch = cy.FinalStretch
		flags.Lapsed = cy.Lapsed
		if cy.AwaitingReview {
			flags.AwaitingReview = true
		}
	}
	if s := me.SuccessorCycle; s != nil {
		flags.SuccessorDraft = s.Status == "draft"
		flags.SuccessorScheduled = s.Status == "scheduled" || (s.ActivatedAt != nil && s.Status != "draft")
	}

	if me.ActiveCycle != nil {
		statuses, err := c.Completions(a.ctx(), me.Today)
		if err != nil {
			return statusReport{}, err
		}
		report.Checkins = todayItems(statuses)
		report.CheckinsTotal = len(statuses)
		for _, s := range statuses {
			if s.Completed {
				report.CheckinsDone++
			}
		}
		flags.CheckinsPending = report.CheckinsDone < report.CheckinsTotal

		week, err := a.weekStatus(c, today, me.ActiveCycle)
		if err != nil {
			return statusReport{}, err
		}
		report.Week = week
		if week != nil && week.DueReviewWeek != "" {
			flags.WeeklyReviewDue = true
		}
	}
	report.Suggestions = suggestions(report)
	return report, nil
}

// weekStatus reads this week's review (and last week's on a Monday). A 404
// from an older server is tolerated so status still works.
func (a *App) weekStatus(c *api.Client, today time.Time, cycle *api.Cycle) (*statusWeek, error) {
	if today.IsZero() {
		return nil, nil
	}
	current, err := c.WeeklyReview(a.ctx(), today.Format("2006-01-02"))
	if err != nil {
		if api.IsStatus(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	week := &statusWeek{WeekStart: current.WeekStart, ReviewSaved: current.WeeklyReview != nil, PreviousCommitment: current.PreviousCommitment}
	switch today.Weekday() {
	case time.Sunday:
		if !week.ReviewSaved {
			week.DueReviewWeek = current.WeekStart
		}
	case time.Monday:
		lastWeek := today.AddDate(0, 0, -7)
		if cycle != nil && cycle.StartDate != "" && cycle.StartDate > today.AddDate(0, 0, -1).Format("2006-01-02") {
			break // the cycle started today: there's no last week to review
		}
		previous, err := c.WeeklyReview(a.ctx(), lastWeek.Format("2006-01-02"))
		if err != nil {
			if api.IsStatus(err, http.StatusNotFound) || api.IsStatus(err, http.StatusUnprocessableEntity) {
				break
			}
			return nil, err
		}
		if previous.WeeklyReview == nil {
			week.DueReviewWeek = previous.WeekStart
		}
	}
	return week, nil
}

func suggestions(r statusReport) []statusSuggestion {
	out := []statusSuggestion{}
	add := func(action, command, reason string) {
		out = append(out, statusSuggestion{Action: action, Command: command, Reason: reason})
	}
	f := r.Flags
	if f.AwaitingReview {
		id := 0
		if r.ClosingCycle != nil {
			id = r.ClosingCycle.ID
		} else if r.ActiveCycle != nil {
			id = r.ActiveCycle.ID
		}
		add("cycle_review", fmt.Sprintf("cadence review --id %d", id), "The last cycle ended and its review is waiting.")
	}
	if f.Lapsed || f.FreshStartOffer {
		add("fresh_start", "cadence cycles fresh-start", "No check-ins for a week; offer a fresh 12 weeks from today (or `cadence cycles fresh-start dismiss`).")
	}
	if f.WeeklyReviewDue && r.Week != nil {
		add("weekly_review", "cadence week review set --week "+r.Week.DueReviewWeek, "It's "+r.Weekday+" and that week's review isn't saved.")
	}
	if f.FinalStretch && r.SuccessorCycle == nil {
		add("plan_next_cycle", fmt.Sprintf("cadence cycles next --id %d --start after", r.ActiveCycle.ID), "The cycle is in its final stretch and nothing is planned next.")
	}
	if f.SuccessorDraft && r.SuccessorCycle != nil {
		add("activate_next_cycle", fmt.Sprintf("cadence cycles show --id %d", r.SuccessorCycle.ID), "A next-cycle draft exists but isn't scheduled; review it, then activate after confirming.")
	}
	if f.NoActiveCycle && r.SuccessorCycle == nil && !f.AwaitingReview {
		add("plan_cycle", "cadence cycles create --key <key> --name <name> --start <date> --dry-run", "There is no active cycle.")
	}
	if f.CheckinsPending {
		add("check_in", "cadence today", fmt.Sprintf("%d of %d goals checked in today.", r.CheckinsDone, r.CheckinsTotal))
	}
	return out
}

func (a *App) renderStatus(r statusReport) {
	date := r.Today
	if t, err := time.Parse("2006-01-02", r.Today); err == nil {
		date = t.Format("Monday, Jan 2 2006")
	}
	tz := ""
	if r.User.TimeZone != "" {
		tz = " (" + r.User.TimeZone + ")"
	}
	a.println(accentStyle.Render("Today is " + date + tz))
	if cy := r.ActiveCycle; cy != nil {
		a.printf("Cycle: %s — week %d of %d (%s to %s)\n", cy.Name, cy.CurrentWeekNumber, cy.TotalWeeks, cy.StartDate, cy.EndDate)
	} else {
		a.println("Cycle: none active today")
	}
	if s := r.SuccessorCycle; s != nil {
		a.printf("Next: %s [%s] from %s (cycle %d)\n", s.Name, s.Status, s.StartDate, s.ID)
	}
	if r.CheckinsTotal > 0 {
		a.printf("\nCheck-ins: %d / %d done\n", r.CheckinsDone, r.CheckinsTotal)
		for _, item := range r.Checkins {
			mark := "✗"
			if item.Completed {
				mark = "✓"
			}
			line := fmt.Sprintf("  %s %s", mark, item.Name)
			if item.Kind != "pass_fail" {
				value := 0.0
				if item.Value != nil {
					value = *item.Value
				}
				line += fmt.Sprintf("  %s / %s", formatNumber(value), formatNumber(item.TargetValue))
				if item.Unit != nil && *item.Unit != "" {
					line += " " + *item.Unit
				}
			}
			a.println(line)
		}
	}
	if r.Week != nil && r.Week.PreviousCommitment != nil && strings.TrimSpace(*r.Week.PreviousCommitment) != "" {
		a.printf("\nLast week's commitment: %s\n", *r.Week.PreviousCommitment)
	}
	if len(r.Suggestions) > 0 {
		a.println()
		a.println(accentStyle.Render("Up next"))
		for _, s := range r.Suggestions {
			a.printf("  • %s\n    %s\n", s.Reason, mutedStyle.Render(s.Command))
		}
	}
}
