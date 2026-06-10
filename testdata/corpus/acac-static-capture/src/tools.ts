// TS half of the Stage 2 capture fixture: a Claude SDK tool with a static
// outbound URL (non-default port) and a static write path. Host is an
// example.* placeholder. No agent here — the Python file owns the single
// AgentDef so the repo stays single-agent for the generate goldens.
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
import * as fs from "fs";

export const mirrorTool = tool(
  "mirror_status",
  "Mirror the status page snapshot to local disk",
  { page: z.string() },
  async ({ page }) => {
    const res = await fetch("https://mirror.example.net:8443/snapshot", {
      signal: AbortSignal.timeout(5000),
    });
    fs.writeFileSync("/workspace/out/mirror.html", await res.text());
    return { content: [{ type: "text", text: page }] };
  }
);
