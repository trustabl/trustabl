// Template: Urgent Email Watcher
// Use case: Notify immediately when specific sender sends urgent emails
// Uses AI to intelligently detect urgency instead of hard-coded keywords

import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "boss_urgent_watcher",
  name: "Boss Urgent Email Watcher",
  description: "Uses AI to detect urgent emails from boss",
  enabled: true,
  event: "email_received"
};

interface UrgencyAnalysis {
  isUrgent: boolean;
  priority: "high" | "normal" | "low";
  reason: string;
  requiresImmediateAction: boolean;
}

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Filter 1: Check sender (only basic filtering, let AI handle the rest)
  // Replace with actual email address
  if (!email.from.includes("boss@company.com")) return;

  // Use AI to intelligently detect urgency (not just keywords)
  const analysis = await context.callAgent<UrgencyAnalysis>({
    prompt: `Analyze this email for urgency and time-sensitivity:

From: ${email.from}
Subject: ${email.subject}
Body: ${email.body.substring(0, 1000)}

Determine:
1. Is this truly urgent? (Consider context, not just keywords like "urgent" or "ASAP")
2. What priority level? (high/normal/low)
3. Brief reason for your assessment
4. Does it require immediate action?

Be discerning - not every email with "important" is actually urgent.`,

    schema: {
      type: "object",
      properties: {
        isUrgent: { type: "boolean" },
        priority: { type: "string", enum: ["high", "normal", "low"] },
        reason: { type: "string" },
        requiresImmediateAction: { type: "boolean" }
      },
      required: ["isUrgent", "priority", "reason", "requiresImmediateAction"]
    },

    model: "haiku" // Fast and cost-effective for classification
  });

  // Only notify if AI determines it's truly urgent
  if (!analysis.isUrgent) return;

  // Action 1: Send notification with AI reasoning
  await context.notify(
    `Urgent email from boss: "${email.subject}"\n${analysis.reason}`,
    {
      priority: analysis.priority
    }
  );

  // Action 2: Star if requires immediate action
  if (analysis.requiresImmediateAction) {
    await context.starEmail(email.messageId);
  }
}
