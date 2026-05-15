// agent/custom_scripts/actions/forward-bug-report-to-engineering.ts
// Template for forwarding bug reports to engineering team with AI analysis

import type { ActionTemplate, ActionContext, ActionResult } from "../types";

export const config: ActionTemplate = {
  id: "forward_bug_to_engineering",
  name: "Forward Bug Report to Engineering",
  description: "Forward customer bug reports to engineering team with AI-powered priority assessment and reproduction steps",
  icon: "🐛",
  parameterSchema: {
    type: "object",
    properties: {
      emailId: {
        type: "string",
        description: "Bug report email ID to forward"
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
        description: "Whether the bug is reproducible",
        default: false
      },
      engineeringEmail: {
        type: "string",
        description: "Engineering team email address",
        default: "engineering@company.com"
      }
    },
    required: ["emailId", "priority", "affectedFeature"]
  }
};

export async function handler(
  params: Record<string, any>,
  context: ActionContext
): Promise<ActionResult> {
  const {
    emailId,
    priority,
    affectedFeature,
    reproducible = false,
    engineeringEmail = "engineering@company.com"
  } = params;

  context.log(`Forwarding bug report to engineering team`);

  try {
    const email = await context.emailAPI.getEmailById(emailId);

    if (!email) {
      return {
        success: false,
        message: "Bug report email not found"
      };
    }

    // Use AI to analyze the bug report
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

Provide the analysis in a structured, readable format.`;

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

Please investigate and update the ticket in your issue tracker.`;

    await context.sendEmail({
      to: engineeringEmail,
      subject: `[${priority}] Bug Report: ${affectedFeature} - ${email.subject}`,
      body: forwardBody
    });

    // Label the original email
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
