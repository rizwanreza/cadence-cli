package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{HTTP: server.Client(), BaseURL: server.URL, UserAgent: "cadence-cli/test (os/arch)"}
}

func TestFetchTwelveWeekYears(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/twelve_week_years" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":7,"start_date":"2025-12-29","end_date":"2026-03-22","review_starts_on":"2026-03-16",
		  "review_window_ends_on":"2026-04-12","review_state":"active_review","review_visible_in_app":true,"review_dismissed":false,
		  "current_week_number":12,"total_weeks":12,"awaiting_review":true,"final_stretch":false,"lapsed":false,
		  "restartable":false,"restart_blocker":"x","successor_id":9,"ended_early":false,"notes":"n"}]`))
	})
	cycles, err := c.Cycles(context.Background())
	if err != nil {
		t.Fatalf("Cycles returned error: %v", err)
	}
	if len(cycles) != 1 || cycles[0].ID != 7 || !cycles[0].ReviewVisibleInApp {
		t.Fatalf("unexpected cycles: %+v", cycles)
	}
	if !cycles[0].AwaitingReview || cycles[0].SuccessorID == nil || *cycles[0].SuccessorID != 9 || cycles[0].TotalWeeks != 12 {
		t.Fatalf("lifecycle flags not decoded: %+v", cycles[0])
	}
}

func TestFetchReview(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/twelve_week_years/7/review" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
		  "id": 7, "state": "final",
		  "period": {"start_date": "2025-12-29", "end_date": "2026-03-22", "current_week_number": 12, "review_visible_in_app": true, "review_dismissed": false},
		  "summary": {
		    "execution_percentage": 82.4, "grade": "Strong", "average_weekly_execution": 80.1, "strong_weeks_count": 7,
		    "best_week": {"number": 4, "start_date": "2026-01-19", "end_date": "2026-01-25", "execution_percentage": 92.0, "grade": "Elite"},
		    "worst_week": {"number": 9, "start_date": "2026-02-23", "end_date": "2026-03-01", "execution_percentage": 51.0, "grade": "Off Track"},
		    "strongest_goal": {"goal_id": 9, "name": "Deep Work", "slug": "deep_work", "execution_percentage": 94.1, "grade": "Elite", "weeks_hit": 9, "trend_label": "Consistent"},
		    "weakest_goal": {"goal_id": 8, "name": "Cricket Bowling", "slug": "cricket_bowling", "execution_percentage": 20.0, "grade": "Off Track", "weeks_hit": 2, "trend_label": "Below Target"},
		    "takeaway": "Deep Work carried the cycle."
		  },
		  "weekly_performance": [],
		  "goal_reviews": [{"goal_id": 9, "goal_name": "Deep Work", "goal_slug": "deep_work", "execution_percentage": 94.1, "grade": "Elite",
		    "weeks_hit": 9, "trend_label": "Consistent", "coaching_insight": "Keep it.",
		    "weekly_performance": [{"number": 1, "start_date": "2025-12-29", "end_date": "2026-01-04", "available": true,
		      "execution_percentage": 90.0, "earned_points": "4.0", "max_points": 4, "hit_target": true}]}],
		  "learnings": {"what_worked": "Deep Work led the cycle at 94.1%.", "what_limited_execution": "x", "what_needs_redesign": "x", "what_to_carry_forward": "x", "next_cycle_adjustment": "x"},
		  "reflection_sections": [{"key": "what_drove_results", "title": "What drove results", "system_insight": "x", "question": "What specifically made your strongest weeks work?", "cue_chips": ["Clear plan"]}],
		  "reflection": {"what_drove_results": "Mornings"}
		}`))
	})
	review, err := c.Review(context.Background(), 7)
	if err != nil {
		t.Fatalf("Review returned error: %v", err)
	}
	if review.ID != 7 || review.Summary.Grade != "Strong" || review.Learnings.WhatWorked == "" {
		t.Fatalf("unexpected review: %+v", review)
	}
	if got := review.GoalReviews[0].WeeklyPerformance[0].EarnedPoints.FloatString(); got != "4.00" {
		t.Fatalf("expected string numeric earned_points to parse, got %s", got)
	}
	if review.Reflection["what_drove_results"] == nil || *review.Reflection["what_drove_results"] != "Mornings" {
		t.Fatalf("reflection answers not decoded: %+v", review.Reflection)
	}
}

func TestFetchCompletionsParsesNumericValueAndUnit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("date"); got != "2026-07-04" {
			t.Fatalf("expected date query 2026-07-04, got %q", got)
		}
		_, _ = w.Write([]byte(`[{"id":26,"slug":"protein","name":"Protein","goal_type":"boolean","frequency":"daily","target_value":160,"unit":"g","date":"2026-07-04","completed":false,"value":105.0}]`))
	})
	statuses, err := c.Completions(context.Background(), "2026-07-04")
	if err != nil {
		t.Fatalf("Completions returned error: %v", err)
	}
	s := statuses[0]
	if s.Unit == nil || *s.Unit != "g" || s.Value == nil || s.Value.Float() != 105.0 {
		t.Fatalf("unexpected status: %+v", s)
	}
	if s.Kind() != "numeric" {
		t.Fatalf("expected kind numeric for boolean goal with target 160, got %q", s.Kind())
	}
}

func TestKindClassification(t *testing.T) {
	for _, tc := range []struct {
		goalType string
		target   float64
		want     string
	}{
		{"boolean", 1, "pass_fail"},
		{"boolean", 160, "numeric"},
		{"duration", 30, "numeric"},
		{"count", 1, "count"},
	} {
		if got := (GoalStatus{GoalType: tc.goalType, TargetValue: tc.target}).Kind(); got != tc.want {
			t.Fatalf("Kind(%s, %g) = %q, want %q", tc.goalType, tc.target, got, tc.want)
		}
	}
}

func TestNewRequestSetsHeaders(t *testing.T) {
	c := &Client{BaseURL: "https://example.com", Token: "secret-token", UserAgent: "cadence-cli/1.2.3 (darwin/arm64)"}
	req, err := c.NewRequest(context.Background(), http.MethodGet, "/goals", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "https://example.com/api/v1/goals" {
		t.Fatalf("expected /api/v1 prefix, got %s", req.URL)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Fatalf("expected bearer header, got %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "cadence-cli/1.2.3 (darwin/arm64)" {
		t.Fatalf("unexpected User-Agent %q", got)
	}
}

func TestNewRequestOmitsAuthorizationWithoutToken(t *testing.T) {
	c := &Client{BaseURL: "https://example.com"}
	req, _ := c.NewRequest(context.Background(), http.MethodGet, "/goals", nil)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected no authorization header, got %q", got)
	}
}

func TestDoMapsUnauthorizedToFriendlyError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
	})
	_, err := c.Goals(context.Background(), "", 0)
	if !IsStatus(err, http.StatusUnauthorized) {
		t.Fatalf("expected a 401 *Error, got %v", err)
	}
	if !strings.Contains(err.Error(), "cadence login") {
		t.Fatalf("expected actionable message, got %q", err)
	}
}

func TestParseErrorEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name, body, code, message string
		details                   []string
	}{
		{"v1 envelope", `{"error":{"code":"validation_failed","message":"Target must be greater than 0","details":["Target must be greater than 0"]},"errors":["Target must be greater than 0"]}`,
			"validation_failed", "Target must be greater than 0", []string{"Target must be greater than 0"}},
		{"legacy errors array", `{"errors":["Date must be ISO8601 (YYYY-MM-DD)."]}`, "validation_failed", "Date must be ISO8601 (YYYY-MM-DD).", []string{"Date must be ISO8601 (YYYY-MM-DD)."}},
		{"legacy error string", `{"error":"Goal not found"}`, "validation_failed", "Goal not found", nil},
		{"plan details objects", `{"error":{"code":"validation_failed","message":"Plan is invalid","details":[{"field":"goals[0].name","message":"can't be blank"}]},"errors":["goals[0].name can't be blank"]}`,
			"validation_failed", "Plan is invalid", []string{"goals[0].name: can't be blank"}},
		{"html", `<html>boom</html>`, "validation_failed", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := ParseError(422, []byte(tc.body))
			if e.Code != tc.code || e.Message != tc.message {
				t.Fatalf("got code=%q message=%q", e.Code, e.Message)
			}
			if strings.Join(e.Details, "|") != strings.Join(tc.details, "|") {
				t.Fatalf("details = %v, want %v", e.Details, tc.details)
			}
		})
	}
	if e := ParseError(429, nil); e.Code != "rate_limited" {
		t.Fatalf("429 code = %q", e.Code)
	}
}

func TestRateLimitedErrorMentionsRetryAfter(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"Too many requests","details":[]},"errors":["Too many requests"]}`))
	})
	_, err := c.Me(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.RetryAfter != "12" || !strings.Contains(err.Error(), "retry after 12s") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNetworkErrorOnTimeout(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	})
	c.HTTP.Timeout = 20 * time.Millisecond
	_, err := c.Me(context.Background())
	var netErr *NetworkError
	if !errors.As(err, &netErr) || !strings.Contains(err.Error(), "CADENCE_TIMEOUT") {
		t.Fatalf("expected a timeout NetworkError, got %v", err)
	}
}

func TestIdempotencyKeyOnlyOnAdd(t *testing.T) {
	var keys []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		_, _ = w.Write([]byte(`{"goal_id":1,"value":2}`))
	})
	if _, err := c.WriteNumericEntry(context.Background(), 1, "", "2", "add", "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteNumericEntry(context.Background(), 1, "", "2", "set", "k2"); err != nil {
		t.Fatal(err)
	}
	if keys[0] != "k1" || keys[1] != "" {
		t.Fatalf("unexpected idempotency keys: %v", keys)
	}
}

func TestAddRetriesOnceWithTheSameKeyAfterAConnectionDrop(t *testing.T) {
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if len(keys) == 1 {
			// Drop the connection without a response, as a flaky network would.
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.Header().Set("Idempotent-Replayed", "true")
		_, _ = w.Write([]byte(`{"goal_id":1,"value":42}`))
	}))
	defer server.Close()
	c := &Client{HTTP: server.Client(), BaseURL: server.URL}
	entry, replayed, err := c.WriteNumericEntryReplay(context.Background(), 1, "", "2", "add", "same-key")
	if err != nil || !replayed || entry.Value.Float() != 42 {
		t.Fatalf("entry=%+v replayed=%v err=%v", entry, replayed, err)
	}
	if len(keys) != 2 || keys[0] != "same-key" || keys[1] != "same-key" {
		t.Fatalf("keys = %v", keys)
	}
}
