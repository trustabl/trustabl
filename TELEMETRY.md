# Telemetry

Trustabl collects anonymous usage data to help improve the product — to understand which SDKs users scan most often, catch reliability issues, and measure adoption. This page is the complete and authoritative list of every event and every property that can be sent. It is updated in the same commit as any event schema change.

**Telemetry is on by default.** Opt out at any time — see [How to opt out](#how-to-opt-out) below.

---

## What we collect

### What we never collect

No matter which events fire, Trustabl never sends:

| Category | Examples |
|---|---|
| Identifiers | Repo names, org names, usernames, email addresses, IP addresses |
| File system artifacts | File paths, directory names, filenames from the scanned repo |
| Source code | No snippets in any form, including inside error messages |
| Finding content | Explanation text, fix text, matched code — anything a rule produced |
| Tool / agent names | Names of discovered tools or agents in the scanned repo |
| LLM details | Provider name, model name, API key presence |
| Exact file counts | Raw file counts (coarse size buckets are used instead) |
| Env var values | CI provider detected by variable presence only, never variable values |
| Raw error strings | Error messages are bucketed into a closed enum before sending |

---

## Anonymous ID

Every event carries an `anonymous_id` that identifies the installation, not the person:

| Environment | Strategy |
|---|---|
| Local (interactive shell) | Random UUID v4, generated once and stored in `~/.config/trustabl/telemetry.json`. Stable across runs on the same machine. |
| CI (`CI=true` or a recognized CI provider env var is set) with no config file | Ephemeral UUID generated per invocation, never written to disk. CI runs are counted but not session-correlated. |

The anonymous ID is **never derived from machine fingerprinting**: no hostname, no MAC address, no username hash. It is a random UUID that identifies this Trustabl installation for product analytics only.

The `repo_id_hash` property on `scan.completed` is separate from the anonymous ID: it is a one-way hash of a CI repo env var (see below) used only to count unique repos, not to identify users.

---

## Base properties

These properties are merged into every event automatically:

| Property | Type | Example | Purpose |
|---|---|---|---|
| `anonymous_id` | string | `"a3f2c…"` | Anonymous installation identifier (UUID v4) |
| `cli_version` | string | `"0.9.1"` | Correlate observations to releases |

---

## Events

### `scan.started`

Fired when a scan begins, after argument validation passes (so an invalid `--detectors` flag does not emit this event).

| Property | Type | Example | Notes |
|---|---|---|---|
| `os` | string | `"darwin"` | `GOOS`: `darwin`, `linux`, `windows` |
| `arch` | string | `"arm64"` | `GOARCH`: `arm64`, `amd64` |
| `target_type` | string | `"local"` | `"local"` or `"remote"` (GitHub URL) |
| `format` | string | `"human"` | Output format: `human`, `json`, `sarif` |
| `strict_mode` | bool | `false` | Whether `--strict` was passed |
| `flags_used` | []string | `["strict","sarif-out"]` | Names of flags explicitly set by the user; **never flag values** |
| `ci_provider` | string | `"github_actions"` | Closed enum: `"github_actions"`, `"gitlab_ci"`, `"circleci"`, `"jenkins"`, `"unknown"`, or `""` (not CI). Detected by env var presence only — no env var values are read. |
| `is_new_install` | bool | `true` | Whether this is the first run on this machine (no prior config file) |

---

### `scan.completed`

Fired on a successful scan (exit code 0 or 1 — findings present or not, but no scanner error).

| Property | Type | Example | Notes |
|---|---|---|---|
| `duration_ms` | int | `4200` | Wall-clock milliseconds from start to finish |
| `repo_size_bucket` | string | `"medium"` | `"small"` (< 20 files), `"medium"` (< 200 files), `"large"` (≥ 200 files). File count includes Python, TypeScript, JavaScript, Go, YAML, JSON, Markdown, C#, PHP, and Rust files. |
| `sdks_detected` | []string | `["openai_sdk","claude_sdk"]` | SDK identifiers observed in code (not just declared as deps) |
| `languages_detected` | []string | `["python","typescript"]` | Languages recognized in the repo |
| `tools_count` | int | `12` | Number of tool definitions discovered |
| `agents_count` | int | `3` | Number of agent declarations discovered |
| `findings_by_severity` | object | `{"high":2,"medium":5}` | Finding count per severity level |
| `rule_ids_fired` | object | `{"CSDK-001":3,"OAI-005":1}` | Count of hits per rule ID. Rule IDs are public identifiers; no finding content is included. |
| `rules_sha` | string | `"abc1234"` | Resolved commit SHA of the rule pack used for this scan |
| `schema_version` | int | `4` | Rule schema version of the resolved pack |
| `exit_code` | int | `1` | Process exit code: `0` (clean) or `1` (findings present) |
| `features_used` | []string | `["attest","sarif_out"]` | Optional features activated for this scan. Possible values: `attest`, `vuln_scan`, `sarif_out`, `json_out`, `bom_out`, `no_rules_update`. |
| `repo_id_hash` | string | `"3a9f…"` | 32-character hex prefix of a salted SHA-256 of the CI repo env var (`GITHUB_REPOSITORY`, `CI_PROJECT_PATH`, `CIRCLE_PROJECT_REPONAME`). Empty when not running in CI or when no recognized repo env var is set. Used only for deduplication — **the hash is one-way and the repo name cannot be recovered from it**. |

---

### `scan.failed`

Fired when the scan exits with code 2 (a scanner or I/O error, not a findings-based exit).

| Property | Type | Example | Notes |
|---|---|---|---|
| `error_category` | string | `"rules_fetch_failed"` | Closed enum — the raw error string is **never sent**. Values: `"rules_fetch_failed"`, `"clone_failed"`, `"parse_error"`, `"no_rules"`, `"unknown"`. |
| `duration_ms` | int | `800` | Wall-clock milliseconds until failure |

---

### `command.run`

Fired for every non-scan subcommand invocation.

| Property | Type | Example | Notes |
|---|---|---|---|
| `command` | string | `"mcp"` | Dotted command name. Possible values: `version`, `mcp`, `enrich`, `attest`, `verify`, `capabilities`, `rules.pull`, `rules.validate`, `vulndb.pull`, `llm.list`, `llm.key.set`, `llm.key.get`, `llm.key.delete`, `llm.model.set`, `llm.provider.set`, `llm.provider.list`. |

---

## How to opt out

Three mechanisms, evaluated in this order:

**1. Environment variable (highest priority)**

```sh
export TRUSTABL_TELEMETRY=0   # disable for this shell session
```

Set `TRUSTABL_TELEMETRY=1` to unconditionally enable (useful in CI when you want telemetry despite no config file).

**2. CLI command (persisted)**

```sh
trustabl telemetry off    # writes enabled:false to config file
trustabl telemetry on     # re-enable
trustabl telemetry status # show current state and its source
```

**3. Config file (manual)**

Edit `~/.config/trustabl/telemetry.json` (created on first run):

```json
{"enabled": false, "anonymous_id": "your-uuid-here"}
```

---

## Where data is stored locally

| File | Contents |
|---|---|
| `~/.config/trustabl/telemetry.json` | `enabled` flag and the stable anonymous UUID. Created on first run, mode `0600`. Never created in CI environments. |

The config file is created with directory permissions `0700` and file permissions `0600`. It is never created in CI (where `CI=true` or a recognized CI provider env var is set).

---

## Backend

Trustabl uses [PostHog](https://posthog.com) as its telemetry backend. Events are sent over HTTPS. PostHog is a product analytics platform; Trustabl does not use it for advertising, profiling, or any purpose other than product improvement. PostHog's privacy policy is at [posthog.com/privacy](https://posthog.com/privacy).

Events are batched and sent asynchronously — no telemetry call ever adds latency to a scan. Network errors and slow endpoints are silently discarded; they never appear on stderr and never affect the exit code.

---

## First-run notice

On the first run in an interactive terminal (TTY), before any scan output, Trustabl prints:

```
Trustabl collects anonymous usage data to help improve the product.
No source code, file paths, repo names, or finding details are ever sent.
Run `trustabl telemetry off` or set TRUSTABL_TELEMETRY=0 to disable.
Learn more: https://trustabl.dev/telemetry
```

This notice is shown once, on TTY only. It is never shown in CI or when output is piped.

---

## Questions

Open an issue at [github.com/trustabl/trustabl/issues](https://github.com/trustabl/trustabl/issues) or reach out on [Discord](https://discord.gg/maQ7QMPsB).
