import type { ListenerConfig, Email, ListenerContext, ListenerResult } from "../types";

export const config: ListenerConfig = {
  id: "finance-email-labeler",
  name: "AI Finance Email Auto-Labeler",
  description: "Uses AI to detect and label finance-related emails intelligently",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<ListenerResult> {
  // Use a subagent to intelligently classify finance emails
  const classification = await context.callAgent<{
    isFinanceRelated: boolean;
    confidence: number;
    category: string | null;
    reasoning: string;
  }>({
    prompt: `Analyze this email to determine if it's finance-related:

From: ${email.from}
Subject: ${email.subject}
Body preview: ${email.body.substring(0, 500)}

Determine if this email is related to:
- Banking (accounts, statements, alerts)
- Payments (bills, invoices, receipts)
- Investments (stocks, crypto, portfolio updates)
- Subscriptions (recurring charges, renewals)
- Expenses (reimbursements, business expenses)
- Tax-related matters
- Other financial transactions

Consider:
1. Sender domain and identity
2. Subject line content
3. Email body content
4. Transaction indicators
5. Financial terminology usage`,
    schema: {
      type: "object",
      properties: {
        isFinanceRelated: {
          type: "boolean",
          description: "Whether this email is finance-related"
        },
        confidence: {
          type: "number",
          description: "Confidence level 0-1 in the classification"
        },
        category: {
          type: "string",
          description: "Specific finance category (e.g., 'Banking', 'Invoice', 'Subscription') or null if not finance-related"
        },
        reasoning: {
          type: "string",
          description: "Brief explanation of why this email was classified this way"
        }
      },
      required: ["isFinanceRelated", "confidence", "category", "reasoning"]
    },
    model: "haiku" // Fast and efficient for classification tasks
  });

  // Only label if we're confident it's finance-related
  if (classification.isFinanceRelated && classification.confidence > 0.7) {
    try {
      const actions: string[] = [];

      // Add the main Finance label
      await context.addLabel(email.messageId, "Finance");
      actions.push("labeled:Finance");

      // Optionally add a more specific subcategory label if identified
      if (classification.category) {
        await context.addLabel(email.messageId, `Finance/${classification.category}`);
        actions.push(`labeled:Finance/${classification.category}`);
      }

      return {
        executed: true,
        reason: `Finance email detected (${Math.round(classification.confidence * 100)}% confidence): ${classification.reasoning}`,
        actions
      };
    } catch (error) {
      return {
        executed: false,
        reason: `Failed to add Finance label: ${error instanceof Error ? error.message : 'Unknown error'}`
      };
    }
  }

  // Not a finance email or confidence too low
  return {
    executed: false,
    reason: classification.isFinanceRelated
      ? `Finance-related but confidence too low (${Math.round(classification.confidence * 100)}%): ${classification.reasoning}`
      : `Not finance-related: ${classification.reasoning}`
  };
}