---
name: cadence
description: Run the user's 12-Week Year in Cadence (cadenceweek.com) with the `cadence` CLI — daily check-ins, weekly reviews, scores and coaching, the end-of-cycle review, planning the next cycle, or a fresh start. Use when the user says things like "log my workout", "I had 40g of protein", "check off meditation", "did I do my reading yesterday", "how's my week", "what's my score", "weekly review", "review my cycle", "plan my next 12 weeks", "plan next cycle", "start fresh", or mentions Cadence, their 12 week year, goals, check-ins or execution score.
argument-hint: "[what to log, review or plan, e.g. 'log 40g protein' or 'weekly review']"
---

# Cadence

Cadence runs the 12-Week Year: a cycle of (normally) 12 weeks, each with a small
set of goals checked in daily, scored weekly, and reviewed at the end. The
`cadence` CLI talks to the user's account at cadenceweek.com. Everything you do
here changes the user's real data.

## Agent invariants

1. **Orient first.** Start every session with `cadence status --json`. It gives
   the user's `today`, active cycle, week N of M, today's check-ins, flags and
   suggested next steps.
2. **Parse JSON, not tables.** Add `--json` to every command you read. Errors
   then arrive on stderr as JSON: `{"error":{"code","message","details"},"exit_code":N}`.
3. **Use the user's date.** "Today" is `today` from `status --json` (the
   account's time zone), never the machine's date. Resolve "yesterday",
   "Monday" etc. from it and pass `--date YYYY-MM-DD` explicitly.
4. **Never blindly retry `add`.** Each `add` is a new addition. If one timed out
   (exit 6), check `cadence today --date <d> --json` before doing anything, or
   retry with the same `--idempotency-key` you used the first time. `set` and
   `complete` are safe to repeat.
5. **Confirm before irreversible or lifecycle actions.** Ask the user before
   `cycles activate`, `goals archive`, `cycles fresh-start` (and activating its
   draft), `cycles unschedule`, and `cycles review dismiss`. Show what will
   happen (dates, goals) and wait for a yes.
6. **Quote goal names verbatim.** Use names exactly as stored ("VO2 max", not
   "Vo2 Max"). Never recase them. In commands, prefer the goal's `slug` or `id`.
7. **Be a coach, not a scold.** ~85% execution is a great week; one week is one
   of twelve. Streaks are background signal: never frame advice around
   protecting a streak. Focus on what to do today.

## Decide what to do (from `status --json`)

```bash
cadence status --json
```

Check `flags`, in this order, and offer (don't force) the matching workflow:

| Flag | Meaning | Offer |
|---|---|---|
| `awaiting_review` | A cycle ended; its review is waiting | End-of-cycle review |
| `lapsed` / `fresh_start_offer` | No check-ins for 7+ days | Fresh start (or dismiss the offer) |
| `weekly_review_due` | Sunday/Monday and that week's review isn't saved (`week.due_review_week`) | Weekly review |
| `final_stretch` (and `successor_cycle` is null) | Last 14 days of the cycle | Plan the next cycle |
| `successor_draft` | A next-cycle draft exists but isn't scheduled | Review and activate it |
| `no_active_cycle` | Nothing running today | Plan a cycle (or the next one) |
| `checkins_pending` | Goals still unchecked today | Daily check-in |

`suggestions[]` lists the same, each with a ready `command`. If the user asked
for something specific, do that first and mention anything urgent afterwards.

## Workflows

### Daily check-in

1. `cadence today --json` (or `status --json` → `checkins`). Each goal has
   `kind`: `pass_fail` (checkbox), `numeric` (hit a threshold, e.g. 160 g), or
   `count` (cumulative units, e.g. sessions).
2. Map what the user said to goals; handle several in one go:
   - checkbox done → `cadence complete --goal <slug>`; undo → `--value 0`
   - "I had another 40 g of protein" → `cadence add --goal protein --value 40`
   - "I read 25 minutes today" (the day's total) → `cadence set --goal reading --value 25`
   - earlier days → add `--date YYYY-MM-DD`
3. Ambiguous whether it's a total or an increment? Ask, or read today's value
   first and say what you'll do.
4. Confirm briefly: what was logged and what's still open today.

Backfilling: `cadence history --from <d> --to <d> --json` lists what's already
stored (days with nothing logged have no rows). Only log what the user tells
you happened.

### How's my week?

```bash
cadence score --json      # execution %, pace, tier, per-goal numbers, risk, leverage, streaks
cadence insight --json    # coaching: assessment, guidance, risk, leverage
```

Execution is points earned against the full week's target (capped per goal);
`pace_percentage` is how the week is tracking so far. Tiers: Reset <60,
Building 60-69, Steady 70-79, Strong 80-89, Elite 90+. If `insight.stale` is
true a fresher one is generating; the current text is still fine to use.
Lead with the one or two actions that matter today.

### Weekly review (Sunday evening or Monday)

1. `cadence week review show --week <due_review_week> --json` includes last
   week's commitment (`previous_commitment`). Ask whether they kept it.
2. `cadence score --week <due_review_week> --json` and
   `cadence insight --week <due_review_week> --json` for the numbers. Share the
   score and one observation.
3. Ask the four questions, one at a time, and use the user's words:
   - **Biggest win**: what moved the needle?
   - **Derail cause**: what got in the way? (the root cause, not an excuse)
   - **One change**: the single adjustment for next week
   - **Constraint**: capacity limits next week (travel, deadlines)?
4. Save (any subset; answers you leave out are kept):
   ```bash
   cadence week review set --week <due_review_week> \
     --win "..." --derail "..." --change "..." --constraint "..." --json
   ```
   `--change` comes back next week as "last week's commitment".

### End-of-cycle review (`awaiting_review`)

1. `cadence review --json` (defaults to the cycle awaiting review): execution,
   grade, best/worst weeks, per-goal trends, learnings and six prompts, each
   with a `system_insight`.
2. Walk through the prompts, sharing the insight for each:
   `--drove` (what made the strongest weeks work), `--limited` (what reduced
   execution), `--redesign` (which goal needs a better design, not more
   discipline), `--carry-forward` (what should stay exactly the same),
   `--adjustment` (the single change for the next 12 weeks),
   `--closing-notes` (anything else to remember).
3. Save as you go: `cadence cycles review set --drove "..." --adjustment "..." --json`.
4. Then offer to plan the next cycle. Only if the user explicitly wants to skip
   the review: `cadence cycles review dismiss` (confirm first).

### Plan the next cycle (`final_stretch`, after a review, or `no_active_cycle`)

1. Discuss which goals continue, change or stop (use the review's
   `--redesign` and `--carry-forward` answers).
2. Draft it. Nothing is activated yet:
   ```bash
   cadence cycles next --start after --json          # right after the current cycle ends
   cadence cycles next --start after-13th --json     # take a 13th week to rest and plan first
   cadence cycles next --start today --json          # only once the cycle has ended
   cadence cycles next --start after --goals reading,protein --json   # carry only some goals
   ```
   If a draft already exists it is returned instead of a second one.
   `carry_forward.invalid` lists goals that couldn't be copied; tell the user.
3. Edit the draft: `cadence goals add|update|archive --cycle <draft-id> ...` and
   `cadence goals reorder --cycle <id> --order <every-id,in,order>`.
4. Show it (`cadence cycles show --id <id>`), get a clear yes, then
   `cadence cycles activate --id <id> --json`. A draft starting within three
   weeks is scheduled; overlapping cycles are rejected. Goal definitions lock
   on activation (notes stay editable). To undo a schedule before it starts:
   `cadence cycles unschedule --id <id>` (confirm first).

### Plan a first cycle from scratch

Good 12-Week Year goals are **lead measures**: actions the user controls and
can do this week, not outcomes. "Write 500 words" beats "finish the book";
"Workout" 4 days a week beats "lose 5 kg".

- 3-7 goals is plenty; each should matter for the next 12 weeks.
- Make each measurable: a checkbox (`--input checkbox`), a daily threshold
  (`--input number --scoring threshold --target 20 --unit min`), or cumulative
  units (`--input number --scoring cumulative --target 1 --unit sessions --weekly-cap 10`).
- Threshold `--weekly-cap` is scored days per week (1-7). Be realistic:
  workouts 3-4, reading 5, sleep 5-7. Extra days are optional and never
  penalized. Cumulative `--weekly-cap` is a weekly point cap.
- Use the user's own words for names and store them verbatim.
- Start today, next Monday, or any date up to three weeks out.

```bash
cadence cycles create --key fall-2026 --name "Fall 2026" --start 2026-10-05 --dry-run --json
cadence cycles create --key fall-2026 --name "Fall 2026" --start 2026-10-05 --json
cadence goals add --cycle <id> --key reading --name "Reading" --input number \
  --scoring threshold --target 20 --unit min --weekly-cap 5 --json
```

For many goals, write a plan document (`schema_version: 1`, `plan_key`, `name`,
`start_date`, `goals[]` with `key`, `name`, `input_kind`, `scoring_mode`,
`target_value`, `unit`, `weekly_cap`, `description`), preview it with
`cadence cycles import --file plan.json --dry-run --json`, then import without
`--dry-run`. Retrying the same document with the same `plan_key` is safe.
`cadence cycles export --id <id>` prints a cycle's document. Then confirm and
`cadence cycles activate --id <id>`.

### Fresh start (`lapsed` / `fresh_start_offer`)

The user fell off for a week or more. Be kind: offer a clean 12 weeks from
today rather than guilt, and ask what pulled them off track.

- Not now: `cadence cycles fresh-start dismiss` hides the offer until their next check-in.
- Start fresh (confirm first):
  1. `cadence cycles fresh-start --json` (optionally `--goals a,b` to keep only
     some goals) drafts it. Nothing changes yet.
  2. Show the draft; after a yes, `cadence cycles activate --id <draft-id> --json`.
     That ends the old cycle yesterday and moves today's check-ins across.

### Cycle notes

`cadence cycles notes --json` shows the active cycle's notes;
`cadence cycles notes --notes "..."` replaces them (active cycles included).
Read them first and append rather than overwrite unless the user asks.

## Quick reference

```bash
cadence status [--json]                                   # orientation; run first
cadence today [--date D] [--slug S] [--json]              # today's goals and values
cadence complete --goal G [--date D] [--value 0|1]        # checkbox goals
cadence set --goal G --value TOTAL [--date D]             # replace a numeric day total
cadence add --goal G --value AMOUNT [--date D] [--idempotency-key K]  # add to it
cadence history [--from D] [--to D] [--goal G]            # stored entries (up to 120 days)
cadence score [--cycle ID] [--week D] [--as-of D]         # weekly scorecard
cadence insight [--week D]                                # coaching
cadence week review show|set [--week D] [--win --derail --change --constraint]
cadence review [--id ID]                                  # end-of-cycle review
cadence cycles [show|export|notes] [--id ID]
cadence cycles create|import|update ... [--dry-run]
cadence cycles activate --id ID                           # confirm first
cadence cycles next --start after|today|after-13th [--id ID] [--goals a,b]
cadence cycles unschedule --id ID                         # confirm first
cadence cycles fresh-start [--goals a,b] | cycles fresh-start dismiss
cadence cycles review show|set|dismiss [--id ID]
cadence goals [--cycle ID] | goals add|update|reorder|archive ...
cadence doctor | auth status | version
```

`--goal` takes a goal id, slug, or exact name. Dates are `YYYY-MM-DD` in the
user's time zone. Every command accepts `--json`, `--url` and `--token`.
Single-dash spellings (`-goal`, `-json`) also work.

### Exit codes

| Code | Meaning | What to do |
|---|---|---|
| 0 | OK | |
| 1 | Other error (server error, rate limited) | Read the message; if rate limited, wait and try later |
| 2 | Usage: bad flag, missing argument, invalid date | Fix the command (`cadence <cmd> --help`) |
| 3 | Not signed in, or the token was rejected | Ask the user to run `cadence login` |
| 4 | Not found (goal, cycle, week) | Re-list with `cadence goals --json` or `cadence cycles --json` |
| 5 | Conflict or validation failure | Explain `error.message` and `details` to the user |
| 6 | Network: unreachable or timed out | Check connectivity; for `add`, verify before retrying |

## Troubleshooting

- Run `cadence doctor` (or `--json`). It checks the binary version, the PATH,
  the saved login, the server (`/api/v1/me` and the minimum CLI version) and
  this skill.
- Exit 3: the token is missing or revoked. The user runs `cadence login` with a
  token from Settings → CLI Access. Never ask the user to paste a token into
  the chat; `cadence login` reads it with hidden input.
- A command or flag "doesn't exist": the CLI may be old, or another copy may
  be first on PATH. Check `cadence version` and `cadence doctor`, then run
  `cadence update`.
- Slow responses: set `CADENCE_TIMEOUT=30s` for that command.
- `today` returns `[]`: no cycle is active on that date (see `status`).
