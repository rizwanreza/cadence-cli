package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAddSendsDeltaWithoutReadingProgress(t *testing.T) {
	requests := 0
	c := mockClient(func(r *http.Request) string {
		requests++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing authentication")
		}
		if r.Method == http.MethodGet {
			if r.URL.Path != "/api/goals" {
				t.Fatalf("unexpected progress read: %s", r.URL)
			}
			return `[{"id":26,"slug":"protein","name":"Protein","goal_type":"boolean","input_kind":"number","target_value":160,"unit":"g"}]`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/goals/26/entries/today/increments" {
			t.Fatalf("wrong increment request: %s %s", r.Method, r.URL)
		}
		var body map[string]map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["increment"]["value"] != "40.25" {
			t.Fatalf("expected unaltered decimal delta: %v", body)
		}
		return `{"goal_id":26,"date":"2026-06-24","previous_value":73,"value":113.25,"completed":false}`
	})
	output, err := captureCommand(t, func() error { return numericEntryCommand(c, "add", []string{"-goal", "protein", "-value", "40.25"}) })
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected only goal lookup and increment; got %d requests", requests)
	}
	for _, expected := range []string{"Added 40.25", "73 → 113.25", "113.25 / 160 g", "incomplete"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %q: %s", expected, output)
		}
	}
}

func TestNumericCompleteNeverWrites(t *testing.T) {
	for _, goalJSON := range []string{
		`{"goal_type":"boolean","input_kind":"number","target_value":160}`,
		`{"goal_type":"boolean","input_kind":"number","target_value":1}`,
		`{"goal_type":"count","target_value":1}`,
		`{"goal_type":"duration","target_value":30}`,
		`{"goal_type":"boolean","target_value":160}`,
	} {
		c := mockClient(func(r *http.Request) string {
			if r.Method != http.MethodGet {
				t.Fatalf("numeric complete must never write: %s", r.Method)
			}
			return "[" + strings.Replace(goalJSON, "{", `{"id":26,"slug":"protein","name":"Protein",`, 1) + "]"
		})
		_, err := captureCommand(t, func() error { return completeCommand(c, []string{"-goal", "protein", "-value", "40"}) })
		if err == nil || !strings.Contains(err.Error(), "no value was written") || !strings.Contains(err.Error(), "cadence add") || !strings.Contains(err.Error(), "cadence set") {
			t.Fatalf("expected safe migration message, got %v", err)
		}
	}
}

func TestCompleteStillSupportsCheckboxes(t *testing.T) {
	c := mockClient(func(r *http.Request) string {
		if r.Method == http.MethodGet {
			return `[{"id":1,"slug":"workout","name":"Workout","input_kind":"checkbox","goal_type":"boolean","target_value":1}]`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/goals/1/entries" {
			t.Fatalf("wrong checkbox request: %s %s", r.Method, r.URL)
		}
		return `{"goal_id":1,"date":"2026-06-24","value":1,"completed":true}`
	})
	output, err := captureCommand(t, func() error { return completeCommand(c, []string{"-goal", "workout"}) })
	if err != nil || !strings.Contains(output, "✓ done") {
		t.Fatalf("checkbox complete: %s %v", output, err)
	}
}

func TestInvalidNumericCommandsDoNotRequest(t *testing.T) {
	c := mockClient(func(r *http.Request) string { t.Fatalf("unexpected request: %s", r.URL); return "" })
	for _, tc := range []struct {
		operation string
		args      []string
	}{
		{"set", []string{"-goal", "protein"}},
		{"add", []string{"-value", "40"}},
		{"set", []string{"-goal", "protein", "-value", "NaN"}},
		{"add", []string{"-goal", "protein", "-value", "Inf"}},
		{"add", []string{"-goal", "protein", "-value", "0"}},
		{"add", []string{"-goal", "protein", "-value", "-1"}},
		{"set", []string{"-goal", "protein", "-value", "1", "-date", "2026-02-30"}},
		{"set", []string{"-goal", "protein", "-value", "1", "unexpected"}},
	} {
		if err := numericEntryCommand(c, tc.operation, tc.args); err == nil {
			t.Fatalf("accepted invalid command: %+v", tc)
		}
	}
}

func TestNumericCommandsRejectCheckboxes(t *testing.T) {
	c := mockClient(func(r *http.Request) string {
		if r.Method != http.MethodGet {
			t.Fatal("unexpected checkbox write")
		}
		return `[{"id":1,"slug":"workout","input_kind":"checkbox","goal_type":"boolean","target_value":1}]`
	})
	for _, operation := range []string{"set", "add"} {
		if err := numericEntryCommand(c, operation, []string{"-goal", "workout", "-value", "1"}); err == nil || !strings.Contains(err.Error(), "use complete") {
			t.Fatalf("unexpected result: %v", err)
		}
	}
}

func TestMissingServerResourcesNeverFallBack(t *testing.T) {
	requests := 0
	c := &client{baseURL: "http://cadence.test", http: &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"error":"Not found"}`)), Header: make(http.Header)}, nil
	})}}
	_, err := c.writeNumericEntry(26, "2026-06-24", "40", "add")
	if requests != 1 || err == nil || !strings.Contains(err.Error(), "no legacy fallback") {
		t.Fatalf("unexpected fallback/result: %d %v", requests, err)
	}
}

func TestTodayShowsReportedProteinProgress(t *testing.T) {
	c := mockClient(func(r *http.Request) string {
		return `[{"id":26,"slug":"protein","name":"Protein","goal_type":"boolean","input_kind":"number","target_value":160,"unit":"g","date":"2026-06-24","value":113,"completed":false}]`
	})
	output, err := captureCommand(t, func() error { return todayCommand(c, []string{"-slug", "protein"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"113 / 160 g", "✗ incomplete"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "✗ done") {
		t.Fatalf("contradictory status: %s", output)
	}
}
