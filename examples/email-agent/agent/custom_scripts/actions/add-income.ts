// agent/custom_scripts/actions/add-income.ts
import type { ActionTemplate, ActionContext, ActionResult } from '../types';
import type { FinancialDashboardState, Income } from '../ui-states/financial-dashboard';

export const config: ActionTemplate = {
  id: 'add_income',
  name: 'Add Income',
  description: 'Add income to the financial dashboard',
  icon: '💵',
  parameterSchema: {
    type: 'object',
    properties: {
      amount: {
        type: 'number',
        description: 'Amount in dollars'
      },
      source: {
        type: 'string',
        description: 'Source of income (e.g., Salary, Freelance, Investment)'
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
    required: ['amount', 'source']
  }
};

export async function handler(
  params: {
    amount: number;
    source: string;
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

    const incomeDate = params.date || new Date().toISOString();

    // Create income object
    const income: Income = {
      id: `inc_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      amount: params.amount,
      source: params.source,
      date: incomeDate
    };

    // Add to income array
    state.income.push(income);

    // Update monthly totals
    const month = incomeDate.substring(0, 7); // YYYY-MM
    if (!state.monthlyTotals[month]) {
      state.monthlyTotals[month] = { expenses: 0, income: 0, net: 0 };
    }
    state.monthlyTotals[month].income += params.amount;
    state.monthlyTotals[month].net =
      state.monthlyTotals[month].income - state.monthlyTotals[month].expenses;

    // Save updated state
    await context.uiState.set(stateId, state);

    context.log(`Added income: $${params.amount} from ${params.source}`);

    return {
      success: true,
      message: `Added income: $${params.amount} from ${params.source}`,
      components: [{
        instanceId: `comp_${Date.now()}`,
        componentId: 'financial_dashboard',
        stateId
      }]
    };
  } catch (error) {
    context.log(`Error adding income: ${String(error)}`, 'error');
    return {
      success: false,
      message: `Failed to add income: ${(error as Error).message}`
    };
  }
}
