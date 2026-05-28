import { Agent, defineInputGuardrail, defineOutputGuardrail, MemorySession } from "@openai/agents";

const blockPII = defineInputGuardrail({
  name: "block_pii",
  execute: async ({ input }) => ({
    tripwireTriggered: typeof input === "string" && input.includes("ssn"),
  }),
});

const sanitize = defineOutputGuardrail({
  name: "sanitize",
  execute: async () => ({ tripwireTriggered: false }),
});

export const safe = Agent.create({
  name: "safe",
  instructions: "Be safe",
  inputGuardrails: [blockPII],
  outputGuardrails: [sanitize],
});

export const session = new MemorySession();
