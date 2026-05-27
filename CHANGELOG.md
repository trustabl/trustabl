# Changelog

All notable changes to Trustabl are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims
to follow Semantic Versioning once it reaches 1.0.

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
