# shellcheck shell=bash
# Shared install logic for the Trustabl plugin. Sourced by check-trustabl.sh
# (the SessionStart hook) and trustabl-mcp.sh (the MCP server launcher).
#
# Every function writes diagnostics to STDERR only — never stdout — so this is
# safe to source in the MCP launcher, whose stdout is the JSON-RPC protocol
# stream and must not be polluted.

# Pinned CLI version the plugin targets. The MCP `scan` tool needs >= 0.1.3
# (0.1.2 has no `mcp` subcommand); 0.1.4 adds dependency vulnerability scanning
# (the `vuln_scan` tool arg / `--vuln-scan`) and renames a finding's `line` to
# `start_line`/`end_line`. Bump on a plugin release to move users on.
TRUSTABL_VERSION="0.1.4"
TRUSTABL_REPO="trustabl/trustabl"
TRUSTABL_INSTALL_HINT="install with 'brew install trustabl/tap/trustabl' (macOS/Linux), 'scoop install trustabl' (Windows), or download from https://github.com/${TRUSTABL_REPO}/releases"

_tdl_log()  { printf '[trustabl] %s\n' "$*" >&2; }
_tdl_have() { command -v "$1" >/dev/null 2>&1; }

# Absolute path to the plugin-managed binary ("" when there is no data dir).
tdl_managed_bin() { [ -n "${CLAUDE_PLUGIN_DATA:-}" ] && printf '%s' "${CLAUDE_PLUGIN_DATA}/bin/trustabl"; }

# Version string reported by the binary at $1 ("" if absent/unreadable).
tdl_bin_version() { [ -x "$1" ] && "$1" version 2>/dev/null | awk 'NR==1 {print $2}'; }

# Ensure the pinned binary is installed at the managed path. Returns 0 when the
# managed binary is present and is exactly TRUSTABL_VERSION, 1 otherwise.
# Idempotent (fast-paths when already correct); all output goes to stderr.
tdl_ensure() {
  local bin; bin="$(tdl_managed_bin)"
  [ -n "$bin" ] || { _tdl_log "no CLAUDE_PLUGIN_DATA set; cannot manage a private binary."; return 1; }
  [ "$(tdl_bin_version "$bin")" = "$TRUSTABL_VERSION" ] && return 0

  local os arch
  os="$(uname -s)"; arch="$(uname -m)"
  case "$os" in
    Darwin) os=darwin ;;
    Linux)  os=linux ;;
    *) _tdl_log "auto-install is unsupported on '${os}'."; return 1 ;;
  esac
  case "$arch" in
    x86_64|amd64)  arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) _tdl_log "auto-install is unsupported on arch '${arch}'."; return 1 ;;
  esac
  _tdl_have curl || { _tdl_log "auto-install needs curl."; return 1; }
  _tdl_have tar  || { _tdl_log "auto-install needs tar."; return 1; }
  { _tdl_have sha256sum || _tdl_have shasum; } || { _tdl_log "auto-install needs sha256sum or shasum."; return 1; }

  local asset base
  asset="trustabl_${TRUSTABL_VERSION}_${os}_${arch}.tar.gz"
  base="https://github.com/${TRUSTABL_REPO}/releases/download/v${TRUSTABL_VERSION}"
  mkdir -p "$(dirname "$bin")" || { _tdl_log "could not create $(dirname "$bin")."; return 1; }

  # Download, verify, and stage in a subshell so its temp dir and staging file
  # are always cleaned up. Staging is in the binary's own dir, so the final mv
  # is atomic (same filesystem) and safe against a concurrent installer.
  (
    tmp="$(mktemp -d)" || exit 1
    stage="$(dirname "$bin")/.trustabl.staging.${BASHPID:-$$}"
    trap 'rm -rf "$tmp" "$stage"' EXIT
    curl -fsSL "${base}/${asset}"        -o "${tmp}/${asset}"        || { _tdl_log "download of ${asset} failed (offline?)."; exit 1; }
    curl -fsSL "${base}/checksums.txt"   -o "${tmp}/checksums.txt"   || { _tdl_log "download of checksums.txt failed."; exit 1; }
    want="$(awk -v f="$asset" '$2==f || $2=="*"f {print $1}' "${tmp}/checksums.txt" | head -n1)"
    if _tdl_have sha256sum; then got="$(sha256sum "${tmp}/${asset}" | awk '{print $1}')"
    else                        got="$(shasum -a 256 "${tmp}/${asset}" | awk '{print $1}')"; fi
    [ -n "$want" ] && [ "$want" = "$got" ] || { _tdl_log "checksum verification FAILED for ${asset}."; exit 1; }
    tar -xzf "${tmp}/${asset}" -C "$tmp" trustabl 2>/dev/null || tar -xzf "${tmp}/${asset}" -C "$tmp" 2>/dev/null
    src="$(find "$tmp" -type f -name trustabl 2>/dev/null | head -n1)"
    [ -n "$src" ] || { _tdl_log "could not find the binary inside ${asset}."; exit 1; }
    install -m 0755 "$src" "$stage" 2>/dev/null || { cp "$src" "$stage" && chmod 0755 "$stage"; } || { _tdl_log "staging the binary failed."; exit 1; }
    mv -f "$stage" "$bin" || { _tdl_log "installing to ${bin} failed."; exit 1; }
    exit 0
  ) || return 1

  [ "$(tdl_bin_version "$bin")" = "$TRUSTABL_VERSION" ] || { _tdl_log "post-install version check failed."; return 1; }
  _tdl_log "installed trustabl ${TRUSTABL_VERSION} at ${bin}."
  return 0
}
