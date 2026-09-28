#!/bin/sh
# GoReleaser before-hook: generate shell completions into completions/ so the
# archives and the Homebrew cask can ship them.
set -eu
rm -rf completions
mkdir -p completions
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
go build -o "$tmp/cadence" ./cmd/cadence
for shell in bash zsh fish; do
  CI=1 "$tmp/cadence" completion "$shell" > "completions/cadence.$shell"
done
