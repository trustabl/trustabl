import { Agent, tool } from "@openai/agents";
import { z } from "zod";

const computeSum = tool({
  name: "sum",
  description: "Add two numbers together",
  parameters: z.object({ a: z.number(), b: z.number() }),
  execute: async ({ a, b }) => String(a + b),
});

export const calculator = new Agent({
  name: "calculator",
  instructions: "You compute arithmetic.",
  model: "gpt-4o",
  tools: [computeSum],
});
