#!/usr/bin/env bash
# Trustabl plugin — SessionStart detector.
#
# Non-destructive: verifies the `trustabl` CLI is installed and recent enough for
# the plugin's skills. Prints a single actionable line (which Claude Code injects
# into the session at start) only when something needs attention; stays silent
# and exits 0 when the CLI is present and current.
#
# This script NEVER installs anything and NEVER fails the session (always exits
# 0). The consented install path lives in the trustabl-scan skill, which asks the
# user before running any install command — appropriate for a security tool.
#
# Platform note: this is a bash script. On native Windows (PowerShell, no
# bash) it will not run; Windows users get the install hint from the skill
# instead. macOS/Linux and Windows-with-Git-Bash/WSL are covered.

set -uo pipefail

# Minimum CLI version the skills require. Bump this when a skill starts depending
# on a newer CLI feature. 0.1.2 is the current floor: it ships `scan`,
# `--format sarif`, and `--strict`, which the scan/enrich skills use today.
MIN_VERSION="0.1.2"

INSTALL_HINT="install with 'brew install trustabl/tap/trustabl' (macOS/Linux), 'scoop install trustabl' (Windows), or download a build from https://github.com/trustabl/trustabl/releases"

if ! command -v trustabl >/dev/null 2>&1; then
  echo "[trustabl] CLI not found on PATH — the trustabl-scan and trustabl-enrich skills cannot run until it is installed. To use them, ${INSTALL_HINT}, then confirm with 'trustabl version'."
  exit 0
fi

have="$(trustabl version 2>/dev/null | awk 'NR==1 {print $2}')"
if [ -z "${have:-}" ]; then
  # CLI is present but its version line could not be read; assume it is usable
  # rather than nag on every session.
  exit 0
fi

# Version compare via `sort -V`: the lower of {MIN, have} equals MIN exactly when
# have >= MIN (covers the have == MIN case too).
lowest="$(printf '%s\n%s\n' "${MIN_VERSION}" "${have}" | sort -V | head -n1)"
if [ "${lowest}" != "${MIN_VERSION}" ]; then
  echo "[trustabl] CLI ${have} is older than the minimum ${MIN_VERSION} the skills need — please upgrade: ${INSTALL_HINT}."
  exit 0
fi

# Present and current: stay silent to keep the session context clean.
exit 0
