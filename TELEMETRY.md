# Telemetry

Trustabl collects anonymous usage data to help improve the product — to understand which SDKs users scan most often, catch reliability issues, and measure adoption. This page is the complete and authoritative list of every event and every property that can be sent. It is updated in the same commit as any event schema change.

**Telemetry is off by default.** On your first interactive scan, Trustabl asks you to choose a level. You can change your choice at any time — see [Manage telemetry](#manage-telemetry) below.

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

## Telemetry levels

| Level | What is sent |
|---|---|
| **Disabled** | Nothing |
| **Minimal** | `anonymous_id`, `cli_version`, `ci_provider`, `is_new_install`, `exit_code` — one event at scan end |
| **Full** | All events and properties listed below |

In CI (`CI=true` or a recognized CI provider env var), telemetry defaults to **Disabled** unless `TRUSTABL_TELEMETRY` is explicitly set.

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
| `phase` | string | `"rules"` | Pipeline phase where the failure occurred. Derived from `error_category` — no additional data collected. Values: `"rules"`, `"clone"`, `"inventory"`, `"unknown"`. |
| `duration_ms` | int | `800` | Wall-clock milliseconds until failure |
| `rules_sha` | string | `"abc1234"` | Resolved rules SHA at time of failure. Empty string if the failure occurred before rules were resolved. |
| `schema_version` | int | `4` | Rule schema version at time of failure. `0` if not yet resolved. |

---

### `command.run`

Fired for every non-scan subcommand invocation.

| Property | Type | Example | Notes |
|---|---|---|---|
| `command` | string | `"mcp"` | Dotted command name. Possible values: `version`, `mcp`, `enrich`, `attest`, `verify`, `capabilities`, `rules.pull`, `rules.validate`, `vulndb.pull`. |

---

### `crash.reported`

Fired **only** when a user explicitly chooses "Send crash report" after a panic. It is never sent automatically. Crash reporting is **independent of the telemetry setting** — this event fires the same way whether telemetry is `full`, `minimal`, or `disabled`, because the per-crash prompt is its own separate consent. The only thing that stops it is the absence of a PostHog key in the build (nowhere to send). Turning telemetry off does **not** turn off the ability to send a crash report; the choice is made fresh at each crash and is never stored.

| Property | Type | Example | Notes |
|---|---|---|---|
| `panic_value` | string | `"runtime error: index out of range [2] with length 2"` | The recovered panic value, passed through best-effort redaction of common secret shapes (e.g. `sk-ant-*`, `sk-proj-*`, long hex/base64 strings). Not guaranteed to be free of all sensitive content. |
| `stack` | string | `"goroutine 1 [running]:\nmain.(...)\n\tmain.go:42\n..."` | Scrubbed stack frames only — no argument values, no source lines, file paths trimmed to `basename:line`. |
| `version` | string | `"0.9.1"` | CLI build version |
| `commit` | string | `"abc1234"` | Build commit SHA |
| `os` | string | `"darwin"` | `GOOS` |
| `arch` | string | `"arm64"` | `GOARCH` |
| `rules_sha` | string | `""` | Always empty for crash reports — build meta carries no resolved SHA at the panic site. Reserved for future use. |

---

## Manage telemetry

Three mechanisms for explicit control, evaluated in this order (the first-run prompt handles the initial choice):

**1. Environment variable (highest priority)**

```sh
export TRUSTABL_TELEMETRY=disabled   # or: 0
export TRUSTABL_TELEMETRY=minimal
export TRUSTABL_TELEMETRY=full       # or: 1
```

**2. CLI commands (persisted to config file)**

```sh
trustabl telemetry off      # disable — no data sent
trustabl telemetry minimal  # version and outcome only
trustabl telemetry full     # all anonymous usage stats
trustabl telemetry status   # show current level and its source
```

**3. Config file (manual)**

Edit `~/.config/trustabl/telemetry.json`:

```json
{"mode": "minimal", "anonymous_id": "your-uuid-here"}
```

Valid values for `mode`: `"disabled"`, `"minimal"`, `"full"`.

---

## Where data is stored locally

| File | Contents |
|---|---|
| `~/.config/trustabl/telemetry.json` | `mode` setting and the stable anonymous UUID. Created when a telemetry level is chosen (first-run prompt or CLI command), mode `0600`. Never created in CI environments. |
| `~/.config/trustabl/crash-<timestamp>.log` | Scrubbed crash report written on an unrecovered panic. See "Crash reports" below. |

The config file is created with directory permissions `0700` and file permissions `0600`. It is never created in CI (where `CI=true` or a recognized CI provider env var is set).

### Crash reports

When Trustabl experiences an unrecovered panic, it always writes a scrubbed crash report to `~/.config/trustabl/crash-<UTC-timestamp>.log` (mode `0600`, directory `0700`). This happens even in CI or non-interactive environments — the local file is the permanent, transparent record of what was captured.

The crash report contains the scrubbed panic value and stack frames (see `crash.reported` properties above for exactly what is included). **Nothing is transmitted without an explicit choice**: after writing the file, Trustabl prompts the user with a numbered menu:

```
Help us fix it? No source code or file contents are sent.
  1. Send crash report
  2. Open GitHub issue
  3. Do nothing

Enter 1, 2, or 3 [default: 3]:
```

This prompt is shown **only** in an interactive terminal (stderr is a TTY and neither `CI` nor a recognized CI provider env var is set). In CI or when output is piped, no prompt appears and nothing is sent. The default action is always "Do nothing". All three options are shown on **every** crash — the "Send crash report" option is **never** hidden or renumbered based on the telemetry setting, because crash reporting is a separate consent from usage telemetry. Choosing "Send crash report" fires the `crash.reported` event and works even when telemetry is `disabled` (it only no-ops if the build has no PostHog key); choosing "Open GitHub issue" opens a pre-filled URL in the browser — no data is transmitted by Trustabl itself.

---

## Backend

Trustabl uses [PostHog](https://posthog.com) as its telemetry backend. Events are sent over HTTPS. PostHog is a product analytics platform; Trustabl does not use it for advertising, profiling, or any purpose other than product improvement. PostHog's privacy policy is at [posthog.com/privacy](https://posthog.com/privacy).

Events are batched and sent asynchronously — no telemetry call ever adds latency to a scan. Network errors and slow endpoints are silently discarded; they never appear on stderr and never affect the exit code.

---

## First-run prompt

On the first scan in an interactive terminal (TTY), before any scan output, Trustabl asks:

```
Trustabl collects anonymous data to help improve the product.
No source code, file paths, repo names, or finding details are ever sent.
Learn more: https://trustabl.ai/telemetry

Choose a telemetry level:
  1. Minimal  - Version and outcome
  2. Full     - Usage stats
  3. Disabled - No data

Enter 1, 2, or 3 [default: 3]: 
```

The choice is saved to `~/.config/trustabl/telemetry.json` and never asked again. Empty input or no response defaults to **Disabled**. The prompt is never shown in CI or when output is piped.

---

## Questions

Open an issue at [github.com/trustabl/trustabl/issues](https://github.com/trustabl/trustabl/issues) or reach out on [Discord](https://discord.gg/maQ7QMPsB).
