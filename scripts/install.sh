#!/usr/bin/env bash
# Install the Cadence CLI (https://cadenceweek.com/cli).
#
#   curl -fsSL https://cadenceweek.com/install-cli | bash
#
# Environment:
#   CADENCE_VERSION         install this version (e.g. 1.2.0) instead of the latest
#   CADENCE_BIN_DIR         install into this directory (default ~/.local/bin or ~/bin)
#   CADENCE_SKIP_SETUP=1    don't run post-install setup (skill install)
#   CADENCE_NO_MODIFY_PATH=1  never edit shell startup files
set -euo pipefail

REPO="rizwanreza/cadence-cli"
MARKER="# Added by the Cadence CLI installer"

say() { printf '%s\n' "$*" >&2; }
fail() { say "cadence install: $*"; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || fail "'$1' is required but not installed"
}

detect_platform() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$os" in
    darwin | linux) ;;
    mingw* | msys* | cygwin*) fail "Windows isn't supported by this script. Download the zip from https://github.com/$REPO/releases/latest" ;;
    *) fail "unsupported operating system: $os" ;;
  esac
  case "$arch" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) fail "unsupported architecture: $arch" ;;
  esac
  printf '%s_%s' "$os" "$arch"
}

# Resolve "latest" by following the releases/latest redirect to .../tag/vX.Y.Z.
latest_version() {
  local url
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
    fail "couldn't reach GitHub to find the latest release"
  url=${url%/}
  case "$url" in
    */tag/*) printf '%s' "${url##*/tag/}" ;;
    *) fail "couldn't find the latest release (no releases published yet?)" ;;
  esac
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail "need sha256sum or shasum to verify the download"
  fi
}

on_path() {
  case ":${PATH}:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

choose_bin_dir() {
  if [ -n "${CADENCE_BIN_DIR:-}" ]; then
    printf '%s' "$CADENCE_BIN_DIR"
  elif on_path "$HOME/.local/bin"; then
    printf '%s' "$HOME/.local/bin"
  elif on_path "$HOME/bin"; then
    printf '%s' "$HOME/bin"
  else
    printf '%s' "$HOME/.local/bin"
  fi
}

# Append a PATH line to the user's shell startup file, once.
patch_path() {
  local dir=$1 shell rc line
  on_path "$dir" && return 0
  if [ -n "${CADENCE_NO_MODIFY_PATH:-}" ]; then
    say "Add $dir to your PATH to run cadence."
    return 0
  fi
  shell=$(basename "${SHELL:-sh}")
  case "$shell" in
    zsh)
      rc="${ZDOTDIR:-$HOME}/.zshrc"
      line="export PATH=\"$dir:\$PATH\""
      ;;
    bash)
      rc="$HOME/.bashrc"
      if [ "$(uname -s)" = "Darwin" ] && [ -f "$HOME/.bash_profile" ]; then
        rc="$HOME/.bash_profile"
      fi
      line="export PATH=\"$dir:\$PATH\""
      ;;
    fish)
      rc="${XDG_CONFIG_HOME:-$HOME/.config}/fish/conf.d/cadence.fish"
      line="fish_add_path \"$dir\""
      ;;
    *)
      say "Add $dir to your PATH to run cadence."
      return 0
      ;;
  esac
  mkdir -p "$(dirname "$rc")"
  if [ -f "$rc" ] && grep -qF "$line" "$rc"; then
    return 0
  fi
  printf '\n%s\n%s\n' "$MARKER" "$line" >>"$rc"
  say "Added $dir to PATH in $rc (open a new terminal, or run: $line)"
}

interactive() {
  [ -t 1 ] && [ -z "${CI:-}" ]
}

main() {
  need curl
  need tar
  need uname

  local platform version base archive tmp bin_dir expected actual
  platform=$(detect_platform)

  version=${CADENCE_VERSION:-}
  if [ -z "$version" ]; then
    [ -z "${CADENCE_DOWNLOAD_BASE:-}" ] || fail "set CADENCE_VERSION when using CADENCE_DOWNLOAD_BASE"
    version=$(latest_version)
  fi
  version=${version#v}

  # CADENCE_DOWNLOAD_BASE is for testing the script against a local build.
  base=${CADENCE_DOWNLOAD_BASE:-"https://github.com/$REPO/releases/download/v$version"}
  archive="cadence_${version}_${platform}.tar.gz"

  tmp=$(mktemp -d)
  # shellcheck disable=SC2064 # expand $tmp now
  trap "rm -rf '$tmp'" EXIT

  say "Downloading cadence $version for $platform..."
  curl -fsSL "$base/$archive" -o "$tmp/$archive" || fail "download failed: $base/$archive"
  curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

  expected=$(awk -v f="$archive" '$2 == f || $2 == "*"f {print $1}' "$tmp/checksums.txt")
  [ -n "$expected" ] || fail "$archive is not listed in checksums.txt"
  actual=$(sha256_of "$tmp/$archive")
  [ "$expected" = "$actual" ] || fail "checksum mismatch for $archive (expected $expected, got $actual)"

  tar -xzf "$tmp/$archive" -C "$tmp"
  [ -f "$tmp/cadence" ] || fail "the archive doesn't contain a cadence binary"

  bin_dir=$(choose_bin_dir)
  mkdir -p "$bin_dir"
  # Install via a temp file + rename so a running cadence is never truncated.
  cp "$tmp/cadence" "$bin_dir/.cadence.new"
  chmod 755 "$bin_dir/.cadence.new"
  mv -f "$bin_dir/.cadence.new" "$bin_dir/cadence"
  say "Installed $("$bin_dir/cadence" version 2>/dev/null || echo "cadence $version") to $bin_dir/cadence"

  patch_path "$bin_dir"

  if [ -n "${CADENCE_SKIP_SETUP:-}" ]; then
    return 0
  fi
  if interactive; then
    say ""
    say "Next steps:"
    say "  cadence login          paste a token from https://cadenceweek.com/settings#cli"
    say "  cadence skill install  teach Claude Code, Codex and other agents to use Cadence"
    say "  cadence --help         everything else"
  else
    # Agents and CI: install the agent skill so the next session can use it.
    "$bin_dir/cadence" skill install >&2 || say "Skill install failed; run 'cadence skill install' later."
    say "Next: cadence login (token from https://cadenceweek.com/settings#cli)"
  fi
}

main "$@"
