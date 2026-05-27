import { query } from "@anthropic-ai/claude-agent-sdk";
import type { HookJSONOutput } from "@anthropic-ai/claude-agent-sdk";
import * as path from "path";
import { EMAIL_AGENT_PROMPT } from "./email-agent-prompt";
import { customServer } from "./custom-tools";
import type { SDKMessage, SDKUserMessage } from "./types";

export interface AIQueryOptions {
  maxTurns?: number;
  cwd?: string;
  model?: string;
  allowedTools?: string[];
  appendSystemPrompt?: string;
  mcpServers?: any;
  hooks?: any;
  resume?: string;
  settingSources?: string[];
}

export class AIClient {
  private defaultOptions: AIQueryOptions;

  constructor(options?: Partial<AIQueryOptions>) {
    this.defaultOptions = {
      maxTurns: 100,
      cwd: path.join(process.cwd(), 'agent'),
      model: "opus",
      allowedTools: [
        "Task", "Bash", "Glob", "Grep", "LS", "Read", "Edit", "Write",
        "WebFetch", "TodoWrite", "WebSearch", "mcp__email__search_inbox", "mcp__email__read_emails", "Skill"
      ],
      appendSystemPrompt: EMAIL_AGENT_PROMPT,
      settingSources: ['local', 'project'],
      mcpServers: {
        "email": customServer
      },
      hooks: {
        PreToolUse: [
          {
            matcher: "Write|Edit|MultiEdit",
            hooks: [
              async (input: any): Promise<HookJSONOutput> => {
                const toolName = input.tool_name;
                const toolInput = input.tool_input;

                if (!['Write', 'Edit', 'MultiEdit'].includes(toolName)) {
                  return { continue: true };
                }

                let filePath = '';
                if (toolName === 'Write' || toolName === 'Edit') {
                  filePath = toolInput.file_path || '';
                } else if (toolName === 'MultiEdit') {
                  filePath = toolInput.file_path || '';
                }

                const ext = path.extname(filePath).toLowerCase();
                if (ext === '.js' || ext === '.ts') {
                  const customScriptsPath = path.join(process.cwd(), 'agent', 'custom_scripts');

                  if (!filePath.startsWith(customScriptsPath)) {
                    return {
                      decision: 'block',
                      stopReason: `Script files (.js and .ts) must be written to the custom_scripts directory. Please use the path: ${customScriptsPath}/${path.basename(filePath)}`,
                      continue: false
                    };
                  }
                }

                return { continue: true };
              }
            ]
          }
        ]
      },
      ...options
    };
  }

  async *queryStream(
    prompt: string | AsyncIterable<SDKUserMessage>,
    options?: Partial<AIQueryOptions>
  ): AsyncIterable<SDKMessage> {
    const mergedOptions = { ...this.defaultOptions, ...options };

    for await (const message of query({
      prompt,
      options: mergedOptions
    })) {
      yield message;
    }
  }

  async querySingle(prompt: string, options?: Partial<AIQueryOptions>): Promise<{
    messages: SDKMessage[];
    cost: number;
    duration: number;
  }> {
    const messages: SDKMessage[] = [];
    let totalCost = 0;
    let duration = 0;

    for await (const message of this.queryStream(prompt, options)) {
      messages.push(message);

      if (message.type === "result" && message.subtype === "success") {
        totalCost = message.total_cost_usd;
        duration = message.duration_ms;
      }
    }

    return { messages, cost: totalCost, duration };
  }
}