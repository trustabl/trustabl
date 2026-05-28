import { LlmAgent, FunctionTool, GoogleSearchTool } from "@google/adk";

const summarize = new FunctionTool({
  name: "summarize",
  description: "Summarize a passage of text",
  parameters: { text: "" },
  execute: async ({ text }) => text.slice(0, 100),
});

export const researcher = new LlmAgent({
  name: "researcher",
  model: "gemini-2.0-flash",
  instruction: "Research and summarize.",
  tools: [summarize, new GoogleSearchTool()],
});
