#!/usr/bin/env bash
# Trustabl plugin — MCP server launcher (the command referenced by .mcp.json).
#
# This bundled script always exists, so Claude Code never hits a "command not
# found" when starting the server (which it would not retry). The script ensures
# the pinned binary is installed, then execs `trustabl mcp`. Because it installs
# synchronously before exec, it removes any race with the SessionStart hook.
#
# The binary's stdout is the JSON-RPC protocol stream, so NOTHING here may write
# to stdout — all install diagnostics go to stderr (the client's server log).
set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib-trustabl.sh
. "${DIR}/lib-trustabl.sh"

BIN=""
if managed="$(tdl_managed_bin)" && [ -n "$managed" ] && tdl_ensure; then
  BIN="$managed"
else
  # Fall back to a PATH binary. It may predate the `mcp` subcommand (in which
  # case the server will exit and Claude Code shows it as failed), but that is
  # strictly better than refusing to launch.
  BIN="$(command -v trustabl || true)"
fi

if [ -z "$BIN" ]; then
  _tdl_log "no trustabl binary available to start the MCP server; ${TRUSTABL_INSTALL_HINT}."
  exit 1
fi

exec "$BIN" mcp "$@"
