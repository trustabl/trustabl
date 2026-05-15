# Listeners Specification

## Overview

Listeners are event-driven scripts that monitor for specific events (like incoming emails) and execute custom logic when conditions are met. Each listener is defined as a TypeScript file that exports a configuration and handler function.

## Purpose

- Proactively monitor for important emails (e.g., urgent messages from boss)
- Automatically respond to events (e.g., archive newsletters, send notifications)
- Create custom workflows triggered by email patterns
- Provide scheduled/time-based actions (e.g., daily summaries)
- Enable complex conditional logic for email handling

## Architecture

### Directory Structure

```
agent/custom_scripts/
├── listeners/
│   ├── boss-urgent-watcher.ts
│   ├── daily-summary.ts
│   ├── package-tracking.ts
│   └── auto-archive-newsletters.ts
└── types.ts
```

### File Format

Each listener file must export:
1. `config` - Listener configuration
2. `handler` - Async function that executes when triggered

```typescript
// agent/custom_scripts/listeners/example-listener.ts
import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "unique_listener_id",
  name: "Human Readable Name",
  description: "What this listener does",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Filter and custom logic here
  if (!email.from.includes("example@email.com")) return;

  // Call context methods to make things happen
  await context.notify("Notification message", {
    priority: "high"
  });
}
```

## Type Definitions

```typescript
// agent/custom_scripts/types.ts

export interface Email {
  messageId: string;
  from: string;
  to: string;
  subject: string;
  body: string;
  date: string;
  isRead: boolean;
  hasAttachments: boolean;
  labels?: string[];
}

export type EventType =
  | "email_received"        // New email arrives
  | "email_sent"            // User sends an email
  | "email_starred"         // Email is starred
  | "email_archived"        // Email is archived
  | "email_labeled"         // Label added to email
  | "scheduled_time";       // Time-based (cron)

export interface ListenerConfig {
  // Unique identifier for this listener
  id: string;

  // Human-readable name
  name: string;

  // Optional description of what this listener does
  description?: string;

  // Whether this listener is active
  enabled: boolean;

  // Type of event to listen for
  event: EventType;
}

export interface NotifyOptions {
  // Priority for notification display
  priority?: "low" | "normal" | "high";
}

export interface SubagentOptions<T = any> {
  // The prompt to send to the agent
  prompt: string;

  // JSON Schema for structured response
  schema: {
    type: "object";
    properties: Record<string, any>;
    required?: string[];
  };

  // Optional model to use (defaults to haiku for speed)
  model?: "opus" | "sonnet" | "haiku";
}

export interface ListenerContext {
  // Show a notification to the user
  notify(message: string, options?: NotifyOptions): Promise<void>;

  // Email actions
  archiveEmail(emailId: string): Promise<void>;
  starEmail(emailId: string): Promise<void>;
  unstarEmail(emailId: string): Promise<void>;
  markAsRead(emailId: string): Promise<void>;
  markAsUnread(emailId: string): Promise<void>;
  addLabel(emailId: string, label: string): Promise<void>;
  removeLabel(emailId: string, label: string): Promise<void>;

  // Call a subagent with a prompt and get structured response
  callAgent<T = any>(options: SubagentOptions<T>): Promise<T>;
}

export interface ListenerModule {
  config: ListenerConfig;
  handler: (data: any, context: ListenerContext) => Promise<void>;
}
```

## Event Types

### 1. `email_received`

Triggered when a new email arrives.

**Handler receives:** `Email` object and `ListenerContext`

**Example:**
```typescript
export const config: ListenerConfig = {
  id: "urgent_watcher",
  name: "Urgent Email Watcher",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Filter in handler
  const subject = email.subject.toLowerCase();
  if (!subject.includes("urgent") && !subject.includes("asap")) return;

  await context.notify(`Urgent email: ${email.subject}`, { priority: "high" });
}
```

### 2. `email_sent`

Triggered when user sends an email.

**Handler receives:** `Email` object (the sent email) and `ListenerContext`

**Example:**
```typescript
export const config: ListenerConfig = {
  id: "track_client_emails",
  name: "Track Client Communications",
  enabled: true,
  event: "email_sent"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Track all emails sent to clients
  if (!email.to.includes("client")) return;

  await context.notify(`Sent to client: ${email.subject}`);
  await context.addLabel(email.messageId, "client-sent");
}
```

### 3. `scheduled_time`

Triggered at specified times (cron-based). Configuration for cron schedule should be handled by the scheduler implementation.

**Handler receives:** `{ timestamp: Date, cron?: string }` and `ListenerContext`

**Example:**
```typescript
export const config: ListenerConfig = {
  id: "daily_summary",
  name: "Daily Morning Summary",
  enabled: true,
  event: "scheduled_time"
};

// Schedule this with cron: "0 9 * * *" in scheduler configuration
export async function handler(data: { timestamp: Date }, context: ListenerContext): Promise<void> {
  await context.notify(`Good morning! Daily summary for ${data.timestamp.toDateString()}`);
}
```

### 4. `email_starred`

Triggered when an email is starred.

**Handler receives:** `Email` object and `ListenerContext`

**Example:**
```typescript
export const config: ListenerConfig = {
  id: "starred_follow_up",
  name: "Follow Up on Starred Emails",
  enabled: true,
  event: "email_starred"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  const from = email.from.toLowerCase();
  if (!from.includes("boss") && !from.includes("client")) return;

  await context.notify(`Starred important email from ${email.from}`, { priority: "high" });
}
```

### 5. `email_archived`

Triggered when an email is archived.

**Handler receives:** `Email` object and `ListenerContext`

**Example:**
```typescript
export const config: ListenerConfig = {
  id: "track_archived",
  name: "Track Archived Emails",
  enabled: true,
  event: "email_archived"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Log important emails that get archived
  if (!email.from.includes("important-sender.com")) return;

  await context.notify(`Archived important email: ${email.subject}`, {
    priority: "low"
  });
}
```

### 6. `email_labeled`

Triggered when a label is added to an email.

**Handler receives:** `{ email: Email, label: string }` and `ListenerContext`

**Example:**
```typescript
export const config: ListenerConfig = {
  id: "workflow_router",
  name: "Workflow Automation Router",
  enabled: true,
  event: "email_labeled"
};

export async function handler(
  data: { email: Email, label: string },
  context: ListenerContext
): Promise<void> {
  // Trigger different workflows based on labels
  if (data.label === "action-required") {
    await context.starEmail(data.email.messageId);
    await context.notify(`Action needed: ${data.email.subject}`, {
      priority: "high"
    });
  }
}
```

## Complete Examples

### 1. Simple Notification Listener

```typescript
// agent/custom_scripts/listeners/boss-urgent-watcher.ts
import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "boss_urgent_watcher",
  name: "Boss Urgent Email Watcher",
  description: "Notifies immediately when boss sends urgent emails",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Filter in handler
  if (!email.from.includes("boss@company.com")) return;

  const subject = email.subject.toLowerCase();
  const isUrgent = subject.includes("urgent") ||
                   subject.includes("asap") ||
                   subject.includes("important") ||
                   subject.includes("critical");

  if (!isUrgent) return;

  // Notify
  await context.notify(`🚨 Urgent email from boss: "${email.subject}"`, {
    priority: "high"
  });

  // Also star it for later
  await context.starEmail(email.messageId);
}
```

### 2. Listener with Complex Logic

```typescript
// agent/custom_scripts/listeners/smart-urgent-detector.ts
import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "smart_urgent_detector",
  name: "Smart Urgency Detection",
  description: "Uses multiple signals to detect urgent emails",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  const urgencyScore = calculateUrgency(email);

  // Only notify if urgency is high enough
  if (urgencyScore < 6) return;

  const isHighPriority = urgencyScore >= 8;

  await context.notify(
    `${getUrgencyEmoji(urgencyScore)} ${urgencyScore}/10 urgency: ${email.subject}`,
    {
      priority: isHighPriority ? "high" : "normal"
    }
  );

  // Star high-priority emails
  if (isHighPriority) {
    await context.starEmail(email.messageId);
  }
}

function calculateUrgency(email: Email): number {
  let score = 0;

  const subject = email.subject.toLowerCase();
  const body = email.body.toLowerCase();

  // Check subject keywords
  if (subject.includes("urgent")) score += 3;
  if (subject.includes("asap")) score += 3;
  if (subject.includes("critical")) score += 4;
  if (subject.includes("important")) score += 2;
  if (subject.includes("deadline")) score += 2;

  // Check body keywords
  if (body.includes("urgent")) score += 1;
  if (body.includes("immediately")) score += 2;
  if (body.includes("by end of day")) score += 2;
  if (body.includes("deadline")) score += 1;

  // Check sender
  if (email.from.includes("boss@")) score += 2;
  if (email.from.includes("ceo@")) score += 3;

  // Check time sensitivity
  const now = new Date();
  const emailDate = new Date(email.date);
  const hoursAgo = (now.getTime() - emailDate.getTime()) / (1000 * 60 * 60);
  if (hoursAgo < 1) score += 1; // Recently sent

  return Math.min(score, 10);
}

function getUrgencyEmoji(score: number): string {
  if (score >= 9) return "🔴";
  if (score >= 7) return "🟠";
  if (score >= 5) return "🟡";
  return "🟢";
}
```

### 3. Scheduled Daily Summary

```typescript
// agent/custom_scripts/listeners/daily-summary.ts
import type { ListenerConfig, ListenerContext } from "../types";
import { EmailAPI } from "../../email-api";

export const config: ListenerConfig = {
  id: "daily_summary",
  name: "Daily Email Summary",
  description: "Shows a summary of unread emails every morning at 9am",
  enabled: true,
  event: "scheduled_time"
  // Schedule with cron: "0 9 * * *" in scheduler configuration
};

export async function handler(
  data: { timestamp: Date },
  context: ListenerContext
): Promise<void> {
  const api = new EmailAPI();

  // Fetch unread emails
  const unread = await api.searchEmails({
    gmailQuery: "is:unread",
    limit: 100
  });

  // Categorize emails
  const urgent = unread.filter(e =>
    e.subject.toLowerCase().includes("urgent") ||
    e.subject.toLowerCase().includes("asap") ||
    e.from.includes("boss@company.com")
  );

  const newsletters = unread.filter(e =>
    e.from.includes("newsletter") ||
    e.from.includes("noreply") ||
    e.subject.includes("unsubscribe")
  );

  const hasAttachments = unread.filter(e => e.hasAttachments);

  const other = unread.length - urgent.length - newsletters.length;

  // Build summary message
  const parts = [`☀️ Good morning! You have ${unread.length} unread emails`];

  if (urgent.length > 0) parts.push(`\n• ${urgent.length} urgent 🚨`);
  if (other > 0) parts.push(`\n• ${other} regular 📬`);
  if (hasAttachments.length > 0) parts.push(`\n• ${hasAttachments.length} with attachments 📎`);
  if (newsletters.length > 0) parts.push(`\n• ${newsletters.length} newsletters 📰`);

  await context.notify(parts.join(""), {
    priority: urgent.length > 0 ? "high" : "normal"
  });
}
```

### 4. Auto-Archive Listener

```typescript
// agent/custom_scripts/listeners/auto-archive-newsletters.ts
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

  // Filter for newsletters
  const isNewsletter =
    from.includes("newsletter") ||
    from.includes("noreply") ||
    from.includes("no-reply") ||
    subject.includes("unsubscribe");

  if (!isNewsletter) return;

  // Auto-archive and mark as read
  try {
    await context.archiveEmail(email.messageId);
    await context.markAsRead(email.messageId);

    // Optional notification (or remove for silent archiving)
    await context.notify(`📦 Auto-archived newsletter: "${email.subject}"`, {
      priority: "low"
    });
  } catch (error) {
    console.error("Failed to archive:", error);
  }
}
```

### 5. AI-Powered Email Classification

```typescript
// agent/custom_scripts/listeners/smart-classifier.ts
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

    model: "haiku" // Fast classification
  });

  // Act on classification
  if (classification.category === "spam") {
    await context.archiveEmail(email.messageId);
    await context.addLabel(email.messageId, "spam");
    return; // Don't notify for spam
  }

  if (classification.suggestedAction === "star") {
    await context.starEmail(email.messageId);
  }

  if (classification.suggestedLabels) {
    for (const label of classification.suggestedLabels) {
      await context.addLabel(email.messageId, label);
    }
  }

  // Notify if important
  if (classification.priority === "high") {
    await context.notify(
      `${classification.category.toUpperCase()}: ${email.subject}\n${classification.reasoning}`,
      {
        priority: "high"
      }
    );
  }
}
```

### 6. Meeting Request Extractor

```typescript
// agent/custom_scripts/listeners/meeting-extractor.ts
import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "meeting_extractor",
  name: "Meeting Request Extractor",
  description: "Extracts meeting details from emails and creates calendar events",
  enabled: true,
  event: "email_received"
};

interface MeetingDetails {
  isMeetingRequest: boolean;
  title?: string;
  date?: string;
  time?: string;
  duration?: string;
  location?: string;
  attendees?: string[];
  agenda?: string;
}

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Only process emails that might contain meeting requests
  const subject = email.subject.toLowerCase();
  const hasMeetingKeywords =
    subject.includes("meeting") ||
    subject.includes("schedule") ||
    subject.includes("call") ||
    subject.includes("sync");

  if (!hasMeetingKeywords) return;

  // Use AI to extract meeting details
  const details = await context.callAgent<MeetingDetails>({
    prompt: `Analyze this email and extract meeting/call details if present:

Subject: ${email.subject}
Body: ${email.body}

Extract:
- Is this a meeting request?
- Meeting title/topic
- Date and time
- Duration
- Location (physical or video link)
- Attendees mentioned
- Agenda or discussion topics`,

    schema: {
      type: "object",
      properties: {
        isMeetingRequest: { type: "boolean" },
        title: { type: "string" },
        date: { type: "string" },
        time: { type: "string" },
        duration: { type: "string" },
        location: { type: "string" },
        attendees: {
          type: "array",
          items: { type: "string" }
        },
        agenda: { type: "string" }
      },
      required: ["isMeetingRequest"]
    },

    model: "sonnet" // Better understanding for extraction
  });

  if (!details.isMeetingRequest) return;

  // Format meeting details for notification
  const meetingInfo = [
    `📅 Meeting: ${details.title || "Untitled"}`,
    details.date && `Date: ${details.date}`,
    details.time && `Time: ${details.time}`,
    details.location && `Location: ${details.location}`
  ]
    .filter(Boolean)
    .join("\n");

  await context.notify(meetingInfo, {
    priority: "high"
  });

  // Label for easy finding
  await context.addLabel(email.messageId, "meetings");
  await context.starEmail(email.messageId);
}
```

### 7. Package Tracking Listener

```typescript
// agent/custom_scripts/listeners/package-tracking.ts
import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "package_tracking",
  name: "Package Tracking Watcher",
  description: "Alerts when package tracking emails arrive",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Filter for package tracking emails
  const from = email.from.toLowerCase();
  const subject = email.subject.toLowerCase();

  const isPackageEmail =
    from.includes("amazon.com") ||
    from.includes("fedex.com") ||
    from.includes("ups.com") ||
    from.includes("usps.com");

  const hasTrackingKeywords =
    subject.includes("shipped") ||
    subject.includes("delivered") ||
    subject.includes("out for delivery") ||
    subject.includes("tracking");

  if (!isPackageEmail || !hasTrackingKeywords) return;

  const trackingNumber = extractTrackingNumber(email.body);
  const status = extractStatus(email.subject);
  const carrier = extractCarrier(email.from);

  const emoji = status === "delivered" ? "📦✅" : "📦";

  await context.notify(
    `${emoji} Package ${status}${carrier ? ` via ${carrier}` : ""}: ${email.subject}`,
    {
      priority: status === "delivered" ? "high" : "normal"
    }
  );
}

function extractTrackingNumber(body: string): string | null {
  // Look for common tracking number patterns
  const patterns = [
    /\b[0-9]{10,}\b/,           // Generic long number
    /\b1Z[A-Z0-9]{16}\b/,       // UPS
    /\b[0-9]{12,22}\b/          // FedEx
  ];

  for (const pattern of patterns) {
    const match = body.match(pattern);
    if (match) return match[0];
  }

  return null;
}

function extractStatus(subject: string): string {
  const lower = subject.toLowerCase();
  if (lower.includes("delivered")) return "delivered";
  if (lower.includes("out for delivery")) return "out for delivery";
  if (lower.includes("shipped")) return "shipped";
  if (lower.includes("label created")) return "label created";
  return "updated";
}

function extractCarrier(from: string): string | null {
  if (from.includes("ups.com")) return "UPS";
  if (from.includes("fedex.com")) return "FedEx";
  if (from.includes("usps.com")) return "USPS";
  if (from.includes("amazon.com")) return "Amazon";
  return null;
}
```

## ListenersManager Implementation

```typescript
// ccsdk/listeners-manager.ts
import { readdir, watch } from "fs/promises";
import { join } from "path";
import type {
  ListenerConfig,
  ListenerModule,
  ListenerContext,
  NotifyOptions,
  SubagentOptions,
  EventType
} from "../agent/custom_scripts/types";

export class ListenersManager {
  private listenersDir = join(process.cwd(), "agent/custom_scripts/listeners");
  private listeners: Map<string, ListenerModule> = new Map();
  private emailApi: any; // Inject your EmailAPI instance
  private notificationCallback?: (notification: any) => void;

  constructor(emailApi: any, notificationCallback?: (notification: any) => void) {
    this.emailApi = emailApi;
    this.notificationCallback = notificationCallback;
  }

  /**
   * Load all listener files from the listeners directory
   */
  async loadAllListeners(): Promise<ListenerConfig[]> {
    this.listeners.clear();

    try {
      const files = await readdir(this.listenersDir);

      for (const file of files) {
        if (file.endsWith(".ts") && !file.startsWith("_")) {
          await this.loadListener(file);
        }
      }
    } catch (error) {
      console.error("Error loading listeners:", error);
    }

    return Array.from(this.listeners.values()).map(l => l.config);
  }

  /**
   * Load a single listener file
   */
  private async loadListener(filename: string) {
    try {
      const filePath = join(this.listenersDir, filename);
      const module = await import(`${filePath}?t=${Date.now()}`);

      if (module.config && module.handler) {
        if (module.config.enabled) {
          this.listeners.set(module.config.id, {
            config: module.config,
            handler: module.handler
          });
          console.log(`Loaded listener: ${module.config.id}`);
        } else {
          console.log(`Skipped disabled listener: ${module.config.id}`);
        }
      }
    } catch (error) {
      console.error(`Error loading listener ${filename}:`, error);
    }
  }

  /**
   * Create a context object for a listener execution
   */
  private createContext(listenerConfig: ListenerConfig): ListenerContext {
    return {
      notify: async (message: string, options?: NotifyOptions) => {
        if (this.notificationCallback) {
          this.notificationCallback({
            type: "listener_notification",
            listenerId: listenerConfig.id,
            listenerName: listenerConfig.name,
            priority: options?.priority || "normal",
            message,
            timestamp: new Date().toISOString()
          });
        }
      },

      archiveEmail: async (emailId: string) => {
        await this.emailApi.archiveEmail(emailId);
      },

      starEmail: async (emailId: string) => {
        await this.emailApi.starEmail(emailId);
      },

      unstarEmail: async (emailId: string) => {
        await this.emailApi.unstarEmail(emailId);
      },

      markAsRead: async (emailId: string) => {
        await this.emailApi.markAsRead(emailId);
      },

      markAsUnread: async (emailId: string) => {
        await this.emailApi.markAsUnread(emailId);
      },

      addLabel: async (emailId: string, label: string) => {
        await this.emailApi.addLabel(emailId, label);
      },

      removeLabel: async (emailId: string, label: string) => {
        await this.emailApi.removeLabel(emailId, label);
      },

      callAgent: async <T = any>(options: SubagentOptions<T>): Promise<T> => {
        const Anthropic = require("@anthropic-ai/sdk");
        const anthropic = new Anthropic({
          apiKey: process.env.ANTHROPIC_API_KEY
        });

        const modelMap = {
          opus: "claude-opus-4-20250514",
          sonnet: "claude-sonnet-4-20250514",
          haiku: "claude-3-5-haiku-20241022"
        };

        const model = modelMap[options.model || "haiku"];

        const response = await anthropic.messages.create({
          model,
          max_tokens: 4096,
          messages: [
            {
              role: "user",
              content: options.prompt
            }
          ],
          tools: [
            {
              name: "respond",
              description: "Respond with structured data matching the schema",
              input_schema: options.schema
            }
          ],
          tool_choice: { type: "tool", name: "respond" }
        });

        // Extract structured response from tool use
        const toolUse = response.content.find((block) => block.type === "tool_use");
        if (!toolUse || toolUse.type !== "tool_use") {
          throw new Error("Agent did not return structured response");
        }

        return toolUse.input as T;
      }
    };
  }

  /**
   * Check if an event matches any listeners and execute handlers
   */
  async checkEvent(event: EventType, data: any): Promise<void> {
    for (const [id, listener] of this.listeners) {
      // Check if event type matches
      if (listener.config.event !== event) continue;

      try {
        console.log(`Executing listener: ${id}`);
        const context = this.createContext(listener.config);
        await listener.handler(data, context);
      } catch (error) {
        console.error(`Listener ${id} failed:`, error);
        // Continue with other listeners, don't fail entire sync
      }
    }
  }

  /**
   * Get all listener configs
   */
  getAllListeners(): ListenerConfig[] {
    return Array.from(this.listeners.values()).map(l => l.config);
  }

  /**
   * Get a specific listener by ID
   */
  getListener(id: string): ListenerModule | undefined {
    return this.listeners.get(id);
  }

  /**
   * Watch for file changes and reload listeners
   */
  async watchListeners(onChange: (listeners: ListenerConfig[]) => void) {
    try {
      const watcher = watch(this.listenersDir);

      for await (const event of watcher) {
        console.log(`Listener file ${event.eventType}: ${event.filename}`);

        if (event.filename?.endsWith(".ts")) {
          const listeners = await this.loadAllListeners();
          onChange(listeners);
        }
      }
    } catch (error) {
      console.error("Error watching listeners:", error);
    }
  }
}
```

## Integration with Email Sync

```typescript
// In email-api.ts or sync handler
import { ListenersManager } from "../ccsdk/listeners-manager";
import { EmailAPI } from "./email-api";

class EmailSyncService {
  private listenersManager: ListenersManager;
  private emailApi: EmailAPI;
  private clients: WebSocket[] = [];

  constructor() {
    this.emailApi = new EmailAPI();

    // Create listeners manager with notification callback
    this.listenersManager = new ListenersManager(
      this.emailApi,
      (notification) => this.broadcastNotification(notification)
    );
  }

  async initialize() {
    await this.listenersManager.loadAllListeners();
  }

  async syncEmails() {
    const newEmails = await this.fetchNewEmails();

    for (const email of newEmails) {
      // Store in database
      await this.storeEmail(email);

      // Check listeners - they will call context methods as needed
      await this.listenersManager.checkEvent("email_received", email);
    }
  }

  private broadcastNotification(notification: any) {
    // Send to all connected WebSocket clients
    for (const client of this.clients) {
      client.send(JSON.stringify(notification));
    }
  }
}
```

## WebSocket Integration

### Message Types

When listener is triggered:

```typescript
{
  type: "listener_notification",
  listenerId: string,
  listenerName: string,
  priority: "low" | "normal" | "high",
  message: string,
  email?: {
    id: string,
    from: string,
    subject: string
  },
  timestamp: string
}
```

Broadcast active listeners:

```typescript
{
  type: "listeners_update",
  listeners: ListenerConfig[]
}
```

Enable/disable listener:

```typescript
// Client → Server
{
  type: "listener_toggle",
  listenerId: string,
  enabled: boolean
}
```

## Frontend Integration

### Notification Component

```typescript
// client/components/ListenerNotification.tsx
interface ListenerNotificationProps {
  notification: {
    listenerId: string;
    listenerName: string;
    priority: "low" | "normal" | "high";
    message: string;
    email?: { id: string; from: string; subject: string };
    timestamp: string;
  };
}

export function ListenerNotification({ notification }: ListenerNotificationProps) {
  const priorityColors = {
    low: "bg-gray-100 border-gray-300",
    normal: "bg-blue-100 border-blue-300",
    high: "bg-red-100 border-red-300"
  };

  return (
    <div className={`p-4 border-l-4 rounded ${priorityColors[notification.priority]}`}>
      <div className="flex items-center justify-between">
        <div className="flex-1">
          <div className="text-xs text-gray-500">{notification.listenerName}</div>
          <div className="mt-1">{notification.message}</div>
        </div>
        <div className="text-xs text-gray-400">
          {new Date(notification.timestamp).toLocaleTimeString()}
        </div>
      </div>
    </div>
  );
}
```

### Listeners Panel

```typescript
// client/components/ListenersPanel.tsx
export function ListenersPanel({ listeners }: { listeners: ListenerConfig[] }) {
  const [enabledMap, setEnabledMap] = useState<Record<string, boolean>>({});

  const toggleListener = (id: string) => {
    const newEnabled = !enabledMap[id];
    setEnabledMap({ ...enabledMap, [id]: newEnabled });

    // Send to server
    ws.send(JSON.stringify({
      type: "listener_toggle",
      listenerId: id,
      enabled: newEnabled
    }));
  };

  return (
    <div className="listeners-panel">
      <h3>Active Listeners</h3>
      <div className="space-y-2">
        {listeners.map(listener => (
          <div key={listener.id} className="flex items-center justify-between p-2 border rounded">
            <div>
              <div className="font-medium">{listener.name}</div>
              {listener.description && (
                <div className="text-sm text-gray-500">{listener.description}</div>
              )}
            </div>
            <button
              onClick={() => toggleListener(listener.id)}
              className={`px-3 py-1 rounded ${
                listener.enabled ? "bg-green-500" : "bg-gray-300"
              }`}
            >
              {listener.enabled ? "Enabled" : "Disabled"}
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
```

## Agent Workflow

### How the Agent Creates Listeners

1. **User request**: "Let me know immediately when boss sends urgent emails"

2. **Agent writes listener file**:

```typescript
// Agent uses Write tool to create:
// agent/custom_scripts/listeners/boss-urgent.ts

import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "boss_urgent",
  name: "Boss Urgent Watcher",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Filter for boss emails with urgent keywords
  if (!email.from.includes("boss@company.com")) return;

  const subject = email.subject.toLowerCase();
  if (!subject.includes("urgent") && !subject.includes("asap")) return;

  // Notify
  await context.notify(`Urgent from boss: ${email.subject}`, {
    priority: "high"
  });

  // Star for later
  await context.starEmail(email.messageId);
}
```

3. **Backend detects new file** via watcher
4. **ListenersManager loads listener**
5. **Listener registered** for email_received events
6. **On next email sync** → triggers listener with event data and context
7. **Handler filters and calls context methods** (notify, starEmail, etc.)
8. **Frontend receives notification** → displays with actions

### Agent System Prompt Addition

```markdown
You have the ability to create event listeners that monitor for specific conditions.

To create a listener:
1. Use the Write tool to create a TypeScript file in `agent/custom_scripts/listeners/`
2. Export a `config` object and `handler` function
3. The listener will automatically activate

Example:
```typescript
import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "unique_id",
  name: "Listener Name",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Filter in handler
  if (!email.from.includes("example@email.com")) return;

  // Call context methods to make things happen
  await context.notify("Notification message", {
    priority: "high"
  });

  await context.starEmail(email.messageId);
  await context.markAsRead(email.messageId);
}
```

Available events:
- email_received: New email arrives
- email_sent: User sends an email
- email_starred: Email is starred
- email_archived: Email is archived
- email_labeled: Label added to email
- scheduled_time: Time-based (cron)

Available context methods:
- notify(message, options): Show notification to user
- archiveEmail(emailId): Archive an email
- starEmail(emailId): Star an email
- unstarEmail(emailId): Remove star from email
- markAsRead(emailId): Mark as read
- markAsUnread(emailId): Mark as unread
- addLabel(emailId, label): Add Gmail label
- removeLabel(emailId, label): Remove Gmail label
- callAgent<T>(options): Call Claude with a prompt and get structured response

The callAgent method allows listeners to use AI for complex analysis:
```typescript
const result = await context.callAgent<ResultType>({
  prompt: "Your prompt here with email content",
  schema: {
    type: "object",
    properties: {
      field: { type: "string" }
    },
    required: ["field"]
  },
  model: "haiku" // or "sonnet" or "opus"
});
```

Handlers should filter emails and perform actions as needed.
```

## Error Handling

### Handler Errors

```typescript
// In ListenersManager.checkEvent()
try {
  console.log(`Executing listener: ${id}`);
  const context = this.createContext(listener.config);
  await listener.handler(data, context);
} catch (error) {
  console.error(`Listener ${id} failed:`, error);
  // Continue with other listeners, don't fail entire sync
}
```

### Context Method Errors

Handlers should handle errors from context methods:

```typescript
export async function handler(email: Email, context: ListenerContext): Promise<void> {
  if (!email.from.includes("important@email.com")) return;

  try {
    await context.archiveEmail(email.messageId);
    await context.notify("Email archived successfully");
  } catch (error) {
    console.error("Failed to archive email:", error);
    await context.notify("Failed to archive email", { priority: "low" });
  }
}
```

### Invalid Listener Files

```typescript
try {
  const module = await import(filePath);
  if (!module.config || !module.handler) {
    console.error(`Invalid listener ${filename}: missing config or handler`);
    return;
  }
  // Load listener...
} catch (error) {
  console.error(`Failed to load ${filename}:`, error);
  // Continue loading other listeners
}
```

## Best Practices

1. **Unique IDs**: Use descriptive, unique listener IDs
2. **Clear Names**: Human-readable names for UI display
3. **Early Returns**: Filter early in handler to avoid unnecessary work
4. **Error Handling**: Wrap context method calls in try-catch
5. **Performance**: Keep handler logic fast (< 1 second)
6. **Specific Filters**: Use precise filtering logic to avoid false positives
7. **Notifications**: Only notify when truly important
8. **Actions**: Provide relevant quick actions with notifications
9. **Priority**: Set appropriate priority levels in notify options
10. **File Naming**: Use kebab-case matching listener purpose
11. **Side Effects**: Be mindful of actions - archiving, starring, etc. are permanent
12. **Testing**: Test handlers thoroughly, especially filtering logic

## Scheduled Listeners (Cron)

For scheduled_time events, you'll need a cron scheduler. Store cron expressions separately (e.g., in a config file or database).

```typescript
// ccsdk/cron-scheduler.ts
import cron from "node-cron";
import { ListenersManager } from "./listeners-manager";

interface ScheduleConfig {
  listenerId: string;
  cron: string;
}

export class CronScheduler {
  private jobs: Map<string, cron.ScheduledTask> = new Map();

  /**
   * Initialize scheduler with cron configurations
   * schedules: Array of { listenerId, cron } mappings
   */
  async initialize(
    listenersManager: ListenersManager,
    schedules: ScheduleConfig[]
  ) {
    for (const schedule of schedules) {
      const listener = listenersManager.getListener(schedule.listenerId);

      if (listener && listener.config.event === "scheduled_time") {
        this.scheduleListener(schedule, listenersManager);
      }
    }
  }

  private scheduleListener(schedule: ScheduleConfig, manager: ListenersManager) {
    const job = cron.schedule(schedule.cron, async () => {
      try {
        console.log(`Running scheduled listener: ${schedule.listenerId}`);
        await manager.checkEvent("scheduled_time", {
          timestamp: new Date(),
          cron: schedule.cron
        });
      } catch (error) {
        console.error(`Scheduled listener ${schedule.listenerId} failed:`, error);
      }
    });

    this.jobs.set(schedule.listenerId, job);
    console.log(`Scheduled listener ${schedule.listenerId} with cron: ${schedule.cron}`);
  }

  /**
   * Stop a scheduled job
   */
  stopJob(listenerId: string) {
    const job = this.jobs.get(listenerId);
    if (job) {
      job.stop();
      this.jobs.delete(listenerId);
      console.log(`Stopped scheduled listener: ${listenerId}`);
    }
  }

  /**
   * Stop all scheduled jobs
   */
  stopAll() {
    for (const [id, job] of this.jobs) {
      job.stop();
      console.log(`Stopped scheduled listener: ${id}`);
    }
    this.jobs.clear();
  }
}

// Usage example:
const scheduler = new CronScheduler();
await scheduler.initialize(listenersManager, [
  { listenerId: "daily_summary", cron: "0 9 * * *" },  // 9am daily
  { listenerId: "weekly_report", cron: "0 9 * * 1" }   // 9am Mondays
]);
```

## Future Enhancements

- Listener categories/grouping
- Listener execution history/logs
- Conditional chaining (listener triggers another listener)
- Rate limiting for high-frequency events
- Listener testing/simulation mode
- Listener analytics (trigger counts, execution times)
- User-level listener management UI
- Listener templates/marketplace
- Multi-condition filters (AND/OR logic)
- Custom event types
