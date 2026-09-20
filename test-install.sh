#!/bin/sh
# Exercise the actual installed executable without touching the user's PATH.
set -eu
install_dir=$(mktemp -d)
trap 'rm -rf "$install_dir"' EXIT HUP INT TERM
GOBIN="$install_dir" go install .
export XDG_CONFIG_HOME="$install_dir/config"
"$install_dir/cadence" -help > "$install_dir/help"
for command in set add complete today; do
  grep -q "cadence $command " "$install_dir/help"
done
for command in set add; do
  if "$install_dir/cadence" "$command" -value 40 > "$install_dir/output" 2>&1; then
    echo "$command did not reject the missing goal" >&2
    exit 1
  fi
  grep -q "$command requires -goal and -value" "$install_dir/output"
done
echo 'Installed cadence exposes set, add, complete, and today; set/add dispatch verified.'
