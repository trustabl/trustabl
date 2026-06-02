#!/usr/bin/env bash
# Fail if the in-repo rules fixture (testdata/rules-fixture) has drifted from the
# production rules pack (the external trustabl-rules repo).
#
# Background: testdata/rules-fixture/ is a *test mirror* of the trustabl-rules
# pack the engine pulls at scan time. The two are separate copies and drift
# unless deliberately kept in sync — a rule shipped to production but not
# mirrored is untested by the engine, and a fixture-only rule is tested but never
# shipped. This guard makes that drift a hard CI failure instead of something
# noticed by accident. See CLAUDE.md "Two-repo rule model".
#
# Compares manifest.yaml + every <category>/*.yaml between the two trees, ignoring
# line endings (the fixture is eol=lf; a Windows checkout of trustabl-rules may be
# CRLF). Docs (README.md, CLAUDE.md, LICENSE) are intentionally NOT compared —
# each repo keeps its own.
#
# Usage:
#   RULES_REPO=../trustabl-rules scripts/check-rules-sync.sh
#   scripts/check-rules-sync.sh                 # defaults to ../trustabl-rules
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fixture="$repo_root/testdata/rules-fixture"
rules="${RULES_REPO:-$repo_root/../trustabl-rules}"

if [ ! -d "$rules" ]; then
  echo "error: rules repo not found at '$rules' (set RULES_REPO)" >&2
  exit 2
fi

# Build the union of rule-file relative paths present in either tree:
# every <category>/*.yaml plus the top-level manifest.yaml.
list_yaml() { # $1 = root
  ( cd "$1"
    for d in claude_sdk openai_sdk google_adk; do
      [ -d "$d" ] && find "$d" -name '*.yaml'
    done
    [ -f manifest.yaml ] && echo manifest.yaml
  ) | sed 's#^\./##' | sort
}

fixture_files="$(list_yaml "$fixture")"
rules_files="$(list_yaml "$rules")"

status=0

# Files present in one tree but not the other.
only_fixture="$(comm -23 <(echo "$fixture_files") <(echo "$rules_files"))"
only_rules="$(comm -13 <(echo "$fixture_files") <(echo "$rules_files"))"
if [ -n "$only_fixture" ]; then
  status=1
  echo "FIXTURE-ONLY (tested but never shipped — add to trustabl-rules or remove):" >&2
  echo "$only_fixture" | sed 's/^/  /' >&2
fi
if [ -n "$only_rules" ]; then
  status=1
  echo "PRODUCTION-ONLY (shipped but untested — mirror into testdata/rules-fixture):" >&2
  echo "$only_rules" | sed 's/^/  /' >&2
fi

# Content drift on files present in both (ignore CR so LF vs CRLF is not a diff).
common="$(comm -12 <(echo "$fixture_files") <(echo "$rules_files"))"
while IFS= read -r f; do
  [ -z "$f" ] && continue
  if ! diff -q <(tr -d '\r' < "$fixture/$f") <(tr -d '\r' < "$rules/$f") >/dev/null 2>&1; then
    status=1
    echo "CONTENT DRIFT: $f (fixture != production)" >&2
  fi
done <<< "$common"

if [ "$status" -ne 0 ]; then
  echo "" >&2
  echo "Rules fixture is out of sync with production trustabl-rules." >&2
  echo "Mirror the change into BOTH repos (see CLAUDE.md 'The sync obligation')." >&2
  exit 1
fi

echo "rules fixture is in sync with production ($(echo "$common" | grep -c . ) files compared)"
