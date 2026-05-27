// Template: AI Email Classifier
// Use case: Use Claude AI to intelligently classify and categorize emails

import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "smart_classifier",
  name: "AI Email Classifier",
  description: "Uses Claude to classify and categorize emails intelligently",
  enabled: true,
  event: "email_received"
};

interface ClassificationResult {
  category: "urgent" | "newsletter" | "personal" | "work" | "spam";
  priority: "high" | "normal" | "low";
  suggestedAction: "archive" | "star" | "label" | "none";
  reasoning: string;
  suggestedLabels?: string[];
}

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Use AI to classify the email
  const classification = await context.callAgent<ClassificationResult>({
    prompt: `Classify this email and suggest actions:

From: ${email.from}
Subject: ${email.subject}
Body: ${email.body.substring(0, 500)}

Analyze the email and provide:
1. Category (urgent/newsletter/personal/work/spam)
2. Priority level (high/normal/low)
3. Suggested action (archive/star/label/none)
4. Brief reasoning for your classification
5. Any suggested Gmail labels`,

    schema: {
      type: "object",
      properties: {
        category: {
          type: "string",
          enum: ["urgent", "newsletter", "personal", "work", "spam"]
        },
        priority: {
          type: "string",
          enum: ["high", "normal", "low"]
        },
        suggestedAction: {
          type: "string",
          enum: ["archive", "star", "label", "none"]
        },
        reasoning: { type: "string" },
        suggestedLabels: {
          type: "array",
          items: { type: "string" }
        }
      },
      required: ["category", "priority", "suggestedAction", "reasoning"]
    },

    model: "haiku" // Fast and cost-effective for classification
  });

  // Act on classification results

  // Handle spam - archive and don't notify
  if (classification.category === "spam") {
    await context.archiveEmail(email.messageId);
    await context.addLabel(email.messageId, "spam");
    return; // Don't notify for spam
  }

  // Star if suggested
  if (classification.suggestedAction === "star") {
    await context.starEmail(email.messageId);
  }

  // Archive if suggested
  if (classification.suggestedAction === "archive") {
    await context.archiveEmail(email.messageId);
  }

  // Add suggested labels
  if (classification.suggestedLabels) {
    for (const label of classification.suggestedLabels) {
      await context.addLabel(email.messageId, label);
    }
  }

  // Notify if high priority
  if (classification.priority === "high") {
    await context.notify(
      `${classification.category.toUpperCase()}: ${email.subject}\n${classification.reasoning}`,
      {
        priority: "high"
      }
    );
  }
}
