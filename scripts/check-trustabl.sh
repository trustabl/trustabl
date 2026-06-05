#!/usr/bin/env bash
# Trustabl plugin — SessionStart bootstrap.
#
# Ensures the recommended `trustabl` CLI version is available to the plugin's
# skills, then tells the session where to find it. Two modes, tried in order:
#
#   1. Auto-install (preferred): download the pinned version from GitHub
#      Releases into the plugin's private data dir (CLAUDE_PLUGIN_DATA),
#      checksum-verified against the release's checksums.txt, and expose its
#      path. Idempotent — it re-downloads only when the pin changes or the
#      installed copy is missing/wrong. No sudo, no change to the user's system
#      package manager or their own `trustabl`; fully reversible (everything
#      lives under CLAUDE_PLUGIN_DATA, which is removed when the plugin is
#      uninstalled).
#
#   2. Detect + hint (fallback): when auto-install cannot run — offline, no
#      CLAUDE_PLUGIN_DATA, an unsupported OS/arch, missing curl/tar/sha256, or a
#      failed checksum — fall back to checking for a `trustabl` already on PATH
#      and printing an install/upgrade hint.
#
# How skills find the binary: this script (a) appends `export TRUSTABL_BIN=...`
# to CLAUDE_ENV_FILE so later Bash tool calls inherit it, and (b) prints the
# absolute path into the session. The skills prefer "$TRUSTABL_BIN", then the
# reported path, then `trustabl` on PATH.
#
# This script ALWAYS exits 0 and never fails the session. It never installs into
# system locations and never runs the user's package manager.

set -uo pipefail

# --- pin ---------------------------------------------------------------------
# The exact CLI version the plugin's skills are tested against. Bump this on a
# plugin release to move users to a newer CLI; the next SessionStart re-installs.
RECOMMENDED_VERSION="0.1.3"
REPO="trustabl/trustabl"

INSTALL_HINT="install with 'brew install trustabl/tap/trustabl' (macOS/Linux), 'scoop install trustabl' (Windows), or download from https://github.com/${REPO}/releases"

# Version string of the binary at $1 ("" if absent/unreadable).
bin_version() { [ -x "$1" ] && "$1" version 2>/dev/null | awk 'NR==1 {print $2}'; }

have_cmd() { command -v "$1" >/dev/null 2>&1; }

# Fallback: report on whatever `trustabl` is on PATH (or its absence).
fallback_detect() {
  if ! have_cmd trustabl; then
    echo "[trustabl] CLI not found and the plugin could not auto-install it here — ${INSTALL_HINT}, then confirm with 'trustabl version'."
    return
  fi
  local have lowest
  have="$(trustabl version 2>/dev/null | awk 'NR==1 {print $2}')"
  lowest="$(printf '%s\n%s\n' "$RECOMMENDED_VERSION" "$have" | sort -V | head -n1)"
  if [ -n "$have" ] && [ "$lowest" != "$RECOMMENDED_VERSION" ]; then
    echo "[trustabl] CLI ${have} on PATH is older than the recommended ${RECOMMENDED_VERSION} — upgrade: ${INSTALL_HINT}."
  fi
  # Present and adequate: stay silent.
}

# Wire the resolved binary into the session (env file + a context line).
announce() {
  local bin="$1" verb="$2"
  [ -n "${CLAUDE_ENV_FILE:-}" ] && echo "export TRUSTABL_BIN=\"${bin}\"" >> "$CLAUDE_ENV_FILE"
  echo "[trustabl] ${verb} plugin-managed trustabl ${RECOMMENDED_VERSION} at ${bin} — use this path (or \$TRUSTABL_BIN) to run trustabl this session."
}

# --- need a private dir to install into; otherwise fall back -----------------
if [ -z "${CLAUDE_PLUGIN_DATA:-}" ]; then
  fallback_detect; exit 0
fi

BIN_DIR="${CLAUDE_PLUGIN_DATA}/bin"
BIN="${BIN_DIR}/trustabl"

# Fast path: already the pinned version (every session after the first).
if [ "$(bin_version "$BIN")" = "$RECOMMENDED_VERSION" ]; then
  announce "$BIN" "Using"
  exit 0
fi

# --- map OS/arch to the release asset ---------------------------------------
os="$(uname -s)"; arch="$(uname -m)"
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) echo "[trustabl] auto-install is unsupported on '${os}'."; fallback_detect; exit 0 ;;
esac
case "$arch" in
  x86_64|amd64)   arch=amd64 ;;
  arm64|aarch64)  arch=arm64 ;;
  *) echo "[trustabl] auto-install is unsupported on arch '${arch}'."; fallback_detect; exit 0 ;;
esac

if ! have_cmd curl || ! have_cmd tar; then
  echo "[trustabl] auto-install needs curl and tar."; fallback_detect; exit 0
fi
if ! have_cmd sha256sum && ! have_cmd shasum; then
  echo "[trustabl] auto-install needs a sha256 tool (sha256sum or shasum)."; fallback_detect; exit 0
fi

asset="trustabl_${RECOMMENDED_VERSION}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/v${RECOMMENDED_VERSION}"

tmp="$(mktemp -d 2>/dev/null)" || { echo "[trustabl] could not create a temp dir."; fallback_detect; exit 0; }
trap 'rm -rf "$tmp"' EXIT

if ! curl -fsSL "${base}/${asset}" -o "${tmp}/${asset}" \
   || ! curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt"; then
  echo "[trustabl] could not download ${asset} (offline?) — using whatever is on PATH."
  fallback_detect; exit 0
fi

# Verify the archive against checksums.txt before touching it.
want_sum="$(awk -v f="$asset" '$2 == f || $2 == "*"f {print $1}' "${tmp}/checksums.txt" | head -n1)"
if have_cmd sha256sum; then
  got_sum="$(sha256sum "${tmp}/${asset}" | awk '{print $1}')"
else
  got_sum="$(shasum -a 256 "${tmp}/${asset}" | awk '{print $1}')"
fi
if [ -z "$want_sum" ] || [ "$want_sum" != "$got_sum" ]; then
  echo "[trustabl] checksum verification FAILED for ${asset} — refusing to install."
  fallback_detect; exit 0
fi

# Extract the binary (archive root holds `trustabl` plus license/readme).
tar -xzf "${tmp}/${asset}" -C "$tmp" trustabl 2>/dev/null || tar -xzf "${tmp}/${asset}" -C "$tmp" 2>/dev/null
src="$(find "$tmp" -type f -name trustabl 2>/dev/null | head -n1)"
if [ -z "$src" ]; then
  echo "[trustabl] could not find the binary inside ${asset} — using whatever is on PATH."
  fallback_detect; exit 0
fi

mkdir -p "$BIN_DIR"
install -m 0755 "$src" "$BIN" 2>/dev/null || { cp "$src" "$BIN" && chmod 0755 "$BIN"; }

if [ "$(bin_version "$BIN")" = "$RECOMMENDED_VERSION" ]; then
  announce "$BIN" "Installed"
else
  echo "[trustabl] post-install version check failed — using whatever is on PATH."
  fallback_detect
fi
exit 0
