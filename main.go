package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// defaultHost is where the CLI points when nothing else is configured. Override
// with -url, the CADENCE_URL env var, or a host saved via `cadence login`.
const defaultHost = "https://cadenceweek.com"

// errUnauthorized is returned when the API rejects our token (or its absence).
var errUnauthorized = errors.New("unauthorized: run `cadence login` to save your API token (Settings → CLI Access in the web app)")

// client carries the resolved base URL and API token for every request.
type client struct {
	http    *http.Client
	baseURL string
	token   string
}

func (c *client) newRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// do sends the request and maps a 401 to errUnauthorized so callers can print a
// single actionable message instead of a raw server body.
func (c *client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, errUnauthorized
	}
	return resp, nil
}

type goal struct {
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

type goalStatus struct {
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
	Value       *entryValue `json:"value"`
}

// kind classifies a goal for automation and human display. The API's goal_type
// enum (boolean/count/duration) is not enough on its own: a "boolean" goal with
// a target above 1 is really a numeric threshold (e.g. hit 160g protein), which
// reads very differently from a simple yes/no goal.
func (s goalStatus) kind() string {
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
	case "count":
		return "count"
	case "boolean", "duration":
		if s.TargetValue > 1 {
			return "numeric"
		}
		return "pass_fail"
	default:
		return s.GoalType
	}
}

// todayItem is the machine-readable shape emitted by `today --json`. It flattens
// entryValue into a plain JSON number so agents never have to parse a string.
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

func newTodayItem(s goalStatus) todayItem {
	item := todayItem{
		ID:          s.ID,
		Slug:        s.Slug,
		Name:        s.Name,
		GoalType:    s.GoalType,
		Kind:        s.kind(),
		InputKind:   s.InputKind,
		ScoringMode: s.ScoringMode,
		Frequency:   s.Frequency,
		TargetValue: s.TargetValue,
		Date:        s.Date,
		Completed:   s.Completed,
		Unit:        s.Unit,
	}
	if s.Value != nil {
		v := s.Value.Float()
		item.Value = &v
	}
	return item
}

type goalEntry struct {
	ID        int        `json:"id"`
	GoalID    int        `json:"goal_id"`
	Date      string     `json:"date"`
	Value     entryValue `json:"value"`
	Completed bool       `json:"completed"`
	Points    entryValue `json:"points"`
}

type twelveWeekYear struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Status             string `json:"status"`
	PlanKey            string `json:"plan_key"`
	StartDate          string `json:"start_date"`
	EndDate            string `json:"end_date"`
	ReviewStartsOn     string `json:"review_starts_on"`
	ReviewWindowEndsOn string `json:"review_window_ends_on"`
	ReviewState        string `json:"review_state"`
	ReviewVisibleInApp bool   `json:"review_visible_in_app"`
	ReviewDismissed    bool   `json:"review_dismissed"`
}

type reviewResponse struct {
	ID                 int                 `json:"id"`
	State              string              `json:"state"`
	Period             reviewPeriod        `json:"period"`
	Summary            reviewSummary       `json:"summary"`
	WeeklyPerformance  []reviewWeek        `json:"weekly_performance"`
	GoalReviews        []reviewGoal        `json:"goal_reviews"`
	Learnings          reviewLearnings     `json:"learnings"`
	ReflectionSections []reflectionSection `json:"reflection_sections"`
}

type reviewPeriod struct {
	StartDate          string `json:"start_date"`
	EndDate            string `json:"end_date"`
	CurrentWeekNumber  int    `json:"current_week_number"`
	ReviewVisibleInApp bool   `json:"review_visible_in_app"`
	ReviewDismissed    bool   `json:"review_dismissed"`
}

type reviewSummary struct {
	ExecutionPercentage    float64          `json:"execution_percentage"`
	Grade                  string           `json:"grade"`
	AverageWeeklyExecution float64          `json:"average_weekly_execution"`
	StrongWeeksCount       int              `json:"strong_weeks_count"`
	BestWeek               reviewWeekDigest `json:"best_week"`
	WorstWeek              reviewWeekDigest `json:"worst_week"`
	StrongestGoal          reviewGoalDigest `json:"strongest_goal"`
	WeakestGoal            reviewGoalDigest `json:"weakest_goal"`
	Takeaway               string           `json:"takeaway"`
}

type reviewWeek struct {
	Number              int        `json:"number"`
	StartDate           string     `json:"start_date"`
	EndDate             string     `json:"end_date"`
	Available           bool       `json:"available"`
	Complete            bool       `json:"complete"`
	ExecutionPercentage entryValue `json:"execution_percentage"`
	Grade               *string    `json:"grade"`
	TotalPoints         entryValue `json:"total_points"`
	MaxPoints           entryValue `json:"max_points"`
}

type reviewWeekDigest struct {
	Number              int     `json:"number"`
	StartDate           string  `json:"start_date"`
	EndDate             string  `json:"end_date"`
	ExecutionPercentage float64 `json:"execution_percentage"`
	Grade               string  `json:"grade"`
}

type reviewGoal struct {
	GoalID              int              `json:"goal_id"`
	GoalName            string           `json:"goal_name"`
	GoalSlug            string           `json:"goal_slug"`
	ExecutionPercentage float64          `json:"execution_percentage"`
	Grade               string           `json:"grade"`
	WeeksHit            int              `json:"weeks_hit"`
	TrendLabel          string           `json:"trend_label"`
	CoachingInsight     string           `json:"coaching_insight"`
	WeeklyPerformance   []reviewGoalWeek `json:"weekly_performance"`
}

type reviewGoalWeek struct {
	Number              int        `json:"number"`
	StartDate           string     `json:"start_date"`
	EndDate             string     `json:"end_date"`
	Available           bool       `json:"available"`
	ExecutionPercentage entryValue `json:"execution_percentage"`
	EarnedPoints        entryValue `json:"earned_points"`
	MaxPoints           entryValue `json:"max_points"`
	HitTarget           bool       `json:"hit_target"`
}

type reviewGoalDigest struct {
	GoalID              int     `json:"goal_id"`
	Name                string  `json:"name"`
	Slug                string  `json:"slug"`
	ExecutionPercentage float64 `json:"execution_percentage"`
	Grade               string  `json:"grade"`
	WeeksHit            int     `json:"weeks_hit"`
	TrendLabel          string  `json:"trend_label"`
}

type reviewLearnings struct {
	WhatWorked           string `json:"what_worked"`
	WhatLimitedExecution string `json:"what_limited_execution"`
	WhatNeedsRedesign    string `json:"what_needs_redesign"`
	WhatToCarryForward   string `json:"what_to_carry_forward"`
	NextCycleAdjustment  string `json:"next_cycle_adjustment"`
}

type reflectionSection struct {
	Key           string   `json:"key"`
	Title         string   `json:"title"`
	SystemInsight string   `json:"system_insight"`
	Question      string   `json:"question"`
	CueChips      []string `json:"cue_chips"`
}

func main() {
	baseURL := flag.String("url", "", "Base URL for the Cadence app (defaults to "+defaultHost+")")
	token := flag.String("token", "", "API token (overrides CADENCE_TOKEN and saved config)")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fatal(err.Error())
	}

	// login/logout manage the saved config and do not need a resolved token.
	switch args[0] {
	case "login":
		if err := loginCommand(cfg, *baseURL, *token, args[1:]); err != nil {
			fatal(err.Error())
		}
		return
	case "logout":
		if err := logoutCommand(); err != nil {
			fatal(err.Error())
		}
		return
	}

	host, err := resolveHost(*baseURL, cfg)
	if err != nil {
		fatal(err.Error())
	}

	c := &client{
		http:    &http.Client{Timeout: 10 * time.Second},
		baseURL: host,
		token:   resolveToken(*token, cfg),
	}

	switch args[0] {
	case "goals":
		if err := goalsCommand(c, args[1:]); err != nil {
			fatal(err.Error())
		}
	case "cycles":
		if err := cyclesCommand(c, args[1:]); err != nil {
			fatal(err.Error())
		}
	case "review":
		if err := reviewCommand(c, args[1:]); err != nil {
			fatal(err.Error())
		}
	case "complete":
		if err := completeCommand(c, args[1:]); err != nil {
			fatal(err.Error())
		}
	case "today":
		if err := todayCommand(c, args[1:]); err != nil {
			fatal(err.Error())
		}
	case "score":
		if err := scoreCommand(c, args[1:]); err != nil {
			fatal(err.Error())
		}
	default:
		usage()
	}
}

// resolveHost picks the base URL from, in order: the -url flag, the CADENCE_URL
// env var, the saved config, then the built-in production default.
func resolveHost(flagURL string, cfg config) (string, error) {
	raw := flagURL
	if raw == "" {
		raw = os.Getenv("CADENCE_URL")
	}
	if raw == "" {
		raw = cfg.Host
	}
	if raw == "" {
		raw = defaultHost
	}
	return normalizeBaseURL(raw)
}

// resolveToken picks the API token from, in order: the -token flag, the
// CADENCE_TOKEN env var, then the saved config.
func resolveToken(flagToken string, cfg config) string {
	if flagToken != "" {
		return flagToken
	}
	if env := os.Getenv("CADENCE_TOKEN"); env != "" {
		return env
	}
	return cfg.Token
}

// config is persisted to ~/.config/cadence/config.json (0600) by `cadence login`.
type config struct {
	Host  string `json:"host,omitempty"`
	Token string `json:"token,omitempty"`
}

func configDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cadence"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cadence"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func loadConfig() (config, error) {
	path, err := configPath()
	if err != nil {
		return config{}, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config{}, nil
	}
	if err != nil {
		return config{}, err
	}

	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("could not parse %s: %w", path, err)
	}
	return cfg, nil
}

func saveConfig(cfg config) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(dir, "config.json")
	return os.WriteFile(path, data, 0o600)
}

// loginCommand saves the API token (and optional host) to the local config so
// later commands authenticate without repeating flags. The token comes from
// -token, else it is read from stdin so it never lands in shell history.
func loginCommand(cfg config, flagURL, flagToken string, args []string) error {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	urlFlag := flags.String("url", "", "Base URL to save (defaults to "+defaultHost+")")
	tokenFlag := flags.String("token", "", "API token to save")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			usage()
			return nil
		}
		return fmt.Errorf("invalid flags for login")
	}

	host := firstNonEmpty(flagURL, *urlFlag, cfg.Host, defaultHost)
	normalized, err := normalizeBaseURL(host)
	if err != nil {
		return err
	}

	token := firstNonEmpty(flagToken, *tokenFlag)
	if token == "" {
		token, err = promptForToken()
		if err != nil {
			return err
		}
	}
	if token == "" {
		return fmt.Errorf("no token provided")
	}

	if err := saveConfig(config{Host: normalized, Token: token}); err != nil {
		return err
	}

	path, _ := configPath()
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	fmt.Printf("%s\nHost:   %s\nConfig: %s\n", accent.Render("Saved CLI credentials."), normalized, path)
	return nil
}

func promptForToken() (string, error) {
	fmt.Fprint(os.Stderr, "Paste your Cadence API token (Settings → CLI Access): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// logoutCommand clears the saved token, leaving the host preference intact.
func logoutCommand() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Token = ""
	if err := saveConfig(cfg); err != nil {
		return err
	}
	fmt.Println("Cleared saved CLI token.")
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func usage() {
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	fmt.Println(heading.Render("Cadence CLI"))
	fmt.Println(subtle.Render("Track goals and log progress from the terminal."))
	fmt.Println()

	fmt.Println(heading.Render("Usage"))
	fmt.Println("  cadence login [-token <token>] [-url <base_url>]")
	fmt.Println("  cadence logout")
	fmt.Println("  cadence goals [-json]")
	fmt.Println("  cadence cycles [-json]")
	fmt.Println("  cadence cycles create -key <stable-key> -name <name> -start YYYY-MM-DD [-dry-run] [-json]")
	fmt.Println("  cadence cycles import -file <plan.json> [-id <draft-id>] [-dry-run] [-json]")
	fmt.Println("  cadence cycles show|export|activate -id <cycle-id> [-json]")
	fmt.Println("  cadence cycles update -id <draft-id> [-name <name>] [-start YYYY-MM-DD] [-json]")
	fmt.Println("  cadence goals add|update -cycle <id> [-goal <id|slug>] -name <name> -input checkbox|number -scoring threshold|cumulative -target <number> -weekly-cap <number> [-unit <unit>] [-notes <notes>] [-json]")
	fmt.Println("  cadence goals reorder -cycle <id> -order <id,id,...> [-json]")
	fmt.Println("  cadence score -cycle <id> [-week YYYY-MM-DD] [-as-of YYYY-MM-DD] [-json]")
	fmt.Println("  cadence review -id <twelve_week_year_id> [-json]")
	fmt.Println("  cadence complete -goal <id|slug> [-date YYYY-MM-DD] [-value <number>]")
	fmt.Println("  cadence today [-date YYYY-MM-DD] [-slug <slug|id>] [-json]")
	fmt.Println()
	fmt.Println(subtle.Render("  Defaults to " + defaultHost + ". Override with -url, CADENCE_URL, or `cadence login`."))
	fmt.Println(subtle.Render("  Auth token comes from -token, CADENCE_TOKEN, or `cadence login`."))
	fmt.Println()

	fmt.Println(heading.Render("Commands & Flags"))
	helper := help.New()
	fmt.Println(helper.View(cliKeyMap()))
}

func normalizeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("base URL must include scheme and host")
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}

// parseListFlags handles the shared -json flag for the simple list commands.
func parseListFlags(name string, args []string) (bool, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			usage()
			return false, nil
		}
		return false, fmt.Errorf("invalid flags for %s", name)
	}
	return *jsonOutput, nil
}

// printJSON marshals v as indented JSON to stdout.
func printJSON(v any) error {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(payload))
	return nil
}

func listGoals(c *client, args []string) error {
	flags := flag.NewFlagSet("goals", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	cycleID := flags.Int("cycle", 0, "Cycle id (including drafts)")
	date := flags.String("date", "", "Date to select the activated cycle")
	if err := flags.Parse(args); err != nil {
		return err
	}

	goals, err := c.fetchGoalsFor(*date, *cycleID)
	if err != nil {
		return err
	}

	if *jsonOutput {
		return printJSON(goals)
	}

	if len(goals) == 0 {
		fmt.Println(lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("245")).Render("No goals found."))
		return nil
	}

	rows := make([]table.Row, 0, len(goals))
	for _, g := range goals {
		rows = append(rows, table.Row{
			strconv.Itoa(g.ID),
			g.Slug,
			g.Name,
			kindLabel(g.status().kind()),
			weeklyTarget(g),
			goalTarget(g),
		})
	}

	columns := []table.Column{
		{Title: "ID", Width: 4},
		{Title: "Slug", Width: 14},
		{Title: "Name", Width: 28},
		{Title: "Type", Width: 10},
		{Title: "Weekly target", Width: 46},
		{Title: "Target", Width: 18},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
	)
	t.SetHeight(len(rows) + 1)
	t.SetStyles(tableStyles())

	fmt.Println(t.View())

	return nil
}

func listTwelveWeekYears(c *client, args []string) error {
	jsonOutput, err := parseListFlags("cycles", args)
	if err != nil {
		return err
	}

	cycles, err := c.fetchTwelveWeekYears()
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(cycles)
	}

	if len(cycles) == 0 {
		fmt.Println(lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("245")).Render("No 12-week years found."))
		return nil
	}

	rows := make([]table.Row, 0, len(cycles))
	for _, cycle := range cycles {
		visible := "no"
		if cycle.ReviewVisibleInApp {
			visible = "yes"
		}

		rows = append(rows, table.Row{
			strconv.Itoa(cycle.ID),
			cycle.Name,
			cycle.StartDate,
			cycle.EndDate,
			cycle.Status,
			cycle.ReviewState,
			visible,
		})
	}

	columns := []table.Column{
		{Title: "ID", Width: 4},
		{Title: "Name", Width: 20},
		{Title: "Start", Width: 12},
		{Title: "End", Width: 12},
		{Title: "Status", Width: 10},
		{Title: "Review", Width: 14},
		{Title: "Visible", Width: 8},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
	)
	t.SetHeight(len(rows) + 1)
	t.SetStyles(tableStyles())

	fmt.Println(t.View())
	return nil
}

func reviewCommand(c *client, args []string) error {
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	reviewID := flags.Int("id", 0, "12-week year id")
	jsonOutput := flags.Bool("json", false, "Render raw JSON")

	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			usage()
			return nil
		}
		return fmt.Errorf("invalid flags for review")
	}

	if *reviewID <= 0 {
		return fmt.Errorf("missing -id")
	}

	review, err := c.fetchReview(*reviewID)
	if err != nil {
		return err
	}

	if *jsonOutput {
		payload, err := json.MarshalIndent(review, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(payload))
		return nil
	}

	fmt.Println(renderReviewSummary(review))
	return nil
}

func completeCommand(c *client, args []string) error {
	flags := flag.NewFlagSet("complete", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	goalIdentifier := flags.String("goal", "", "Goal id or slug")
	date := flags.String("date", "", "Date in YYYY-MM-DD (defaults to today)")
	value := flags.String("value", "", "Entry value (optional)")
	jsonOutput := flags.Bool("json", false, "Emit the recorded entry as JSON")

	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			usage()
			return nil
		}
		return fmt.Errorf("invalid flags for complete")
	}

	if *goalIdentifier == "" {
		return fmt.Errorf("missing -goal")
	}

	var parsedDate string
	if *date != "" {
		if _, err := time.Parse("2006-01-02", *date); err != nil {
			return fmt.Errorf("invalid date: %s (expected YYYY-MM-DD)", *date)
		}
		parsedDate = *date
	}

	goals, err := c.fetchGoalsFor(parsedDate, 0)
	if err != nil {
		return err
	}

	goal, err := matchGoal(goals, *goalIdentifier)
	if err != nil {
		return err
	}
	if goal.status().kind() != "pass_fail" && *value == "" {
		return fmt.Errorf("numeric goals require -value; target is %s", goalTarget(goal))
	}
	if *value != "" {
		number, err := strconv.ParseFloat(*value, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("-value must be a finite number")
		}
	}

	entry, err := c.createEntry(goal.ID, parsedDate, *value)
	if err != nil {
		return err
	}

	if *jsonOutput {
		return printJSON(map[string]any{"id": entry.ID, "goal_id": entry.GoalID, "date": entry.Date, "value": entry.Value.Float(), "points": entry.Points.Float(), "completed": entry.Completed, "input_kind": goal.InputKind, "scoring_mode": goal.ScoringMode, "target_value": goal.Target, "unit": goal.Unit})
	}
	fmt.Println(renderEntrySummary(goal, entry))
	return nil
}

func todayCommand(c *client, args []string) error {
	flags := flag.NewFlagSet("today", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	date := flags.String("date", "", "Date in YYYY-MM-DD (defaults to today)")
	slug := flags.String("slug", "", "Only show the goal with this slug (or id)")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")

	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			usage()
			return nil
		}
		return fmt.Errorf("invalid flags for today")
	}

	var parsedDate string
	if *date != "" {
		if _, err := time.Parse("2006-01-02", *date); err != nil {
			return fmt.Errorf("invalid date: %s (expected YYYY-MM-DD)", *date)
		}
		parsedDate = *date
	}

	statuses, err := c.fetchCompletions(parsedDate)
	if err != nil {
		return err
	}

	if *slug != "" {
		statuses = filterStatuses(statuses, *slug)
	}

	if *jsonOutput {
		return renderTodayJSON(statuses)
	}

	if len(statuses) == 0 {
		fmt.Println(lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("245")).Render("No goals found."))
		return nil
	}
	if parsedDate == "" {
		parsedDate = statuses[0].Date
	}

	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	fmt.Println(accent.Render("Goal Status for " + parsedDate))
	fmt.Println()

	rows := make([]table.Row, 0, len(statuses))
	for _, s := range statuses {
		rows = append(rows, table.Row{
			s.Name,
			kindLabel(s.kind()),
			todayProgress(s),
			todayStatus(s),
		})
	}

	columns := []table.Column{
		{Title: "Goal", Width: 24},
		{Title: "Type", Width: 10},
		{Title: "Progress", Width: 16},
		{Title: "Status", Width: 14},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
	)
	t.SetHeight(len(rows) + 1)
	t.SetStyles(tableStyles())

	fmt.Println(t.View())

	// Summary line
	done := 0
	for _, s := range statuses {
		if s.Completed {
			done++
		}
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fmt.Println(muted.Render(fmt.Sprintf("%d / %d goals completed", done, len(statuses))))

	return nil
}

// filterStatuses narrows the list to a single goal matched by slug or numeric id.
func filterStatuses(statuses []goalStatus, identifier string) []goalStatus {
	id, isID := strconv.Atoi(identifier)
	filtered := make([]goalStatus, 0, 1)
	for _, s := range statuses {
		if s.Slug == identifier || (isID == nil && s.ID == id) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func renderTodayJSON(statuses []goalStatus) error {
	items := make([]todayItem, 0, len(statuses))
	for _, s := range statuses {
		items = append(items, newTodayItem(s))
	}
	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(payload))
	return nil
}

// kindLabel turns the machine kind into a compact, human-friendly column label.
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

// todayProgress renders the value/target column per goal kind:
//   - pass/fail goals have no meaningful magnitude, so show a dash
//   - numeric and count goals show "value / target unit" (e.g. "105 / 160 g")
func todayProgress(s goalStatus) string {
	if s.kind() == "pass_fail" {
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

// todayStatus reflects the real completed flag (never contradicts the ✓/✗ glyph).
func todayStatus(s goalStatus) string {
	if s.Completed {
		return "✓ done"
	}
	return "✗ incomplete"
}

// formatNumber prints a float without trailing zeros: 105 not 105.00, 2.5 stays 2.5.
func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func (c *client) fetchCompletions(date string) ([]goalStatus, error) {
	req, err := c.newRequest(http.MethodGet, "/api/completions?date="+date, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch completions: %s", strings.TrimSpace(string(body)))
	}

	var statuses []goalStatus
	if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
		return nil, err
	}

	return statuses, nil
}

func (c *client) fetchGoals() ([]goal, error) {
	return c.fetchGoalsFor("", 0)
}

func (c *client) fetchGoalsFor(date string, cycleID int) ([]goal, error) {
	query := url.Values{}
	if date != "" {
		query.Set("date", date)
	}
	if cycleID > 0 {
		query.Set("cycle_id", strconv.Itoa(cycleID))
	}
	path := "/api/goals"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	req, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch goals: %s", strings.TrimSpace(string(body)))
	}

	var goals []goal
	if err := json.NewDecoder(resp.Body).Decode(&goals); err != nil {
		return nil, err
	}

	return goals, nil
}

func (c *client) fetchTwelveWeekYears() ([]twelveWeekYear, error) {
	req, err := c.newRequest(http.MethodGet, "/api/twelve_week_years", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch 12-week years: %s", strings.TrimSpace(string(body)))
	}

	var cycles []twelveWeekYear
	if err := json.NewDecoder(resp.Body).Decode(&cycles); err != nil {
		return nil, err
	}

	return cycles, nil
}

func (c *client) fetchReview(id int) (reviewResponse, error) {
	req, err := c.newRequest(http.MethodGet, fmt.Sprintf("/api/twelve_week_years/%d/review", id), nil)
	if err != nil {
		return reviewResponse{}, err
	}

	resp, err := c.do(req)
	if err != nil {
		return reviewResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return reviewResponse{}, fmt.Errorf("failed to fetch review: %s", strings.TrimSpace(string(body)))
	}

	var review reviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&review); err != nil {
		return reviewResponse{}, err
	}

	return review, nil
}

func matchGoal(goals []goal, identifier string) (goal, error) {
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
	}

	return goal{}, fmt.Errorf("goal not found: %s", identifier)
}

func (c *client) createEntry(goalID int, date string, rawValue string) (goalEntry, error) {
	payload := map[string]any{
		"goal_entry": map[string]any{},
	}

	entry := payload["goal_entry"].(map[string]any)
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

	body, err := json.Marshal(payload)
	if err != nil {
		return goalEntry{}, err
	}

	req, err := c.newRequest(http.MethodPost, fmt.Sprintf("/api/goals/%d/entries", goalID), bytes.NewReader(body))
	if err != nil {
		return goalEntry{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return goalEntry{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return goalEntry{}, fmt.Errorf("failed to create entry: %s", strings.TrimSpace(string(body)))
	}

	var created goalEntry
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return goalEntry{}, err
	}

	return created, nil
}

type entryValue struct {
	raw     string
	isFloat bool
	float   float64
}

func (value *entryValue) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}

	if data[0] == '"' {
		var decoded string
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		if asNumber, err := strconv.ParseFloat(decoded, 64); err == nil {
			value.float = asNumber
			value.isFloat = true
			value.raw = ""
			return nil
		}
		value.raw = decoded
		value.isFloat = false
		return nil
	}

	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		if asBool {
			value.raw = "true"
		} else {
			value.raw = "false"
		}
		value.isFloat = false
		return nil
	}

	var asNumber float64
	if err := json.Unmarshal(data, &asNumber); err != nil {
		return err
	}
	value.float = asNumber
	value.isFloat = true
	value.raw = ""
	return nil
}

// Float returns the numeric value, parsing a stringified number if needed. It
// returns 0 for non-numeric or empty values.
func (value entryValue) Float() float64 {
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

func (value entryValue) String() string {
	if value.isFloat {
		return fmt.Sprintf("%.2f", value.float)
	}
	if value.raw != "" {
		return value.raw
	}
	return "0"
}

func (value entryValue) FloatString() string {
	if value.isFloat {
		return fmt.Sprintf("%.2f", value.float)
	}
	if value.raw != "" {
		return value.raw
	}
	return "0.00"
}

func (value entryValue) BoolString() string {
	if value.isFloat {
		if value.float != 0 {
			return "true"
		}
		return "false"
	}

	raw := strings.TrimSpace(strings.ToLower(value.raw))
	if raw == "" {
		return "false"
	}

	switch raw {
	case "true", "1", "1.0", "yes":
		return "true"
	case "false", "0", "0.0", "no":
		return "false"
	default:
		return raw
	}
}

func formatEntryValue(goal goal, entry goalEntry) string {
	if goal.status().kind() == "pass_fail" {
		return entry.Value.BoolString()
	}
	return entry.Value.String()
}

func formatEntryPoints(goal goal, entry goalEntry) string {
	return entry.Points.FloatString()
}

func renderEntrySummary(goal goal, entry goalEntry) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	date := dateStyle.Render(entry.Date)
	status := goal.status()
	status.Value = &entry.Value
	status.Completed = entry.Completed
	if status.kind() == "pass_fail" {
		return fmt.Sprintf("%s: %s for %s.", accent.Render(goal.Name), todayStatus(status), date)
	}
	return fmt.Sprintf("%s: %s — %s for %s.", accent.Render(goal.Name), todayProgress(status), todayStatus(status), date)
}

func renderReviewSummary(review reviewResponse) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	var builder strings.Builder
	builder.WriteString(accent.Render("12-Week Year Review"))
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("%s to %s\n", review.Period.StartDate, review.Period.EndDate))
	builder.WriteString(fmt.Sprintf("State: %s | Execution: %.1f%% | Grade: %s\n", review.State, review.Summary.ExecutionPercentage, review.Summary.Grade))
	builder.WriteString(fmt.Sprintf("Takeaway: %s\n\n", review.Summary.Takeaway))

	builder.WriteString(accent.Render("Performance"))
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Average weekly execution: %.1f%%\n", review.Summary.AverageWeeklyExecution))
	builder.WriteString(fmt.Sprintf("Strong weeks: %d\n", review.Summary.StrongWeeksCount))
	builder.WriteString(fmt.Sprintf("Best week: W%d at %.1f%%\n", review.Summary.BestWeek.Number, review.Summary.BestWeek.ExecutionPercentage))
	builder.WriteString(fmt.Sprintf("Lowest week: W%d at %.1f%%\n\n", review.Summary.WorstWeek.Number, review.Summary.WorstWeek.ExecutionPercentage))

	builder.WriteString(accent.Render("Goals"))
	builder.WriteString("\n")
	for _, goal := range topGoalReviews(review.GoalReviews, 3) {
		builder.WriteString(fmt.Sprintf("- %s: %.1f%%, %s, %s\n", goal.GoalName, goal.ExecutionPercentage, goal.TrendLabel, goal.CoachingInsight))
	}
	builder.WriteString("\n")

	builder.WriteString(accent.Render("Learnings"))
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("- What worked: %s\n", review.Learnings.WhatWorked))
	builder.WriteString(fmt.Sprintf("- What limited execution: %s\n", review.Learnings.WhatLimitedExecution))
	builder.WriteString(fmt.Sprintf("- What needs redesign: %s\n", review.Learnings.WhatNeedsRedesign))
	builder.WriteString(fmt.Sprintf("- What to carry forward: %s\n", review.Learnings.WhatToCarryForward))
	builder.WriteString(fmt.Sprintf("- Next cycle adjustment: %s\n\n", review.Learnings.NextCycleAdjustment))

	builder.WriteString(accent.Render("Reflection Prompts"))
	builder.WriteString("\n")
	for _, section := range review.ReflectionSections {
		builder.WriteString(fmt.Sprintf("- %s: %s\n", section.Title, section.Question))
		builder.WriteString(muted.Render(fmt.Sprintf("  Insight: %s\n", section.SystemInsight)))
	}

	return builder.String()
}

func topGoalReviews(reviews []reviewGoal, limit int) []reviewGoal {
	cloned := append([]reviewGoal(nil), reviews...)
	sort.Slice(cloned, func(i, j int) bool {
		return cloned[i].ExecutionPercentage > cloned[j].ExecutionPercentage
	})

	if len(cloned) <= limit {
		return cloned
	}
	return cloned[:limit]
}

func tableStyles() table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		Bold(true).
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	styles.Cell = styles.Cell.Foreground(lipgloss.Color("252"))
	return styles
}

func fatal(message string) {
	for _, arg := range os.Args[1:] {
		if arg == "-json" || arg == "--json" {
			_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"errors": []string{message}})
			os.Exit(1)
		}
	}
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

type cliKeys struct {
	Login    key.Binding
	Logout   key.Binding
	Goals    key.Binding
	Cycles   key.Binding
	Review   key.Binding
	Complete key.Binding
	Today    key.Binding
	URL      key.Binding
	Token    key.Binding
	ID       key.Binding
	JSON     key.Binding
	Goal     key.Binding
	Date     key.Binding
	Value    key.Binding
	Slug     key.Binding
}

func (k cliKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Login, k.Goals, k.Cycles, k.Review, k.Complete, k.Today}
}

func (k cliKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Login, k.Logout, k.Goals, k.Cycles, k.Review, k.Complete, k.Today},
		{k.URL, k.Token, k.ID, k.JSON, k.Goal, k.Date, k.Value, k.Slug},
	}
}

func cliKeyMap() cliKeys {
	return cliKeys{
		Login: key.NewBinding(
			key.WithKeys("login"),
			key.WithHelp("login", "save API token locally"),
		),
		Logout: key.NewBinding(
			key.WithKeys("logout"),
			key.WithHelp("logout", "clear saved token"),
		),
		Goals: key.NewBinding(
			key.WithKeys("goals"),
			key.WithHelp("goals", "list goals"),
		),
		Cycles: key.NewBinding(
			key.WithKeys("cycles"),
			key.WithHelp("cycles", "list 12-week years"),
		),
		Review: key.NewBinding(
			key.WithKeys("review"),
			key.WithHelp("review", "show 12-week review"),
		),
		Complete: key.NewBinding(
			key.WithKeys("complete"),
			key.WithHelp("complete", "log progress"),
		),
		Today: key.NewBinding(
			key.WithKeys("today"),
			key.WithHelp("today", "show goal status for a date"),
		),
		URL: key.NewBinding(
			key.WithKeys("-url <base_url>"),
			key.WithHelp("-url", "base URL (optional; defaults to production)"),
		),
		Token: key.NewBinding(
			key.WithKeys("-token <token>"),
			key.WithHelp("-token", "API token (optional; overrides saved config)"),
		),
		ID: key.NewBinding(
			key.WithKeys("-id <twelve_week_year_id>"),
			key.WithHelp("-id", "review cycle id"),
		),
		JSON: key.NewBinding(
			key.WithKeys("-json"),
			key.WithHelp("-json", "emit machine-readable JSON (today, goals, cycles, review)"),
		),
		Goal: key.NewBinding(
			key.WithKeys("-goal <id|slug>"),
			key.WithHelp("-goal", "goal identifier (required)"),
		),
		Date: key.NewBinding(
			key.WithKeys("-date YYYY-MM-DD"),
			key.WithHelp("-date", "entry date"),
		),
		Value: key.NewBinding(
			key.WithKeys("-value <number>"),
			key.WithHelp("-value", "entry value"),
		),
		Slug: key.NewBinding(
			key.WithKeys("-slug <slug|id>"),
			key.WithHelp("-slug", "filter `today` to a single goal"),
		),
	}
}
