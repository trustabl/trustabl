import { tool, query, createSdkMcpServer, AgentDefinition } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";

export const searchTool = tool(
  "search",
  "Search the web",
  { query: z.string() },
  async ({ query }) => ({ content: [{ type: "text", text: `Results for ${query}` }] }),
  { annotations: { readOnlyHint: true } }
);

export const myServer = createSdkMcpServer({
  name: "my-tools",
  version: "1.0.0"
});

export const reviewer: AgentDefinition = {
  description: "Code review specialist",
  tools: ["Read", "Grep"],
  prompt: "You are a code reviewer."
};

export const q = query({
  prompt: "Analyze this code",
  options: {
    agents: {
      analyst: {
        description: "Data analyst",
        tools: ["Read"],
        prompt: "Analyze data."
      }
    },
    mcpServers: {
      "my-tools": { type: "sdk", name: "my-tools", instance: myServer.instance },
      "fs":        { type: "stdio", command: "node", args: ["./fs.js"] }
    }
  }
});
