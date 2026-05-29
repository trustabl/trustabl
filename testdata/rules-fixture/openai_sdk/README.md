# OpenAI Agents SDK rule pack

Rules under this directory target the [OpenAI Agents SDK for Python](https://openai.github.io/openai-agents-python/).

## Supported SDK version

This pack is calibrated against the OpenAI Agents SDK as documented at the URL above (snapshot taken 2026-05-18). Since the SDK is pre-1.0, decorator names and `Agent(...)` kwargs may change. If a future SDK version renames `@function_tool` or restructures kwarg names, rule matches will silently degrade. Track upstream releases and bump this README's version line in the same PR.

## Layout

- `tool_definition.yaml` — OAI-001 (no docstring), OAI-002 (no typed params), OAI-007 (ambiguous name)
- `decorator_config.yaml` — OAI-003 (strict_mode=False), OAI-004 (no failure_error_function)
- `network.yaml` — OAI-005 (requests/httpx call without timeout), OAI-011 (urllib urlopen without timeout)
- `path_safety.yaml` — OAI-006 (unsafe path in I/O)
- `error_handling.yaml` — OAI-008 (raises without a structured error contract)
- `idempotency.yaml` — OAI-009 (mutating tool without idempotency key)
- `observability.yaml` — OAI-010 (prints to stdout)
- `shell_safety.yaml` — OAI-012 (subprocess spawn)
- `code_execution.yaml` — OAI-013 (eval/exec/compile on dynamic input)
- `agent_safety.yaml` — OAI-101 (no input_guardrails + shell tools), OAI-102 (stop_on_first_tool), OAI-103 (loop pattern), OAI-104 (raw Agent + FS tools), OAI-109 (WebSearchTool without input_guardrails)
- `mcp_safety.yaml` — OAI-106 (mcp_servers + no input_guardrails)
- `tracing.yaml` — OAI-201 (default tracing in use)
