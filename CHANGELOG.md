# Changelog

All notable changes to Trustabl are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims
to follow Semantic Versioning once it reaches 1.0.

## [Unreleased]

### Changed

- **`ScanID` now folds the rules origin.** A provenance tag
  (`signed:<channel>` / `unsigned:custom` / `unsigned:default`) is folded into
  `ScanID` so two scans of the same code with rules of different provenance get
  distinct IDs. **This is a one-time change to every `ScanID`**: an otherwise
  unchanged scan produces a new ID after upgrading to this release. Provenance
  is now part of a scan's identity by design; baselines pinned to old IDs must
  be re-captured once.

### Fixed

- **Attestation now works with cosign v3.** cosign v3 removed the
  `--tlog-upload` flag (it defaults `--use-signing-config=true`), which broke the
  offline `--no-tlog` signing path (`--tlog-upload=false is not supported with
  --signing-config`). Trustabl now detects the cosign major version and, on v3+,
  signs against a generated no-Rekor `--signing-config`; v2 keeps
  `--tlog-upload=false`. Verification is unchanged. CI runs the attestation e2e
  against both cosign v2 and v3 so this cannot regress.

### Added

- **Scan attestation (opt-in).** New `internal/attest` package plus `trustabl
  attest` / `trustabl verify` subcommands and a `scan --attest` flag. Trustabl
  renders a deterministic in-toto predicate (type
  `https://trustabl.dev/attestation/scan/v1`) from a `ScanResult` and shells out
  to the **cosign** CLI to sign the JSON report (the attestation subject) and to
  verify it. Signing is **keyless by default** (ambient CI OIDC; the event is
  logged to the public Rekor transparency log) with a `--key` escape hatch for
  offline/private signing (`--no-tlog`). Verification is consumer-side and pins
  the signer identity + OIDC issuer. Requires the cosign CLI on PATH; a plain
  `scan` is unchanged and free of any new dependency. The predicate is **not**
  folded into `ScanID`.
- **Signed rules distribution — trust core (opt-in).** New `internal/rulesign`
  package: a reproducible bundle digest (sha256 over a normalized tar), an
  embedded Ed25519 trust keyring (verify-by-key-ID with validity windows,
  fail-closed), and signed channel statements that bind a channel to one bundle
  by digest with freshness and anti-rollback checks. A new `releaseSource`
  (behind the new `rulesource.Source` interface) resolves rules from a signed
  release channel — verify the statement, fetch the bundle by digest, re-derive
  and match the digest, install to a content-addressed cache — with an offline
  fallback that serves (and watermarks as stale) the last verified bundle.
  Reachable via the new opt-in **`--channel <name>`** flag; the **default scan
  is unchanged** (still the git source). Until signing keys are published a
  `--channel` scan refuses with exit `2` rather than running unverified rules.
  A scan that did not use blessed production rules — a pre-release channel or an
  unsigned `--rules-repo` source — is now **watermarked** in the human report
  and in the JSON `rules_origin` field.

- **`--verbose` / `--debug` diagnostics.** New global (persistent) flags on the
  root command, valid on every subcommand and placeable before or after it
  (`-v`/`--verbose`, `--debug`; `--debug` implies `--verbose`). `--verbose`
  narrates the scan on stderr — rule provenance (repo, ref, resolved SHA, cache
  fallback), per-phase discovery counts (languages, tools, agents, detected and
  unaudited SDKs, loaded detectors), output destinations, and a result summary
  (scan ID, score, findings by severity, exit code). `--debug` adds per-phase
  timing and capped per-entity / per-finding detail. Backed by a new leaf
  package `internal/logx` (nil-safe leveled logger). All diagnostics are
  **stderr-only**, so the report on stdout and the JSON/SARIF byte-stability
  contract are unaffected (`--format json --debug` still emits a clean
  document). Diagnostic color follows the report's rules (off under
  `--no-color`, `NO_COLOR`, or a non-terminal stderr). Because an animated
  progress panel and interleaved log lines would corrupt each other,
  `--verbose`/`--debug` render progress as plain `[phase]` lines.

- **`trustabl llm provider` — provider switching.** New subcommand group for
  managing which LLM provider is active:
  - `trustabl llm provider set <provider>` — switch the active provider;
    auto-creates an entry with a per-provider default model if not yet
    configured (`anthropic → claude-haiku-4-5`, `openai → gpt-4.1-nano`,
    `google → gemini-2.5-flash-lite`). Prints a key hint when a new provider
    is created.
  - `trustabl llm provider list` — list all configured providers; active
    provider marked with `*`.

- **`trustabl llm` — LLM provider configuration.** New command group for
  managing LLM provider keys and models, stored at
  `~/.config/trustabl/keys.json` (mode 0600, atomic write):
  - `trustabl llm list` — table of configured providers with masked keys;
    active provider marked with `*`.
  - `trustabl llm key set [key]` — store an API key (prompts securely if
    key is omitted; validates format for `anthropic` keys).
  - `trustabl llm key get` — display the masked key for the active provider.
  - `trustabl llm key delete` — delete the key with a `y/N` confirmation prompt.
  - `trustabl llm model set <model>` — set the model for the active provider.
  Defaults: provider `anthropic`, model `claude-haiku-4-5`. Multi-provider
  shape is in place from day one. Pre-requisite for `trustabl enrich`.

## [0.1.2] - 2026-06-03

### Added

- **`AGENTS.md` as a vendor-neutral repo-hygiene signal.** The scanner now
  discovers `AGENTS.md` alongside `CLAUDE.md`; the repo-guidance rules
  (`CSDK-203` / `OAI-202` / `ADK-201`) accept either file and fire only when
  neither is present, so a repo that documents its agents in `AGENTS.md` is no
  longer flagged.

### Changed

- **Interrupt cleanup.** Ctrl-C during a scan now cancels the in-flight work and
  drains it, so the rules-clone temp directory is removed instead of being
  leaked on interrupt.
- **`--strict` floors at low severity.** `info`/`META` findings no longer fail
  the build under `--strict`; only real findings gate CI.
- **Clearer schema-incompatibility error.** When a resolved rule pack targets a
  schema the engine cannot evaluate, the CLI now explains what to do (including
  `--rules-ref` troubleshooting) instead of failing opaquely.
- Determinism hardening: tools and agents are sorted by `(FilePath, Line, Name)`
  before edge resolution, and findings carry a total-order sort with adjacent
  dedup, so the report stays byte-stable regardless of walk order.

### Fixed

- **Subagent discovery no longer reports scaffolding as live agents.** The
  flat-collection fallback now skips `TEMPLATE.md` and `*-template.md` files,
  whose subagent-shaped frontmatter is a fill-in example rather than a real
  declaration. Files placed under the canonical `.claude/agents/` path are
  unaffected.

## [0.1.1] - 2026-06-01

### Added

- **TypeScript discovery for the OpenAI Agents SDK and Google ADK.** Previously
  only the Claude TypeScript surface was understood; the scanner now discovers
  tools, agents, guardrails, sessions, MCP servers, and hosted tools in the
  OpenAI Agents (`tool({...})`, `Agent({...})` / `Agent.create(...)`) and Google
  ADK TypeScript/JavaScript shapes, resolving agent→tool/handoff/guardrail edges
  by variable name. Backed by vendored, licensed example corpora.
- **Markdown agent surface.** Discovers and parses Claude Code skills
  (`SKILL.md`), slash commands (frontmatter `allowed-tools` / `model`), and
  plugins (`.claude-plugin/plugin.json`, `marketplace.json`, including the
  object form of `plugins[].source`), plus flat-collection subagents matched by
  frontmatter shape. These surface in the scan summary, and the presence of
  subagents/skills now triggers the Claude SDK rule pack even with no SDK code.
- **Repo-scope permission-bypass detection.** `CSDK-201` flags a
  `.claude/settings.json` `defaultMode: bypassPermissions`; `CSDK-202` flags
  `ClaudeAgentOptions(permission_mode="bypassPermissions")`. (Rule
  `schema_version` advanced to 8.)
- **Hosted-tool approval and policy checks.** Hosted-tool kwargs are captured and
  new predicates evaluate them, powering rules such as `OAI-111` and `ADK-008`
  for missing approval gates / safety policies on privileged hosted tools.
- **Per-finding line attribution.** Findings now carry a real
  `file:line`–`end_line` range for MCP servers, hosted tools, subagents,
  guardrails, sessions, and individual permission rules (a `Location` type is
  embedded across the inventory; the `line` JSON field was renamed to
  `start_line`).
- New and expanded rules across all three SDKs (Tier-1 additions for Claude,
  OpenAI, and ADK; `has_code_exec_call` and `has_print_call` predicates;
  `mcp_tool` applicability on `CSDK-004/005/006`).
- An `llms.txt` index pointing at the project documentation.

### Changed

- **Production-readiness hardening.** A panicking detector is now recovered and
  skipped (one malformed rule can no longer crash a scan); all network git
  operations are bounded by a timeout; rules-repo and target URLs are restricted
  to `https`/`ssh` (`file://` and `git://` are rejected); individual scanned
  files are size-capped and symlinks are skipped; SARIF results are sorted
  deterministically and file URIs no longer leak the local clone path; and parse
  **coverage** (files parsed vs. skipped) is now surfaced so an incomplete scan
  is never mistaken for a clean one.
- The risk-surfaces summary line now reports a count, examples, why it matters,
  and how to fix it — and no longer claims an OpenShell audit that does not ship.
- The example corpus moved to `testdata/corpus/`.

### Fixed

- **Rules that never fired.** `CSDK-102`; `ADK-104` (had been firing on every
  `LlmAgent`, re-pointed to `generate_content_config.safety_settings`); `ADK-105`
  (class-name mismatch).
- **Determinism.** `ScanID` now folds in every inventoried file list (not just
  Python), and TypeScript parameter names are sorted, so the ID and report stay
  byte-stable across more inputs.
- **Rule sourcing.** A local install fault (disk full, permission denied, failed
  rename, corrupt clone) is propagated instead of being masked as a cache hit.
- **Scoring.** Per-tool readiness is keyed by `(FilePath, Name)` so same-named
  tools in different files no longer collide.
- **Rule loading.** The loader rejects match predicates the rule's scope never
  evaluates, enforces the confidence upper bound (and rejects `NaN`), and applies
  the rule `language` gate at repo scope.
- AST-walk recursion is depth-bounded against adversarial nesting, and
  tree-sitter parsers are reused with parsed trees closed to bound memory.

## [0.1.0] - 2026-05-26

First tagged release. A single-binary, read-only static analyzer for agent-SDK
repositories that discovers an agent/tool inventory and reports reliability and
safety weaknesses.

### Added

- **Scan pipeline.** Flat, deterministic pipeline — recon (cheap, no AST) →
  per-language AST inventory → data-driven policy selection → analysis →
  scoring → review — exposed through a single `trustabl scan <target>` over a
  local path or a remote repo URL (cloned read-only; nothing is written into
  the scanned repo).
- **SDK and language coverage.**
  - Claude Agent SDK — Python (tools, agents, subagents at any path depth,
    `.claude/settings.json` permissions) and TypeScript (`tool()` factory,
    `query()` main-thread agents, inline/typed-const `AgentDefinition`,
    `createSdkMcpServer` + MCP config literals).
  - OpenAI Agents SDK — Python (tools, 11 hosted-tool classes, agents, 3 MCP
    server transports, guardrails, sessions).
  - Google ADK — Python (`LlmAgent`/`Sequential`/`Parallel`/`Loop`/`Langgraph`,
    `FunctionTool` wrapping, 13 built-in hosted tools, `sub_agents` edges).
  - MCP — Python tool registrations and config files.
  - OpenShell — shell-invocation risk surface (`subprocess` / `os.system` /
    `os.popen`) and `openshell/*.yaml` policy files.
- **Four-scope detection model.** Rules fire at `tool`, `agent`, `subagent`, or
  `repo` scope, each receiving a typed input; agent-scoped findings attribute to
  the specific agent (with resolved tool / handoff / guardrail edges), not the
  repo.
- **YAML-driven rule engine.** Validating loader (required fields, enum and
  scope checks, cross-file rule-ID uniqueness), a recursive conjunctive
  match-expression evaluator, and per-tool + overall reliability scoring. Ships
  the CSDK / OAI / ADK rule packs plus engine-emitted META findings (META-001..004)
  for honest "unaudited SDK" / "unused dep" / "opaque agent" / "audited but no
  applicable rule" signals.
- **External rule resolution.** Rules are not embedded in the binary; they are
  resolved at scan time from the `trustabl-rules` git repository, cached locally
  under the user cache dir, with offline cache fallback and a `schema_version`
  compatibility gate. `trustabl rules pull` fetches eagerly without scanning.
- **Output formats.** Human summary, `--format json`, and `--format sarif`
  (SARIF 2.1.0, accepted by `github/codeql-action/upload-sarif`).
- **Determinism contract.** Identical inputs produce an identical `ScanID`
  (which folds in the resolved rules SHA) and a byte-stable report; enforced by
  a determinism regression test.
- **CLI surface.** `trustabl scan` (with `--detectors`, `--format`, `--strict`,
  `--no-color`, `--no-progress`, `--rules-repo`, `--rules-ref`,
  `--no-rules-update`), `trustabl rules pull`, and `trustabl version`. Exit
  codes: `0` clean, `1` findings ≥ medium (or any finding under `--strict`),
  `2` scanner error or no usable rules.
- **Progress reporting.** Real-time recon / inventory / analysis progress on
  stderr (animated on a TTY, plain lines when piped, silent for JSON) that never
  touches stdout or the report.
- Release distribution via GitHub Releases, Homebrew tap, Scoop bucket, and a
  multi-arch `ghcr.io/trustabl/trustabl` Docker image.
