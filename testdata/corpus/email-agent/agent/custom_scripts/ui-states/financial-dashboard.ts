// agent/custom_scripts/ui-states/financial-dashboard.ts
import type { UIStateTemplate } from '../types';

/**
 * Financial Dashboard State
 * Tracks income, expenses, and financial metrics
 */

export interface Expense {
  id: string;
  amount: number;
  category: string;
  date: string;
  description: string;
  source: string; // Email ID or 'manual'
}

export interface Income {
  id: string;
  amount: number;
  source: string;
  date: string;
}

export interface CategorySummary {
  total: number;
  count: number;
}

export interface MonthlyTotal {
  expenses: number;
  income: number;
  net: number;
}

export interface FinancialDashboardState {
  expenses: Expense[];
  income: Income[];
  categories: Record<string, CategorySummary>;
  monthlyTotals: Record<string, MonthlyTotal>;
}

export const config: UIStateTemplate<FinancialDashboardState> = {
  id: 'financial_dashboard',
  name: 'Financial Dashboard',
  description: 'Tracks income, expenses, and financial metrics over time',
  initialState: {
    expenses: [],
    income: [],
    categories: {},
    monthlyTotals: {}
  }
};
