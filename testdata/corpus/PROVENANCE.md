# Provenance and licensing — `testdata/corpus/`

These subdirectories are the corpus Trustabl scans during testing (see
`internal/scanner/scanner_test.go::TestScanExamples_NoCrash`). The corpus
mixes (a) fixtures we wrote ourselves, (b) third-party demos vendored
under their original license, and (c) full third-party projects that ship
with their own `LICENSE` file in-tree.

This file documents the source and license of every entry so a public
release does not silently redistribute third-party work without
attribution. The third-party LICENSE texts that are NOT inlined per-example
live in [`LICENSES/`](LICENSES/).

## Our own (covered by the repo's top-level Apache-2.0 LICENSE)

These were authored in this repository as test fixtures.

| Directory | Purpose |
| --- | --- |
| `claude-settings-fixture/` | Minimal `.claude/settings.json` fixture for the settings discovery + permission-parser tests |
| `google-adk-demo/` | Hand-written Google ADK demo exercising `LlmAgent` / `SequentialAgent` / `ParallelAgent` / `LoopAgent` / `LanggraphAgent` shapes for the ADK-* rule tests |
| `ts-claude-sdk-min/` | Minimal TypeScript Claude Agent SDK fixture exercising `tool()` / inline-`query()` agents / typed-const `AgentDefinition` / `createSdkMcpServer` for the TS discovery tests |
| `langchain-demo/` | Hand-written LangChain / LangGraph demo exercising the `@tool` decorator, a `StructuredTool` shell-out, and `create_react_agent` wiring `PythonREPLTool` for the LC-* rule tests |
| `skill-vuln-fixtures/` | Synthetic Claude Code Agent Skills (a deliberately-vulnerable `leak-helper` + a benign `safe-reader`) exercising enriched skill discovery and the CSKILL-* rule tests. No real payload; all hosts are `example.*` placeholders |
| `acac-static-capture/` | Minimal Python + TS fixture for the Stage 2 typed captures (static HTTP hosts, static write paths, retry decorators) and the ACaC / OpenShell-export goldens. All hosts are `example.*` placeholders |

## Vendored from `openai/openai-agents-python` (MIT)

These are demo programs shipped under the OpenAI Agents SDK's MIT license.
The license text lives at [`LICENSES/openai-agents-python-MIT.txt`](LICENSES/openai-agents-python-MIT.txt).

| Directory | Upstream path |
| --- | --- |
| `basic-openai-agent/` | `examples/basic/` |
| `financial_research_agent/` | `examples/financial_research_agent/` |
| `openai-hosted-mcp/` | `examples/mcp/hosted_mcp/` (approx.) |
| `openai-mcp-filesystem/` | `examples/mcp/filesystem_example/` (approx.) |
| `research_bot/` | `examples/research_bot/` |

## Vendored from `anthropics/email-agent` (ISC)

| Directory | License source |
| --- | --- |
| `email-agent/` | ISC, per the upstream `package.json` `"license": "ISC"`. License text inlined at [`LICENSES/anthropics-email-agent-ISC.txt`](LICENSES/anthropics-email-agent-ISC.txt) |

The vendored README at `email-agent/README.md` preserves the upstream's
"demo by Anthropic — local development only" notice verbatim.

## Vendored with their own in-tree `LICENSE` file

These ship a top-level `LICENSE` file inside the example directory, so the
license travels with the example.

| Directory | License (from in-tree `LICENSE`) |
| --- | --- |
| `deep-research/` | MIT |
| `excel-demo/` | MIT |
| `ToolBench/` | Apache-2.0 |

## Unverified provenance — needs confirmation before public reuse

These were added to the corpus in the initial scaffolding commit
(`ba34b99`) without a recorded upstream URL. Their READMEs reference
`ANTHROPIC_API_KEY` and `@anthropic-ai/claude-agent-sdk`, suggesting they
were copied from an Anthropic-published demo. Treat them as
**unverified-provenance third-party material** until the upstream is
confirmed and the appropriate license recorded.

| Directory | Notes |
| --- | --- |
| `research-agent/` | "Multi-Agent Research System" — references `ANTHROPIC_API_KEY`; likely an anthropics demo |
| `simple-chatapp/` | "Simple Chat App" — declares `@anthropic-ai/claude-agent-sdk` dep; likely an anthropics demo |

If you can identify the upstream for either, add a license file in
`LICENSES/` and move the directory to the appropriate section above. If
the upstream cannot be located, remove the vendored copy rather than
ship it under uncertain terms.

## Modification policy

We do not modify vendored examples. The corpus exists to scan
real-world-shape code; editing it would defeat that purpose. The single
exception is removing transitively-installed `node_modules/` and other
build artefacts (covered by `.gitignore`), which are not redistribution.
