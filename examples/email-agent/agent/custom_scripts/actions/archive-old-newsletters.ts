// agent/custom_scripts/actions/archive-old-newsletters.ts
import type { ActionTemplate, ActionContext, ActionResult } from "../types";

export const config: ActionTemplate = {
  id: "archive_old_newsletters",
  name: "Archive Old Newsletters",
  description: "Archive newsletter emails older than 30 days from TechCrunch, Morning Brew, and Hacker News",
  icon: "📰",
  parameterSchema: {
    type: "object",
    properties: {
      daysOld: {
        type: "number",
        description: "Archive newsletters older than this many days",
        default: 30
      }
    },
    required: []
  }
};

export async function handler(
  params: Record<string, any>,
  context: ActionContext
): Promise<ActionResult> {
  const { daysOld = 30 } = params;

  // Calculate date threshold
  const cutoffDate = new Date();
  cutoffDate.setDate(cutoffDate.getDate() - daysOld);
  const dateStr = cutoffDate.toISOString().split('T')[0].replace(/-/g, '/');

  // Gmail query for newsletters from specific senders older than threshold
  const query = `(from:newsletter@techcrunch.com OR from:crew@morningbrew.com OR from:noreply@hackernewsletter.com) before:${dateStr}`;

  context.log(`Archiving newsletters older than ${daysOld} days`);

  try {
    const emails = await context.emailAPI.searchWithGmailQuery(query);

    context.log(`Found ${emails.length} old newsletters to archive`);

    let archived = 0;
    for (const email of emails) {
      await context.archiveEmail(email.messageId);
      archived++;
    }

    context.notify(`Archived ${archived} old newsletters`, {
      type: "success",
      priority: "normal"
    });

    return {
      success: true,
      message: `Archived ${archived} newsletters older than ${daysOld} days`,
      data: {
        archivedCount: archived,
        daysOld,
        sources: ["TechCrunch", "Morning Brew", "Hacker News"]
      },
      refreshInbox: true
    };
  } catch (error: any) {
    context.log(`Failed to archive newsletters: ${error}`, "error");
    return {
      success: false,
      message: `Failed to archive newsletters: ${error.message}`
    };
  }
}
