# Cadence CLI

Run your 12-Week Year from the terminal, or let your agent do it.

`cadence` is the command-line client for [Cadence](https://cadenceweek.com). It
covers the whole loop: plan a cycle, check in every day, review each week,
close the cycle with a review, then plan the next one or start fresh. Every
command has `--json` output and stable exit codes, so scripts and AI agents
(Claude Code, Codex and others) can drive it. An agent skill ships inside the
binary.

More at **https://cadenceweek.com/cli**.

## Install

**macOS and Linux**

```bash
curl -fsSL https://cadenceweek.com/install-cli | bash
```

The script downloads the latest release for your platform, verifies it against
`checksums.txt` (SHA-256), installs `cadence` into `~/.local/bin` (or `~/bin`,
whichever is already on your `PATH`), and adds that directory to your shell's
`PATH` if needed. Options:

| Variable | Effect |
|---|---|
| `CADENCE_VERSION=1.2.0` | Install a specific version |
| `CADENCE_BIN_DIR=/usr/local/bin` | Install somewhere else |
| `CADENCE_SKIP_SETUP=1` | Skip the post-install step |
| `CADENCE_NO_MODIFY_PATH=1` | Never edit shell startup files |

When it runs non-interactively (an agent, CI), the installer also runs
`cadence skill install`.

**Homebrew**

```bash
brew install rizwanreza/tap/cadence
```

**Go**

```bash
go install github.com/rizwanreza/cadence-cli/cmd/cadence@latest
```

**Windows**: download the zip from the
[latest release](https://github.com/rizwanreza/cadence-cli/releases/latest).

Update any time with `cadence update`. It re-runs the installer, or tells
Homebrew and `go install` users which command to run. When a new release is
out, `cadence` mentions it on stderr at most once a day. The notice only shows
in an interactive terminal, and never with `--json` or in CI. Set
`CADENCE_NO_UPDATE_CHECK=1` to turn it off.

## Sign in

```bash
cadence login
```

Create a token under **Settings → CLI Access**
([cadenceweek.com/settings#cli](https://cadenceweek.com/settings#cli)); `login`
offers to open that page. Paste the token at the prompt, which hides your
input. The CLI checks the token with the server before saving it to
`~/.config/cadence/config.json` (mode `0600`), then prints who you're signed in
as.

```bash
cadence auth status        # host, account, token prefix, last used
cadence logout             # forget the token on this machine
cadence logout --revoke    # ...and revoke it on the server
```

Non-interactive: `printf '%s' "$TOKEN" | cadence login`, or skip saving
entirely with `CADENCE_TOKEN=... cadence status`.

## Use it with your agent

```bash
cadence skill install
```

This writes the Cadence skill (`SKILL.md`) to `~/.agents/skills/cadence/` (read
by Codex and other agents) and, if you use Claude Code, to
`~/.claude/skills/cadence/`. Use `--agent claude|codex|all` to choose. Each
copy carries a `.managed-by-cadence-cli` marker, and the CLI refreshes managed
copies automatically after an upgrade. A skill you wrote yourself is never
overwritten unless you pass `--force`.

Then just ask: *"log today's workout"*, *"I had 40g of protein"*, *"how's my
week?"*, *"let's do my weekly review"*, *"plan my next 12 weeks"*.

`cadence skill show` prints the skill; `cadence skill uninstall` removes it.

## Commands

```text
Daily check-ins:
  status      Where you are today: cycle, week, check-ins, and what's due
  today       Show each goal's check-in status for a day
  complete    Check off a yes/no goal (--value 0 unchecks)
  set         Replace a numeric goal's total for a day
  add         Add to a numeric goal's total for a day
  history     List logged entries over a date range

Weekly rhythm and reviews:
  score       Show the week's execution score, pace, risk, leverage and streaks
  insight     Show this week's coaching: assessment, guidance, risk and leverage
  week        Weekly review: show or save the four prompts
  review      Show a cycle's end-of-cycle review

Plan and manage cycles:
  cycles      List, plan, activate, review and restart 12-week cycles
  goals       List goals, or add, edit, reorder and archive them

Setup and maintenance:
  login, logout, auth status, doctor, skill, update, version, completion
```

`cadence <command> --help` shows every flag, with examples.

### A day

```bash
cadence status                          # today, week N of 12, check-ins, what's due
cadence complete --goal workout         # checkbox goal
cadence add --goal protein --value 40   # add to today's total
cadence set --goal reading --value 25   # replace today's total
cadence complete --goal journal --date 2026-09-27   # yesterday
```

`--goal` accepts a goal's id, slug, or exact name. Dates are `YYYY-MM-DD` in
your account's time zone, and default to today there. `complete` only works on
checkbox goals; numeric goals need `set` (replace) or `add` (increase). `set`
and `complete` are safe to repeat.

Each `add` is a new addition. It sends an `Idempotency-Key`. If the connection
drops, the CLI retries once with the same key, and the server replays the first
result instead of adding twice. After a timeout, check `cadence today`, or
retry with the key from the error message: `--idempotency-key <key>`.

### A week

```bash
cadence score                       # execution %, pace, tier, what's at risk, best move
cadence insight                     # coaching for the week
cadence week review show            # includes last week's commitment
cadence week review set --win "Four deep-work blocks" --derail "Late nights" \
  --change "Phone out of the bedroom" --constraint "Travel Thu-Fri"
cadence history --from 2026-09-21 --to 2026-09-27
```

### A cycle

```bash
cadence cycles                                   # all cycles
cadence review                                   # the end-of-cycle review
cadence cycles review set --what-drove-results "Morning blocks" \
  --next-cycle-adjustment "Plan on Sundays"      # any of the six reflection fields
cadence cycles next --start after                # draft the next cycle (after | today | after-13th)
cadence cycles show --id 43                      # check the draft
cadence cycles activate --id 43                  # schedule or start it
cadence cycles unschedule --id 43                # back to a draft, before it starts
cadence cycles fresh-start                       # lapsed? draft a fresh 12 weeks from today
cadence cycles fresh-start dismiss               # "not now"
cadence cycles notes --notes "Travel weeks 5-6"  # cycle notes, even on an active cycle
```

`cycles next` carries every active goal unless you pass `--goals a,b` or
`--blank`. Without `--start`, the first available start is used. If a draft for
the next cycle already exists, it is returned instead of a second one.

### Planning from a document

Plans can be written as JSON (`schema_version: 1`), previewed, and imported.
Retrying the same document with the same `plan_key` is safe.

```bash
cadence cycles import --file examples/next-cycle-18-goals.json --dry-run
cadence cycles import --file plan.json --json            # saves a draft
cadence cycles export --id 42 > plan.json                # edit, then:
cadence cycles import --id 42 --file plan.json
cadence goals add --cycle 42 --key reading --name "Reading" --input number \
  --scoring threshold --target 20 --unit min --weekly-cap 4
cadence goals reorder --cycle 42 --order 101,103,102
cadence goals archive --goal cold-shower
```

A plan has `plan_key`, `name`, `start_date`, optional `notes`, and an ordered
`goals` array. Each goal has a stable `key`, `name`, `input_kind`
(`checkbox`|`number`), `scoring_mode` (`threshold`|`cumulative`),
`target_value` (a positive integer), an optional `unit`, `weekly_cap` and
`description`. For threshold goals `weekly_cap` is the number of scored days
per week (1-7); for cumulative goals it is a weekly point cap. Importing a
complete plan replaces the draft's goal list. Activation is always a separate,
explicit step. You can activate a draft up to three weeks before it starts
(which schedules it); after that, goal definitions are locked.
[`examples/next-cycle-18-goals.json`](examples/next-cycle-18-goals.json) is an
illustrative plan.

## JSON output

Add `--json` to any command for machine-readable output on stdout. Examples:

- `today --json`: an array with one object per goal: `id`, `slug`, `name`,
  `goal_type`, `kind` (`pass_fail` | `numeric` | `count`), `input_kind`,
  `scoring_mode`, `target_value`, `date`, `completed`, `value` (a number, or
  `null` if nothing is logged) and `unit`.
- `status --json`: `today`, `weekday`, `user`, `active_cycle`, `closing_cycle`,
  `successor_cycle`, `week` (`week_start`, `review_saved`,
  `previous_commitment`, `due_review_week`), `checkins` (as in `today`),
  `checkins_done`, `checkins_total`, `flags` (`no_active_cycle`,
  `awaiting_review`, `final_stretch`, `lapsed`, `fresh_start_offer`,
  `successor_draft`, `successor_scheduled`, `weekly_review_due`,
  `checkins_pending`) and `suggestions` (`action`, `command`, `reason`).
- `set`/`add --json`: `previous_value`, `value`, `operation`, `completed`,
  `points`, plus the goal's target, unit and modes. `add` also returns
  `idempotency_key` and `replayed`.
- `score --json`: the server's weekly scorecard, including per-goal
  numerators and denominators. `risk` is `{message, goals: [{goal_id,
  goal_name}]}`, `leverage` is `{message, goal: {goal_id, goal_name,
  pace_percentage}}` and `streaks` is `{message, streaks: [{goal_id,
  goal_name, days}]}`. Each one is `null` when there's nothing to say.

Other commands pass through the API's response. The API is documented at
https://cadenceweek.com/cli.

## Errors and exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Other error (server error, rate limited, `doctor` found a problem) |
| 2 | Usage: unknown command or flag, missing or invalid argument, malformed date (HTTP 400) |
| 3 | Not signed in, or the token was rejected |
| 4 | Not found (goal, cycle, week) |
| 5 | Conflict or validation failure (HTTP 409 or 422) |
| 6 | Network: host unreachable or request timed out |

Errors go to stderr. With `--json` they are JSON, and stdout stays empty:

```json
{"error":{"code":"validation_failed","message":"Target must be greater than 0","details":["Target must be greater than 0"],"status":422},
 "errors":["Target must be greater than 0"],"exit_code":5}
```

## Configuration

| Setting | Flag | Environment | Default |
|---|---|---|---|
| Host | `--url` | `CADENCE_URL` | saved by `login`, else `https://cadenceweek.com` |
| Token | `--token` | `CADENCE_TOKEN` | saved by `login` |
| Request timeout | | `CADENCE_TIMEOUT` (`30s`, `1m` or seconds) | `10s` |
| Config directory | | `XDG_CONFIG_HOME` | `~/.config/cadence` |

Flags win over the environment, and the environment wins over saved config.
For local development against a Cadence checkout:
`cadence login --url http://localhost:3000`.

Every request sends `User-Agent: cadence-cli/<version> (<os>/<arch>)` and uses
the versioned API at `/api/v1`.

### Single-dash flags

Older releases used Go's standard `flag` package, so scripts in the wild spell
long flags with one dash (`cadence set -goal protein -value 40 -json`). Those
still work. Before parsing, the CLI rewrites `-name` to `--name` when `name` is
a known flag. A value that happens to start with a dash is left alone, as are
arguments after `--`. Underscores work in place of dashes
(`--closing_notes`). New scripts should use `--flag`.

## Troubleshooting

```bash
cadence doctor
```

`doctor` checks the binary against the latest release, whether the `cadence`
on your `PATH` is this binary, the saved login and its permissions, the server
connection and token, the server's minimum CLI version, and the agent skill.
Add `--json` for a machine-readable report. It exits 1 if any check fails.

Shell completions come in the release archives and the Homebrew cask. You can
also generate them: `cadence completion bash|zsh|fish|powershell --help`.

## Development

```bash
go test ./...
go vet ./...
golangci-lint run
sh scripts/test-install.sh                       # go install + smoke test the binary
goreleaser release --snapshot --clean && bash scripts/test-installer.sh
```

Releases are cut by pushing a `v*` tag. GoReleaser builds the archives,
`checksums.txt`, `install.sh` and the Homebrew cask.

## License

MIT © 2026 Mindpeck LLC. See [LICENSE](LICENSE).
