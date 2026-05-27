import type { ListenerConfig, Email, ListenerContext } from "../types";

export const config: ListenerConfig = {
  id: "todo-extractor",
  name: "Todo Item Extractor",
  description: "Extracts todo items from emails and adds them to dashboard",
  enabled: true,
  event: "email_received"
};

export async function handler(email: Email, context: ListenerContext): Promise<void> {
  // Skip emails that are likely automated or from mailing lists
  if (email.from.includes("noreply") ||
      email.from.includes("no-reply") ||
      email.from.includes("newsletter") ||
      email.labels?.includes("Promotions") ||
      email.labels?.includes("Social")) {
    return;
  }

  // Use AI to extract todo items from the email
  const analysis = await context.callAgent<{
    hasTodos: boolean;
    todos: Array<{
      task: string;
      priority: "high" | "normal" | "low";
      dueDate?: string;
      context?: string;
    }>;
    summary: string;
  }>({
    prompt: `Analyze this email and extract any todo items or action items:

Subject: ${email.subject}
From: ${email.from}
Date: ${email.date}
Body: ${email.body}

Extract any tasks, action items, or things the recipient needs to do. For each todo:
1. Identify the specific task/action
2. Determine priority based on language and context
3. Extract any mentioned due dates or deadlines
4. Add brief context about why this is a todo

Look for:
- Direct requests or assignments
- Commitments the recipient has made
- Follow-up actions mentioned
- Deadlines or time-sensitive items
- Items requiring response or review
- Meeting preparation tasks
- Documents to review or sign

Return hasTodos=false if there are no clear action items.`,

    schema: {
      type: "object",
      properties: {
        hasTodos: { type: "boolean" },
        todos: {
          type: "array",
          items: {
            type: "object",
            properties: {
              task: { type: "string" },
              priority: {
                type: "string",
                enum: ["high", "normal", "low"]
              },
              dueDate: { type: "string" },
              context: { type: "string" }
            },
            required: ["task", "priority"]
          }
        },
        summary: { type: "string" }
      },
      required: ["hasTodos", "todos", "summary"]
    },
    model: "sonnet" // Using sonnet for better extraction accuracy
  });

  // If no todos found, skip
  if (!analysis.hasTodos || analysis.todos.length === 0) {
    return;
  }

  // Format the todos for dashboard
  const todoList = analysis.todos.map((todo, index) => {
    let item = `${index + 1}. ${todo.task}`;
    if (todo.dueDate) {
      item += ` (Due: ${todo.dueDate})`;
    }
    if (todo.priority === "high") {
      item = `🔴 ${item}`;
    } else if (todo.priority === "normal") {
      item = `🟡 ${item}`;
    } else {
      item = `🟢 ${item}`;
    }
    if (todo.context) {
      item += `\n   Context: ${todo.context}`;
    }
    return item;
  }).join("\n\n");

  // Notify about extracted todos with dashboard formatting
  const notification = `📋 New todos extracted from email

**From:** ${email.from}
**Subject:** ${email.subject}
**Email:** [email:${email.messageId}]

**Extracted Todos:**
${todoList}

**Summary:** ${analysis.summary}

---
*These todos have been added to your dashboard*`;

  // Send notification with normal priority
  await context.notify(notification, {
    priority: "normal"
  });

  // Star the email if it contains high-priority todos
  const hasHighPriority = analysis.todos.some(todo => todo.priority === "high");
  if (hasHighPriority) {
    await context.starEmail(email.messageId);
  }

  // Add a "Has-Todos" label to help track emails with action items
  try {
    await context.addLabel(email.messageId, "Has-Todos");
  } catch (error) {
    // Label might not exist, that's okay
    console.log("Could not add label, it may not exist");
  }
}