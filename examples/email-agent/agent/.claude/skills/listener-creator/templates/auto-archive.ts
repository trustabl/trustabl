// Template: Auto-Archive Newsletters
// Use case: Automatically archive newsletters and promotional emails

import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "auto_archive_newsletters",
  name: "Auto-Archive Newsletters",
  description: "Automatically archives newsletters and promotional emails",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  const from = email.from.toLowerCase();
  const subject = email.subject.toLowerCase();

  // Filter: Check if this is a newsletter or promotional email
  const isNewsletter =
    from.includes("newsletter") ||
    from.includes("noreply") ||
    from.includes("no-reply") ||
    from.includes("notifications") ||
    subject.includes("unsubscribe") ||
    subject.includes("newsletter");

  if (!isNewsletter) return;

  // Perform archiving actions
  try {
    // Action 1: Archive the email
    await context.archiveEmail(email.messageId);

    // Action 2: Mark as read (optional - removes from unread count)
    await context.markAsRead(email.messageId);

    // Action 3: Optional low-priority notification
    // Comment out if you want silent archiving
    await context.notify(
      `Auto-archived newsletter: "${email.subject}"`,
      {
        priority: "low"
      }
    );
  } catch (error) {
    // Handle errors gracefully
    console.error("Failed to archive email:", error);
  }
}
