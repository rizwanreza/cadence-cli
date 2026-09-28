package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rizwanreza/cadence-cli/internal/config"
	"github.com/rizwanreza/cadence-cli/internal/skill"
	"github.com/rizwanreza/cadence-cli/internal/version"
	"github.com/rizwanreza/cadence-cli/skills"
)

// --- exit codes and JSON errors -------------------------------------------

func TestExitCodesForAPIErrors(t *testing.T) {
	testEnv(t)
	for _, tc := range []struct {
		status int
		code   int
		errKey string
	}{
		{401, ExitAuth, "unauthorized"},
		{403, ExitAuth, "forbidden"},
		{404, ExitNotFound, "not_found"},
		{409, ExitConflict, "conflict"},
		{422, ExitConflict, "validation_failed"},
		{400, ExitUsage, "bad_request"},
		{429, ExitError, "rate_limited"},
		{500, ExitError, "server_error"},
	} {
		f := newFake(t)
		f.on("GET", "/api/v1/completions", tc.status, `{"error":{"code":"`+tc.errKey+`","message":"Nope","details":["Nope"]},"errors":["Nope"]}`)
		res := run(t, f, "today", "--json")
		if res.code != tc.code {
			t.Fatalf("status %d: exit %d, want %d", tc.status, res.code, tc.code)
		}
		if res.stdout != "" {
			t.Fatalf("status %d: stdout should be empty on error, got %q", tc.status, res.stdout)
		}
		body := decodeJSON(t, res.stderr)
		errObj := body["error"].(map[string]any)
		if errObj["code"] != tc.errKey || body["exit_code"] != float64(tc.code) || errObj["status"] != float64(tc.status) {
			t.Fatalf("status %d: unexpected JSON error %s", tc.status, res.stderr)
		}
		if errs, _ := body["errors"].([]any); len(errs) != 1 {
			t.Fatalf("legacy errors array missing: %s", res.stderr)
		}
	}
}

func TestNetworkErrorExitsSix(t *testing.T) {
	testEnv(t)
	t.Setenv("CADENCE_URL", "http://127.0.0.1:1")
	res := run(t, nil, "today", "-json")
	if res.code != ExitNetwork {
		t.Fatalf("exit %d, want %d (%s)", res.code, ExitNetwork, res.stderr)
	}
	if decodeJSON(t, res.stderr)["error"].(map[string]any)["code"] != "network" {
		t.Fatal(res.stderr)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	testEnv(t)
	for _, args := range [][]string{
		{"bogus"},
		{"cycles", "bogus"},
		{"today", "--nope"},
		{"today", "--date", "yesterday"},
		{"history", "--from", "2026-09-10", "--to", "2026-09-01"},
		{"cycles", "next", "--id", "1", "--start", "someday"},
		{"week", "review", "set"},
	} {
		res := run(t, nil, args...)
		if res.code != ExitUsage {
			t.Fatalf("%v: exit %d, want 2 (%s)", args, res.code, res.stderr)
		}
	}
	res := run(t, nil, "bogus", "--json")
	if decodeJSON(t, res.stderr)["error"].(map[string]any)["code"] != "usage" {
		t.Fatalf("expected a JSON usage error: %s", res.stderr)
	}
	if res := run(t, nil, "stauts"); !strings.Contains(res.stderr, "Did you mean") {
		t.Fatalf("expected a suggestion: %s", res.stderr)
	}
}

func TestNotSignedInExitsThree(t *testing.T) {
	testEnv(t)
	t.Setenv("CADENCE_TEST_NO_TOKEN", "1")
	res := run(t, nil, "status")
	if res.code != ExitAuth || !strings.Contains(res.stderr, "cadence login") {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
}

func TestEveryRequestSendsUserAgent(t *testing.T) {
	testEnv(t)
	f := newFake(t).on("GET", "/api/v1/completions", 200, `[]`)
	run(t, f, "today")
	ua := f.last("GET", "/api/v1/completions").Header.Get("User-Agent")
	if !strings.HasPrefix(ua, "cadence-cli/"+version.Version+" (") {
		t.Fatalf("User-Agent = %q", ua)
	}
}

// --- flag normalization -------------------------------------------------------

func TestNormalizeArgs(t *testing.T) {
	root := NewRootCmd(&App{})
	for _, tc := range []struct{ in, want []string }{
		{[]string{"today", "-json", "-slug", "protein"}, []string{"today", "--json", "--slug", "protein"}},
		{[]string{"-url", "http://x", "goals", "-cycle=3"}, []string{"--url", "http://x", "goals", "--cycle=3"}},
		{[]string{"add", "-goal", "p", "-value", "-1"}, []string{"add", "--goal", "p", "--value", "-1"}},
		{[]string{"cycles", "notes", "-notes", "-json"}, []string{"cycles", "notes", "--notes", "-json"}},
		{[]string{"cycles", "create", "-dry-run", "-key", "k"}, []string{"cycles", "create", "--dry-run", "--key", "k"}},
		{[]string{"-h"}, []string{"-h"}},
		{[]string{"-help"}, []string{"--help"}},
		{[]string{"set", "--", "-goal"}, []string{"set", "--", "-goal"}},
		{[]string{"today", "-unknownflag"}, []string{"today", "-unknownflag"}},
	} {
		if got := NormalizeArgs(root, tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("NormalizeArgs(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// --- help (golden-ish) --------------------------------------------------------

func TestRootHelpListsEveryCommand(t *testing.T) {
	testEnv(t)
	res := run(t, nil, "--help")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	mustContain(t, res.stdout, "Daily check-ins:", "Weekly rhythm and reviews:", "Plan and manage cycles:", "Setup and maintenance:")
	for _, name := range []string{"status", "today", "complete", "set", "add", "history", "score", "insight", "week", "review",
		"cycles", "goals", "login", "logout", "auth", "doctor", "skill", "update", "version", "completion"} {
		if !strings.Contains(res.stdout, "\n  "+name+" ") {
			t.Fatalf("help is missing %q:\n%s", name, res.stdout)
		}
	}
	// Per-command help works with the old single-dash spelling and exits 0.
	res = run(t, nil, "cycles", "next", "-help")
	if res.code != 0 {
		t.Fatalf("exit %d", res.code)
	}
	mustContain(t, res.stdout, "after-13th", "--goals", "--start")
	res = run(t, nil, "cycles", "--help")
	for _, sub := range []string{"activate", "create", "export", "fresh-start", "import", "next", "notes", "review", "show", "unschedule", "update"} {
		mustContain(t, res.stdout, "  "+sub+" ")
	}
}

func TestVersionJSON(t *testing.T) {
	testEnv(t)
	res := run(t, nil, "version", "--json")
	v := decodeJSON(t, res.stdout)
	for _, key := range []string{"version", "commit", "date", "go", "os", "arch"} {
		if _, ok := v[key]; !ok {
			t.Fatalf("missing %q: %s", key, res.stdout)
		}
	}
	if res := run(t, nil, "--version"); !strings.HasPrefix(res.stdout, "cadence ") {
		t.Fatal(res.stdout)
	}
}

func TestCompletionScripts(t *testing.T) {
	testEnv(t)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		res := run(t, nil, "completion", shell)
		if res.code != 0 || !strings.Contains(res.stdout, "cadence") {
			t.Fatalf("%s completion failed: %d %s", shell, res.code, res.stderr)
		}
	}
}

// --- status -----------------------------------------------------------------

func statusFake(t *testing.T, me string) *fake {
	f := newFake(t)
	f.on("GET", "/api/v1/me", 200, me)
	f.on("GET", "/api/v1/completions", 200, `[
	  {"id":1,"slug":"workout","name":"Workout","goal_type":"boolean","input_kind":"checkbox","target_value":1,"date":"2026-09-28","completed":true,"value":1},
	  {"id":2,"slug":"protein","name":"Protein","goal_type":"boolean","input_kind":"number","target_value":160,"unit":"g","date":"2026-09-28","completed":false,"value":null}]`)
	f.handle("GET", "/api/v1/weekly_reviews/2026-09-28", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"week_start":"2026-09-28","weekly_review":null,"previous_commitment":"Phone out of the bedroom"}`))
	})
	f.handle("GET", "/api/v1/weekly_reviews/2026-09-21", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"week_start":"2026-09-21","weekly_review":null,"previous_commitment":null}`))
	})
	return f
}

func TestStatusJSON(t *testing.T) {
	testEnv(t)
	f := statusFake(t, meActive)
	res := run(t, f, "status", "--json")
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &report); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(report))
	for k := range report {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"active_cycle", "checkins", "checkins_done", "checkins_total", "closing_cycle", "flags", "now", "successor_cycle", "suggestions", "today", "user", "week", "weekday"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("status keys = %v, want %v", keys, want)
	}
	flags := report["flags"].(map[string]any)
	// 2026-09-28 is a Monday with last week's review missing.
	if flags["weekly_review_due"] != true || flags["checkins_pending"] != true || flags["awaiting_review"] != false || flags["lapsed"] != false {
		t.Fatalf("unexpected flags: %v", flags)
	}
	week := report["week"].(map[string]any)
	if week["previous_commitment"] != "Phone out of the bedroom" || week["due_review_week"] != "2026-09-21" {
		t.Fatalf("unexpected week: %v", week)
	}
	if report["weekday"] != "Monday" || report["checkins_done"] != float64(1) || report["checkins_total"] != float64(2) {
		t.Fatalf("unexpected report: %s", res.stdout)
	}
	checkins := report["checkins"].([]any)
	if checkins[1].(map[string]any)["value"] != nil || checkins[1].(map[string]any)["kind"] != "numeric" {
		t.Fatalf("unexpected check-in: %v", checkins[1])
	}
	actions := []string{}
	for _, s := range report["suggestions"].([]any) {
		actions = append(actions, s.(map[string]any)["action"].(string))
	}
	if !reflect.DeepEqual(actions, []string{"weekly_review", "check_in"}) {
		t.Fatalf("suggestions = %v", actions)
	}
}

func TestStatusFlagsForClosingLapsedAndFinalStretch(t *testing.T) {
	testEnv(t)
	me := strings.NewReplacer(
		`"final_stretch": false, "lapsed": false`, `"final_stretch": true, "lapsed": true`,
		`"closing_cycle": null`, `"closing_cycle": {"id": 41, "name": "Summer", "status": "ended", "awaiting_review": true}`,
		`"successor_cycle": null`, `"successor_cycle": {"id": 43, "name": "Winter", "status": "draft", "start_date": "2026-12-07"}`,
		`"fresh_start_offer": false`, `"fresh_start_offer": true`,
	).Replace(meActive)
	f := statusFake(t, me)
	res := run(t, f, "status", "--json")
	report := decodeJSON(t, res.stdout)
	flags := report["flags"].(map[string]any)
	for _, key := range []string{"awaiting_review", "final_stretch", "lapsed", "fresh_start_offer", "successor_draft"} {
		if flags[key] != true {
			t.Fatalf("%s should be true: %v", key, flags)
		}
	}
	text := run(t, f, "status")
	mustContain(t, text.stdout, "Today is Monday, Sep 28 2026 (America/Chicago)", "week 3 of 12", "Check-ins: 1 / 2 done",
		"✓ Workout", "✗ Protein  0 / 160 g", "Last week's commitment: Phone out of the bedroom", "cadence review --id 41", "cadence cycles fresh-start")
}

func TestStatusWithoutActiveCycle(t *testing.T) {
	testEnv(t)
	me := strings.Replace(meActive, meActive[strings.Index(meActive, `"active_cycle"`):strings.Index(meActive, `"closing_cycle"`)], `"active_cycle": null, `, 1)
	f := newFake(t).on("GET", "/api/v1/me", 200, me)
	res := run(t, f, "status", "--json")
	report := decodeJSON(t, res.stdout)
	if report["flags"].(map[string]any)["no_active_cycle"] != true || report["week"] != nil {
		t.Fatalf("unexpected: %s", res.stdout)
	}
}

// --- history ------------------------------------------------------------------

func TestHistory(t *testing.T) {
	testEnv(t)
	f := newFake(t).on("GET", "/api/v1/entries", 200, `{"from":"2026-09-01","to":"2026-09-07","entries":[
	  {"goal_id":2,"goal_key":"protein","goal_slug":"protein","goal_name":"Protein","date":"2026-09-01","value":"120.0","completed":false,"unit":"g"}]}`)
	res := run(t, f, "history", "--from", "2026-09-01", "--to", "2026-09-07", "--goal", "protein", "--json")
	req := f.last("GET", "/api/v1/entries")
	if req.Query != "from=2026-09-01&goal=protein&to=2026-09-07" {
		t.Fatalf("query = %s", req.Query)
	}
	entry := decodeJSON(t, res.stdout)["entries"].([]any)[0].(map[string]any)
	if entry["value"] != float64(120) || entry["completed"] != false {
		t.Fatalf("value should be a JSON number: %s", res.stdout)
	}
	mustContain(t, run(t, f, "history").stdout, "2026-09-01", "Protein", "120 g")
}

// --- weekly review --------------------------------------------------------------

func TestWeekReviewShowAndSet(t *testing.T) {
	testEnv(t)
	f := newFake(t)
	f.on("GET", "/api/v1/weekly_reviews/current", 200, `{"week_start":"2026-09-28","weekly_review":{"biggest_win":"Shipped","derail_root_cause":null,"one_change_next_week":null,"next_week_constraint":null},"previous_commitment":"Plan Sundays"}`)
	f.on("PUT", "/api/v1/weekly_reviews/2026-09-21", 200, `{"week_start":"2026-09-21","weekly_review":{"biggest_win":"Four blocks","derail_root_cause":"Late nights","one_change_next_week":"Phone out","next_week_constraint":null},"previous_commitment":null}`)

	res := run(t, f, "week", "review", "show")
	mustContain(t, res.stdout, "week of 2026-09-28", "Last week's commitment: Plan Sundays", "Biggest win: Shipped")

	res = run(t, f, "week", "review", "set", "--week", "2026-09-21", "--win", "Four blocks", "--derail", "Late nights", "--change", "Phone out", "--json")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	var body map[string]map[string]string
	_ = json.Unmarshal([]byte(f.last("PUT", "/api/v1/weekly_reviews/2026-09-21").Body), &body)
	want := map[string]string{"biggest_win": "Four blocks", "derail_root_cause": "Late nights", "one_change_next_week": "Phone out"}
	if !reflect.DeepEqual(body["weekly_review"], want) {
		t.Fatalf("PUT body = %v (only changed fields must be sent)", body)
	}
	if decodeJSON(t, res.stdout)["week_start"] != "2026-09-21" {
		t.Fatal(res.stdout)
	}
}

// --- cycle lifecycle -------------------------------------------------------------

func TestCycleReviewSetDefaultsToClosingCycleAndDismiss(t *testing.T) {
	testEnv(t)
	me := strings.Replace(meActive, `"closing_cycle": null`, `"closing_cycle": {"id": 41, "status": "ended", "awaiting_review": true}`, 1)
	f := newFake(t).on("GET", "/api/v1/me", 200, me)
	f.on("PATCH", "/api/v1/twelve_week_years/41/review", 200, `{"id":41}`)
	f.on("POST", "/api/v1/twelve_week_years/41/review_dismissal", 200, `{"id":41,"review_dismissed":true}`)
	res := run(t, f, "cycles", "review", "set", "--what-drove-results", "Mornings", "-closing_notes", "Rest more")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	var body map[string]map[string]string
	_ = json.Unmarshal([]byte(f.last("PATCH", "/api/v1/twelve_week_years/41/review").Body), &body)
	if !reflect.DeepEqual(body["review"], map[string]string{"what_drove_results": "Mornings", "closing_notes": "Rest more"}) {
		t.Fatalf("PATCH body = %v", body)
	}
	if res := run(t, f, "cycles", "review", "dismiss", "--json"); res.code != 0 || decodeJSON(t, res.stdout)["review_dismissed"] != true {
		t.Fatalf("dismiss: %+v", res)
	}
}

func TestCyclesNextResolvesSlugsAndStartKey(t *testing.T) {
	testEnv(t)
	f := newFake(t)
	f.on("GET", "/api/v1/goals", 200, `[{"id":7,"slug":"reading","name":"Reading"},{"id":8,"slug":"protein","name":"Protein"}]`)
	f.on("POST", "/api/v1/twelve_week_years/42/next_cycle", 201, `{"twelve_week_year":{"id":43,"name":"Winter 2026","status":"draft","start_date":"2026-12-14","end_date":"2027-03-07"},
	  "goals":[{"id":90,"name":"Reading","target_value":20,"unit":"min","weekly_cap":4}],
	  "carry_forward":{"copied":["Reading"],"invalid":[{"name":"Protein","errors":["Target is too big"]}]}}`)
	res := run(t, f, "cycles", "next", "--id", "42", "--start", "after-13th", "--goals", "reading,8")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(f.last("POST", "/api/v1/twelve_week_years/42/next_cycle").Body), &body)
	if body["start"] != "after_13th" || !reflect.DeepEqual(body["goal_ids"], []any{float64(7), float64(8)}) {
		t.Fatalf("body = %v", body)
	}
	mustContain(t, res.stdout, "Winter 2026 [draft]", "cycle 43", "Not carried: Protein (Target is too big)", "cadence cycles activate --id 43")
}

func TestUnscheduleFreshStartAndNotes(t *testing.T) {
	testEnv(t)
	f := newFake(t).on("GET", "/api/v1/me", 200, meActive)
	f.on("DELETE", "/api/v1/twelve_week_years/43/activation", 200, `{"id":43,"status":"draft"}`)
	f.on("POST", "/api/v1/fresh_start", 201, `{"twelve_week_year":{"id":50,"name":"Fresh","status":"draft","start_date":"2026-09-28"},"goals":[]}`)
	f.on("POST", "/api/v1/fresh_start_dismissal", 204, ``)
	f.on("PATCH", "/api/v1/twelve_week_years/42", 200, `{"id":42,"notes":"Travel weeks 5-6"}`)
	f.on("GET", "/api/v1/goals", 200, `[{"id":7,"slug":"reading","name":"Reading"}]`)

	if res := run(t, f, "cycles", "unschedule", "--id", "43", "--json"); res.code != 0 || decodeJSON(t, res.stdout)["status"] != "draft" {
		t.Fatalf("unschedule: %+v", res)
	}
	res := run(t, f, "cycles", "fresh-start", "--goals", "reading", "--json")
	if res.code != 0 || decodeJSON(t, res.stdout)["twelve_week_year"].(map[string]any)["id"] != float64(50) {
		t.Fatalf("fresh-start: %+v", res)
	}
	if body := f.last("POST", "/api/v1/fresh_start").Body; body != `{"goal_ids":[7]}` {
		t.Fatalf("fresh-start body = %s", body)
	}
	if res := run(t, f, "cycles", "fresh-start", "dismiss", "--json"); res.code != 0 || decodeJSON(t, res.stdout)["dismissed"] != true {
		t.Fatalf("dismiss: %+v", res)
	}
	res = run(t, f, "cycles", "notes", "--notes", "Travel weeks 5-6", "--json")
	if res.code != 0 || decodeJSON(t, res.stdout)["notes"] != "Travel weeks 5-6" {
		t.Fatalf("notes: %+v", res)
	}
	if body := f.last("PATCH", "/api/v1/twelve_week_years/42").Body; body != `{"twelve_week_year":{"notes":"Travel weeks 5-6"}}` {
		t.Fatalf("notes body = %s", body)
	}
}

func TestFreshStartBlockedIsAConflict(t *testing.T) {
	testEnv(t)
	f := newFake(t).on("POST", "/api/v1/fresh_start", 422, `{"error":{"code":"validation_failed","message":"Fall 2026 started today. There's nothing to start over yet.","details":[]},"errors":[]}`)
	res := run(t, f, "cycles", "fresh-start")
	if res.code != ExitConflict || !strings.Contains(res.stderr, "nothing to start over") {
		t.Fatalf("%+v", res)
	}
}

func TestGoalsArchive(t *testing.T) {
	testEnv(t)
	f := newFake(t)
	f.on("GET", "/api/v1/goals", 200, `[{"id":7,"slug":"cold-shower","name":"Cold shower"}]`)
	f.on("DELETE", "/api/v1/goals/7", 204, ``)
	res := run(t, f, "goals", "archive", "--goal", "Cold shower", "--json")
	if res.code != 0 || decodeJSON(t, res.stdout)["goal_id"] != float64(7) {
		t.Fatalf("%+v", res)
	}
	if res := run(t, f, "goals", "archive", "--goal", "nope"); res.code != ExitNotFound {
		t.Fatalf("missing goal should exit 4, got %d", res.code)
	}
}

func TestInsightAndScoreSignals(t *testing.T) {
	testEnv(t)
	f := newFake(t).on("GET", "/api/v1/me", 200, meActive)
	f.on("GET", "/api/v1/progress_insight", 200, `{"week_start":"2026-09-28","source":"llm","stale":true,"generated_at":"2026-09-28T07:00:00Z",
	  "assessment":"Solid start.","guidance":"Protect mornings.","risk":"Protein slipped twice.","leverage":"Prep lunches."}`)
	f.on("GET", "/api/v1/twelve_week_years/42/scorecard", 200, `{"execution_percentage":40,"total_points":4,"max_possible_points":10,"pace_percentage":90,"rating":"On Track",
	  "period_start_date":"2026-09-28","period_end_date":"2026-10-04","as_of_date":"2026-09-28",
	  "risk":{"message":"Risk: Protein — avoid a second miss today.","goals":[{"goal_id":2,"goal_name":"Protein"}]},
	  "leverage":{"message":"Most leverage: complete Reading today to get back on pace.","goal":{"goal_id":7,"goal_name":"Reading","pace_percentage":33.3}},
	  "streaks":null}`)
	res := run(t, f, "insight", "--week", "2026-09-28")
	mustContain(t, res.stdout, "Assessment: Solid start.", "Leverage: Prep lunches.", "refreshing")
	if q := f.last("GET", "/api/v1/progress_insight").Query; q != "week_start=2026-09-28" {
		t.Fatalf("query = %s", q)
	}
	res = run(t, f, "score")
	mustContain(t, res.stdout, "Risk: Protein — avoid a second miss today.", "Most leverage: complete Reading")
}

// --- auth -------------------------------------------------------------------------

func TestLoginValidatesBeforeSaving(t *testing.T) {
	testEnv(t)
	t.Setenv("CADENCE_TEST_NO_TOKEN", "1")
	f := newFake(t)
	f.handle("GET", "/api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cad_good" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Unauthorized","details":[]},"errors":["Unauthorized"]}`))
			return
		}
		_, _ = w.Write([]byte(meActive))
	})
	t.Setenv("CADENCE_URL", f.srv.URL)

	res := runWith(t, f, "cad_bad\n", "login")
	if res.code != ExitAuth || !strings.Contains(res.stderr, "nothing was saved") {
		t.Fatalf("bad token: %+v", res)
	}
	if cfg, _ := config.Load(); cfg.Token != "" {
		t.Fatal("a rejected token was saved")
	}

	res = runWith(t, f, "cad_good\n", "login", "--json")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	out := decodeJSON(t, res.stdout)
	if out["signed_in"] != true || out["user"].(map[string]any)["email"] != "demo@example.com" {
		t.Fatal(res.stdout)
	}
	cfg, _ := config.Load()
	if cfg.Token != "cad_good" || cfg.Email != "demo@example.com" || cfg.Host != f.srv.URL {
		t.Fatalf("saved config = %+v", cfg)
	}
	path, _ := config.Path()
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode %v", info.Mode().Perm())
	}

	res = runWith(t, f, "", "auth", "status")
	mustContain(t, res.stdout, "Signed in: demo@example.com (America/Chicago)", "cad_Ab3dEf7h", "from config")
}

func TestLoginInteractiveOffersBrowserAndHidesInput(t *testing.T) {
	testEnv(t)
	f := newFake(t).on("GET", "/api/v1/me", 200, meActive)
	t.Setenv("CADENCE_URL", f.srv.URL)
	t.Setenv("CADENCE_TEST_NO_TOKEN", "1")
	app, out, errOut := newTestApp(f, "y\n")
	app.StdinIsTTY = func() bool { return true }
	var opened string
	app.OpenBrowser = func(u string) error { opened = u; return nil }
	app.ReadSecret = func() (string, error) { return "cad_hidden", nil }
	if code := app.Run([]string{"login"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if opened != f.srv.URL+"/settings#cli" {
		t.Fatalf("opened %q", opened)
	}
	mustContain(t, errOut.String(), "input is hidden")
	mustContain(t, out.String(), "Signed in as demo@example.com (America/Chicago).")
}

func TestLogoutRevoke(t *testing.T) {
	testEnv(t)
	f := newFake(t).on("DELETE", "/api/v1/token", 204, ``)
	if err := config.Save(config.Config{Host: f.srv.URL, Token: "cad_saved", Email: "demo@example.com"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CADENCE_TEST_NO_TOKEN", "1")
	res := run(t, f, "logout", "--revoke", "--json")
	if res.code != 0 || decodeJSON(t, res.stdout)["revoked"] != true {
		t.Fatalf("%+v", res)
	}
	if got := f.last("DELETE", "/api/v1/token").Header.Get("Authorization"); got != "Bearer cad_saved" {
		t.Fatalf("revoked with %q", got)
	}
	if cfg, _ := config.Load(); cfg.Token != "" || cfg.Host != f.srv.URL {
		t.Fatalf("config after logout = %+v", cfg)
	}
}

func TestAuthStatusNotSignedIn(t *testing.T) {
	testEnv(t)
	t.Setenv("CADENCE_TEST_NO_TOKEN", "1")
	res := run(t, nil, "auth", "status", "--json")
	if res.code != ExitAuth || decodeJSON(t, res.stdout)["signed_in"] != false {
		t.Fatalf("%+v", res)
	}
}

// --- skill ------------------------------------------------------------------------

func TestSkillInstallMarkerRefuseAndForce(t *testing.T) {
	home := testEnv(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := run(t, nil, "skill", "install", "--json")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	results := decodeJSON(t, res.stdout)["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected ~/.agents and ~/.claude installs: %s", res.stdout)
	}
	for _, dir := range []string{".agents/skills/cadence", ".claude/skills/cadence"} {
		content, err := os.ReadFile(filepath.Join(home, dir, "SKILL.md"))
		if err != nil || string(content) != string(skills.Cadence) {
			t.Fatalf("%s not written: %v", dir, err)
		}
		marker, _ := os.ReadFile(filepath.Join(home, dir, skill.MarkerFile))
		if strings.TrimSpace(string(marker)) != version.Version {
			t.Fatalf("marker = %q", marker)
		}
	}
	if res := run(t, nil, "skill", "install"); !strings.Contains(res.stdout, "Unchanged") {
		t.Fatalf("second install should be unchanged: %s", res.stdout)
	}

	// A hand-written skill is never overwritten without --force.
	custom := filepath.Join(home, ".claude", "skills", "cadence")
	_ = os.Remove(filepath.Join(custom, skill.MarkerFile))
	_ = os.WriteFile(filepath.Join(custom, "SKILL.md"), []byte("mine"), 0o644)
	res = run(t, nil, "skill", "install", "--agent", "claude")
	if res.code != ExitConflict || !strings.Contains(res.stderr, "--force") {
		t.Fatalf("expected refusal: %+v", res)
	}
	if content, _ := os.ReadFile(filepath.Join(custom, "SKILL.md")); string(content) != "mine" {
		t.Fatal("unmanaged skill was overwritten")
	}
	if res := run(t, nil, "skill", "install", "--agent", "claude", "--force"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if res := run(t, nil, "skill", "install", "--agent", "cursor"); res.code != ExitUsage {
		t.Fatalf("unknown agent should be a usage error: %d", res.code)
	}
}

func TestManagedSkillRefreshesAfterUpgrade(t *testing.T) {
	home := testEnv(t)
	old := version.Version
	t.Cleanup(func() { version.Version = old })
	version.Version = "1.0.0"
	dir := filepath.Join(home, ".agents", "skills", "cadence")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("old skill"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, skill.MarkerFile), []byte("0.9.0\n"), 0o644)
	unmanaged := filepath.Join(home, ".claude", "skills", "cadence")
	_ = os.MkdirAll(unmanaged, 0o755)
	_ = os.WriteFile(filepath.Join(unmanaged, "SKILL.md"), []byte("hand written"), 0o644)

	f := newFake(t).on("GET", "/api/v1/completions", 200, `[]`)
	if res := run(t, f, "today"); res.code != 0 {
		t.Fatal(res.stderr)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "SKILL.md")); string(content) != string(skills.Cadence) {
		t.Fatal("managed skill was not refreshed")
	}
	if marker, _ := os.ReadFile(filepath.Join(dir, skill.MarkerFile)); strings.TrimSpace(string(marker)) != "1.0.0" {
		t.Fatalf("marker = %q", marker)
	}
	if content, _ := os.ReadFile(filepath.Join(unmanaged, "SKILL.md")); string(content) != "hand written" {
		t.Fatal("unmanaged skill was touched")
	}
}

func TestEmbeddedSkillFrontmatter(t *testing.T) {
	s := string(skills.Cadence)
	mustContain(t, s, "---\nname: cadence\n", "description:", "argument-hint:", "cadence status --json")
	for _, stale := range []string{"./cadence", "is the server running", "go build"} {
		if strings.Contains(s, stale) {
			t.Fatalf("skill still contains stale %q", stale)
		}
	}
}

// --- doctor and update -------------------------------------------------------------

func TestDoctorJSON(t *testing.T) {
	testEnv(t)
	f := newFake(t).on("GET", "/api/v1/me", 200, meActive)
	f.on("GET", "/releases/latest", 200, `{"tag_name":"v9.9.9"}`)
	res := run(t, f, "doctor", "--json")
	report := decodeJSON(t, res.stdout)
	checks := map[string]string{}
	for _, c := range report["checks"].([]any) {
		m := c.(map[string]any)
		checks[m["name"].(string)] = m["status"].(string)
	}
	if checks["auth"] != "ok" || checks["skill"] != "warn" || checks["version"] != "warn" {
		t.Fatalf("checks = %v", checks)
	}
	if res.code != 0 || report["ok"] != true {
		t.Fatalf("doctor should pass with only warnings: %d %s", res.code, res.stdout)
	}

	t.Setenv("CADENCE_TOKEN", "cad_bad")
	f.on("GET", "/api/v1/me", 401, `{"error":{"code":"unauthorized","message":"Unauthorized","details":[]}}`)
	res = run(t, f, "doctor")
	if res.code != ExitError || !strings.Contains(res.stdout, "✗ auth") || res.stderr != "" {
		t.Fatalf("failing doctor: %+v", res)
	}
}

func TestDoctorFailsWhenServerNeedsNewerCLI(t *testing.T) {
	testEnv(t)
	old := version.Version
	t.Cleanup(func() { version.Version = old })
	version.Version = "1.0.0"
	f := newFake(t).on("GET", "/api/v1/me", 200, strings.Replace(meActive, `"min_version": "1.0.0"`, `"min_version": "1.2.0"`, 1))
	f.on("GET", "/releases/latest", 200, `{"tag_name":"v1.2.0"}`)
	res := run(t, f, "doctor", "--json")
	if res.code != ExitError || !strings.Contains(res.stdout, "requires cadence ≥ 1.2.0") {
		t.Fatalf("%+v", res)
	}
}

func TestUpdateMethods(t *testing.T) {
	testEnv(t)
	old := version.Version
	t.Cleanup(func() { version.Version = old })
	version.Version = "1.0.0"
	f := newFake(t).on("GET", "/releases/latest", 200, `{"tag_name":"v1.1.0"}`)

	app, out, _ := newTestApp(f, "")
	app.Executable = func() (string, error) { return "/opt/homebrew/Caskroom/cadence/1.0.0/cadence", nil }
	if code := app.Run([]string{"update", "--json"}); code != 0 {
		t.Fatal(code)
	}
	got := decodeJSON(t, out.String())
	if got["method"] != "brew" || got["command"] != "brew upgrade rizwanreza/tap/cadence" || got["update_available"] != true || got["ran"] != false {
		t.Fatalf("brew: %v", got)
	}

	app, out, _ = newTestApp(f, "")
	var ranIn string
	app.RunInstaller = func(dir string) error { ranIn = dir; return nil }
	app.Executable = func() (string, error) { return "/home/me/.local/bin/cadence", nil }
	if code := app.Run([]string{"update"}); code != 0 || ranIn != "" {
		t.Fatalf("without --yes and no TTY the installer must not run (ran in %q)", ranIn)
	}
	mustContain(t, out.String(), "1.1.0 is available", "--yes")
	if code := app.Run([]string{"update", "--yes"}); code != 0 || ranIn != "/home/me/.local/bin" {
		t.Fatalf("installer should run into the binary's dir, ran in %q", ranIn)
	}

	f2 := newFake(t).on("GET", "/releases/latest", 200, `{"tag_name":"v1.0.0"}`)
	app, out, _ = newTestApp(f2, "")
	app.RunInstaller = func(dir string) error { t.Fatal("already current"); return nil }
	app.Run([]string{"update", "--yes"})
	mustContain(t, out.String(), "is the latest release")
}

func TestUpdateNoticeOnlyOnTTYWithoutJSONOrCI(t *testing.T) {
	testEnv(t)
	old := version.Version
	t.Cleanup(func() { version.Version = old })
	version.Version = "1.0.0"
	f := newFake(t).on("GET", "/api/v1/completions", 200, `[]`)
	f.on("GET", "/releases/latest", 200, `{"tag_name":"v1.1.0"}`)
	t.Setenv("CADENCE_URL", f.srv.URL)
	t.Setenv("CADENCE_TOKEN", "test-token")

	runTTY := func(args ...string) string {
		app, _, errOut := newTestApp(f, "")
		app.StderrIsTTY = func() bool { return true }
		app.Run(args)
		return errOut.String()
	}
	if got := runTTY("today"); strings.Contains(got, "new cadence") {
		t.Fatalf("CI must suppress the notice: %q", got)
	}
	t.Setenv("CI", "")
	if got := runTTY("today", "--json"); strings.Contains(got, "new cadence") {
		t.Fatalf("--json must suppress the notice: %q", got)
	}
	if got := runTTY("today"); !strings.Contains(got, "1.0.0 → 1.1.0") {
		t.Fatalf("expected the notice on a TTY: %q", got)
	}
	// The result is cached: a second run doesn't hit GitHub again.
	before := 0
	for _, c := range f.calls() {
		if c.Path == "/releases/latest" {
			before++
		}
	}
	runTTY("today")
	after := 0
	for _, c := range f.calls() {
		if c.Path == "/releases/latest" {
			after++
		}
	}
	if after != before {
		t.Fatalf("expected a cached check, GitHub was hit %d more time(s)", after-before)
	}
}

func TestCyclesNextBlankDefaultStartAndConflicts(t *testing.T) {
	testEnv(t)
	f := newFake(t)
	f.on("POST", "/api/v1/twelve_week_years/42/next_cycle", 200, `{"twelve_week_year":{"id":43,"name":"Winter","status":"draft"},"goals":[],"carry_forward":null}`)
	res := run(t, f, "cycles", "next", "--id", "42", "--blank")
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	if body := f.last("POST", "/api/v1/twelve_week_years/42/next_cycle").Body; body != `{"goal_ids":[]}` {
		t.Fatalf("body = %s (no start: the server picks the first option)", body)
	}
	mustContain(t, res.stdout, "already exists")

	f.on("POST", "/api/v1/twelve_week_years/42/next_cycle", 409, `{"error":{"code":"conflict","message":"The next cycle is already scheduled.","details":[]},"errors":[]}`)
	if res := run(t, f, "cycles", "next", "--id", "42"); res.code != ExitConflict || !strings.Contains(res.stderr, "already scheduled") {
		t.Fatalf("%+v", res)
	}
	f.on("POST", "/api/v1/twelve_week_years/42/next_cycle", 422, `{"error":{"code":"validation_failed","message":"start must be one of: today, after_13th.","details":["start must be one of: today, after_13th."]},"errors":["start must be one of: today, after_13th."]}`)
	if res := run(t, f, "cycles", "next", "--id", "42", "--start", "after"); res.code != ExitConflict || !strings.Contains(res.stderr, "start must be one of: today, after_13th.") {
		t.Fatalf("%+v", res)
	}
}

func TestAddReportsIdempotentReplay(t *testing.T) {
	testEnv(t)
	f := newFake(t)
	f.on("GET", "/api/v1/goals", 200, `[{"id":26,"slug":"protein","name":"Protein","input_kind":"number","target_value":160,"unit":"g"}]`)
	f.handle("POST", "/api/v1/goals/26/entries/today/increments", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "meal-1" {
			t.Fatalf("key = %q", r.Header.Get("Idempotency-Key"))
		}
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"goal_id":26,"date":"2026-09-28","previous_value":0,"value":40,"completed":false}`))
	})
	res := run(t, f, "add", "--goal", "protein", "--value", "40", "--idempotency-key", "meal-1", "--json")
	out := decodeJSON(t, res.stdout)
	if out["replayed"] != true || out["idempotency_key"] != "meal-1" {
		t.Fatalf("%s", res.stdout)
	}
}
