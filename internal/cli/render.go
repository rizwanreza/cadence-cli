package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/rizwanreza/cadence-cli/internal/api"
)

var (
	accentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	emptyStyle  = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("245"))
)

// todayItem is the machine-readable shape emitted by `today --json` and in
// `status --json`. Value is a plain JSON number (or null), never a string.
type todayItem struct {
	ID          int      `json:"id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	GoalType    string   `json:"goal_type"`
	Kind        string   `json:"kind"`
	InputKind   string   `json:"input_kind"`
	ScoringMode string   `json:"scoring_mode"`
	Frequency   string   `json:"frequency"`
	TargetValue float64  `json:"target_value"`
	Date        string   `json:"date"`
	Completed   bool     `json:"completed"`
	Value       *float64 `json:"value"`
	Unit        *string  `json:"unit"`
}

func newTodayItem(s api.GoalStatus) todayItem {
	item := todayItem{
		ID: s.ID, Slug: s.Slug, Name: s.Name, GoalType: s.GoalType, Kind: s.Kind(),
		InputKind: s.InputKind, ScoringMode: s.ScoringMode, Frequency: s.Frequency,
		TargetValue: s.TargetValue, Date: s.Date, Completed: s.Completed, Unit: s.Unit,
	}
	if s.Value != nil {
		v := s.Value.Float()
		item.Value = &v
	}
	return item
}

func todayItems(statuses []api.GoalStatus) []todayItem {
	items := make([]todayItem, 0, len(statuses))
	for _, s := range statuses {
		items = append(items, newTodayItem(s))
	}
	return items
}

// kindLabel turns the machine kind into a compact column label.
func kindLabel(kind string) string {
	switch kind {
	case "pass_fail":
		return "yes/no"
	case "numeric":
		return "threshold"
	case "count":
		return "cumulative"
	default:
		return kind
	}
}

// todayProgress renders "value / target unit" (a dash for yes/no goals).
func todayProgress(s api.GoalStatus) string {
	if s.Kind() == "pass_fail" {
		return "—"
	}
	value := 0.0
	if s.Value != nil {
		value = s.Value.Float()
	}
	progress := fmt.Sprintf("%s / %s", formatNumber(value), formatNumber(s.TargetValue))
	if s.Unit != nil && *s.Unit != "" {
		progress += " " + *s.Unit
	}
	return progress
}

// todayStatus reflects the real completed flag.
func todayStatus(s api.GoalStatus) string {
	if s.Completed {
		return "✓ done"
	}
	return "✗ incomplete"
}

// formatNumber prints a float without trailing zeros: 105, 2.5.
func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func filterStatuses(statuses []api.GoalStatus, identifier string) []api.GoalStatus {
	id, idErr := strconv.Atoi(identifier)
	filtered := make([]api.GoalStatus, 0, 1)
	for _, s := range statuses {
		if s.Slug == identifier || (idErr == nil && s.ID == id) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func matchGoal(goals []api.Goal, identifier string) (api.Goal, error) {
	if id, err := strconv.Atoi(identifier); err == nil {
		for _, g := range goals {
			if g.ID == id {
				return g, nil
			}
		}
	} else {
		for _, g := range goals {
			if g.Slug == identifier {
				return g, nil
			}
		}
		for _, g := range goals {
			if g.Name == identifier {
				return g, nil
			}
		}
	}
	return api.Goal{}, notFoundf("goal not found: %s (run `cadence goals` to list ids and slugs)", identifier)
}

func weeklyCap(g api.Goal) int {
	if g.WeeklyCap != nil {
		return *g.WeeklyCap
	}
	return 7
}

func weeklyTarget(g api.Goal) string {
	weight := g.PointsPerUnit
	if weight <= 0 {
		weight = 1
	}
	if g.Status().Kind() == "count" {
		points := float64(weeklyCap(g))
		if g.WeeklyCap == nil {
			points *= weight
		}
		return fmt.Sprintf("%s points/week (%s points per %s)", formatNumber(points), formatNumber(weight), goalTarget(g))
	}
	return fmt.Sprintf("%d scored days/week (%s points/day)", weeklyCap(g), formatNumber(weight))
}

func goalTarget(g api.Goal) string {
	value := formatNumber(g.Target)
	if g.Unit != nil && *g.Unit != "" {
		value += " " + *g.Unit
	}
	return value
}

func renderEntrySummary(g api.Goal, entry api.Entry) string {
	date := mutedStyle.Render(entry.Date)
	status := g.Status()
	status.Value = &entry.Value
	status.Completed = entry.Completed
	if status.Kind() == "pass_fail" {
		return fmt.Sprintf("%s: %s for %s.", accentStyle.Render(g.Name), todayStatus(status), date)
	}
	return fmt.Sprintf("%s: %s — %s for %s.", accentStyle.Render(g.Name), todayProgress(status), todayStatus(status), date)
}

func renderReviewSummary(review api.Review) string {
	var b strings.Builder
	b.WriteString(accentStyle.Render("12-Week Year Review") + "\n")
	fmt.Fprintf(&b, "%s to %s\n", review.Period.StartDate, review.Period.EndDate)
	fmt.Fprintf(&b, "State: %s | Execution: %.1f%% | Grade: %s\n", review.State, review.Summary.ExecutionPercentage, review.Summary.Grade)
	fmt.Fprintf(&b, "Takeaway: %s\n\n", review.Summary.Takeaway)

	b.WriteString(accentStyle.Render("Performance") + "\n")
	fmt.Fprintf(&b, "Average weekly execution: %.1f%%\n", review.Summary.AverageWeeklyExecution)
	fmt.Fprintf(&b, "Strong weeks: %d\n", review.Summary.StrongWeeksCount)
	fmt.Fprintf(&b, "Best week: W%d at %.1f%%\n", review.Summary.BestWeek.Number, review.Summary.BestWeek.ExecutionPercentage)
	fmt.Fprintf(&b, "Lowest week: W%d at %.1f%%\n\n", review.Summary.WorstWeek.Number, review.Summary.WorstWeek.ExecutionPercentage)

	b.WriteString(accentStyle.Render("Goals") + "\n")
	for _, g := range topGoalReviews(review.GoalReviews, 3) {
		fmt.Fprintf(&b, "- %s: %.1f%%, %s, %s\n", g.GoalName, g.ExecutionPercentage, g.TrendLabel, g.CoachingInsight)
	}
	b.WriteString("\n")

	b.WriteString(accentStyle.Render("Learnings") + "\n")
	fmt.Fprintf(&b, "- What worked: %s\n", review.Learnings.WhatWorked)
	fmt.Fprintf(&b, "- What limited execution: %s\n", review.Learnings.WhatLimitedExecution)
	fmt.Fprintf(&b, "- What needs redesign: %s\n", review.Learnings.WhatNeedsRedesign)
	fmt.Fprintf(&b, "- What to carry forward: %s\n", review.Learnings.WhatToCarryForward)
	fmt.Fprintf(&b, "- Next cycle adjustment: %s\n\n", review.Learnings.NextCycleAdjustment)

	b.WriteString(accentStyle.Render("Reflection Prompts") + "\n")
	for _, section := range review.ReflectionSections {
		fmt.Fprintf(&b, "- %s: %s\n", section.Title, section.Question)
		answer := section.Response
		if answer == nil && review.Reflection != nil {
			answer = review.Reflection[section.Key]
		}
		if answer != nil && strings.TrimSpace(*answer) != "" {
			fmt.Fprintf(&b, "  Your answer: %s\n", *answer)
		}
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  Insight: %s", section.SystemInsight)) + "\n")
	}
	return b.String()
}

func topGoalReviews(reviews []api.ReviewGoal, limit int) []api.ReviewGoal {
	cloned := append([]api.ReviewGoal(nil), reviews...)
	sort.SliceStable(cloned, func(i, j int) bool {
		return cloned[i].ExecutionPercentage > cloned[j].ExecutionPercentage
	})
	if len(cloned) <= limit {
		return cloned
	}
	return cloned[:limit]
}

func tableStyles() table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	styles.Cell = styles.Cell.Foreground(lipgloss.Color("252"))
	return styles
}

func renderTable(columns []table.Column, rows []table.Row) string {
	t := table.New(table.WithColumns(columns), table.WithRows(rows), table.WithFocused(false))
	t.SetHeight(len(rows) + 1)
	t.SetStyles(tableStyles())
	return t.View()
}

// renderPlan prints a cycle/plan payload for humans.
func renderPlan(plan map[string]any) string {
	var parsed struct {
		ID      int        `json:"id"`
		Name    string     `json:"name"`
		Start   string     `json:"start_date"`
		End     string     `json:"end_date"`
		Status  string     `json:"status"`
		Preview bool       `json:"preview"`
		Notes   *string    `json:"notes"`
		Goals   []api.Goal `json:"goals"`
	}
	data, _ := json.Marshal(plan)
	_ = json.Unmarshal(data, &parsed)
	var b strings.Builder
	if parsed.Preview {
		b.WriteString("Preview — nothing saved or activated.\n")
	}
	fmt.Fprintf(&b, "%s [%s] — %s to %s", parsed.Name, parsed.Status, parsed.Start, parsed.End)
	if parsed.ID > 0 {
		fmt.Fprintf(&b, " (cycle %d)", parsed.ID)
	}
	b.WriteString("\n")
	for i, g := range parsed.Goals {
		fmt.Fprintf(&b, "%d. %s [%s] — %s; %s", i+1, g.Name, kindLabel(g.Status().Kind()), goalTarget(g), weeklyTarget(g))
		if g.ID > 0 {
			fmt.Fprintf(&b, " (goal %d, key %s)", g.ID, g.Slug)
		}
		b.WriteString("\n")
		if g.Description != "" {
			fmt.Fprintf(&b, "   %s\n", g.Description)
		}
	}
	if parsed.Notes != nil && *parsed.Notes != "" {
		fmt.Fprintf(&b, "Notes: %s\n", *parsed.Notes)
	}
	if parsed.Status == "draft" {
		b.WriteString("Draft only. Activate explicitly (up to three weeks before its start date, and before it ends).\n")
	}
	return b.String()
}

// exportDocument strips response metadata from a plan. Goal keys and ordering
// remain unchanged for retry safety.
func exportDocument(plan map[string]any) map[string]any {
	result := map[string]any{}
	for _, key := range []string{"schema_version", "plan_key", "name", "start_date", "notes", "goals"} {
		if value, ok := plan[key]; ok {
			result[key] = value
		}
	}
	return result
}

// describe renders a free-form server value (string, object or null).
func describe(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case map[string]any:
		// Scorecard alerts: {"message": "...", ...}
		if msg, ok := value["message"].(string); ok {
			return msg
		}
		data, _ := json.Marshal(value)
		return string(data)
	default:
		data, _ := json.Marshal(value)
		return string(data)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
