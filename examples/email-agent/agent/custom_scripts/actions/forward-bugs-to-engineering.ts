// agent/custom_scripts/actions/forward-bugs-to-engineering.ts
import type { ActionTemplate, ActionContext, ActionResult } from "../types";

export const config: ActionTemplate = {
  id: "forward_bugs_to_engineering",
  name: "Forward Bug Reports to Engineering",
  description: "Forward customer bug reports to the engineering team with priority assessment and reproduction steps",
  icon: "🐛",
  parameterSchema: {
    type: "object",
    properties: {
      emailId: {
        type: "string",
        description: "Bug report email to forward"
      },
      priority: {
        type: "string",
        description: "Bug priority level",
        enum: ["P0 - Critical", "P1 - High", "P2 - Medium", "P3 - Low"]
      },
      affectedFeature: {
        type: "string",
        description: "Which feature/component is affected"
      },
      reproducible: {
        type: "boolean",
        description: "Whether the bug is reproducible"
      }
    },
    required: ["emailId", "priority", "affectedFeature"]
  }
};

export async function handler(
  params: Record<string, any>,
  context: ActionContext
): Promise<ActionResult> {
  const { emailId, priority, affectedFeature, reproducible = false } = params;

  const engineeringTeam = "engineering@company.com";
  const engineeringSlack = "#eng-bugs";

  context.log(`Forwarding bug report to engineering team`);

  try {
    const email = await context.emailAPI.getEmailById(emailId);

    if (!email) {
      return {
        success: false,
        message: "Bug report email not found"
      };
    }

    // Extract key info using AI
    const analysisPrompt = `Analyze this bug report and extract:
1. Steps to reproduce (if mentioned)
2. Expected behavior
3. Actual behavior
4. User environment (browser, OS, version, etc.)
5. Error messages or screenshots mentioned

Bug Report:
From: ${email.from}
Subject: ${email.subject}

${email.body}

Provide the analysis in a structured format.`;

    const analysis = await context.callAgent<string>({
      prompt: analysisPrompt,
      maxTokens: 1000
    });

    const forwardBody = `🐛 **Bug Report Forwarded from Customer Support**

**Priority:** ${priority}
**Affected Feature:** ${affectedFeature}
**Reproducible:** ${reproducible ? "Yes" : "Unknown"}
**Reporter:** ${email.from}
**Reported:** ${new Date(email.date).toLocaleString()}

---

**AI Analysis:**

${analysis}

---

**Original Email:**

Subject: ${email.subject}

${email.body}

---

Please investigate and update the ticket in Linear. Tag in ${engineeringSlack} when you have an update.`;

    await context.sendEmail({
      to: engineeringTeam,
      subject: `[${priority}] Bug Report: ${affectedFeature} - ${email.subject}`,
      body: forwardBody
    });

    // Add label to original email
    await context.addLabel(emailId, "FORWARDED_TO_ENG");

    context.notify(`Bug report forwarded to engineering team`, {
      type: "success",
      priority: priority.startsWith("P0") || priority.startsWith("P1") ? "high" : "normal"
    });

    return {
      success: true,
      message: `Bug report forwarded to engineering team with ${priority} priority`,
      data: {
        priority,
        affectedFeature,
        originalSender: email.from
      }
    };
  } catch (error: any) {
    context.log(`Failed to forward bug report: ${error}`, "error");
    return {
      success: false,
      message: `Failed to forward bug report: ${error.message}`
    };
  }
}
