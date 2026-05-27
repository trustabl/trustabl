# ts-claude-sdk-min

Minimal Claude Agent SDK fixture in TypeScript. Exercises:

- `tool()` factory with Zod schema and `extras` config
- `createSdkMcpServer()` with name/version
- Typed-const `AgentDefinition`
- Inline-in-`query()` agent with `tools: [...]` and `mcpServers: {...}`
- Both `sdk` (SDK-instance) and `stdio` MCP server config types

Used by `internal/scanner/scanner_test.go::TestScanExamples_NoCrash`.
