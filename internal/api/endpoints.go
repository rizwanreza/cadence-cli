package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Me calls GET /identity.json.
func (c *Client) Me(ctx context.Context) (Me, error) {
	var me Me
	err := c.RequestJSON(ctx, http.MethodGet, "/identity", nil, &me)
	return me, err
}

// RevokeToken calls DELETE /settings/cli_token.json (revokes the token used for the request).
func (c *Client) RevokeToken(ctx context.Context) error {
	return c.RequestJSON(ctx, http.MethodDelete, "/settings/cli_token", nil, nil)
}

// Completions calls GET /daily_logs/:date.json (empty date = today in the user's zone).
func (c *Client) Completions(ctx context.Context, date string) ([]GoalStatus, error) {
	var statuses []GoalStatus
	if date == "" {
		date = "today"
	}
	err := c.RequestJSON(ctx, http.MethodGet, "/daily_logs/"+url.PathEscape(date), nil, &statuses)
	return statuses, err
}

// Goals calls GET /goals, selecting a cycle by id or the activated cycle on date.
func (c *Client) Goals(ctx context.Context, date string, cycleID int) ([]Goal, error) {
	query := url.Values{}
	if date != "" {
		query.Set("date", date)
	}
	if cycleID > 0 {
		query.Set("cycle_id", strconv.Itoa(cycleID))
	}
	path := "/goals"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var goals []Goal
	err := c.RequestJSON(ctx, http.MethodGet, path, nil, &goals)
	return goals, err
}

// ArchiveGoal calls POST /goals/:id/archive.json.
func (c *Client) ArchiveGoal(ctx context.Context, goalID, cycleID int) error {
	path := fmt.Sprintf("/goals/%d/archive", goalID)
	if cycleID > 0 {
		path += fmt.Sprintf("?cycle_id=%d", cycleID)
	}
	return c.RequestJSON(ctx, http.MethodPost, path, nil, nil)
}

// Cycles calls GET /twelve_week_years.
func (c *Client) Cycles(ctx context.Context) ([]Cycle, error) {
	var cycles []Cycle
	err := c.RequestJSON(ctx, http.MethodGet, "/twelve_week_years", nil, &cycles)
	return cycles, err
}

// Cycle calls GET /twelve_week_years/:id (cycle payload merged with its plan).
func (c *Client) Cycle(ctx context.Context, id int) (map[string]any, error) {
	var plan map[string]any
	err := c.RequestJSON(ctx, http.MethodGet, fmt.Sprintf("/twelve_week_years/%d", id), nil, &plan)
	return plan, err
}

// UpdateCycleNotes calls PATCH /twelve_week_years/:id with notes only.
func (c *Client) UpdateCycleNotes(ctx context.Context, id int, notes string) (map[string]any, error) {
	var result map[string]any
	body := map[string]any{"twelve_week_year": map[string]any{"notes": notes}}
	err := c.RequestJSON(ctx, http.MethodPatch, fmt.Sprintf("/twelve_week_years/%d", id), body, &result)
	return result, err
}

// Review calls GET /twelve_week_years/:id/review.
func (c *Client) Review(ctx context.Context, id int) (Review, error) {
	var review Review
	err := c.RequestJSON(ctx, http.MethodGet, fmt.Sprintf("/twelve_week_years/%d/review", id), nil, &review)
	return review, err
}

// ReviewRaw is Review without decoding into structs (for --json passthrough).
func (c *Client) ReviewRaw(ctx context.Context, id int) (map[string]any, error) {
	var review map[string]any
	err := c.RequestJSON(ctx, http.MethodGet, fmt.Sprintf("/twelve_week_years/%d/review", id), nil, &review)
	return review, err
}

// UpdateReview calls PATCH /twelve_week_years/:id/review with a subset of the
// six reflection fields.
func (c *Client) UpdateReview(ctx context.Context, id int, fields map[string]string) (map[string]any, error) {
	var result map[string]any
	err := c.RequestJSON(ctx, http.MethodPatch, fmt.Sprintf("/twelve_week_years/%d/review", id), map[string]any{"review": fields}, &result)
	return result, err
}

// DismissReview calls POST /twelve_week_years/:id/review_dismissal.
func (c *Client) DismissReview(ctx context.Context, id int) (map[string]any, error) {
	var result map[string]any
	err := c.RequestJSON(ctx, http.MethodPost, fmt.Sprintf("/twelve_week_years/%d/review_dismissal", id), nil, &result)
	return result, err
}

// NextCycle calls POST /twelve_week_years/:id/next_cycle.
func (c *Client) NextCycle(ctx context.Context, id int, start string, goalIDs []int) (NextCycleResult, map[string]any, error) {
	body := map[string]any{}
	if start != "" {
		body["start"] = start
	}
	if goalIDs != nil {
		body["goal_ids"] = goalIDs
	}
	var raw map[string]any
	if err := c.RequestJSON(ctx, http.MethodPost, fmt.Sprintf("/twelve_week_years/%d/next_cycle", id), body, &raw); err != nil {
		return NextCycleResult{}, nil, err
	}
	var result NextCycleResult
	err := remarshal(raw, &result)
	return result, raw, err
}

// Unschedule calls DELETE /twelve_week_years/:id/activation.
func (c *Client) Unschedule(ctx context.Context, id int) (map[string]any, error) {
	var result map[string]any
	err := c.RequestJSON(ctx, http.MethodDelete, fmt.Sprintf("/twelve_week_years/%d/activation", id), nil, &result)
	return result, err
}

// FreshStart calls POST /fresh_start.
func (c *Client) FreshStart(ctx context.Context, goalIDs []int) (NextCycleResult, map[string]any, error) {
	body := map[string]any{}
	if goalIDs != nil {
		body["goal_ids"] = goalIDs
	}
	var raw map[string]any
	if err := c.RequestJSON(ctx, http.MethodPost, "/fresh_start", body, &raw); err != nil {
		return NextCycleResult{}, nil, err
	}
	var result NextCycleResult
	err := remarshal(raw, &result)
	return result, raw, err
}

// DismissFreshStart calls POST /fresh_start_dismissal.
func (c *Client) DismissFreshStart(ctx context.Context) error {
	return c.RequestJSON(ctx, http.MethodPost, "/fresh_start_dismissal", nil, nil)
}

// Scorecard calls GET /twelve_week_years/:id/scorecard.
func (c *Client) Scorecard(ctx context.Context, id int, weekStart, asOf string) (map[string]any, error) {
	query := url.Values{}
	if weekStart != "" {
		query.Set("week_start", weekStart)
	}
	if asOf != "" {
		query.Set("as_of_date", asOf)
	}
	path := fmt.Sprintf("/twelve_week_years/%d/scorecard", id)
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var result map[string]any
	err := c.RequestJSON(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

// WeeklyReview calls GET /weekly_reviews/:week_start (a date or "current").
func (c *Client) WeeklyReview(ctx context.Context, week string) (WeeklyReviewResponse, error) {
	var result WeeklyReviewResponse
	err := c.RequestJSON(ctx, http.MethodGet, "/weekly_reviews/"+url.PathEscape(week), nil, &result)
	return result, err
}

// SaveWeeklyReview calls PUT /weekly_reviews/:week_start with a partial review.
func (c *Client) SaveWeeklyReview(ctx context.Context, week string, fields map[string]string) (WeeklyReviewResponse, error) {
	var result WeeklyReviewResponse
	err := c.RequestJSON(ctx, http.MethodPut, "/weekly_reviews/"+url.PathEscape(week), map[string]any{"weekly_review": fields}, &result)
	return result, err
}

// Entries calls GET /entries?from&to[&goal].
func (c *Client) Entries(ctx context.Context, from, to, goal string) (EntriesRange, error) {
	query := url.Values{}
	if from != "" {
		query.Set("from", from)
	}
	if to != "" {
		query.Set("to", to)
	}
	if goal != "" {
		query.Set("goal", goal)
	}
	path := "/entries"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var result EntriesRange
	err := c.RequestJSON(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

// ProgressInsight calls GET /progress_insight[?week_start=].
func (c *Client) ProgressInsight(ctx context.Context, weekStart string) (Insight, error) {
	path := "/progress_insight"
	if weekStart != "" {
		path += "?week_start=" + url.QueryEscape(weekStart)
	}
	var result Insight
	err := c.RequestJSON(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

// CreateEntry is the legacy POST /goals/:id/entries (checkbox complete).
func (c *Client) CreateEntry(ctx context.Context, goalID int, date, rawValue string) (Entry, error) {
	entry := map[string]any{}
	if date != "" {
		entry["date"] = date
	}
	if rawValue != "" {
		if parsed, err := strconv.ParseFloat(rawValue, 64); err == nil {
			entry["value"] = parsed
		} else {
			entry["value"] = rawValue
		}
	}
	var created Entry
	err := c.RequestJSON(ctx, http.MethodPost, fmt.Sprintf("/goals/%d/entries", goalID), map[string]any{"goal_entry": entry}, &created)
	return created, err
}

// WriteNumericEntry replaces (set) or increments (add) a day's total. An add
// carries idempotencyKey so a retried request is not applied twice: when the
// connection fails (but didn't time out) it is retried once with the same key,
// and the server replays the first result if it had already been applied.
// Replayed reports the server's Idempotent-Replayed header.
func (c *Client) WriteNumericEntry(ctx context.Context, goalID int, date, value, operation, idempotencyKey string) (Entry, error) {
	entry, _, err := c.WriteNumericEntryReplay(ctx, goalID, date, value, operation, idempotencyKey)
	return entry, err
}

// WriteNumericEntryReplay is WriteNumericEntry that also reports a replay.
func (c *Client) WriteNumericEntryReplay(ctx context.Context, goalID int, date, value, operation, idempotencyKey string) (Entry, bool, error) {
	if date == "" {
		date = "today"
	}
	path := fmt.Sprintf("/goals/%d/entries/%s", goalID, date)
	method, key := http.MethodPut, "goal_entry"
	var headers http.Header
	if operation == "add" {
		path += "/increments"
		method, key = http.MethodPost, "increment"
		if idempotencyKey != "" {
			headers = http.Header{"Idempotency-Key": []string{idempotencyKey}}
		}
	}
	body := map[string]any{key: map[string]string{"value": value}} // decimal text kept intact
	attempts := 1
	if headers != nil {
		attempts = 2
	}
	var result Entry
	var respHeader http.Header
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		result = Entry{}
		respHeader, err = c.DoJSON(ctx, method, path, headers, body, &result)
		var netErr *NetworkError
		if err == nil || !errors.As(err, &netErr) || netErr.Timeout() {
			break
		}
	}
	replayed := respHeader != nil && respHeader.Get("Idempotent-Replayed") == "true"
	return result, replayed, err
}
