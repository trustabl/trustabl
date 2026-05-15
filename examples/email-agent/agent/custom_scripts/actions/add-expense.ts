// agent/custom_scripts/actions/add-expense.ts
import type { ActionTemplate, ActionContext, ActionResult } from '../types';
import type { FinancialDashboardState, Expense } from '../ui-states/financial-dashboard';

export const config: ActionTemplate = {
  id: 'add_expense',
  name: 'Add Expense',
  description: 'Add an expense to the financial dashboard',
  icon: '💰',
  parameterSchema: {
    type: 'object',
    properties: {
      amount: {
        type: 'number',
        description: 'Amount in dollars'
      },
      category: {
        type: 'string',
        description: 'Expense category',
        enum: ['Food', 'Transportation', 'Shopping', 'Entertainment', 'Utilities', 'Healthcare', 'Travel', 'Other']
      },
      description: {
        type: 'string',
        description: 'Description of the expense'
      },
      date: {
        type: 'string',
        description: 'Date (ISO format, defaults to today)'
      },
      emailId: {
        type: 'string',
        description: 'Source email ID (optional)'
      }
    },
    required: ['amount', 'category', 'description']
  }
};

export async function handler(
  params: {
    amount: number;
    category: string;
    description: string;
    date?: string;
    emailId?: string;
  },
  context: ActionContext
): Promise<ActionResult> {
  try {
    const stateId = 'financial_dashboard';

    // Get current state (or use initial state)
    let state = await context.uiState.get<FinancialDashboardState>(stateId);

    if (!state) {
      // Initialize with empty state
      state = {
        expenses: [],
        income: [],
        categories: {},
        monthlyTotals: {}
      };
    }

    const expenseDate = params.date || new Date().toISOString();

    // Create expense object
    const expense: Expense = {
      id: `exp_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      amount: params.amount,
      category: params.category,
      description: params.description,
      date: expenseDate,
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
    const month = expenseDate.substring(0, 7); // YYYY-MM
    if (!state.monthlyTotals[month]) {
      state.monthlyTotals[month] = { expenses: 0, income: 0, net: 0 };
    }
    state.monthlyTotals[month].expenses += params.amount;
    state.monthlyTotals[month].net =
      state.monthlyTotals[month].income - state.monthlyTotals[month].expenses;

    // Save updated state
    await context.uiState.set(stateId, state);

    context.log(`Added expense: $${params.amount} for ${params.category}`);

    return {
      success: true,
      message: `Added expense: $${params.amount} for ${params.category}`,
      components: [{
        instanceId: `comp_${Date.now()}`,
        componentId: 'financial_dashboard',
        stateId
      }]
    };
  } catch (error) {
    context.log(`Error adding expense: ${String(error)}`, 'error');
    return {
      success: false,
      message: `Failed to add expense: ${(error as Error).message}`
    };
  }
}
