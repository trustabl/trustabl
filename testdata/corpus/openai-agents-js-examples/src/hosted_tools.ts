import { Agent } from "@openai/agents";
import { webSearchTool, fileSearchTool } from "@openai/agents-openai";
import { shellTool } from "@openai/agents-core";

export const researcher = new Agent({
  name: "researcher",
  instructions: "Research things",
  tools: [
    webSearchTool({ maxResults: 5 }),
    fileSearchTool(),
    shellTool(),
  ],
});
