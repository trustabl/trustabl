import { LlmAgent, FunctionTool, GoogleSearchTool } from "@google/adk";

const summarize = new FunctionTool({
  name: "summarize",
  description: "Summarize",
  parameters: { text: "" },
  execute: async () => "",
});

const writer = new LlmAgent({
  name: "writer",
  model: "gemini-2.0-flash",
  instruction: "Write",
});

export const researcher = new LlmAgent({
  name: "researcher",
  model: "gemini-2.0-flash",
  instruction: "Research",
  tools: [summarize, new GoogleSearchTool()],
  subAgents: [writer],
});
