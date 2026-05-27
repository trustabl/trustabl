# Email Agent Subagent Selection Guide

## Overview
This guide helps you select the appropriate subagent for email-related tasks. Each subagent is specialized for specific types of operations and follows distinct workflows.

## Available Subagents

### 1. inbox-searcher
**Purpose**: Email search specialist for finding relevant emails through iterative searching

**Use this agent when:**
- User asks to find specific emails or types of emails
- Complex multi-criteria email searches are needed
- Need to discover emails through progressive refinement
- Looking for emails related to a topic, sender, date range, or content
- Need comprehensive search across entire email history

**Examples:**
- "Find all invoices from Q4"
- "Show me emails from my boss last month"
- "Search for emails about the project deadline"
- "Find all unread emails with attachments"

## Output Formatting Guidelines

When presenting email results:
1. Use markdown formatting for readability
2. Use `[email:MESSAGE_ID]` format for email references (clickable in the UI) - e.g., `[email:<abc123@example.com>]`
3. Use **bold** for important info (subjects, senders)
4. Limit output to reasonable amounts (20-50 emails)
5. Include relevant metadata (date, sender, attachments)

Example output format:
```markdown
Found 3 matching emails:

- **Quarterly Report Q4** from finance@company.com [email:<d0c43898-d632-3779-b1d1-a2ac8f72d428@company.com>]
  *Sent: 2024-01-15 10:30 AM* | 📎 Has attachments

- **Invoice #2024-001** from billing@vendor.com [email:<f8e91234-a876-4321-b098-c3d2e1f09876@vendor.com>]
  *Sent: 2024-01-14 3:45 PM* | 📎 Has attachments
```

## Best Practices

1. **Choose the right agent**: Each agent is optimized for specific tasks
2. **Provide context**: Give agents clear instructions about what you're looking for
4. **Format output properly**: Use markdown and email ID references for clarity