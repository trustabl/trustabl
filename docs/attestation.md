# Evaluating a Trustabl scan attestation

How to read, verify, and trust the output of `trustabl attest` — the signed
claim a Trustabl scan makes about a repository.

This covers the **open-source CLI only**. Everything here works with the
Apache-2.0 binary from [trustabl/trustabl](https://github.com/trustabl/trustabl)
and needs no Trustabl account.

---

## What an attestation actually claims

An attestation is a **signed statement that a specific scan produced a specific
result**. It is a claim about a report, not a certificate of safety.

It proves:

- this exact `report.json` (pinned by SHA-256) came out of a Trustabl scan
- which engine version and which rules produced it
- the verdict and severity histogram at the time of the scan
- that nobody altered the report afterwards without breaking the signature

It does **not** prove the agent is safe, that the repo is compliant, or that the
code behaves correctly at runtime. Trustabl is a static scanner; the attestation
inherits exactly the coverage the scan had, no more.

> **Tier.** This is the free-tier **basic attestation** — it signs the scan
> result (verdict, score, severity counts). Behavioral and policy-aware
> attestations are on the paid roadmap, not in the OSS CLI.

---

## The three artifacts

| File | Default name | What it is |
|---|---|---|
| Report | `report.json` | The scan output. The thing being attested |
| Predicate | `trustabl-predicate.json` | The claim, as plain JSON — human-readable |
| Bundle | `trustabl-attestation.bundle.json` | The Sigstore bundle: DSSE envelope, signature, certificate, transparency-log entry |

The predicate is written out separately purely for readability. The
authoritative copy is the one embedded inside the bundle's DSSE payload — that
is the copy the signature covers.

---

## Reading the predicate

A real predicate, from a scan of `google/adk-python`:

```json
{
  "scanId": "scan_ad1aaeb9154c046f",
  "engineVersion": "dev",
  "rulesSha": "aa5c508e39c478dffcdf3aea5517b02fec1f83c8",
  "rulesOrigin": "unsigned:default",
  "repo": "https://github.com/google/adk-python",
  "overallScore": 0.6339651799104253,
  "verdict": "fail",
  "severityCounts": {
    "critical": 0, "high": 44, "medium": 1833, "low": 2, "info": 19
  }
}
```

### Field by field

**`verdict`** — `fail` means the scan found **at least one finding of medium
severity or higher**. `info` and `low` findings never fail the verdict. This is
the same gate the default (non-strict) exit code uses, so a consumer reading the
predicate and a CI step reading the exit code always agree.

**`overallScore`** — a float from 0 to 1. Multiply by 100 for the readiness
score shown in the report. `0.634` above is **63.4 / 100**.

**`severityCounts`** — a fixed-field histogram, not a map, so the rendered JSON
stays byte-stable. A large `medium` count is normal on a big repo and is not by
itself alarming; read `critical` and `high` first.

**`rulesSha`** — the exact rules commit that produced these findings. Two scans
with different `rulesSha` are not comparable.

**`rulesOrigin`** — *the trust signal most people miss.* Values:

| Value | Meaning |
|---|---|
| `signed:<channel>` | Rules came from a signed channel, blessed source. Highest trust |
| `signed:<channel>:custom` | Signed channel, but a non-default repo |
| `unsigned:default` | Default repo, signature not verified |
| `unsigned:custom` | Non-default repo, unverified. Lowest trust |

An attestation over an `unsigned:custom` scan is cryptographically valid and
still only as trustworthy as whoever supplied those rules. **A valid signature
on an untrusted ruleset is not assurance.**

**`engineVersion`** — `dev` means a local build, not a release. Treat `dev`
attestations as informational; a release version is what you archive.

**`scanId`** — deterministic. The same inputs always yield the same scan ID, and
it folds in the resolved rules SHA. Two identical scan IDs mean two genuinely
identical scans.

### What is deliberately absent

There is **no commit field** in `v1`. The data model carries no commit SHA, and
the subject digest plus `scanId` already pin the scanned state
cryptographically. Binding a human-friendly commit label is a follow-up. If you
need commit traceability today, record it alongside the bundle yourself.

---

## The statement wrapper

Inside the bundle, the predicate is wrapped in an in-toto statement:

```json
{
  "_type": "https://in-toto.io/Statement/v0.1",
  "subject": [{ "name": "report.json", "digest": { "sha256": "ee2a7238…" } }],
  "predicateType": "https://trustabl.dev/attestation/scan/v1",
  "predicate": { }
}
```

**Pin `predicateType` exactly.** The `/v1` suffix versions the schema — a future
breaking change ships as `/v2` rather than silently redefining `v1`. A consumer
that accepts any `trustabl.dev/attestation/scan/*` will eventually parse a schema
it does not understand.

The `subject` digest is the SHA-256 of the report. **This is the binding.** If
the report is edited by one byte, verification fails.

---

## Verifying

`attest` and `verify` shell out to [cosign](https://github.com/sigstore/cosign).
cosign is needed **only** for attestation — a plain `scan` never touches it. Both
cosign v2 and v3 are supported and tested.

### Key mode (offline, no transparency log)

```bash
trustabl scan . --json-out report.json --attest \
  --attest-key cosign.key --attest-bundle att.bundle.json --attest-no-tlog

trustabl verify report.json --bundle att.bundle.json --key cosign.pub --no-tlog
```

Use this for air-gapped or internal pipelines where you hold the key and do not
want a public record.

### Keyless mode (CI identity, public transparency log)

```bash
trustabl attest report.json --bundle att.bundle.json

trustabl verify report.json --bundle att.bundle.json \
  --certificate-identity "https://github.com/<org>/<repo>/.github/workflows/<file>@refs/heads/main" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"
```

Signing uses the runner's ambient GitHub OIDC identity through Fulcio, and the
entry lands in Rekor.

> **Every keyless run writes a permanent entry to the PUBLIC Rekor transparency
> log.** It cannot be deleted. Do not run keyless attestation on a private repo
> whose name you do not want published, and do not wire it to run on every push.

**`--certificate-identity` is not optional in practice.** Verifying without
pinning the identity and issuer confirms only that *somebody* signed this — not
that your pipeline did.

---

## What a verification failure means

| Symptom | Cause |
|---|---|
| Digest mismatch | The report changed after signing. Re-scan; do not re-sign the edited file |
| Identity mismatch | Signed by a different workflow, branch, or repo than you pinned |
| Certificate expired | Fulcio certs are short-lived. Verification relies on the log entry, so check `--no-tlog` is not wrongly set |
| No log entry | Signed with `--no-tlog`. Verify with `--no-tlog` too, and supply the public key |

A failed verification is a real signal. Do not work around it by re-signing.

---

## Triage order

1. **Verify first.** An unverified predicate is just a JSON file — anyone can write one.
2. **Check `rulesOrigin`.** `unsigned:custom` means the findings came from rules you should confirm.
3. **Check `engineVersion`.** `dev` is a local build.
4. **Read `critical`, then `high`.** These drive the verdict and are the actionable set.
5. **Read `verdict` last.** It is a derived boolean; the counts carry the detail.
6. **Compare only like with like.** Different `rulesSha` means different rules, so the counts are not comparable across scans.

---

## What this does not do

- It does not certify compliance with any framework. Compliance mapping is a
  Trustabl Console feature, and even there it maps findings to controls and
  generates evidence — certification is an auditor's judgement.
- It does not attest runtime behaviour. Static scan only.
- It does not prove the rules were correct, only which rules ran.
- It does not include a commit SHA in `v1`.
- A `pass` verdict means no medium-or-higher findings **that Trustabl checks
  for**. It is not a statement that the repo is safe.

---

## How this is tested upstream

The engine's `Attestation` workflow proves the feature two ways:

- **`attest-e2e`** — runs on every push and PR, against **both** cosign v2.4.3
  and v3.1.1. Hermetic: scan → attest → verify → tamper → confirm verification
  fails. A single pinned cosign version silently missed a v3 breakage once, so
  the matrix is deliberate.
- **`attest-keyless`** — manual dispatch only, precisely because each run writes
  a permanent public Rekor entry. It proves the production Fulcio + Rekor path.

If you are evaluating whether to depend on this, the tamper step in `attest-e2e`
is the one to read: it is the test that would catch the signature failing open.
