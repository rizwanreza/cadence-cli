#!/usr/bin/env bash
# Exercise scripts/install.sh against a local snapshot build in dist/
# (goreleaser release --snapshot --clean), served over HTTP. Nothing outside a
# temporary HOME is touched.
set -euo pipefail
cd "$(dirname "$0")/.."
[ -f dist/checksums.txt ] || { echo "run: goreleaser release --snapshot --clean" >&2; exit 1; }
version=$(sed -n 's/^.*  cadence_\(.*\)_linux_amd64\.tar\.gz$/\1/p' dist/checksums.txt)
port=${PORT:-18765}
tmp=$(mktemp -d)
(cd dist && exec python3 -m http.server "$port" >/dev/null 2>&1) &
server=$!
trap 'kill $server 2>/dev/null; rm -rf "$tmp"' EXIT
sleep 1

# shellcheck disable=SC2119,SC2120 # extra env assignments are optional
run() {
  env -i PATH=/usr/bin:/bin HOME="$tmp/home" SHELL=/bin/bash \
    CADENCE_VERSION="$version" CADENCE_DOWNLOAD_BASE="http://127.0.0.1:$port" "$@" \
    bash scripts/install.sh
}
mkdir -p "$tmp/home"
run
"$tmp/home/.local/bin/cadence" version | grep -q "$version"
[ -f "$tmp/home/.agents/skills/cadence/.managed-by-cadence-cli" ]
run >/dev/null 2>&1
rc="$tmp/home/.bashrc"
[ "$(uname -s)" = Darwin ] && [ -f "$tmp/home/.bash_profile" ] && rc="$tmp/home/.bash_profile"
[ "$(grep -c 'Cadence CLI installer' "$rc")" -eq 1 ] || { echo "PATH line added twice" >&2; exit 1; }

# A tampered checksum must abort before installing.
mkdir -p "$tmp/bad"
cp dist/*.tar.gz "$tmp/bad/"
# Replace every hash outright (flipping one character is a no-op when it already matches).
awk '{print "0000000000000000000000000000000000000000000000000000000000000000  " $2}' dist/checksums.txt >"$tmp/bad/checksums.txt"
(cd "$tmp/bad" && exec python3 -m http.server $((port + 1)) >/dev/null 2>&1) &
bad=$!
sleep 1
if env -i PATH=/usr/bin:/bin HOME="$tmp/home2" CADENCE_VERSION="$version" \
  CADENCE_DOWNLOAD_BASE="http://127.0.0.1:$((port + 1))" bash scripts/install.sh 2>/dev/null; then
  kill $bad; echo "installed despite a checksum mismatch" >&2; exit 1
fi
kill $bad
[ ! -e "$tmp/home2/.local/bin/cadence" ]
echo "install.sh verified against snapshot $version."
