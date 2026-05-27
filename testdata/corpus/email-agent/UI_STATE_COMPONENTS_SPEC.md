# UI State and Components Specification

## Overview

This specification defines a system for managing persistent UI state and rendering custom components in the email agent. The system separates **state management** (data) from **rendering** (presentation), allowing actions and listeners to update state while components visualize that state.

### Key Concepts

- **UI State**: Persistent, structured data that represents application state (e.g., expenses, packages, tasks)
- **Components**: React components that render UI state in the chat interface
- **Type Safety**: Full TypeScript support with generics for state typing
- **Real-time Updates**: Components automatically re-render when their state changes via WebSocket

### Design Goals

1. **Separation of Concerns**: Actions/listeners manipulate data; components handle presentation
2. **Persistence**: UI state survives across sessions
3. **Reusability**: Multiple components can render the same state differently
4. **Real-time**: State updates propagate to all connected clients immediately
5. **Type Safety**: Full TypeScript support with schemas
6. **Extensibility**: Users can create custom state types and components easily

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        User Interface                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │  Component   │  │  Component   │  │  Component   │      │
│  │  (Chart)     │  │  (Table)     │  │  (Timeline)  │      │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘      │
│         │                  │                  │              │
│         └──────────────────┴──────────────────┘              │
│                            │                                 │
└────────────────────────────┼─────────────────────────────────┘
                             │ Read
                    ┌────────▼────────┐
                    │   UI State      │
                    │   (Database)    │
                    └────────▲────────┘
                             │ Write
              ┌──────────────┼──────────────┐
              │              │              │
      ┌───────▼───────┐  ┌──▼─────────┐  ┌▼────────────┐
      │    Actions    │  │ Listeners  │  │  Components │
      │   (Update)    │  │  (Update)  │  │ (Lifecycle) │
      └───────────────┘  └────────────┘  └─────────────┘
```

---

## 1. UI State System

### 1.1 UI State Definition

UI State is structured data stored in the database with:
- **State ID**: Unique identifier for the state instance (e.g., "financial_dashboard", "task_board:session123")
- **Data**: Strongly-typed object conforming to the template's type
- **Persistence**: Stored in database, accessed via HTTP endpoints
- **Real-time sync**: Updates broadcast to all connected clients via WebSocket

State IDs can include scope suffixes (e.g., `:global`, `:session:{id}`) if needed, but this is determined at runtime by actions/listeners, not in the template definition.

### 1.2 File Structure

```
agent/custom_scripts/
├── ui-states/
│   ├── financial-dashboard.ts
│   ├── package-tracker.ts
│   ├── task-board.ts
│   └── [custom-state].ts
└── .logs/
    └── ui-states/
        └── {date}.jsonl
```

### 1.3 UI State Template Structure

Each UI state file exports a configuration:

```typescript
import { UIStateTemplate } from '../types';

// Define your state type
interface FinancialDashboardState {
  expenses: Array<{
    id: string;
    amount: number;
    category: string;
    date: string;
    description: string;
    source: string;
  }>;
  income: Array<{
    id: string;
    amount: number;
    source: string;
    date: string;
  }>;
  categories: Record<string, {
    total: number;
    count: number;
  }>;
  monthlyTotals: Record<string, {
    expenses: number;
    income: number;
    net: number;
  }>;
}

export const config: UIStateTemplate<FinancialDashboardState> = {
  id: "financial_dashboard",
  name: "Financial Dashboard",
  description: "Tracks income, expenses, and financial metrics",

  // Initial state when created
  initialState: {
    expenses: [],
    income: [],
    categories: {},
    monthlyTotals: {}
  }
};
```

### 1.4 UI State Operations

The UI State context provides simple operations:

```typescript
// Get state by ID (returns null if doesn't exist)
uiState.get<T>(stateId: string): Promise<T | null>

// Set/update entire state
uiState.set<T>(stateId: string, data: T): Promise<void>
```

**Note**: State is stored in the database and accessed via HTTP endpoints. Actions and listeners manipulate the full state object using get/set.

---

## 2. Components System

### 2.1 Component Definition

Components are UI rendering specifications that:
- Define what UI state they depend on
- Specify how to render that state
- Can include interactive elements that trigger actions
- Are React components on the frontend
- Are defined as TypeScript/React files

### 2.2 File Structure

```
agent/custom_scripts/
└── components/
    ├── spending-chart.tsx
    ├── package-timeline.tsx
    ├── task-kanban.tsx
    └── [custom-component].tsx

client/components/custom/
└── (dynamically loaded components)
```

### 2.3 Component Template Structure

Each component file exports configuration + React component:

```typescript
import { ComponentTemplate } from '../../types';
import React from 'react';

// Import the state type
import { FinancialDashboardState } from '../ui-states/financial-dashboard';

export const config: ComponentTemplate = {
  id: "spending_chart",
  name: "Spending Chart",
  description: "Visualizes expenses over time by category",
  stateId: "financial_dashboard" // Which UI state this component uses
};

// React component receives the entire state
export const Component: React.FC<ComponentProps<FinancialDashboardState>> = ({
  state,
  onAction
}) => {
  const { expenses, categories, monthlyTotals } = state;

  const handleCategoryClick = (category: string) => {
    // Trigger action with parameters
    onAction('filter_by_category', { category });
  };

  return (
    <div className="spending-chart">
      {/* Recharts or other visualization library */}
      <LineChart data={Object.entries(monthlyTotals).map(([month, data]) => ({
        month,
        ...data
      }))}>
        {/* ... */}
      </LineChart>

      <div className="categories">
        {Object.entries(categories).map(([name, data]) => (
          <button
            key={name}
            onClick={() => handleCategoryClick(name)}
          >
            {name}: ${data.total}
          </button>
        ))}
      </div>
    </div>
  );
};
```

### 2.4 Component Context

Components can access backend operations through lifecycle hooks (if needed):

```typescript
interface ComponentContext {
  // UI State operations
  uiState: {
    get<T>(stateId: string): Promise<T | null>;
    set<T>(stateId: string, data: T): Promise<void>;
  };

  // Trigger actions
  triggerAction(templateId: string, params: Record<string, unknown>): Promise<ActionResult>;

  // Notifications
  notify(message: string, type?: 'info' | 'success' | 'warning' | 'error'): void;

  // Logging
  log(message: string, data?: Record<string, unknown>): void;
}
```

### 2.5 Component Manager

The Component Manager handles:

```typescript
class ComponentManager {
  // Load all component templates
  loadAllComponents(): Promise<ComponentTemplate[]>;

  // Hot reload on file changes
  watchComponents(onChange: (components: ComponentTemplate[]) => void): void;

  // Register component instance in chat
  registerInstance(instance: ComponentInstance): void;

  // Get component data (state + config)
  getComponentData(instanceId: string): Promise<ComponentData>;

  // Handle component lifecycle events
  handleLifecycleEvent(event: LifecycleEvent): Promise<void>;

  // Prune old instances
  pruneOldInstances(): void;
}
```

---

## 3. Integration with Actions

### 3.1 Enhanced ActionContext

Add UI state operations to ActionContext:

```typescript
interface ActionContext {
  // Existing methods...
  emailAPI: EmailAPI;
  callAgent: (options: AgentOptions) => Promise<StructuredOutput>;
  notify: (message: string) => void;
  log: (message: string, data?: Record<string, unknown>) => void;
  sendEmail: (params: SendEmailParams) => Promise<void>;
  addUserMessage: (content: string) => void;
  addAssistantMessage: (content: string) => void;
  // ... etc

  // NEW: UI State operations
  uiState: {
    // Get state by ID (returns null if doesn't exist)
    get<T>(stateId: string): Promise<T | null>;

    // Set/update entire state
    set<T>(stateId: string, data: T): Promise<void>;
  };
}
```

### 3.2 Enhanced ActionResult

Actions can now return component instances to render:

```typescript
interface ActionResult {
  success: boolean;
  message: string;
  data?: Record<string, unknown>;

  // NEW: Components to render
  components?: ComponentInstance[];

  suggestedActions?: ActionInstance[];
  refreshInbox?: boolean;
}

interface ComponentInstance {
  instanceId: string; // Unique ID for this render
  componentId: string; // Component template ID
  stateId: string; // Which UI state instance to bind to
}
```

### 3.3 Example: Action That Updates UI State

```typescript
// agent/custom_scripts/actions/add-expense.ts
import { ActionTemplate, ActionContext, ActionResult } from '../types';
import { FinancialDashboardState } from '../ui-states/financial-dashboard';

export const config: ActionTemplate = {
  id: "add_expense",
  name: "Add Expense",
  description: "Add an expense to the financial dashboard",
  icon: "💰",
  parameterSchema: {
    type: "object",
    properties: {
      amount: { type: "number", description: "Amount in dollars" },
      category: { type: "string", description: "Expense category" },
      description: { type: "string", description: "Description" },
      date: { type: "string", description: "Date (ISO format)" },
      emailId: { type: "string", description: "Source email ID" }
    },
    required: ["amount", "category", "description", "date"]
  }
};

export async function handler(
  params: {
    amount: number;
    category: string;
    description: string;
    date: string;
    emailId?: string;
  },
  context: ActionContext
): Promise<ActionResult> {
  try {
    const stateId = 'financial_dashboard';

    // Get current state (or use initial state if doesn't exist)
    let state = await context.uiState.get<FinancialDashboardState>(stateId);
    if (!state) {
      state = {
        expenses: [],
        income: [],
        categories: {},
        monthlyTotals: {}
      };
    }

    // Create expense object
    const expense = {
      id: `exp_${Date.now()}`,
      amount: params.amount,
      category: params.category,
      description: params.description,
      date: params.date,
      source: params.emailId || 'manual'
    };

    // Add to expenses array
    state.expenses.push(expense);

    // Update category totals
    if (!state.categories[params.category]) {
      state.categories[params.category] = { total: 0, count: 0 };
    }
    state.categories[params.category].total += params.amount;
    state.categories[params.category].count += 1;

    // Update monthly totals
    const month = params.date.substring(0, 7); // YYYY-MM
    if (!state.monthlyTotals[month]) {
      state.monthlyTotals[month] = { expenses: 0, income: 0, net: 0 };
    }
    state.monthlyTotals[month].expenses += params.amount;
    state.monthlyTotals[month].net =
      state.monthlyTotals[month].income - state.monthlyTotals[month].expenses;

    // Save updated state
    await context.uiState.set(stateId, state);

    context.log('Added expense', { expense });

    return {
      success: true,
      message: `Added $${params.amount} expense in ${params.category}`,

      // Return component to show updated dashboard
      components: [{
        instanceId: `comp_${Date.now()}`,
        componentId: 'spending_chart',
        stateId
      }]
    };
  } catch (error) {
    context.log('Error adding expense', { error: String(error) });
    return {
      success: false,
      message: `Failed to add expense: ${(error as Error).message}`
    };
  }
}
```

---

## 4. Integration with Listeners

### 4.1 Enhanced ListenerContext

Add UI state operations to ListenerContext:

```typescript
interface ListenerContext {
  // Existing methods...
  archiveEmail: (emailId: string) => Promise<void>;
  starEmail: (emailId: string) => Promise<void>;
  unstarEmail: (emailId: string) => Promise<void>;
  markAsRead: (emailId: string) => Promise<void>;
  markAsUnread: (emailId: string) => Promise<void>;
  addLabel: (emailId: string, label: string) => Promise<void>;
  removeLabel: (emailId: string, label: string) => Promise<void>;
  callAgent: (options: AgentOptions) => Promise<StructuredOutput>;
  notify: (message: string) => void;
  // ... etc

  // NEW: UI State operations (same as ActionContext)
  uiState: {
    get<T>(stateId: string): Promise<T | null>;
    set<T>(stateId: string, data: T): Promise<void>;
  };
}
```

### 4.2 Enhanced ListenerResult

Listeners can optionally return components (no changes to existing structure):

```typescript
interface ListenerResult {
  executed: boolean;
  reason: string;
  actions?: string[]; // Email actions taken

  // NEW: Components to show (if listener wants to notify user visually)
  components?: ComponentInstance[];
}
```

### 4.3 Example: Listener That Updates UI State

```typescript
// agent/custom_scripts/listeners/finance-email-tracker.ts
import { ListenerConfig, ListenerContext, ListenerResult, Email } from '../types';
import { FinancialDashboardState } from '../ui-states/financial-dashboard';

export const config: ListenerConfig = {
  id: "finance_email_tracker",
  name: "Finance Email Tracker",
  description: "Automatically tracks expenses and income from emails",
  enabled: true,
  event: "email_received"
};

interface FinancialClassification {
  isFinancial: boolean;
  confidence: number;
  amount: number;
  category: string;
  type: 'expense' | 'income' | 'other';
  date: string;
  description: string;
}

export async function handler(
  email: Email,
  context: ListenerContext
): Promise<ListenerResult> {
  try {
    // Use AI to classify financial email
    const classification = await context.callAgent({
      prompt: `Analyze this email and extract financial information:
Subject: ${email.subject}
From: ${email.from}
Body: ${email.bodyText?.substring(0, 1000)}

Is this a financial email (receipt, invoice, payment confirmation)?
If yes, extract: amount, category, type (expense/income), date.`,
      schema: {
        type: "object",
        properties: {
          isFinancial: { type: "boolean" },
          confidence: { type: "number" },
          amount: { type: "number" },
          category: { type: "string" },
          type: { type: "string", enum: ["expense", "income", "other"] },
          date: { type: "string" },
          description: { type: "string" }
        },
        required: ["isFinancial", "confidence"]
      },
      model: "haiku"
    }) as FinancialClassification;

    if (!classification.isFinancial || classification.confidence < 0.7) {
      return {
        executed: false,
        reason: `Not a financial email (confidence: ${classification.confidence})`
      };
    }

    const stateId = 'financial_dashboard';

    // Get current state (or initialize if doesn't exist)
    let state = await context.uiState.get<FinancialDashboardState>(stateId);
    if (!state) {
      state = {
        expenses: [],
        income: [],
        categories: {},
        monthlyTotals: {}
      };
    }

    // Add expense or income
    const item = {
      id: `${classification.type}_${Date.now()}`,
      amount: classification.amount,
      category: classification.category,
      description: classification.description,
      date: classification.date,
      source: email.messageId
    };

    if (classification.type === 'expense') {
      state.expenses.push(item);

      // Update category totals
      if (!state.categories[classification.category]) {
        state.categories[classification.category] = { total: 0, count: 0 };
      }
      state.categories[classification.category].total += classification.amount;
      state.categories[classification.category].count += 1;
    } else if (classification.type === 'income') {
      state.income.push(item);
    }

    // Update monthly totals
    const month = classification.date.substring(0, 7);
    if (!state.monthlyTotals[month]) {
      state.monthlyTotals[month] = { expenses: 0, income: 0, net: 0 };
    }

    if (classification.type === 'expense') {
      state.monthlyTotals[month].expenses += classification.amount;
    } else if (classification.type === 'income') {
      state.monthlyTotals[month].income += classification.amount;
    }

    state.monthlyTotals[month].net =
      state.monthlyTotals[month].income - state.monthlyTotals[month].expenses;

    // Save updated state
    await context.uiState.set(stateId, state);

    // Label the email
    await context.addLabel(email.messageId, 'Finance');
    await context.addLabel(email.messageId, `Finance/${classification.category}`);

    return {
      executed: true,
      reason: `Tracked ${classification.type} of $${classification.amount} in ${classification.category}`,
      actions: [
        `labeled:Finance`,
        `labeled:Finance/${classification.category}`
      ]
    };
  } catch (error) {
    return {
      executed: false,
      reason: `Error: ${(error as Error).message}`
    };
  }
}
```

---

## 5. WebSocket Protocol Extensions

### 5.1 New Server → Client Messages

```typescript
// UI State instance created/updated (broadcast to all clients)
{
  type: 'ui_state_update',
  stateId: string,
  data: unknown // The full state object
}

// Component instance registered (render in chat)
{
  type: 'component_instance',
  instance: ComponentInstance,
  sessionId: string
}
```

### 5.2 New Client → Server Messages

```typescript
// Component triggered action
{
  type: 'component_action',
  instanceId: string,
  actionId: string,
  params: Record<string, unknown>,
  sessionId: string
}
```

### 5.3 HTTP Endpoints

UI state is accessed via HTTP endpoints:

```
GET  /api/ui-state/:stateId - Get UI state by ID
PUT  /api/ui-state/:stateId - Set/update UI state
GET  /api/ui-states - List all UI states
```

---

## 6. Complete Example Use Cases

### 6.1 Financial Dashboard

**UI State**: `financial-dashboard.ts`
```typescript
export const config: UIStateTemplate<FinancialDashboardState> = {
  id: "financial_dashboard",
  name: "Financial Dashboard",
  description: "Tracks income, expenses, and financial metrics",
  initialState: {
    expenses: [],
    income: [],
    categories: {},
    monthlyTotals: {}
  }
};
```

**Components**:
- `spending-chart.tsx` - Line/bar chart of monthly expenses
- `category-breakdown.tsx` - Pie chart of expenses by category
- `transaction-table.tsx` - Sortable table of all transactions

**Actions**:
- `add-expense.ts` - Manually add expense
- `categorize-expense.ts` - Re-categorize existing expense
- `export-financial-data.ts` - Export to CSV

**Listeners**:
- `finance-email-tracker.ts` - Auto-extract from receipts/invoices

### 6.2 Package Tracker

**UI State**: `package-tracker.ts`
```typescript
export const config: UIStateTemplate<PackageTrackerState> = {
  id: "package_tracker",
  name: "Package Tracker",
  description: "Tracks package deliveries",
  initialState: { packages: [] }
};
```

**Components**:
- `package-timeline.tsx` - Visual timeline of package journey
- `delivery-map.tsx` - Map showing package locations
- `package-list.tsx` - List view with filters

**Actions**:
- `mark-delivered.ts` - Mark package as delivered
- `report-package-issue.ts` - Report missing/damaged
- `track-package.ts` - Manually add tracking number

**Listeners**:
- `shipping-notification-listener.ts` - Detect shipping emails, add to tracker
- `delivery-update-listener.ts` - Update status from carrier emails

### 6.3 Task Board

**UI State**: `task-board.ts`
```typescript
export const config: UIStateTemplate<TaskBoardState> = {
  id: "task_board",
  name: "Task Board",
  description: "Manages tasks and their status",
  initialState: {
    tasks: [],
    columns: { todo: [], in_progress: [], done: [] }
  }
};
```

**Components**:
- `task-kanban.tsx` - Drag-and-drop Kanban board
- `task-priority-matrix.tsx` - Eisenhower matrix
- `task-timeline.tsx` - Gantt-style timeline

**Actions**:
- `create-task.ts` - Create new task
- `update-task-status.ts` - Move task between columns
- `set-task-priority.ts` - Change priority
- `complete-task.ts` - Mark as done

**Listeners**:
- `task-extractor-listener.ts` - Extract tasks from emails with action items

### 6.4 Contact Relationship Tracker

**UI State**: `contact-tracker.ts`
```typescript
export const config: UIStateTemplate<ContactTrackerState> = {
  id: "contact_tracker",
  name: "Contact Relationship Tracker",
  description: "Tracks relationships and interactions with contacts",
  initialState: { contacts: {} }
};
```

**Components**:
- `contact-timeline.tsx` - Interaction history timeline
- `contact-heatmap.tsx` - Frequency heatmap
- `contact-network.tsx` - Graph of relationships

**Actions**:
- `add-contact-note.ts` - Add note to contact
- `set-followup-reminder.ts` - Set reminder
- `update-contact-importance.ts` - Change importance level

**Listeners**:
- `contact-tracker-listener.ts` - Update on every email sent/received

### 6.5 Email Analytics Dashboard

**UI State**: `email-analytics.ts`
```typescript
export const config: UIStateTemplate<EmailAnalyticsState> = {
  id: "email_analytics",
  name: "Email Analytics",
  description: "Tracks email volume and response metrics",
  initialState: {
    volumeByDay: {},
    volumeByHour: {},
    volumeBySender: {},
    responseTimeByContact: {},
    avgResponseTime: 0,
    unreadCount: 0,
    totalEmails: 0
  }
};
```

**Components**:
- `email-volume-chart.tsx` - Volume trends
- `response-time-graph.tsx` - Response time analysis
- `sender-breakdown.tsx` - Top senders

**Actions**:
- `filter-analytics.ts` - Filter by date range
- `export-analytics.ts` - Export report

**Listeners**:
- `analytics-tracker-listener.ts` - Update on every email event

### 6.6 Subscription Manager

**UI State**: `subscription-manager.ts`
```typescript
export const config: UIStateTemplate<SubscriptionManagerState> = {
  id: "subscription_manager",
  name: "Subscription Manager",
  description: "Tracks recurring subscriptions and costs",
  initialState: {
    subscriptions: [],
    totalMonthly: 0,
    totalYearly: 0
  }
};
```

**Components**:
- `subscription-timeline.tsx` - Billing timeline
- `subscription-cost-breakdown.tsx` - Cost by category
- `subscription-list.tsx` - Sortable list

**Actions**:
- `unsubscribe.ts` - Mark as cancelled
- `update-subscription-cost.ts` - Update cost
- `add-subscription.ts` - Manually add

**Listeners**:
- `billing-email-listener.ts` - Detect billing emails

### 6.7 Event/Meeting Calendar

**UI State**: `meeting-calendar.ts`
```typescript
export const config: UIStateTemplate<MeetingCalendarState> = {
  id: "meeting_calendar",
  name: "Meeting Calendar",
  description: "Tracks events and meetings",
  initialState: { events: [] }
};
```

**Components**:
- `calendar-week-view.tsx` - Week calendar
- `upcoming-events-list.tsx` - List of upcoming events
- `event-timeline.tsx` - Timeline view

**Actions**:
- `accept-meeting.ts` - Accept invite
- `decline-meeting.ts` - Decline invite
- `propose-new-time.ts` - Suggest alternative

**Listeners**:
- `calendar-invite-listener.ts` - Parse calendar invites

### 6.8 Email Thread Visualizer

**UI State**: `thread-visualizer.ts`
```typescript
export const config: UIStateTemplate<ThreadVisualizerState> = {
  id: "thread_visualizer",
  name: "Email Thread Visualizer",
  description: "Visualizes email thread relationships",
  initialState: { threads: [] }
};
```

**Components**:
- `thread-graph.tsx` - D3 graph visualization
- `participant-network.tsx` - Network of participants

**Actions**:
- `archive-thread.ts` - Archive entire thread
- `mute-thread.ts` - Mute conversation

**Listeners**:
- `thread-builder-listener.ts` - Build thread relationships

---

## 7. Implementation Checklist

### Phase 1: Core Infrastructure
- [ ] Implement UIStateManager class with get/set operations
- [ ] Create UIStateTemplate type definitions
- [ ] Add file-based discovery for ui-states/
- [ ] Implement hot reload for UI state templates
- [ ] Add HTTP endpoints for UI state (GET/PUT)
- [ ] Add UI state operations to ActionContext
- [ ] Add UI state operations to ListenerContext

### Phase 2: Component System
- [ ] Implement ComponentManager class
- [ ] Create ComponentTemplate type definitions
- [ ] Add file-based discovery for components/
- [ ] Create ComponentContext for backend operations

### Phase 3: WebSocket Integration
- [ ] Add UI state templates broadcast on connect
- [ ] Add UI state update messages
- [ ] Add component templates broadcast
- [ ] Add component instance messages
- [ ] Add component lifecycle handlers
- [ ] Add component action handlers

### Phase 4: Frontend
- [ ] Create custom component loader
- [ ] Add UI state subscription system
- [ ] Implement component renderer in chat
- [ ] Add interactive component actions
- [ ] Create component panel system

### Phase 5: Examples
- [ ] Implement financial dashboard example
- [ ] Implement package tracker example
- [ ] Implement task board example
- [ ] Create documentation and tutorials

---

## 8. Type Definitions Summary

```typescript
// UI State
interface UIStateTemplate<T = unknown> {
  id: string;
  name: string;
  description?: string;
  initialState: T;
}

// Components
interface ComponentTemplate {
  id: string;
  name: string;
  description?: string;
  stateId: string; // Which UI state this component uses
}

interface ComponentInstance {
  instanceId: string;
  componentId: string;
  stateId: string;
}

interface ComponentProps<T = unknown> {
  state: T;
  onAction: (actionId: string, params: Record<string, unknown>) => void;
}

interface ComponentContext {
  uiState: {
    get<T>(stateId: string): Promise<T | null>;
    set<T>(stateId: string, data: T): Promise<void>;
  };
  triggerAction: (templateId: string, params: Record<string, unknown>) => Promise<ActionResult>;
  notify: (message: string, type?: 'info' | 'success' | 'warning' | 'error') => void;
  log: (message: string, data?: Record<string, unknown>) => void;
}

// Updates to existing types
interface ActionContext {
  // Existing properties...
  emailAPI: EmailAPI;
  callAgent: (options: AgentOptions) => Promise<StructuredOutput>;
  notify: (message: string) => void;
  log: (message: string, data?: Record<string, unknown>) => void;
  sendEmail: (params: SendEmailParams) => Promise<void>;
  addUserMessage: (content: string) => void;
  addAssistantMessage: (content: string) => void;

  // NEW
  uiState: {
    get<T>(stateId: string): Promise<T | null>;
    set<T>(stateId: string, data: T): Promise<void>;
  };
}

interface ListenerContext {
  // Existing properties...
  archiveEmail: (emailId: string) => Promise<void>;
  starEmail: (emailId: string) => Promise<void>;
  unstarEmail: (emailId: string) => Promise<void>;
  markAsRead: (emailId: string) => Promise<void>;
  markAsUnread: (emailId: string) => Promise<void>;
  addLabel: (emailId: string, label: string) => Promise<void>;
  removeLabel: (emailId: string, label: string) => Promise<void>;
  callAgent: (options: AgentOptions) => Promise<StructuredOutput>;
  notify: (message: string) => void;

  // NEW
  uiState: {
    get<T>(stateId: string): Promise<T | null>;
    set<T>(stateId: string, data: T): Promise<void>;
  };
}

interface ActionResult {
  success: boolean;
  message: string;
  data?: Record<string, unknown>;
  components?: ComponentInstance[]; // NEW
  suggestedActions?: ActionInstance[];
  refreshInbox?: boolean;
}

interface ListenerResult {
  executed: boolean;
  reason: string;
  actions?: string[];
  components?: ComponentInstance[]; // NEW
}
```

---

## 9. Benefits of This Architecture

1. **Separation of Concerns**: State management is independent of presentation
2. **Reusability**: Same state can power multiple different visualizations
3. **Real-time**: Changes propagate immediately to all connected clients
4. **Persistence**: State survives across sessions
5. **Composability**: Actions, listeners, and components work together seamlessly
6. **Extensibility**: Users can add new state types and components easily
7. **Type Safety**: Full TypeScript support throughout
8. **Auditability**: All state changes are logged
9. **Flexibility**: Components can be inline, panel, or modal
10. **Interactivity**: Components can trigger actions with parameters

---

## 10. Migration Path

For existing implementations:

1. **Backward Compatible**: Existing actions and listeners continue to work
2. **Opt-in**: UI state is optional - only used when explicitly accessed
3. **Gradual Adoption**: Can migrate one feature at a time
4. **No Breaking Changes**: All new features are additive

---

## Conclusion

This specification defines a comprehensive system for managing UI state and rendering custom components in the email agent. The architecture seamlessly integrates with existing actions and listeners, providing a powerful foundation for building rich, interactive, data-driven user experiences.
