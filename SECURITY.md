# Security Policy

## Reporting a vulnerability

Report privately through GitHub, not in a public issue:

**[Open a private security advisory](https://github.com/trustabl/trustabl/security/advisories/new)**

That route is private between you and the maintainers until a fix ships. Please
do not open a public issue, a pull request, or a discussion for a suspected
vulnerability.

Useful things to include: the version (`trustabl --version`), the platform, the
smallest input that reproduces it, and what you expected instead. A proof of
concept is welcome but not required.

We will acknowledge the report and tell you whether we consider it in scope. If
we accept it, we will keep you updated through the advisory and credit you in
the release notes unless you would rather we did not.

## Supported versions

Fixes land on the latest release. There are no long-term support branches.

| Version | Supported |
|---|---|
| Latest release | Yes |
| Anything older | No — upgrade first |

## Scope

Trustabl is a local CLI. It reads source code, resolves detection rules from a
separate repository, and writes reports. There is no server, no hosted service,
and no account.

**In scope**

- Code execution, path traversal, or file writes outside the intended output
  paths, triggered by a scanned repository's contents
- Anything that causes the scanner to transmit source code off the machine
  during `trustabl scan`, which is meant to be fully local
- Attestation flaws: a tampered report that still verifies, a signature that
  passes when it should not, or identity checks that can be bypassed
- Rule resolution flaws: loading rules from an unintended source, or accepting a
  rules bundle that fails its signature or schema gate
- Credential handling in the CLI or the shipped CI integrations, including
  leaking a token into logs or process arguments

**Out of scope**

- **A missed finding or a false positive is a bug, not a vulnerability.** Open a
  normal issue. Detection coverage is documented in `COVERAGE.md`, including
  known gaps
- Vulnerabilities in a repository you scanned. Those belong to that repository
- Findings that require an attacker to already control the machine running the
  scan
- Reports produced only by an automated scanner, with no demonstrated impact

## Applying fixes uses your own key

`trustabl scan` never needs a network connection or an API key. `trustabl enrich
--apply` is opt-in and sends the flagged code region to the LLM provider whose
key you supply. That is a deliberate boundary rather than a defect, but if you
find `enrich` transmitting more than the region it reports, that is in scope.
