package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

// set replaces a daily total; add sends a delta for the server to apply atomically.
// Neither command reads the current entry or falls back to the legacy POST API.
func numericEntryCommand(c *client, operation string, args []string) error {
	flags := flag.NewFlagSet(operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	identifier := flags.String("goal", "", "Goal id or slug")
	date := flags.String("date", "", "YYYY-MM-DD (defaults to today in the account timezone)")
	rawValue := flags.String("value", "", "Total for set, positive amount for add")
	jsonOutput := flags.Bool("json", false, "Emit the recorded entry as JSON")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			usage()
			return nil
		}
		return fmt.Errorf("invalid flags for %s", operation)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *identifier == "" || *rawValue == "" {
		return fmt.Errorf("%s requires -goal and -value", operation)
	}
	value, err := strconv.ParseFloat(*rawValue, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("-value must be a finite number")
	}
	if operation == "add" && value <= 0 {
		return fmt.Errorf("add -value must be greater than zero; use set to correct a total")
	}
	if *date != "" {
		if _, err := time.Parse("2006-01-02", *date); err != nil {
			return fmt.Errorf("invalid date: %s (expected YYYY-MM-DD)", *date)
		}
	}
	goals, err := c.fetchGoalsFor(*date, 0)
	if err != nil {
		return err
	}
	goal, err := matchGoal(goals, *identifier)
	if err != nil {
		return err
	}
	if goal.status().kind() == "pass_fail" {
		return fmt.Errorf("%s requires a numeric goal; use complete for checkbox goals", operation)
	}
	entry, err := c.writeNumericEntry(goal.ID, *date, *rawValue, operation)
	if err != nil {
		return err
	}
	if *jsonOutput {
		var previous any
		if entry.PreviousValue != nil {
			previous = entry.PreviousValue.Float()
		}
		return printJSON(map[string]any{
			"id": entry.ID, "goal_id": entry.GoalID, "date": entry.Date,
			"operation": operation, "previous_value": previous,
			"value": entry.Value.Float(), "points": entry.Points.Float(), "completed": entry.Completed,
			"input_kind": goal.InputKind, "scoring_mode": goal.ScoringMode, "target_value": goal.Target, "unit": goal.Unit,
		})
	}
	before := "no entry"
	if entry.PreviousValue != nil {
		before = formatNumber(entry.PreviousValue.Float())
	}
	if operation == "add" {
		fmt.Printf("Added %s (%s → %s). %s\n", formatNumber(value), before, formatNumber(entry.Value.Float()), renderEntrySummary(goal, entry))
	} else {
		fmt.Printf("Set total (%s → %s). %s\n", before, formatNumber(entry.Value.Float()), renderEntrySummary(goal, entry))
	}
	return nil
}

func (c *client) writeNumericEntry(goalID int, date, value, operation string) (goalEntry, error) {
	if date == "" {
		date = "today"
	}
	path := fmt.Sprintf("/api/goals/%d/entries/%s", goalID, date)
	method, key := http.MethodPut, "goal_entry"
	if operation == "add" {
		path += "/increments"
		method, key = http.MethodPost, "increment"
	}
	var result goalEntry
	// Keep the decimal text intact; the server validates and sums with decimals.
	err := c.requestJSON(method, path, map[string]any{key: map[string]string{"value": value}}, &result)
	if err != nil {
		return goalEntry{}, fmt.Errorf("%s failed: %w (requires a Cadence server with set/add entry resources; no legacy fallback was attempted)", operation, err)
	}
	return result, nil
}
