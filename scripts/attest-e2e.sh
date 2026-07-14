#!/usr/bin/env bash
#
# attest-e2e.sh — hermetic, key-mode end-to-end test of Trustabl scan attestation.
#
#   scan -> attest -> verify (expect PASS) -> tamper -> verify (expect FAIL)
#   plus the `scan --attest` one-shot path.
#
# It runs in KEY mode with --no-tlog, so it needs NO OIDC identity and NO network
# for the cryptography (only the scan's rule resolution may touch the network /
# cache). The KEYLESS path needs CI ambient OIDC and is exercised in CI, not here.
#
# Usage:  scripts/attest-e2e.sh [target]      (target defaults to this repo)
# Skips cleanly (exit 0) when cosign is not installed — it is an optional dep.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${1:-$repo_root}"
target="$(cd "$target" 2>/dev/null && pwd || echo "$target")" # absolutize a dir target

note() { printf '\n=== %s ===\n' "$*"; }
fail=0
# check <desc> <expected-exit> <actual-exit>
check() {
	if [ "$2" -eq "$3" ]; then
		printf 'PASS  %s (exit %s)\n' "$1" "$3"
	else
		printf 'FAIL  %s (want exit %s, got %s)\n' "$1" "$2" "$3"
		fail=1
	fi
}

if ! command -v cosign >/dev/null 2>&1; then
	echo "SKIP: cosign not found on PATH."
	echo "      install it to run this e2e: https://docs.sigstore.dev/cosign/system_config/installation/"
	exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export COSIGN_PASSWORD="" # non-interactive key generation + signing

note "build trustabl"
bin="$work/trustabl"
(cd "$repo_root" && go build -o "$bin" ./cmd/trustabl)

note "generate cosign key pair (key mode — no OIDC, no log)"
(cd "$work" && cosign generate-key-pair >/dev/null)
key="$work/cosign.key"
pub="$work/cosign.pub"

# Run the binary from $work so any relative default outputs (e.g. the predicate
# from `scan --attest`) land in the temp dir, never in the repo.
cd "$work"
report="$work/report.json"
bundle="$work/att.bundle.json"
pred="$work/predicate.json"

note "scan target -> JSON report"
# The scan may exit 1 on findings; that is fine — we only need a valid JSON report.
set +e
"$bin" scan "$target" --json-out "$report" --no-progress >/dev/null 2>"$work/scan.err"
scan_exit=$?
set -e
if [ ! -s "$report" ]; then
	echo "FAIL: scan wrote no report (exit $scan_exit). Rules must be resolvable (network or cache):"
	tail -n 5 "$work/scan.err" | sed 's/^/  scan: /'
	exit 1
fi
echo "report written ($(wc -c <"$report" | tr -d ' ') bytes), scan exit $scan_exit"

note "attest the report (key mode, no transparency log)"
set +e
"$bin" attest "$report" --key "$key" --bundle "$bundle" --predicate-out "$pred" --no-tlog
check "attest succeeds" 0 $?
set -e
[ -s "$bundle" ] && echo "bundle written" || { echo "FAIL: no bundle"; fail=1; }
[ -s "$pred" ] && echo "predicate written" || { echo "FAIL: no predicate"; fail=1; }

note "verify the untouched report (expect PASS / exit 0)"
set +e
"$bin" verify "$report" --key "$pub" --bundle "$bundle" --no-tlog
check "verify untampered report" 0 $?
set -e

note "tamper the report, then verify (expect FAIL / exit 1)"
printf '\n{"tampered":true}\n' >>"$report"
set +e
"$bin" verify "$report" --key "$pub" --bundle "$bundle" --no-tlog
check "tampered report is rejected" 1 $?
set -e

note "scan --attest one-shot path"
report2="$work/report2.json"
bundle2="$work/att2.bundle.json"
set +e
"$bin" scan "$target" --json-out "$report2" \
	--attest --attest-key "$key" --attest-bundle "$bundle2" --attest-no-tlog \
	--no-progress >/dev/null 2>>"$work/scan.err"
set -e
if [ -s "$bundle2" ]; then
	set +e
	"$bin" verify "$report2" --key "$pub" --bundle "$bundle2" --no-tlog
	check "scan --attest bundle verifies" 0 $?
	set -e
else
	echo "FAIL: scan --attest wrote no bundle"
	fail=1
fi

note "result"
if [ "$fail" -eq 0 ]; then
	echo "ALL ATTESTATION E2E CHECKS PASSED"
else
	echo "SOME CHECKS FAILED"
fi
exit "$fail"
