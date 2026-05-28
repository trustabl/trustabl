import { Agent, MCPServerStdio, MCPServerSSE } from "@openai/agents";

const fs = new MCPServerStdio({ command: "node", args: ["./fs-server.js"] });
const events = new MCPServerSSE({ url: "https://example.com/events" });

export const integrated = new Agent({
  name: "integrated",
  instructions: "Use external tools",
  mcpServers: [fs, events],
});
