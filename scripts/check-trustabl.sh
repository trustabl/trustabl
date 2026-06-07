#!/usr/bin/env bash
# Trustabl plugin — SessionStart bootstrap.
#
# Ensures the pinned trustabl CLI is installed into the plugin's private data
# dir, so (a) the bundled MCP scan server can launch cleanly and (b) the
# enrich skill's direct-CLI fallback has a binary to call. It then exposes the
# binary to the session as $TRUSTABL_BIN.
#
# Scanning itself goes through the MCP `scan` tool (mcp__trustabl__scan), not
# through this hook. This hook is the install/announce step only.
#
# Install details (download from GitHub Releases into CLAUDE_PLUGIN_DATA,
# checksum-verified, idempotent) live in lib-trustabl.sh, shared with the MCP
# launcher. This script ALWAYS exits 0 and never fails the session.
set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib-trustabl.sh
. "${DIR}/lib-trustabl.sh"

if bin="$(tdl_managed_bin)" && [ -n "$bin" ] && tdl_ensure; then
  # Expose the managed binary to later Bash tool calls (idempotent: only append
  # the export once per env file).
  if [ -n "${CLAUDE_ENV_FILE:-}" ] && ! grep -q 'TRUSTABL_BIN=' "$CLAUDE_ENV_FILE" 2>/dev/null; then
    echo "export TRUSTABL_BIN=\"$bin\"" >> "$CLAUDE_ENV_FILE"
  fi
  echo "[trustabl] Plugin-managed trustabl ${TRUSTABL_VERSION} ready at ${bin}. Scanning uses the mcp__trustabl__scan tool; \$TRUSTABL_BIN is set for direct CLI use (e.g. enrich)."
  exit 0
fi

# Auto-install could not run — report on whatever is on PATH so the session
# still knows the state.
if command -v trustabl >/dev/null 2>&1; then
  have="$(trustabl version 2>/dev/null | awk 'NR==1 {print $2}')"
  lowest="$(printf '%s\n%s\n' "$TRUSTABL_VERSION" "$have" | sort -V | head -n1)"
  if [ -n "$have" ] && [ "$lowest" != "$TRUSTABL_VERSION" ]; then
    echo "[trustabl] CLI ${have} on PATH is older than ${TRUSTABL_VERSION}; the MCP scan server needs ${TRUSTABL_VERSION}+. Upgrade: ${TRUSTABL_INSTALL_HINT}."
  fi
else
  echo "[trustabl] could not provide the trustabl CLI automatically — ${TRUSTABL_INSTALL_HINT}, then confirm with 'trustabl version'."
fi
exit 0
