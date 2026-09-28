# Skill: Cadence CLI

## When to use
- Use this skill when you need to list goals, inspect 12-week cycles, read a 12-week review, or log goal progress from the command line.
- The CLI talks to the Cadence API endpoints for goals, goal entries, completions, and 12-week year reviews.

## CLI location
- Source lives in `cli/`.
- Binary name is `cadence` when built.

## Build
From the repo root:

```bash
cd cli
go build -o cadence
```

## Authentication
The API authenticates the CLI with a personal token (not the web session).

1. Generate a token in the web app under **Settings → CLI Access**.
2. Save it: `./cadence login` (paste the token when prompted).

The token and host are stored in `~/.config/cadence/config.json` (mode `0600`).
Alternatively pass `-token <token>` / set `CADENCE_TOKEN` per invocation. Clear a
saved token with `./cadence logout`.

## Host
- Defaults to `https://cadenceweek.com`.
- Override with `-url`, the `CADENCE_URL` env var, or the host saved by `login`.

## Commands
List goals:

```bash
./cadence goals
```

Show goal status for a date (defaults to today):

```bash
./cadence today
./cadence today -date 2026-02-05
./cadence today -date 2026-02-05 -slug protein   # one goal only
./cadence today --json                           # machine-readable
```

`today --json` returns an array of objects. Each object carries: `id`, `slug`,
`name`, `goal_type` (real enum), `kind` (`pass_fail` | `numeric` | `count`),
`frequency`, `target_value`, `date`, `completed`, `value` (JSON number or
`null`), and `unit` (string or `null`). Filter to a single goal with
`-slug <slug|id>` — this covers the "single goal for a date" case, so no separate
command is needed.

Complete checkbox goals or explicitly set/add numeric progress:

```bash
./cadence complete -goal workout
./cadence complete -goal 3 -date 2026-02-05
./cadence add -goal deep_work -value 2
./cadence set -goal protein -value 113
./cadence add -goal protein -value 40
```

`complete` only accepts checkbox goals (optional `-value 0` unchecks).
Numeric goals, including threshold, count, and duration, require explicit
`set -value <total>` or `add -value <positive amount>`. `set` replaces the total;
`add` applies an atomic server increment. Never read, calculate, then set a total
to simulate adding: concurrent updates can be lost. A timed-out add may already
have succeeded; inspect `today` before retrying. Repeating `set` is idempotent.

If commands are missing, rebuild/install the current CLI as described in
[README.md](README.md), check `command -v cadence` and `cadence -help`, and ensure
the server has the new entry/increment resources. Do not use the old `complete`
command as a fallback for numeric writes. CLI installation does not deploy the
server. `set/add -json` include previous and final numeric values.

List available 12-week years:

```bash
./cadence cycles
```

Read a 12-week review:

```bash
./cadence review -id 3
./cadence review -id 3 -json
```

## Parameters
- `-url` is optional; defaults to production. Must include scheme and host if set.
- `-token` is optional; overrides the saved config and `CADENCE_TOKEN`.
- `cycles` lists saved 12-week years and their review window state.
- `review -id` reads the full review document for a cycle.
- `-json` emits machine-readable JSON. Supported by `today`, `goals`, `cycles`,
  `review`, `set`, `add`, and `complete`. In `today --json`, `value` is a real number (or `null`) and each
  goal carries a derived `kind` and optional `unit`.
- `today -slug <slug|id>` filters the output to a single goal.
- `-goal` accepts a goal `id` or `slug`.
- `-date` is optional; must be `YYYY-MM-DD` if provided.
- `-value` is required for numeric goals; only yes/no goals default to `1`.

## Troubleshooting
- `unauthorized` errors mean the token is missing or invalid — run `./cadence login` with a fresh token from Settings → CLI Access.
- If goals are missing, confirm the server is running and you have an active 12-week cycle.
- If you get a `422` on dates, ensure `-date` is ISO8601 (`YYYY-MM-DD`).

## Draft cycle plans

Use the versioned JSON contract and examples in [README.md](README.md) and
[examples/next-cycle-18-goals.json](examples/next-cycle-18-goals.json).

- Preview: `cadence cycles import -file plan.json -dry-run -json`.
- Save: `cadence cycles import -file plan.json -json`. Stable `plan_key` and goal
  keys make identical retries safe; changed content requires `-id <draft-id>`.
- Inspect/export: `cadence cycles show|export -id <id>`.
- Edit: `cadence goals update -cycle <id> -goal <key> -target 20 -weekly-cap 4`.
- Order: `cadence goals reorder -cycle <id> -order <every-goal-id,in-order>`.
- Activation is a distinct authorized action: `cadence cycles activate -id <id>`.
  It is permitted during the cycle date range or up to three weeks before the
  start (scheduling it), rejects overlaps, and locks goal definitions. Notes
  remain editable. Never activate as an import side effect.
- Numeric logging requires `-value`; count and legacy duration logging increments
  rather than replaces the day's total. Do not blindly retry those progress writes.
- `cadence score -cycle <id> [-week YYYY-MM-DD] [-as-of YYYY-MM-DD] -json`
  reads the full-week execution snapshot; pace and projection are separate metrics.
