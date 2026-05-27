// agent/custom_scripts/listeners/example-listener.ts
import type { ListenerConfig, Email, ListenerContext, ListenerResult } from "../types";

/**
 * Example listener demonstrating how to use subagents for intelligent email processing
 *
 * This listener uses AI subagents to:
 * 1. Analyze email importance
 * 2. Detect newsletters
 * 3. Make intelligent decisions about email handling
 */

export const config: ListenerConfig = {
  id: "example_listener",
  name: "Example AI Email Listener",
  description: "Demonstrates using subagents for intelligent email classification",
  enabled: false, // Set to true to enable
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<ListenerResult> {
  const actions: string[] = [];

  // Use a subagent to analyze the email's importance
  const importanceAnalysis = await context.callAgent<{
    isImportant: boolean;
    isUrgent: boolean;
    reason: string;
  }>({
    prompt: `Analyze this email for importance and urgency:

From: ${email.from}
Subject: ${email.subject}
Body preview: ${email.body.substring(0, 300)}

Determine if this email is important (requires attention) or urgent (time-sensitive).`,
    schema: {
      type: "object",
      properties: {
        isImportant: {
          type: "boolean",
          description: "Whether this email requires attention"
        },
        isUrgent: {
          type: "boolean",
          description: "Whether this email is time-sensitive"
        },
        reason: {
          type: "string",
          description: "Brief explanation of the assessment"
        }
      },
      required: ["isImportant", "isUrgent", "reason"]
    },
    model: "haiku" // Fast, efficient model for quick analysis
  });

  // Handle important/urgent emails
  if (importanceAnalysis.isImportant || importanceAnalysis.isUrgent) {
    const priority = importanceAnalysis.isUrgent ? "high" : "normal";
    await context.notify(`${importanceAnalysis.isUrgent ? "Urgent" : "Important"} email: ${email.subject}`, {
      priority: priority as "high" | "normal"
    });
    await context.starEmail(email.messageId);
    actions.push("starred", "notified");

    return {
      executed: true,
      reason: importanceAnalysis.reason,
      actions
    };
  }

  // Use a subagent to detect if this is a newsletter
  const newsletterAnalysis = await context.callAgent<{
    isNewsletter: boolean;
    confidence: number;
    reasoning: string;
  }>({
    prompt: `Determine if this email is a newsletter or promotional content:

From: ${email.from}
Subject: ${email.subject}
Body preview: ${email.body.substring(0, 500)}

Consider:
- Sender patterns (noreply@, newsletter@, etc.)
- Content structure (promotional, bulk mail indicators)
- Unsubscribe links presence
- Marketing language`,
    schema: {
      type: "object",
      properties: {
        isNewsletter: {
          type: "boolean",
          description: "Whether this appears to be a newsletter"
        },
        confidence: {
          type: "number",
          description: "Confidence level 0-1"
        },
        reasoning: {
          type: "string",
          description: "Explanation for the classification"
        }
      },
      required: ["isNewsletter", "confidence", "reasoning"]
    },
    model: "haiku"
  });

  // Auto-archive newsletters
  if (newsletterAnalysis.isNewsletter && newsletterAnalysis.confidence > 0.7) {
    await context.archiveEmail(email.messageId);
    await context.markAsRead(email.messageId);
    actions.push("archived", "marked-read");

    return {
      executed: true,
      reason: `Newsletter detected (${Math.round(newsletterAnalysis.confidence * 100)}% confidence): ${newsletterAnalysis.reasoning}`,
      actions
    };
  }

  return {
    executed: false,
    reason: "Email did not match any automation criteria"
  };
}
