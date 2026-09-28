package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchTwelveWeekYears(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/twelve_week_years" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
		  {
		    "id": 7,
		    "start_date": "2025-12-29",
		    "end_date": "2026-03-22",
		    "review_starts_on": "2026-03-16",
		    "review_window_ends_on": "2026-04-12",
		    "review_state": "active_review",
		    "review_visible_in_app": true,
		    "review_dismissed": false
		  }
		]`))
	}))
	defer server.Close()

	c := &client{http: server.Client(), baseURL: server.URL}
	cycles, err := c.fetchTwelveWeekYears()
	if err != nil {
		t.Fatalf("fetchTwelveWeekYears returned error: %v", err)
	}

	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(cycles))
	}

	if cycles[0].ID != 7 {
		t.Fatalf("expected cycle id 7, got %d", cycles[0].ID)
	}

	if !cycles[0].ReviewVisibleInApp {
		t.Fatalf("expected review to be visible in app")
	}
}

func TestFetchReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/twelve_week_years/7/review" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "id": 7,
		  "state": "final",
		  "period": {
		    "start_date": "2025-12-29",
		    "end_date": "2026-03-22",
		    "current_week_number": 12,
		    "review_visible_in_app": true,
		    "review_dismissed": false
		  },
		  "summary": {
		    "execution_percentage": 82.4,
		    "grade": "Strong",
		    "average_weekly_execution": 80.1,
		    "strong_weeks_count": 7,
		    "best_week": {"number": 4, "start_date": "2026-01-19", "end_date": "2026-01-25", "execution_percentage": 92.0, "grade": "Elite"},
		    "worst_week": {"number": 9, "start_date": "2026-02-23", "end_date": "2026-03-01", "execution_percentage": 51.0, "grade": "Off Track"},
		    "strongest_goal": {"goal_id": 9, "name": "Deep Work", "slug": "deep_work", "execution_percentage": 94.1, "grade": "Elite", "weeks_hit": 9, "trend_label": "Consistent"},
		    "weakest_goal": {"goal_id": 8, "name": "Cricket Bowling", "slug": "cricket_bowling", "execution_percentage": 20.0, "grade": "Off Track", "weeks_hit": 2, "trend_label": "Below Target"},
		    "takeaway": "Deep Work carried the cycle, while Cricket Bowling created the biggest drag on overall execution."
		  },
		  "weekly_performance": [],
		  "goal_reviews": [
		    {
		      "goal_id": 9,
		      "goal_name": "Deep Work",
		      "goal_slug": "deep_work",
		      "execution_percentage": 94.1,
		      "grade": "Elite",
		      "weeks_hit": 9,
		      "trend_label": "Consistent",
		      "coaching_insight": "Deep Work stayed dependable across the cycle. Keep the same operating rhythm next block.",
		      "weekly_performance": [
		        {
		          "number": 1,
		          "start_date": "2025-12-29",
		          "end_date": "2026-01-04",
		          "available": true,
		          "execution_percentage": 90.0,
		          "earned_points": "4.0",
		          "max_points": 4,
		          "hit_target": true
		        }
		      ]
		    }
		  ],
		  "learnings": {
		    "what_worked": "Deep Work led the cycle at 94.1%.",
		    "what_limited_execution": "Cricket Bowling created the biggest drag.",
		    "what_needs_redesign": "Cricket Bowling likely needs a better design.",
		    "what_to_carry_forward": "Carry forward the system behind Deep Work.",
		    "next_cycle_adjustment": "Make one structural change around Cricket Bowling first."
		  },
		  "reflection_sections": [
		    {
		      "key": "what_drove_results",
		      "title": "What drove results",
		      "system_insight": "Deep Work led the cycle at 94.1%.",
		      "question": "What specifically made your strongest weeks work?",
		      "cue_chips": ["Clear plan"]
		    }
		  ]
		}`))
	}))
	defer server.Close()

	c := &client{http: server.Client(), baseURL: server.URL}
	review, err := c.fetchReview(7)
	if err != nil {
		t.Fatalf("fetchReview returned error: %v", err)
	}

	if review.ID != 7 {
		t.Fatalf("expected review id 7, got %d", review.ID)
	}

	if review.Summary.Grade != "Strong" {
		t.Fatalf("expected grade Strong, got %s", review.Summary.Grade)
	}

	if review.Learnings.WhatWorked == "" {
		t.Fatalf("expected learnings to be populated")
	}

	if review.GoalReviews[0].WeeklyPerformance[0].EarnedPoints.FloatString() != "4.00" {
		t.Fatalf("expected string numeric earned_points to parse, got %s", review.GoalReviews[0].WeeklyPerformance[0].EarnedPoints.FloatString())
	}
}

func TestFetchCompletionsParsesNumericValueAndUnit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("date"); got != "2026-07-04" {
			t.Fatalf("expected date query 2026-07-04, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		// value arrives as a JSON number now that the API coerces the decimal.
		_, _ = w.Write([]byte(`[
		  {"id":26,"slug":"protein","name":"Protein","goal_type":"boolean","frequency":"daily","target_value":160,"unit":"g","date":"2026-07-04","completed":false,"value":105.0}
		]`))
	}))
	defer server.Close()

	c := &client{http: server.Client(), baseURL: server.URL}
	statuses, err := c.fetchCompletions("2026-07-04")
	if err != nil {
		t.Fatalf("fetchCompletions returned error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	s := statuses[0]
	if s.Unit == nil || *s.Unit != "g" {
		t.Fatalf("expected unit g, got %v", s.Unit)
	}
	if s.Value == nil || s.Value.Float() != 105.0 {
		t.Fatalf("expected value 105.0, got %v", s.Value)
	}
	if s.kind() != "numeric" {
		t.Fatalf("expected kind numeric for boolean goal with target 160, got %q", s.kind())
	}
}

func TestNewTodayItemEmitsNumericValue(t *testing.T) {
	unit := "g"
	value := &entryValue{isFloat: true, float: 105.0}
	item := newTodayItem(goalStatus{
		ID: 26, Slug: "protein", Name: "Protein", GoalType: "boolean",
		Frequency: "daily", TargetValue: 160, Unit: &unit, Date: "2026-07-04",
		Completed: false, Value: value,
	})

	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal returned error: %v", err)
	}
	out := string(payload)

	for _, expected := range []string{
		`"kind":"numeric"`,
		`"value":105`,
		`"unit":"g"`,
		`"target_value":160`,
		`"completed":false`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("expected JSON to contain %q\n%s", expected, out)
		}
	}
	// value must be a number, never a quoted string.
	if strings.Contains(out, `"value":"105`) {
		t.Fatalf("value was emitted as a string, want a number: %s", out)
	}
}

func TestNewTodayItemNullValueWhenNoEntry(t *testing.T) {
	item := newTodayItem(goalStatus{ID: 1, Slug: "meditation", GoalType: "boolean", TargetValue: 1})
	payload, _ := json.Marshal(item)
	if !strings.Contains(string(payload), `"value":null`) {
		t.Fatalf("expected null value when no entry, got %s", payload)
	}
	if item.Kind != "pass_fail" {
		t.Fatalf("expected pass_fail kind, got %q", item.Kind)
	}
}

func TestKindClassification(t *testing.T) {
	cases := []struct {
		goalType string
		target   float64
		want     string
	}{
		{"boolean", 1, "pass_fail"},
		{"boolean", 160, "numeric"},
		{"duration", 30, "numeric"},
		{"count", 1, "count"},
	}
	for _, tc := range cases {
		got := goalStatus{GoalType: tc.goalType, TargetValue: tc.target}.kind()
		if got != tc.want {
			t.Fatalf("kind(%s, %g) = %q, want %q", tc.goalType, tc.target, got, tc.want)
		}
	}
}

func TestTodayProgressAndStatus(t *testing.T) {
	unit := "g"
	numeric := goalStatus{GoalType: "boolean", TargetValue: 160, Unit: &unit,
		Value: &entryValue{isFloat: true, float: 105}, Completed: false}
	if got := todayProgress(numeric); got != "105 / 160 g" {
		t.Fatalf("numeric progress = %q, want %q", got, "105 / 160 g")
	}
	if got := todayStatus(numeric); got != "✗ incomplete" {
		t.Fatalf("numeric status = %q, want %q", got, "✗ incomplete")
	}

	passFail := goalStatus{GoalType: "boolean", TargetValue: 1,
		Value: &entryValue{isFloat: true, float: 1}, Completed: true}
	if got := todayProgress(passFail); got != "—" {
		t.Fatalf("pass/fail progress = %q, want dash", got)
	}
	if got := todayStatus(passFail); got != "✓ done" {
		t.Fatalf("pass/fail status = %q, want %q", got, "✓ done")
	}

	// count goal with no entry yet: value defaults to 0.
	count := goalStatus{GoalType: "count", TargetValue: 5}
	if got := todayProgress(count); got != "0 / 5" {
		t.Fatalf("count progress = %q, want %q", got, "0 / 5")
	}
}

func TestFilterStatuses(t *testing.T) {
	statuses := []goalStatus{
		{ID: 26, Slug: "protein"},
		{ID: 9, Slug: "deep_work"},
	}
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
	review := reviewResponse{
		State:  "final",
		Period: reviewPeriod{StartDate: "2025-12-29", EndDate: "2026-03-22"},
		Summary: reviewSummary{
			ExecutionPercentage:    82.4,
			Grade:                  "Strong",
			AverageWeeklyExecution: 80.1,
			StrongWeeksCount:       7,
			BestWeek:               reviewWeekDigest{Number: 4, ExecutionPercentage: 92.0},
			WorstWeek:              reviewWeekDigest{Number: 9, ExecutionPercentage: 51.0},
			Takeaway:               "Deep Work carried the cycle.",
		},
		GoalReviews: []reviewGoal{{GoalName: "Deep Work", ExecutionPercentage: 94.1, TrendLabel: "Consistent", CoachingInsight: "Keep the same operating rhythm next block."}},
		Learnings: reviewLearnings{
			WhatWorked:           "Deep Work led the cycle.",
			WhatLimitedExecution: "Cricket Bowling created the biggest drag.",
			WhatNeedsRedesign:    "Cricket Bowling likely needs a better design.",
			WhatToCarryForward:   "Carry forward the system behind Deep Work.",
			NextCycleAdjustment:  "Make one structural change first.",
		},
		ReflectionSections: []reflectionSection{{Title: "What drove results", Question: "What specifically made your strongest weeks work?", SystemInsight: "Deep Work led the cycle."}},
	}

	output := renderReviewSummary(review)

	for _, expected := range []string{"12-Week Year Review", "Execution: 82.4%", "Goals", "Learnings", "Reflection Prompts", "What specifically made your strongest weeks work?"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected output to contain %q\n%s", expected, output)
		}
	}
}

func TestNewRequestSetsBearerToken(t *testing.T) {
	c := &client{http: http.DefaultClient, baseURL: "https://example.com", token: "secret-token"}

	req, err := c.newRequest(http.MethodGet, "/api/goals", nil)
	if err != nil {
		t.Fatalf("newRequest returned error: %v", err)
	}

	if got := req.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Fatalf("expected bearer header, got %q", got)
	}
}

func TestNewRequestOmitsAuthorizationWithoutToken(t *testing.T) {
	c := &client{http: http.DefaultClient, baseURL: "https://example.com"}

	req, err := c.newRequest(http.MethodGet, "/api/goals", nil)
	if err != nil {
		t.Fatalf("newRequest returned error: %v", err)
	}

	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected no authorization header, got %q", got)
	}
}

func TestDoMapsUnauthorizedToFriendlyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
	}))
	defer server.Close()

	c := &client{http: server.Client(), baseURL: server.URL}
	_, err := c.fetchGoals()
	if err == nil {
		t.Fatal("expected an error for a 401 response")
	}
	if err != errUnauthorized {
		t.Fatalf("expected errUnauthorized, got %v", err)
	}
}

func TestResolveHostPrecedence(t *testing.T) {
	t.Setenv("CADENCE_URL", "")

	host, err := resolveHost("", config{})
	if err != nil {
		t.Fatalf("resolveHost returned error: %v", err)
	}
	if host != defaultHost {
		t.Fatalf("expected default host %q, got %q", defaultHost, host)
	}

	host, err = resolveHost("", config{Host: "https://saved.example.com"})
	if err != nil {
		t.Fatalf("resolveHost returned error: %v", err)
	}
	if host != "https://saved.example.com" {
		t.Fatalf("expected saved host, got %q", host)
	}

	t.Setenv("CADENCE_URL", "https://env.example.com")
	host, err = resolveHost("", config{Host: "https://saved.example.com"})
	if err != nil {
		t.Fatalf("resolveHost returned error: %v", err)
	}
	if host != "https://env.example.com" {
		t.Fatalf("expected env host to win over config, got %q", host)
	}

	host, err = resolveHost("http://localhost:3000", config{Host: "https://saved.example.com"})
	if err != nil {
		t.Fatalf("resolveHost returned error: %v", err)
	}
	if host != "http://localhost:3000" {
		t.Fatalf("expected flag host to win, got %q", host)
	}
}

func TestResolveTokenPrecedence(t *testing.T) {
	t.Setenv("CADENCE_TOKEN", "")

	if got := resolveToken("", config{Token: "config-token"}); got != "config-token" {
		t.Fatalf("expected config token, got %q", got)
	}

	t.Setenv("CADENCE_TOKEN", "env-token")
	if got := resolveToken("", config{Token: "config-token"}); got != "env-token" {
		t.Fatalf("expected env token to win over config, got %q", got)
	}

	if got := resolveToken("flag-token", config{Token: "config-token"}); got != "flag-token" {
		t.Fatalf("expected flag token to win, got %q", got)
	}
}
