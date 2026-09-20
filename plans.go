package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func (g goal) status() goalStatus {
	return goalStatus{GoalType: g.GoalType, InputKind: g.InputKind, ScoringMode: g.ScoringMode, TargetValue: g.Target, Unit: g.Unit}
}

func weeklyCap(g goal) int {
	if g.WeeklyCap != nil {
		return *g.WeeklyCap
	}
	return 7
}

func weeklyTarget(g goal) string {
	weight := g.PointsPerUnit
	if weight <= 0 {
		weight = 1
	}
	if g.status().kind() == "count" {
		points := float64(weeklyCap(g))
		if g.WeeklyCap == nil {
			points *= weight
		}
		return fmt.Sprintf("%s points/week (%s points per %s)", formatNumber(points), formatNumber(weight), goalTarget(g))
	}
	return fmt.Sprintf("%d scored days/week (%s points/day)", weeklyCap(g), formatNumber(weight))
}

func goalTarget(g goal) string {
	value := formatNumber(g.Target)
	if g.Unit != nil && *g.Unit != "" {
		value += " " + *g.Unit
	}
	return value
}

func (c *client) requestJSON(method, path string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := c.newRequest(method, path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if result == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

func cyclesCommand(c *client, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return listTwelveWeekYears(c, args)
	}
	command := args[0]
	flags := flag.NewFlagSet("cycles "+command, flag.ContinueOnError)
	id := flags.Int("id", 0, "Cycle id")
	key := flags.String("key", "", "Stable client plan key (required when creating)")
	name := flags.String("name", "", "Cycle name")
	start := flags.String("start", "", "Start date YYYY-MM-DD")
	notes := flags.String("notes", "", "Cycle notes")
	file := flags.String("file", "", "JSON plan file, or - for stdin")
	dryRun := flags.Bool("dry-run", false, "Validate and preview without saving")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	visited := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	var plan map[string]any
	path := "/api/twelve_week_years"
	if *id > 0 {
		path += "/" + strconv.Itoa(*id)
	}

	switch command {
	case "show", "export", "activate":
		if *id <= 0 {
			return fmt.Errorf("%s requires -id", command)
		}
		method := http.MethodGet
		if command == "activate" {
			if *dryRun {
				return fmt.Errorf("activate does not support -dry-run; inspect the draft with cycles show")
			}
			method = http.MethodPost
			path += "/activation"
		}
		if err := c.requestJSON(method, path, nil, &plan); err != nil {
			return err
		}
		if command == "export" {
			return printJSON(exportDocument(plan))
		}
	case "create", "import", "update":
		if command == "import" {
			if *file == "" {
				return fmt.Errorf("import requires -file")
			}
			var data []byte
			var err error
			if *file == "-" {
				data, err = io.ReadAll(os.Stdin)
			} else {
				data, err = os.ReadFile(*file)
			}
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &plan); err != nil {
				return fmt.Errorf("invalid plan JSON: %w", err)
			}
			if plan == nil {
				return fmt.Errorf("plan must be a JSON object")
			}
		} else if command == "update" {
			if *id <= 0 {
				return fmt.Errorf("update requires -id")
			}
			if err := c.requestJSON(http.MethodGet, path, nil, &plan); err != nil {
				return err
			}
		} else {
			if *id != 0 {
				return fmt.Errorf("create does not accept -id; use update")
			}
			if *key == "" || *name == "" || *start == "" {
				return fmt.Errorf("create requires -key, -name, and -start")
			}
			plan = map[string]any{"schema_version": 1, "goals": []any{}}
		}
		if visited["key"] {
			plan["plan_key"] = *key
		}
		if visited["name"] {
			plan["name"] = *name
		}
		if visited["start"] {
			plan["start_date"] = *start
		}
		if visited["notes"] {
			plan["notes"] = *notes
		}
		method := http.MethodPost
		if *id > 0 {
			method = http.MethodPatch
		}
		var response map[string]any
		if err := c.requestJSON(method, path, map[string]any{"plan": exportDocument(plan), "dry_run": *dryRun}, &response); err != nil {
			return err
		}
		plan = response
	default:
		return fmt.Errorf("unknown cycles command: %s", command)
	}
	if *jsonOutput {
		return printJSON(plan)
	}
	return renderPlan(plan)
}

// Remove response metadata. Goal keys and ordering remain unchanged for retry
// safety; copying into a new cycle requires a new plan key and start date.
func exportDocument(plan map[string]any) map[string]any {
	result := map[string]any{}
	for _, key := range []string{"schema_version", "plan_key", "name", "start_date", "notes", "goals"} {
		if value, ok := plan[key]; ok {
			result[key] = value
		}
	}
	return result
}

func renderPlan(plan map[string]any) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	var parsed struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Start   string `json:"start_date"`
		End     string `json:"end_date"`
		Status  string `json:"status"`
		Preview bool   `json:"preview"`
		Goals   []goal `json:"goals"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	if parsed.Preview {
		fmt.Println("Preview — nothing saved or activated.")
	}
	fmt.Printf("%s [%s] — %s to %s", parsed.Name, parsed.Status, parsed.Start, parsed.End)
	if parsed.ID > 0 {
		fmt.Printf(" (cycle %d)", parsed.ID)
	}
	fmt.Println()
	for i, g := range parsed.Goals {
		fmt.Printf("%d. %s [%s] — %s; %s", i+1, g.Name, kindLabel(g.status().kind()), goalTarget(g), weeklyTarget(g))
		if g.ID > 0 {
			fmt.Printf(" (goal %d, key %s)", g.ID, g.Slug)
		}
		fmt.Println()
		if g.Description != "" {
			fmt.Printf("   %s\n", g.Description)
		}
	}
	if parsed.Status == "draft" {
		fmt.Println("Draft only. Activate explicitly on or after its start date, before it ends.")
	}
	return nil
}

func goalsCommand(c *client, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return listGoals(c, args)
	}
	command := args[0]
	flags := flag.NewFlagSet("goals "+command, flag.ContinueOnError)
	cycleID := flags.Int("cycle", 0, "Cycle id")
	identifier := flags.String("goal", "", "Goal id or stable key")
	key := flags.String("key", "", "Stable goal key for creation")
	name := flags.String("name", "", "Goal name")
	notes := flags.String("notes", "", "Goal notes")
	input := flags.String("input", "checkbox", "checkbox or number")
	scoring := flags.String("scoring", "threshold", "threshold or cumulative")
	target := flags.Int("target", 1, "Target value")
	cap := flags.Int("weekly-cap", 7, "Threshold days/week or cumulative weekly point cap")
	unit := flags.String("unit", "", "Unit label")
	order := flags.String("order", "", "Every goal id in desired order, separated by commas")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *cycleID <= 0 {
		return fmt.Errorf("%s requires -cycle", command)
	}
	visited := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if command == "reorder" {
		if *order == "" {
			return fmt.Errorf("reorder requires -order")
		}
		var ids []int
		for _, raw := range strings.Split(*order, ",") {
			id, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || id <= 0 {
				return fmt.Errorf("order must contain positive goal ids")
			}
			ids = append(ids, id)
		}
		var goals []goal
		path := fmt.Sprintf("/api/twelve_week_years/%d/goal_order", *cycleID)
		if err := c.requestJSON(http.MethodPatch, path, map[string]any{"order": ids}, &goals); err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(goals)
		}
		fmt.Println("Goal order saved.")
		return nil
	}
	if command != "add" && command != "update" {
		return fmt.Errorf("unknown goals command: %s", command)
	}
	attributes := map[string]any{}
	values := map[string]any{"name": *name, "description": *notes, "input_kind": *input, "scoring_mode": *scoring, "target_value": *target, "weekly_cap": *cap, "unit": *unit, "key": *key}
	flagNames := map[string]string{"name": "name", "description": "notes", "input_kind": "input", "scoring_mode": "scoring", "target_value": "target", "weekly_cap": "weekly-cap", "unit": "unit", "key": "key"}
	for field, value := range values {
		if command == "add" || visited[flagNames[field]] {
			attributes[field] = value
		}
	}
	path := fmt.Sprintf("/api/goals?cycle_id=%d", *cycleID)
	method := http.MethodPost
	if command == "add" {
		if *key == "" || *name == "" {
			return fmt.Errorf("add requires -key and -name")
		}
	} else {
		if *identifier == "" {
			return fmt.Errorf("update requires -goal")
		}
		goals, err := c.fetchGoalsFor("", *cycleID)
		if err != nil {
			return err
		}
		g, err := matchGoal(goals, *identifier)
		if err != nil {
			return err
		}
		path = fmt.Sprintf("/api/goals/%d?cycle_id=%d", g.ID, *cycleID)
		method = http.MethodPatch
	}
	var result goal
	if err := c.requestJSON(method, path, map[string]any{"goal": attributes}, &result); err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(result)
	}
	fmt.Printf("%s saved: %s, %s, %s (goal %d).\n", result.Name, kindLabel(result.status().kind()), goalTarget(result), weeklyTarget(result), result.ID)
	return nil
}

func scoreCommand(c *client, args []string) error {
	flags := flag.NewFlagSet("score", flag.ContinueOnError)
	cycleID := flags.Int("cycle", 0, "Cycle id")
	week := flags.String("week", "", "Week containing YYYY-MM-DD")
	asOf := flags.String("as-of", "", "Snapshot date YYYY-MM-DD")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *cycleID <= 0 {
		return fmt.Errorf("score requires -cycle")
	}
	path := fmt.Sprintf("/api/twelve_week_years/%d/scorecard?week_start=%s&as_of_date=%s", *cycleID, *week, *asOf)
	var result map[string]any
	if err := c.requestJSON(http.MethodGet, path, nil, &result); err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(result)
	}
	fmt.Printf("Weekly execution: %v%% (%v / %v points)\n", result["execution_percentage"], result["total_points"], result["max_possible_points"])
	fmt.Printf("Pace: %v%%; Sunday projection: %v%%\n", result["pace_percentage"], result["projected_execution_percentage"])
	fmt.Printf("Cycle %d, %v to %v; snapshot %v\n", *cycleID, result["period_start_date"], result["period_end_date"], result["as_of_date"])
	return nil
}
