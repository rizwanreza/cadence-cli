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
 Protein     numeric     105 / 160 g       ✗ incomplete
 Deep Work   count       3 / 1 sessions    ✓ done
 Meditation  pass/fail   —                 ✗ incomplete
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
- If `-value` is omitted, boolean goals default to `1` and numeric goals default to the goal target value.
