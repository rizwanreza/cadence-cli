package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockClient(handler func(*http.Request) string) *client {
	return &client{baseURL: "http://cadence.test", token: "test-token", http: &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(handler(r))), Header: make(http.Header)}, nil
	})}}
}

func captureCommand(t *testing.T, command func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	defer func() { os.Stdout = original; read.Close() }()
	commandErr := command()
	write.Close()
	data, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), commandErr
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
		g := goal{Name: tc.name, GoalType: "boolean", InputKind: "number", ScoringMode: "threshold", Target: tc.target, Unit: &unit}
		entry := goalEntry{Value: entryValue{isFloat: true, float: tc.value}, Date: "2026-09-19", Completed: tc.completed}
		got := renderEntrySummary(g, entry)
		progress := formatNumber(tc.value) + " / " + formatNumber(tc.target) + " min"
		if !strings.Contains(got, progress) {
			t.Fatalf("%s missing %q", got, progress)
		}
		if tc.completed && !strings.Contains(got, "✓ done") {
			t.Fatalf("unexpected completed summary: %s", got)
		}
		if !tc.completed && !strings.Contains(got, "incomplete") {
			t.Fatalf("unexpected incomplete summary: %s", got)
		}
	}
	g := goal{Name: "Workout", GoalType: "boolean", InputKind: "checkbox", Target: 1}
	if got := renderEntrySummary(g, goalEntry{Completed: false}); !strings.Contains(got, "incomplete") {
		t.Fatal(got)
	}
}

func TestCompleteRejectsNumericGoalsBeforeWriting(t *testing.T) {
	writes := 0
	c := mockClient(func(r *http.Request) string {
		if r.Method != http.MethodGet {
			writes++
		}
		return `[{"id":1,"slug":"reading","name":"Reading","goal_type":"boolean","input_kind":"number","scoring_mode":"threshold","target_value":20,"unit":"min"}]`
	})
	_, err := captureCommand(t, func() error { return completeCommand(c, []string{"-goal", "reading"}) })
	if err == nil || !strings.Contains(err.Error(), "complete only supports checkbox") {
		t.Fatalf("expected explicit numeric value error, got %v", err)
	}
	if writes != 0 {
		t.Fatalf("unexpected writes: %d", writes)
	}
}

func TestSetUsesHistoricalCycleAndJSONNumericResult(t *testing.T) {
	c := mockClient(func(r *http.Request) string {
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("date") != "2026-09-10" {
				t.Fatalf("missing date selection: %s", r.URL)
			}
			return `[{"id":17,"slug":"reading","name":"Reading","goal_type":"boolean","input_kind":"number","scoring_mode":"threshold","target_value":20,"unit":"min"}]`
		}
		if r.URL.Path != "/api/goals/17/entries/2026-09-10" || r.Method != http.MethodPut {
			t.Fatalf("wrong historical goal: %s", r.URL.Path)
		}
		return `{"id":8,"goal_id":17,"date":"2026-09-10","value":15,"points":0,"completed":false}`
	})
	output, err := captureCommand(t, func() error {
		return numericEntryCommand(c, "set", []string{"-goal", "reading", "-date", "2026-09-10", "-value", "15", "-json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result["value"] != float64(15) || result["completed"] != false || result["input_kind"] != "number" {
		t.Fatalf("unexpected result: %s", output)
	}
}

func TestPlanPreviewUsesCreateWithoutActivation(t *testing.T) {
	requests := 0
	c := mockClient(func(r *http.Request) string {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/twelve_week_years" {
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
	output, err := captureCommand(t, func() error {
		return cyclesCommand(c, []string{"create", "-key", "stable-key", "-name", "Next", "-start", "2026-10-01", "-dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !strings.Contains(output, "nothing saved or activated") {
		t.Fatalf("unexpected preview: %s", output)
	}
}

func TestGoalReorderUsesRESTResource(t *testing.T) {
	c := mockClient(func(r *http.Request) string {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/twelve_week_years/3/goal_order" {
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
	_, err := captureCommand(t, func() error { return goalsCommand(c, []string{"reorder", "-cycle", "3", "-order", "5,4", "-json"}) })
	if err != nil {
		t.Fatal(err)
	}
}

func TestScoreRendersServerSnapshotWithoutRecalculation(t *testing.T) {
	c := mockClient(func(r *http.Request) string {
		if r.URL.Path != "/api/twelve_week_years/3/scorecard" || r.URL.Query().Get("as_of_date") != "2026-09-19" {
			t.Fatalf("unexpected score request: %s", r.URL)
		}
		return `{"execution_percentage":57.1,"total_points":4,"max_possible_points":7,"pace_percentage":80,"projected_execution_percentage":80,"period_start_date":"2026-09-14","period_end_date":"2026-09-20","as_of_date":"2026-09-19"}`
	})
	output, err := captureCommand(t, func() error { return scoreCommand(c, []string{"-cycle", "3", "-as-of", "2026-09-19"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "57.1% (4 / 7 points)") || !strings.Contains(output, "Pace: 80%") {
		t.Fatal(output)
	}
}

func TestWeeklyTargetsDistinguishDaysAndWeightedPoints(t *testing.T) {
	cap := 4
	threshold := goal{GoalType: "boolean", InputKind: "number", Target: 20, WeeklyCap: &cap, PointsPerUnit: 2}
	if got := weeklyTarget(threshold); got != "4 scored days/week (2 points/day)" {
		t.Fatal(got)
	}
	unit := "sessions"
	cap = 10
	cumulative := goal{GoalType: "count", InputKind: "number", Target: 1, WeeklyCap: &cap, PointsPerUnit: 2, Unit: &unit}
	if got := weeklyTarget(cumulative); got != "10 points/week (2 points per 1 sessions)" {
		t.Fatal(got)
	}
	cumulative.WeeklyCap = nil
	if got := weeklyTarget(cumulative); !strings.HasPrefix(got, "14 points/week") {
		t.Fatal(got)
	}
}
