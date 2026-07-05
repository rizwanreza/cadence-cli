# Skill: Cadence CLI

## When to use
- Use this skill when you need to list goals, inspect 12-week cycles, read a 12-week review, or mark a goal as complete from the command line.
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
```

Mark a goal complete by slug or id:

```bash
./cadence complete -goal meditation
./cadence complete -goal 3 -date 2026-02-05
./cadence complete -goal deep_work -value 2
```

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
- `-json` is useful for agents that want the raw API payload.
- `-goal` accepts a goal `id` or `slug`.
- `-date` is optional; must be `YYYY-MM-DD` if provided.
- `-value` is optional; if omitted, boolean goals default to `1` and numeric goals default to the goal target value.

## Troubleshooting
- `unauthorized` errors mean the token is missing or invalid — run `./cadence login` with a fresh token from Settings → CLI Access.
- If goals are missing, confirm the server is running and you have an active 12-week cycle.
- If you get a `422` on dates, ensure `-date` is ISO8601 (`YYYY-MM-DD`).
