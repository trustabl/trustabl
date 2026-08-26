# Open source vs Trustabl Console

Trustabl is open core. This page draws the line explicitly, so you know what the
Apache-2.0 CLI does before you install it.

Open-core projects that leave the boundary implicit get punished twice.
Developers assume the open-source repo does everything, hit a paywall, and
conclude they were misled. And an LLM asked "can Trustabl map findings to
SOC 2?" answers yes without qualification, which sets that same disappointment
up at scale.

Drawing the line clearly is how Grafana, GitLab, Sentry and HashiCorp all
present themselves.

## Capabilities

| | Open source (Apache-2.0) | Trustabl Console (commercial) |
|---|---|---|
| Scanner CLI + local MCP server | ✅ `trustabl/trustabl` | |
| Detection rule packs | ✅ `trustabl/trustabl-rules` | |
| Autofix (`enrich --apply`) | ✅ | |
| Findings, readiness score, SARIF, deterministic scan ID | ✅ | |
| GitHub Action | ✅ | |
| VS Code extension | ✅ | |
| Claude Code plugin · Gemini CLI extension | ✅ *(in this repo)* | |
| Docs · examples | ✅ | |
| Rule rationale / threat-model docs (`trustabl-rulebook`) | | Console *(currently public under GPL-3.0)* |
| OpenShell policy generation | | Console, live — no public `openshell-policy-gen` repo; it stays closed source |
| OPA/Rego · ACS manifest · conformance spec | | Console, live |
| Compliance mapping (NIST 800-53, ISO 27002, SOC 2, EU AI Act, PCI DSS) | | Console, live |
| Signed Governance snapshots (in-toto / DSSE) | | Console, live |
| OpenShell Policy Prover | | Console, live |
| Console — single pane of glass | | Console, live |
| Threat-intel advisory feed, auto-tightening contracts | | Console, roadmap |

## CI integrations — free to use, but not Apache-2.0

These wrap the same open-source CLI and are free to install and run. They are
source-available under a proprietary licence, not open source. Calling them
Apache-2.0 would be wrong.

| Integration | Where to get it | Licence |
|---|---|---|
| GitHub Action | [repo](https://github.com/trustabl/trustabl-action) · [Marketplace](https://github.com/marketplace/actions/trustabl-fix-agent-reliability-issues) | Apache-2.0 |
| VS Code extension | [repo](https://github.com/trustabl/trustabl-vscode) | Apache-2.0 |
| GitLab CI/CD component | [repo](https://gitlab.com/trustabl-ai/components) · CI/CD Catalog | Proprietary (source-available) |
| Bitbucket pipe | [repo](https://bitbucket.org/hoolisoftware/trustabl-pipe) · Atlassian's official Pipes catalog | Proprietary (source-available) |
| Cursor plugin | [repo](https://github.com/trustabl/trustabl-cursor) · [cursor.directory](https://cursor.directory/plugins/trustabl) | Proprietary (source-available) |
| AWS (CodePipeline / CodeCatalyst) | [repo](https://github.com/trustabl/trustabl-aws) | Proprietary (source-available) |
| Azure DevOps extension | [repo](https://github.com/trustabl/trustabl-azure-devops) · [Marketplace](https://marketplace.visualstudio.com/items?itemName=trustabl.trustabl-azure-devops-extension) | Proprietary (source-available) |

None of these require a Console subscription.

## What this means in practice

**The open-source CLI is complete for its job.** It scans, scores, finds defects,
and fixes them. Nothing about it is a trial or a teaser: scanning is fully local
and needs no key, and autofix uses your own LLM key rather than ours.

**Console renders the same scan into other artifacts.** One scan produces one
canonical contract per agent. Console takes that contract and renders policy
bundles, control mappings and signed snapshots from it. The scan is the same
scan; the difference is what gets generated downstream.

**Compliance mapping does not certify compliance.** It maps findings to controls
and generates evidence toward them. Certification is an auditor's judgement, not
a tool's output.

## Licensing

The engine, the rule packs, the GitHub Action and the VS Code extension are
Apache-2.0. The remaining CI integrations are source-available proprietary — see
the table above. The `trustabl-rulebook` repository is
currently public under GPL-3.0, which is inconsistent with the rest and is being
reconciled.

Commercial licence terms for Console components are being finalised and are not
published here yet.
