// Template: Package Tracking Watcher
// Use case: Alert when package tracking emails arrive with status updates

import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "package_tracking",
  name: "Package Tracking Watcher",
  description: "Alerts when package tracking emails arrive with status updates",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  const from = email.from.toLowerCase();
  const subject = email.subject.toLowerCase();

  // Filter 1: Check if from known package carriers
  const isPackageEmail =
    from.includes("amazon.com") ||
    from.includes("fedex.com") ||
    from.includes("ups.com") ||
    from.includes("usps.com") ||
    from.includes("dhl.com");

  // Filter 2: Check for tracking keywords
  const hasTrackingKeywords =
    subject.includes("shipped") ||
    subject.includes("delivered") ||
    subject.includes("out for delivery") ||
    subject.includes("tracking") ||
    subject.includes("delivery");

  if (!isPackageEmail || !hasTrackingKeywords) return;

  // Extract information
  const trackingNumber = extractTrackingNumber(email.body);
  const status = extractStatus(email.subject);
  const carrier = extractCarrier(email.from);

  // Choose emoji based on status
  const emoji = status === "delivered" ? "📦✅" : "📦🚚";

  // Build notification message
  let message = `${emoji} Package ${status}`;
  if (carrier) message += ` via ${carrier}`;
  message += `\nSubject: ${email.subject}`;
  if (trackingNumber) message += `\nTracking: ${trackingNumber}`;

  // Notify with appropriate priority
  await context.notify(message, {
    priority: status === "delivered" ? "high" : "normal"
  });

  // Add label for organization
  await context.addLabel(email.messageId, "packages");
}

// Helper function: Extract tracking number from email body
function extractTrackingNumber(body: string): string | null {
  // Common tracking number patterns
  const patterns = [
    /\b[0-9]{10,}\b/,           // Generic long number
    /\b1Z[A-Z0-9]{16}\b/,       // UPS
    /\b[0-9]{12,22}\b/,         // FedEx
    /\b[0-9]{20,30}\b/          // USPS
  ];

  for (const pattern of patterns) {
    const match = body.match(pattern);
    if (match) return match[0];
  }

  return null;
}

// Helper function: Extract delivery status from subject
function extractStatus(subject: string): string {
  const lower = subject.toLowerCase();

  if (lower.includes("delivered")) return "delivered";
  if (lower.includes("out for delivery")) return "out for delivery";
  if (lower.includes("shipped")) return "shipped";
  if (lower.includes("label created")) return "label created";
  if (lower.includes("in transit")) return "in transit";

  return "updated";
}

// Helper function: Extract carrier name from email address
function extractCarrier(from: string): string | null {
  const lower = from.toLowerCase();

  if (lower.includes("ups.com")) return "UPS";
  if (lower.includes("fedex.com")) return "FedEx";
  if (lower.includes("usps.com")) return "USPS";
  if (lower.includes("amazon.com")) return "Amazon";
  if (lower.includes("dhl.com")) return "DHL";

  return null;
}
