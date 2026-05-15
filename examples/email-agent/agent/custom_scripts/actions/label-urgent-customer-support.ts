// agent/custom_scripts/actions/label-urgent-customer-support.ts
import type { ActionTemplate, ActionContext, ActionResult } from "../types";

export const config: ActionTemplate = {
  id: "label_urgent_customer_support",
  name: "Label Urgent Customer Support Emails",
  description: "Identify and label customer support emails that contain urgent keywords or are from VIP customers",
  icon: "🚨",
  parameterSchema: {
    type: "object",
    properties: {
      hoursBack: {
        type: "number",
        description: "How many hours back to check (default: 24)",
        default: 24
      }
    },
    required: []
  }
};

export async function handler(
  params: Record<string, any>,
  context: ActionContext
): Promise<ActionResult> {
  const { hoursBack = 24 } = params;

  context.log(`Checking for urgent customer support emails from the last ${hoursBack} hours`);

  try {
    // Search for support emails from the last N hours
    const query = `to:support@company.com newer_than:${Math.ceil(hoursBack / 24)}d`;
    const emails = await context.emailAPI.searchWithGmailQuery(query);

    context.log(`Found ${emails.length} support emails`);

    // VIP customer domains
    const vipDomains = ["@bigclient.com", "@enterprise.com", "@fortune500.com"];

    // Urgent keywords
    const urgentKeywords = [
      "urgent", "asap", "critical", "emergency", "down",
      "not working", "broken", "immediately", "production issue",
      "security breach", "data loss"
    ];

    const urgentEmails = emails.filter(email => {
      // Check if from VIP domain
      const isVIP = vipDomains.some(domain => email.from.includes(domain));

      // Check for urgent keywords in subject or body
      const text = `${email.subject} ${email.body}`.toLowerCase();
      const hasUrgentKeyword = urgentKeywords.some(keyword => text.includes(keyword));

      return isVIP || hasUrgentKeyword;
    });

    context.log(`Identified ${urgentEmails.length} urgent support emails`);

    // Label urgent emails
    for (const email of urgentEmails) {
      await context.addLabel(email.messageId, "URGENT");
      await context.starEmail(email.messageId);
    }

    // Create a summary message
    const vipCount = urgentEmails.filter(e =>
      vipDomains.some(domain => e.from.includes(domain))
    ).length;

    const summaryMsg = urgentEmails.length > 0
      ? `🚨 Labeled ${urgentEmails.length} urgent support emails (${vipCount} from VIP customers)`
      : "No urgent support emails found";

    context.notify(summaryMsg, {
      type: urgentEmails.length > 0 ? "warning" : "info",
      priority: urgentEmails.length > 0 ? "high" : "normal"
    });

    return {
      success: true,
      message: summaryMsg,
      data: {
        totalChecked: emails.length,
        urgentFound: urgentEmails.length,
        vipCount
      },
      refreshInbox: true
    };
  } catch (error: any) {
    context.log(`Failed to label urgent support emails: ${error}`, "error");
    return {
      success: false,
      message: `Failed to label urgent emails: ${error.message}`
    };
  }
}
