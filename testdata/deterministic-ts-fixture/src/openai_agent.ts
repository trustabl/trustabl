import { Agent, tool, defineInputGuardrail, MCPServerStdio } from "@openai/agents";
import { webSearchTool } from "@openai/agents-openai";

const computeSum = tool({
  name: "sum",
  description: "Add",
  parameters: { a: 0, b: 0 },
  execute: async () => "",
});

const blockPII = defineInputGuardrail({
  name: "block_pii",
  execute: async () => ({ tripwireTriggered: false }),
});

const fsServer = new MCPServerStdio({ command: "node" });

export const researcher = new Agent({
  name: "researcher",
  instructions: "research",
  tools: [computeSum, webSearchTool({ maxResults: 5 })],
  inputGuardrails: [blockPII],
  mcpServers: [fsServer],
});
