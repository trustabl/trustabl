import { LlmAgent, SequentialAgent, AgentTool } from "@google/adk";

const drafter = new LlmAgent({
  name: "drafter",
  model: "gemini-2.0-flash",
  instruction: "Draft an answer.",
});

const reviewer = new LlmAgent({
  name: "reviewer",
  model: "gemini-2.0-flash",
  instruction: "Review the draft.",
  tools: [new AgentTool({ agent: drafter })],
});

export const pipeline = new SequentialAgent({
  name: "pipeline",
  subAgents: [drafter, reviewer],
});
