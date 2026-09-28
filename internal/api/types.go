package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Goal is a goal definition as returned by GET /goals and the plan endpoints.
type Goal struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	GoalType      string   `json:"goal_type"`
	InputKind     string   `json:"input_kind"`
	ScoringMode   string   `json:"scoring_mode"`
	Description   string   `json:"description"`
	DisplayOrder  int      `json:"display_order"`
	PointsPerUnit float64  `json:"points_per_unit"`
	Frequency     string   `json:"frequency"`
	Target        float64  `json:"target_value"`
	WeeklyCap     *int     `json:"weekly_cap"`
	MinValue      *float64 `json:"min_value"`
	MaxValue      *float64 `json:"max_value"`
	ActiveDays    []int    `json:"active_days"`
	Active        bool     `json:"active"`
	Archived      bool     `json:"archived"`
	Unit          *string  `json:"unit"`
}

// Status returns the goal's definition as a GoalStatus (for Kind and display).
func (g Goal) Status() GoalStatus {
	return GoalStatus{GoalType: g.GoalType, InputKind: g.InputKind, ScoringMode: g.ScoringMode, TargetValue: g.Target, Unit: g.Unit}
}

// GoalStatus is one goal's progress on a date (GET /completions).
type GoalStatus struct {
	ID          int         `json:"id"`
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	GoalType    string      `json:"goal_type"`
	InputKind   string      `json:"input_kind"`
	ScoringMode string      `json:"scoring_mode"`
	Frequency   string      `json:"frequency"`
	TargetValue float64     `json:"target_value"`
	Unit        *string     `json:"unit"`
	Date        string      `json:"date"`
	Completed   bool        `json:"completed"`
	Value       *EntryValue `json:"value"`
}

// Kind classifies a goal for automation and display: pass_fail, numeric or
// count. The API's goal_type enum alone is not enough: a "boolean" goal with a
// target above 1 is really a numeric threshold (e.g. hit 160 g protein).
func (s GoalStatus) Kind() string {
	if s.ScoringMode == "cumulative" || s.GoalType == "count" {
		return "count"
	}
	if s.InputKind == "number" {
		return "numeric"
	}
	if s.InputKind == "checkbox" {
		return "pass_fail"
	}
	switch s.GoalType {
	case "boolean", "duration":
		if s.TargetValue > 1 {
			return "numeric"
		}
		return "pass_fail"
	default:
		return s.GoalType
	}
}

// Entry is a write result from the entry endpoints.
type Entry struct {
	Operation     string      `json:"operation"`
	PreviousValue *EntryValue `json:"previous_value"`
	ID            int         `json:"id"`
	GoalID        int         `json:"goal_id"`
	Date          string      `json:"date"`
	Value         EntryValue  `json:"value"`
	Completed     bool        `json:"completed"`
	Points        EntryValue  `json:"points"`
}

// Cycle is a 12-week year ("cycle") as serialized by the API.
type Cycle struct {
	ID                 int     `json:"id"`
	Name               string  `json:"name"`
	Status             string  `json:"status"`
	PlanKey            string  `json:"plan_key"`
	StartDate          string  `json:"start_date"`
	EndDate            string  `json:"end_date"`
	TotalWeeks         int     `json:"total_weeks"`
	CurrentWeekNumber  int     `json:"current_week_number"`
	ActivatedAt        *string `json:"activated_at"`
	EndedEarlyAt       *string `json:"ended_early_at"`
	EndedEarly         bool    `json:"ended_early"`
	RestartOfID        *int    `json:"restart_of_id"`
	ReviewStartsOn     string  `json:"review_starts_on"`
	ReviewWindowEndsOn string  `json:"review_window_ends_on"`
	ReviewState        string  `json:"review_state"`
	ReviewVisibleInApp bool    `json:"review_visible_in_app"`
	ReviewDismissed    bool    `json:"review_dismissed"`
	AwaitingReview     bool    `json:"awaiting_review"`
	FinalStretch       bool    `json:"final_stretch"`
	Lapsed             bool    `json:"lapsed"`
	Restartable        bool    `json:"restartable"`
	RestartBlocker     *string `json:"restart_blocker"`
	SuccessorID        *int    `json:"successor_id"`
	Notes              *string `json:"notes"`
}

// User identifies the account a token belongs to.
type User struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	TimeZone string `json:"time_zone"`
}

// Me is GET /me: who the token belongs to and where they are in the loop.
type Me struct {
	User            User   `json:"user"`
	Today           string `json:"today"`
	Now             string `json:"now"`
	ActiveCycle     *Cycle `json:"active_cycle"`
	ClosingCycle    *Cycle `json:"closing_cycle"`
	SuccessorCycle  *Cycle `json:"successor_cycle"`
	FreshStartOffer bool   `json:"fresh_start_offer"`
	Token           *struct {
		Prefix     string  `json:"prefix"`
		LastUsedAt *string `json:"last_used_at"`
	} `json:"token"`
	CLI struct {
		MinVersion string `json:"min_version"`
	} `json:"cli"`
}

// WeeklyReview holds the four weekly review prompts.
type WeeklyReview struct {
	BiggestWin         *string `json:"biggest_win"`
	DerailRootCause    *string `json:"derail_root_cause"`
	OneChangeNextWeek  *string `json:"one_change_next_week"`
	NextWeekConstraint *string `json:"next_week_constraint"`
	UpdatedAt          *string `json:"updated_at,omitempty"`
}

// WeeklyReviewResponse is GET/PUT /weekly_reviews/:week_start.
type WeeklyReviewResponse struct {
	WeekStart          string        `json:"week_start"`
	WeeklyReview       *WeeklyReview `json:"weekly_review"`
	PreviousCommitment *string       `json:"previous_commitment"`
}

// RangeEntry is one stored entry from GET /entries.
type RangeEntry struct {
	GoalID   int         `json:"goal_id"`
	GoalKey  string      `json:"goal_key"`
	GoalSlug string      `json:"goal_slug"`
	GoalName string      `json:"goal_name"`
	Date     string      `json:"date"`
	Value    *EntryValue `json:"value"`
	Unit     *string     `json:"unit"`
}

// MarshalJSON keeps value a JSON number.
func (e RangeEntry) MarshalJSON() ([]byte, error) {
	var value any
	if e.Value != nil {
		value = e.Value.Float()
	}
	return json.Marshal(map[string]any{
		"goal_id": e.GoalID, "goal_key": e.GoalKey, "goal_slug": e.GoalSlug, "goal_name": e.GoalName,
		"date": e.Date, "value": value, "unit": e.Unit,
	})
}

// EntriesRange is GET /entries.
type EntriesRange struct {
	From    string       `json:"from"`
	To      string       `json:"to"`
	Entries []RangeEntry `json:"entries"`
}

// Insight is GET /progress_insight.
type Insight struct {
	WeekStart   string  `json:"week_start"`
	Source      string  `json:"source"`
	Stale       bool    `json:"stale"`
	GeneratedAt *string `json:"generated_at"`
	Assessment  *string `json:"assessment"`
	Guidance    *string `json:"guidance"`
	Risk        *string `json:"risk"`
	Leverage    *string `json:"leverage"`
}

// NextCycleResult is POST /twelve_week_years/:id/next_cycle and /fresh_start.
type NextCycleResult struct {
	Cycle        Cycle  `json:"twelve_week_year"`
	Goals        []Goal `json:"goals"`
	CarryForward *struct {
		Copied  []string `json:"copied"`
		Invalid []struct {
			Name   string   `json:"name"`
			Errors []string `json:"errors"`
		} `json:"invalid"`
	} `json:"carry_forward,omitempty"`
}

// Review is GET /twelve_week_years/:id/review.
type Review struct {
	ID                 int                 `json:"id"`
	State              string              `json:"state"`
	Period             ReviewPeriod        `json:"period"`
	Summary            ReviewSummary       `json:"summary"`
	WeeklyPerformance  []ReviewWeek        `json:"weekly_performance"`
	GoalReviews        []ReviewGoal        `json:"goal_reviews"`
	Learnings          ReviewLearnings     `json:"learnings"`
	ReflectionSections []ReflectionSection `json:"reflection_sections"`
	Reflection         map[string]*string  `json:"reflection,omitempty"`
}

type ReviewPeriod struct {
	StartDate          string `json:"start_date"`
	EndDate            string `json:"end_date"`
	CurrentWeekNumber  int    `json:"current_week_number"`
	ReviewVisibleInApp bool   `json:"review_visible_in_app"`
	ReviewDismissed    bool   `json:"review_dismissed"`
}

type ReviewSummary struct {
	ExecutionPercentage    float64          `json:"execution_percentage"`
	Grade                  string           `json:"grade"`
	AverageWeeklyExecution float64          `json:"average_weekly_execution"`
	StrongWeeksCount       int              `json:"strong_weeks_count"`
	BestWeek               ReviewWeekDigest `json:"best_week"`
	WorstWeek              ReviewWeekDigest `json:"worst_week"`
	StrongestGoal          ReviewGoalDigest `json:"strongest_goal"`
	WeakestGoal            ReviewGoalDigest `json:"weakest_goal"`
	Takeaway               string           `json:"takeaway"`
}

type ReviewWeek struct {
	Number              int        `json:"number"`
	StartDate           string     `json:"start_date"`
	EndDate             string     `json:"end_date"`
	Available           bool       `json:"available"`
	Complete            bool       `json:"complete"`
	ExecutionPercentage EntryValue `json:"execution_percentage"`
	Grade               *string    `json:"grade"`
	TotalPoints         EntryValue `json:"total_points"`
	MaxPoints           EntryValue `json:"max_points"`
}

type ReviewWeekDigest struct {
	Number              int     `json:"number"`
	StartDate           string  `json:"start_date"`
	EndDate             string  `json:"end_date"`
	ExecutionPercentage float64 `json:"execution_percentage"`
	Grade               string  `json:"grade"`
}

type ReviewGoal struct {
	GoalID              int              `json:"goal_id"`
	GoalName            string           `json:"goal_name"`
	GoalSlug            string           `json:"goal_slug"`
	ExecutionPercentage float64          `json:"execution_percentage"`
	Grade               string           `json:"grade"`
	WeeksHit            int              `json:"weeks_hit"`
	TrendLabel          string           `json:"trend_label"`
	CoachingInsight     string           `json:"coaching_insight"`
	WeeklyPerformance   []ReviewGoalWeek `json:"weekly_performance"`
}

type ReviewGoalWeek struct {
	Number              int        `json:"number"`
	StartDate           string     `json:"start_date"`
	EndDate             string     `json:"end_date"`
	Available           bool       `json:"available"`
	ExecutionPercentage EntryValue `json:"execution_percentage"`
	EarnedPoints        EntryValue `json:"earned_points"`
	MaxPoints           EntryValue `json:"max_points"`
	HitTarget           bool       `json:"hit_target"`
}

type ReviewGoalDigest struct {
	GoalID              int     `json:"goal_id"`
	Name                string  `json:"name"`
	Slug                string  `json:"slug"`
	ExecutionPercentage float64 `json:"execution_percentage"`
	Grade               string  `json:"grade"`
	WeeksHit            int     `json:"weeks_hit"`
	TrendLabel          string  `json:"trend_label"`
}

type ReviewLearnings struct {
	WhatWorked           string `json:"what_worked"`
	WhatLimitedExecution string `json:"what_limited_execution"`
	WhatNeedsRedesign    string `json:"what_needs_redesign"`
	WhatToCarryForward   string `json:"what_to_carry_forward"`
	NextCycleAdjustment  string `json:"next_cycle_adjustment"`
}

type ReflectionSection struct {
	Key           string   `json:"key"`
	Title         string   `json:"title"`
	SystemInsight string   `json:"system_insight"`
	Question      string   `json:"question"`
	CueChips      []string `json:"cue_chips"`
	Answer        *string  `json:"answer,omitempty"`
}

// EntryValue tolerates the API's mix of JSON numbers, numeric strings
// (Rails decimals) and booleans.
type EntryValue struct {
	raw     string
	isFloat bool
	float   float64
}

// NewFloatValue builds a numeric EntryValue.
func NewFloatValue(f float64) EntryValue { return EntryValue{isFloat: true, float: f} }

func (value *EntryValue) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var decoded string
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		if asNumber, err := strconv.ParseFloat(decoded, 64); err == nil {
			*value = EntryValue{isFloat: true, float: asNumber}
			return nil
		}
		*value = EntryValue{raw: decoded}
		return nil
	}
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*value = EntryValue{raw: strconv.FormatBool(asBool)}
		return nil
	}
	var asNumber float64
	if err := json.Unmarshal(data, &asNumber); err != nil {
		return err
	}
	*value = EntryValue{isFloat: true, float: asNumber}
	return nil
}

// MarshalJSON emits a number where possible.
func (value EntryValue) MarshalJSON() ([]byte, error) {
	if value.isFloat {
		return json.Marshal(value.float)
	}
	if value.raw == "" {
		return []byte("null"), nil
	}
	return json.Marshal(value.raw)
}

// Float returns the numeric value (0 for non-numeric or empty).
func (value EntryValue) Float() float64 {
	if value.isFloat {
		return value.float
	}
	if value.raw != "" {
		if f, err := strconv.ParseFloat(value.raw, 64); err == nil {
			return f
		}
	}
	return 0
}

func (value EntryValue) String() string {
	if value.isFloat {
		return fmt.Sprintf("%.2f", value.float)
	}
	if value.raw != "" {
		return value.raw
	}
	return "0"
}

// FloatString always renders two decimals.
func (value EntryValue) FloatString() string {
	if value.isFloat {
		return fmt.Sprintf("%.2f", value.float)
	}
	if value.raw != "" {
		return value.raw
	}
	return "0.00"
}

// BoolString renders a checkbox value as true/false.
func (value EntryValue) BoolString() string {
	if value.isFloat {
		return strconv.FormatBool(value.float != 0)
	}
	raw := strings.TrimSpace(strings.ToLower(value.raw))
	switch raw {
	case "":
		return "false"
	case "true", "1", "1.0", "yes":
		return "true"
	case "false", "0", "0.0", "no":
		return "false"
	default:
		return raw
	}
}
