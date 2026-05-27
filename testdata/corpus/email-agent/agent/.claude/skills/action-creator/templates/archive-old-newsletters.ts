// agent/custom_scripts/actions/archive-newsletters-from-sources.ts
// Template for archiving newsletters from specific sources older than N days

import type { ActionTemplate, ActionContext, ActionResult } from "../types";

export const config: ActionTemplate = {
  id: "archive_newsletters_from_sources",
  name: "Archive Old Newsletters from Sources",
  description: "Archive newsletter emails older than specified days from specific sender addresses",
  icon: "📰",
  parameterSchema: {
    type: "object",
    properties: {
      sources: {
        type: "string",
        description: "Comma-separated list of sender email addresses (e.g., 'newsletter@techcrunch.com, crew@morningbrew.com')"
      },
      daysOld: {
        type: "number",
        description: "Archive newsletters older than this many days",
        default: 30
      }
    },
    required: ["sources"]
  }
};

export async function handler(
  params: Record<string, any>,
  context: ActionContext
): Promise<ActionResult> {
  const { sources, daysOld = 30 } = params;

  // Parse sources into array
  const sourceEmails = sources.split(',').map((s: string) => s.trim());

  context.log(`Archiving newsletters from ${sourceEmails.length} sources older than ${daysOld} days`);

  // Calculate date threshold
  const cutoffDate = new Date();
  cutoffDate.setDate(cutoffDate.getDate() - daysOld);
  const dateStr = cutoffDate.toISOString().split('T')[0].replace(/-/g, '/');

  // Build Gmail query for multiple sources
  const sourceQueries = sourceEmails.map(email => `from:${email}`).join(' OR ');
  const query = `(${sourceQueries}) before:${dateStr}`;

  try {
    const emails = await context.emailAPI.searchWithGmailQuery(query);

    context.log(`Found ${emails.length} old newsletters to archive`);

    if (emails.length === 0) {
      return {
        success: true,
        message: `No newsletters found older than ${daysOld} days from specified sources`,
        data: {
          archivedCount: 0,
          daysOld,
          sources: sourceEmails
        }
      };
    }

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
        sources: sourceEmails
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
