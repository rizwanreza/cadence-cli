# Cadence CLI

Track goals and log progress from the terminal. The CLI talks to the Cadence
JSON API and authenticates as a real user with a personal API token.

## Build

```bash
cd cli
go build -o cadence
```

## Authentication

The API requires a personal token (session cookies are for the web app only).

1. In the web app, open **Settings → CLI Access** and click **Generate CLI Token**.
2. Save it locally:

   ```bash
   ./cadence login
   # Paste your token when prompted
   ```

`login` writes `~/.config/cadence/config.json` (mode `0600`) with your token and
host. Every later command reads it automatically. Run `./cadence logout` to clear
the saved token.

You can skip the config and pass credentials ad hoc:

```bash
./cadence -token "$MY_TOKEN" goals
CADENCE_TOKEN=... ./cadence goals
```

## Host

The CLI targets `https://cadenceweek.com` by default. Override it, in order of
precedence:

- `-url http://localhost:3000` flag
- `CADENCE_URL` environment variable
- the `host` saved by `cadence login`

For local development:

```bash
./cadence login -url http://localhost:3000 -token <dev-token>
./cadence goals
```

## Commands

List goals in the current cycle:

```bash
./cadence goals
```

List available 12-week years:

```bash
./cadence cycles
```

Show goal status for a date (defaults to today):

```bash
./cadence today
./cadence today -date 2026-02-05
./cadence today -date 2026-02-05 -slug protein   # filter to one goal
./cadence today --json                           # machine-readable output
```

The human table reads clearly for every goal kind:

```
 Goal        Type        Progress          Status
 Protein     threshold     105 / 160 g       ✗ incomplete
 Deep Work   cumulative       3 / 1 sessions    ✓ done
 Meditation  yes/no   —                 ✗ incomplete
```

`today --json` emits an array of objects (one per goal), ideal for agents:

```json
[
  {
    "id": 26,
    "slug": "protein",
    "name": "Protein",
    "goal_type": "boolean",
    "kind": "numeric",
    "frequency": "daily",
    "target_value": 160,
    "date": "2026-07-04",
    "completed": false,
    "value": 105,
    "unit": "g"
  }
]
```

Field notes:

- `value` is a JSON **number** (or `null` when no entry is logged yet), never a
  quoted string.
- `goal_type` is the real underlying enum (`boolean`, `count`, or legacy
  `duration`). `kind` is a derived, automation-friendly classification:
  `pass_fail` (yes/no), `numeric` (a boolean goal with a threshold above 1, e.g.
  160 g protein), or `count` (cumulative).
- `unit` is the goal's optional unit label (e.g. `"g"`, `"sessions"`), or `null`.
- Combine `-slug <slug|id>` with `--json` to fetch a single goal's object — this
  covers the "one goal for a date" case without a separate command.

`goals` and `cycles` also accept `--json` for raw list payloads.

Read a 12-week review by cycle id:

```bash
./cadence review -id 3
./cadence review -id 3 -json
```

Mark a goal complete by slug or id (optional date/value):

```bash
./cadence complete -goal meditation
./cadence complete -goal 3 -date 2026-02-05
./cadence complete -goal deep_work -value 2
```

## Notes

- `login`/`logout` manage the saved token; all other commands need a valid token
  (via config, `-token`, or `CADENCE_TOKEN`) or the API returns 401.
- `cycles` lists saved 12-week years and whether the review window is visible in app.
- `review -id` reads the full review payload for one 12-week year.
- `-json` renders machine-readable JSON for agents and automation. Supported by
  `today`, `goals`, `cycles`, and `review`.
- `today` additionally accepts `-slug <slug|id>` to filter to a single goal.
- `-date` expects `YYYY-MM-DD` if provided.
- Numeric goals require `-value`; only yes/no goals default to `1`.

## Draft and activate the next cycle

A draft never changes the current cycle or its progress. Activation is explicit,
allowed only from the start date through the end date in your account timezone,
and rejects overlapping activated cycles. Once activated, goal definitions and
order are locked; goal notes remain editable.

The [18-goal example](examples/next-cycle-18-goals.json) is illustrative. Replace
its goals, start date, and stable `plan_key` with your agreed plan first:

```bash
# Validate and inspect the complete plan without saving anything.
./cadence cycles import -file examples/next-cycle-18-goals.json -dry-run

# Save a draft, then inspect the returned cycle ID (42 in this example).
./cadence cycles import -file examples/next-cycle-18-goals.json -json
./cadence cycles show -id 42
./cadence goals -cycle 42 -json

# Edit a goal without changing its stable key or creating a duplicate.
./cadence goals update -cycle 42 -goal reading -target 20 -weekly-cap 4 \
  -notes 'Audiobooks count. Four scored days; seven optional.' -json

# Supply every goal ID exactly once in the desired order.
./cadence goals reorder -cycle 42 -order 101,102,103,104,105,106,107,108,109,110,111,112,113,114,115,116,117,118 -json

# Export the exact plan; editing/importing by ID updates the existing draft.
./cadence cycles export -id 42 > next-cycle.json
./cadence cycles import -id 42 -file next-cycle.json -dry-run
./cadence cycles import -id 42 -file next-cycle.json -json

# Run only when authorized, on/after the intended start and before the cycle ends.
./cadence cycles activate -id 42 -json
./cadence score -cycle 42 -json
```

Retrying the original create/import with its unchanged `plan_key` and content
returns the same cycle and goal IDs. Different content under an existing key
returns a conflict; use `import -id` or `update` to edit the draft. A failed full
import/update saves none of its changes. Goal `key` values identify goals across
renames and reordering. Importing a complete plan replaces the draft's goal list;
omitted goals are removed. Activated plans cannot be replaced.

To create an empty draft and add goals individually:

```bash
./cadence cycles create -key next-quarter -name 'Next quarter' -start 2026-10-01 -json
./cadence goals add -cycle 42 -key reading -name Reading \
  -input number -scoring threshold -target 20 -unit min -weekly-cap 4 -json
./cadence cycles update -id 42 -name 'Autumn goals' -start 2026-10-02 -json
```

To copy goals without progress, export a cycle and import the document with a new
key and start date. Review the dry run before saving:

```bash
./cadence cycles export -id 42 > copied-plan.json
./cadence cycles import -file copied-plan.json -key next-copy -start 2027-01-01 -dry-run
```

## Tracking and score contract

- `goal_type` remains the raw legacy enum. `input_kind` is independently stored:
  `checkbox` or `number`. A one-minute threshold may use `number`.
- `scoring_mode` is `threshold` (all-or-nothing at the target) or `cumulative`
  (points for each complete target-sized unit). Legacy `duration` still scores
  as threshold. There is no reason to convert a minute goal to `duration`.
- Threshold `weekly_cap` is the number of scored days, 1–7. Additional days are
  optional and do not raise the weekly score. Cumulative `weekly_cap` remains
  a weekly point cap and can exceed 7; `points_per_unit` defaults to 1.
- Numeric CLI logging requires an explicit value. `complete -goal reading -value
  15` reports **15 / 20 min, incomplete**; `-value 20` reports complete.
- Boolean entry writes replace the day's value. Count and legacy duration entry
  writes **add** to the day's existing value. These legacy API semantics remain
  unchanged; do not retry cumulative logging blindly. Only plan creation/import
  and activation have the retry guarantees described above.
- `complete -json` emits numeric values, completion, target, unit, and modes.
  `today` without a date uses the account timezone. Historical `complete -date`
  resolves goals in the cycle containing that date.
- Weekly execution means points earned against the **full week target**, capped
  per goal. Pace and Sunday projection are separate values. `score -cycle 42
  -week 2026-10-05 -as-of 2026-10-08 -json` returns the same server snapshot used
  by the UI, including per-goal numerators and denominators.

A plan document uses `schema_version: 1`, `plan_key`, `name`, `start_date`, optional
`notes`, and an ordered `goals` array. Each goal carries a stable `key`, `name`,
`input_kind`, `scoring_mode`, positive integer `target_value`, optional `unit`,
`weekly_cap`, and optional `description`. Advanced fields include `points_per_unit`,
`min_value`, `max_value`, `step`, and legacy `goal_type`/`frequency`/`active_days`.
Export retains these fields; no progress entries are imported or exported.

Canonical REST endpoints are cycle create/show/update at `/api/twelve_week_years`,
activation creation at `/api/twelve_week_years/:id/activation`, goal ordering via
`PATCH /api/twelve_week_years/:id/goal_order`, and score inspection via
`GET /api/twelve_week_years/:id/scorecard`. Create/update receive `{ "plan": ...,
"dry_run": true|false }`. Goal reads accept `cycle_id` or `date`; goal mutations
use the existing goal resources with `cycle_id` to make the selected draft explicit.
