#!/bin/sh
# Build and `go install` the CLI into a temporary directory, then exercise the
# installed executable without touching the user's PATH, config or skills.
set -eu
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

GOBIN="$tmp/bin" go install ./cmd/cadence
cadence="$tmp/bin/cadence"
export HOME="$tmp/home" XDG_CONFIG_HOME="$tmp/home/.config" CI=1
unset CADENCE_URL CADENCE_TOKEN
mkdir -p "$HOME"

"$cadence" --help > "$tmp/help"
for command in status today complete set add history score insight week review cycles goals login logout auth doctor skill update version completion; do
  grep -q "^  $command " "$tmp/help" || { echo "help is missing '$command'" >&2; exit 1; }
done

"$cadence" version --json | grep -q '"version"'

# Old single-dash flags still parse, and usage errors exit 2 before any request.
for command in set add; do
  set +e
  "$cadence" "$command" -value 40 > "$tmp/output" 2>&1
  status=$?
  set -e
  [ "$status" -eq 2 ] || { echo "$command exited $status, want 2" >&2; cat "$tmp/output" >&2; exit 1; }
  grep -q "requires --goal" "$tmp/output"
done

# No token: auth errors exit 3 with a JSON error on stderr.
set +e
"$cadence" today -json 2> "$tmp/err" > /dev/null
status=$?
set -e
[ "$status" -eq 3 ] || { echo "today without a token exited $status, want 3" >&2; exit 1; }
grep -q '"code":"unauthorized"' "$tmp/err"

"$cadence" skill install --agent codex > /dev/null
[ -f "$HOME/.agents/skills/cadence/SKILL.md" ] && [ -f "$HOME/.agents/skills/cadence/.managed-by-cadence-cli" ]

for shell in bash zsh fish; do
  "$cadence" completion "$shell" | grep -q cadence
done

echo "Installed cadence: help, version, flags, exit codes, skill install and completions verified."
