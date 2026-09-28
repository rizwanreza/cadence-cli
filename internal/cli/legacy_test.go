package cli

// Tests ported from the pre-cobra CLI (cli/*_test.go in the monorepo). They
// deliberately use the old single-dash flag spelling to prove it still works.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rizwanreza/cadence-cli/internal/api"
)

func TestAddSendsDeltaWithoutReadingProgress(t *testing.T) {
	testEnv(t)
	requests := 0
	f := serve(t, func(r *http.Request) string {
		requests++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing authentication")
		}
		if r.Method == http.MethodGet {
			if r.URL.Path != "/goals.json" {
				t.Fatalf("unexpected progress read: %s", r.URL)
			}
			return `[{"id":26,"slug":"protein","name":"Protein","goal_type":"boolean","input_kind":"number","target_value":160,"unit":"g"}]`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/goals/26/entries/today/increments.json" {
			t.Fatalf("wrong increment request: %s %s", r.Method, r.URL)
		}
		var body map[string]map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["increment"]["value"] != "40.25" {
			t.Fatalf("expected unaltered decimal delta: %v", body)
		}
		if !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "cli-") {
			t.Fatalf("add must send an Idempotency-Key, got %q", r.Header.Get("Idempotency-Key"))
		}
		return `{"goal_id":26,"date":"2026-06-24","previous_value":73,"value":113.25,"completed":false}`
	})
	res := run(t, f, "add", "-goal", "protein", "-value", "40.25")
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	if requests != 2 {
		t.Fatalf("expected only goal lookup and increment; got %d requests", requests)
	}
	mustContain(t, res.stdout, "Added 40.25", "73 → 113.25", "113.25 / 160 g", "incomplete")
}

func TestNumericCompleteNeverWrites(t *testing.T) {
	testEnv(t)
	for _, goalJSON := range []string{
		`{"goal_type":"boolean","input_kind":"number","target_value":160}`,
		`{"goal_type":"boolean","input_kind":"number","target_value":1}`,
		`{"goal_type":"count","target_value":1}`,
		`{"goal_type":"duration","target_value":30}`,
		`{"goal_type":"boolean","target_value":160}`,
	} {
		f := serve(t, func(r *http.Request) string {
			if r.Method != http.MethodGet {
				t.Fatalf("numeric complete must never write: %s", r.Method)
			}
			return "[" + strings.Replace(goalJSON, "{", `{"id":26,"slug":"protein","name":"Protein",`, 1) + "]"
		})
		res := run(t, f, "complete", "-goal", "protein", "-value", "1")
		if res.code != ExitUsage {
			t.Fatalf("expected usage exit, got %d", res.code)
		}
		mustContain(t, res.stderr, "no value was written", "cadence add", "cadence set")
	}
}

func TestCompleteStillSupportsCheckboxes(t *testing.T) {
	testEnv(t)
	f := serve(t, func(r *http.Request) string {
		if r.Method == http.MethodGet {
			return `[{"id":1,"slug":"workout","name":"Workout","input_kind":"checkbox","goal_type":"boolean","target_value":1}]`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/goals/1/entries.json" {
			t.Fatalf("wrong checkbox request: %s %s", r.Method, r.URL)
		}
		return `{"goal_id":1,"date":"2026-06-24","value":1,"completed":true}`
	})
	res := run(t, f, "complete", "-goal", "workout")
	if res.code != 0 || !strings.Contains(res.stdout, "✓ done") {
		t.Fatalf("checkbox complete: %+v", res)
	}
}

func TestInvalidNumericCommandsDoNotRequest(t *testing.T) {
	testEnv(t)
	f := serve(t, func(r *http.Request) string { t.Fatalf("unexpected request: %s", r.URL); return "" })
	for _, args := range [][]string{
		{"set", "-goal", "protein"},
		{"add", "-value", "40"},
		{"set", "-goal", "protein", "-value", "NaN"},
		{"add", "-goal", "protein", "-value", "Inf"},
		{"add", "-goal", "protein", "-value", "0"},
		{"add", "-goal", "protein", "-value", "-1"},
		{"set", "-goal", "protein", "-value", "1", "-date", "2026-02-30"},
		{"set", "-goal", "protein", "-value", "1", "unexpected"},
	} {
		if res := run(t, f, args...); res.code != ExitUsage {
			t.Fatalf("accepted invalid command %v: exit %d %s", args, res.code, res.stderr)
		}
	}
}

func TestNumericCommandsRejectCheckboxes(t *testing.T) {
	testEnv(t)
	f := serve(t, func(r *http.Request) string {
		if r.Method != http.MethodGet {
			t.Fatal("unexpected checkbox write")
		}
		return `[{"id":1,"slug":"workout","input_kind":"checkbox","goal_type":"boolean","target_value":1}]`
	})
	for _, operation := range []string{"set", "add"} {
		res := run(t, f, operation, "-goal", "workout", "-value", "1")
		if res.code == 0 || !strings.Contains(res.stderr, "complete") {
			t.Fatalf("unexpected result: %+v", res)
		}
	}
}

func TestMissingServerResourcesNeverFallBack(t *testing.T) {
	testEnv(t)
	f := newFake(t)
	f.on("GET", "/goals.json", 200, `[{"id":26,"slug":"protein","name":"Protein","input_kind":"number","target_value":160}]`)
	f.on("POST", "/goals/26/entries/2026-06-24/increments.json", 404, `{"error":"Not found"}`)
	res := run(t, f, "add", "-goal", "protein", "-date", "2026-06-24", "-value", "40")
	if res.code != ExitNotFound {
		t.Fatalf("expected exit 4, got %d (%s)", res.code, res.stderr)
	}
	for _, c := range f.calls() {
		if c.Path == "/goals/26/entries.json" {
			t.Fatal("fell back to the legacy entries API")
		}
	}
	if len(f.calls()) != 2 {
		t.Fatalf("expected exactly one write attempt, saw %v", f.calls())
	}
}

func TestTodayShowsReportedProteinProgress(t *testing.T) {
	testEnv(t)
	f := serve(t, func(r *http.Request) string {
		return `[{"id":26,"slug":"protein","name":"Protein","goal_type":"boolean","input_kind":"number","target_value":160,"unit":"g","date":"2026-06-24","value":113,"completed":false}]`
	})
	res := run(t, f, "today", "-slug", "protein")
	mustContain(t, res.stdout, "113 / 160 g", "✗ incomplete")
	if strings.Contains(res.stdout, "✗ done") {
		t.Fatalf("contradictory status: %s", res.stdout)
	}
}

func TestEntrySummaryThresholds(t *testing.T) {
	unit := "min"
	for _, tc := range []struct {
		name          string
		target, value float64
		completed     bool
	}{
		{"Meditation", 10, 5, false}, {"Meditation", 10, 10, true},
		{"Reading", 20, 15, false}, {"Reading", 20, 20, true},
		{"Focused work", 60, 30, false}, {"Focused work", 60, 60, true},
		{"One minute", 1, 0, false}, {"One minute", 1, 1, true},
	} {
		g := api.Goal{Name: tc.name, GoalType: "boolean", InputKind: "number", ScoringMode: "threshold", Target: tc.target, Unit: &unit}
		entry := api.Entry{Value: api.NewFloatValue(tc.value), Date: "2026-09-19", Completed: tc.completed}
		got := renderEntrySummary(g, entry)
		mustContain(t, got, formatNumber(tc.value)+" / "+formatNumber(tc.target)+" min")
		if tc.completed && !strings.Contains(got, "✓ done") {
			t.Fatalf("unexpected completed summary: %s", got)
		}
		if !tc.completed && !strings.Contains(got, "incomplete") {
			t.Fatalf("unexpected incomplete summary: %s", got)
		}
	}
	g := api.Goal{Name: "Workout", GoalType: "boolean", InputKind: "checkbox", Target: 1}
	if got := renderEntrySummary(g, api.Entry{Completed: false}); !strings.Contains(got, "incomplete") {
		t.Fatal(got)
	}
}

func TestCompleteRejectsNumericGoalsBeforeWriting(t *testing.T) {
	testEnv(t)
	writes := 0
	f := serve(t, func(r *http.Request) string {
		if r.Method != http.MethodGet {
			writes++
		}
		return `[{"id":1,"slug":"reading","name":"Reading","goal_type":"boolean","input_kind":"number","scoring_mode":"threshold","target_value":20,"unit":"min"}]`
	})
	res := run(t, f, "complete", "-goal", "reading")
	if res.code == 0 || !strings.Contains(res.stderr, "complete only supports checkbox") {
		t.Fatalf("expected explicit numeric value error, got %+v", res)
	}
	if writes != 0 {
		t.Fatalf("unexpected writes: %d", writes)
	}
}

func TestSetUsesHistoricalCycleAndJSONNumericResult(t *testing.T) {
	testEnv(t)
	f := serve(t, func(r *http.Request) string {
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("date") != "2026-09-10" {
				t.Fatalf("missing date selection: %s", r.URL)
			}
			return `[{"id":17,"slug":"reading","name":"Reading","goal_type":"boolean","input_kind":"number","scoring_mode":"threshold","target_value":20,"unit":"min"}]`
		}
		if r.URL.Path != "/goals/17/entries/2026-09-10.json" || r.Method != http.MethodPut {
			t.Fatalf("wrong historical goal: %s", r.URL.Path)
		}
		return `{"id":8,"goal_id":17,"date":"2026-09-10","value":15,"points":0,"completed":false}`
	})
	res := run(t, f, "set", "-goal", "reading", "-date", "2026-09-10", "-value", "15", "-json")
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	result := decodeJSON(t, res.stdout)
	if result["value"] != float64(15) || result["completed"] != false || result["input_kind"] != "number" {
		t.Fatalf("unexpected result: %s", res.stdout)
	}
}

func TestPlanPreviewUsesCreateWithoutActivation(t *testing.T) {
	testEnv(t)
	requests := 0
	f := serve(t, func(r *http.Request) string {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/twelve_week_years.json" {
			t.Fatalf("unexpected preview request: %s %s", r.Method, r.URL)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["dry_run"] != true {
			t.Fatal("dry_run was not sent")
		}
		plan := payload["plan"].(map[string]any)
		if plan["plan_key"] != "stable-key" || plan["schema_version"] != float64(1) {
			t.Fatalf("unexpected document: %v", plan)
		}
		return `{"schema_version":1,"name":"Next","status":"draft","start_date":"2026-10-01","end_date":"2026-12-23","preview":true,"goals":[]}`
	})
	res := run(t, f, "cycles", "create", "-key", "stable-key", "-name", "Next", "-start", "2026-10-01", "-dry-run")
	if requests != 1 || !strings.Contains(res.stdout, "nothing saved or activated") {
		t.Fatalf("unexpected preview: %+v", res)
	}
}

func TestGoalReorderUsesRESTResource(t *testing.T) {
	testEnv(t)
	f := serve(t, func(r *http.Request) string {
		if r.Method != http.MethodPatch || r.URL.Path != "/twelve_week_years/3/goal_order.json" {
			t.Fatalf("unexpected order resource: %s %s", r.Method, r.URL)
		}
		var body struct {
			Order []int `json:"order"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Order) != 2 || body.Order[0] != 5 || body.Order[1] != 4 {
			t.Fatalf("unexpected order: %v", body.Order)
		}
		return `[]`
	})
	if res := run(t, f, "goals", "reorder", "-cycle", "3", "-order", "5,4", "-json"); res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
}

func TestScoreRendersServerSnapshotWithoutRecalculation(t *testing.T) {
	testEnv(t)
	f := serve(t, func(r *http.Request) string {
		if r.URL.Path != "/twelve_week_years/3/scorecard.json" || r.URL.Query().Get("as_of_date") != "2026-09-19" {
			t.Fatalf("unexpected score request: %s", r.URL)
		}
		return `{"execution_percentage":57.1,"total_points":4,"max_possible_points":7,"pace_percentage":80,"rating":"Strong","period_start_date":"2026-09-14","period_end_date":"2026-09-20","as_of_date":"2026-09-19"}`
	})
	res := run(t, f, "score", "-cycle", "3", "-as-of", "2026-09-19")
	mustContain(t, res.stdout, "57.1% (4 / 7 points)", "Pace: 80%; tier: Strong")
}

func TestWeeklyTargetsDistinguishDaysAndWeightedPoints(t *testing.T) {
	cap := 4
	threshold := api.Goal{GoalType: "boolean", InputKind: "number", Target: 20, WeeklyCap: &cap, PointsPerUnit: 2}
	if got := weeklyTarget(threshold); got != "4 scored days/week (2 points/day)" {
		t.Fatal(got)
	}
	unit := "sessions"
	cap = 10
	cumulative := api.Goal{GoalType: "count", InputKind: "number", Target: 1, WeeklyCap: &cap, PointsPerUnit: 2, Unit: &unit}
	if got := weeklyTarget(cumulative); got != "10 points/week (2 points per 1 sessions)" {
		t.Fatal(got)
	}
	cumulative.WeeklyCap = nil
	if got := weeklyTarget(cumulative); !strings.HasPrefix(got, "14 points/week") {
		t.Fatal(got)
	}
}

func TestNewTodayItemEmitsNumericValue(t *testing.T) {
	unit := "g"
	value := api.NewFloatValue(105)
	item := newTodayItem(api.GoalStatus{ID: 26, Slug: "protein", Name: "Protein", GoalType: "boolean",
		Frequency: "daily", TargetValue: 160, Unit: &unit, Date: "2026-07-04", Value: &value})
	payload, _ := json.Marshal(item)
	out := string(payload)
	mustContain(t, out, `"kind":"numeric"`, `"value":105`, `"unit":"g"`, `"target_value":160`, `"completed":false`)
	if strings.Contains(out, "goal_type") {
		t.Fatalf("internal goal_type leaked into output: %s", out)
	}
	if strings.Contains(out, `"value":"105`) {
		t.Fatalf("value was emitted as a string, want a number: %s", out)
	}
}

func TestNewTodayItemNullValueWhenNoEntry(t *testing.T) {
	item := newTodayItem(api.GoalStatus{ID: 1, Slug: "meditation", GoalType: "boolean", TargetValue: 1})
	payload, _ := json.Marshal(item)
	if !strings.Contains(string(payload), `"value":null`) || item.Kind != "pass_fail" {
		t.Fatalf("unexpected item: %s", payload)
	}
}

func TestTodayProgressAndStatus(t *testing.T) {
	unit := "g"
	v105, v1 := api.NewFloatValue(105), api.NewFloatValue(1)
	numeric := api.GoalStatus{GoalType: "boolean", TargetValue: 160, Unit: &unit, Value: &v105}
	if got := todayProgress(numeric); got != "105 / 160 g" {
		t.Fatalf("numeric progress = %q", got)
	}
	if got := todayStatus(numeric); got != "✗ incomplete" {
		t.Fatalf("numeric status = %q", got)
	}
	passFail := api.GoalStatus{GoalType: "boolean", TargetValue: 1, Value: &v1, Completed: true}
	if todayProgress(passFail) != "—" || todayStatus(passFail) != "✓ done" {
		t.Fatalf("pass/fail rendering wrong")
	}
	if got := todayProgress(api.GoalStatus{GoalType: "count", TargetValue: 5}); got != "0 / 5" {
		t.Fatalf("count progress = %q", got)
	}
}

func TestFilterStatuses(t *testing.T) {
	statuses := []api.GoalStatus{{ID: 26, Slug: "protein"}, {ID: 9, Slug: "deep_work"}}
	if got := filterStatuses(statuses, "protein"); len(got) != 1 || got[0].Slug != "protein" {
		t.Fatalf("filter by slug failed: %+v", got)
	}
	if got := filterStatuses(statuses, "9"); len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("filter by id failed: %+v", got)
	}
	if got := filterStatuses(statuses, "missing"); len(got) != 0 {
		t.Fatalf("expected no matches, got %+v", got)
	}
}

func TestRenderReviewSummary(t *testing.T) {
	answer := "Morning blocks"
	review := api.Review{
		State:  "final",
		Period: api.ReviewPeriod{StartDate: "2025-12-29", EndDate: "2026-03-22"},
		Summary: api.ReviewSummary{ExecutionPercentage: 82.4, Grade: "Strong", AverageWeeklyExecution: 80.1, StrongWeeksCount: 7,
			BestWeek: api.ReviewWeekDigest{Number: 4, ExecutionPercentage: 92.0}, WorstWeek: api.ReviewWeekDigest{Number: 9, ExecutionPercentage: 51.0},
			Takeaway: "Deep Work carried the cycle."},
		GoalReviews: []api.ReviewGoal{{GoalName: "Deep Work", ExecutionPercentage: 94.1, TrendLabel: "Consistent", CoachingInsight: "Keep the same operating rhythm next block."}},
		Learnings:   api.ReviewLearnings{WhatWorked: "Deep Work led the cycle."},
		ReflectionSections: []api.ReflectionSection{{Key: "what_drove_results", Title: "What drove results",
			Question: "What specifically made your strongest weeks work?", SystemInsight: "Deep Work led the cycle."}},
		Reflection: map[string]*string{"what_drove_results": &answer},
	}
	mustContain(t, renderReviewSummary(review), "12-Week Year Review", "Execution: 82.4%", "Goals", "Learnings",
		"Reflection Prompts", "What specifically made your strongest weeks work?", "Your answer: Morning blocks")
}
