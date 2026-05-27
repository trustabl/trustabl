// client/components/custom/FinancialDashboard.tsx
import React, { useMemo } from 'react';
import type { ComponentProps } from './ComponentRegistry';

interface Expense {
  id: string;
  amount: number;
  category: string;
  date: string;
  description: string;
  source: string;
}

interface Income {
  id: string;
  amount: number;
  source: string;
  date: string;
}

interface CategorySummary {
  total: number;
  count: number;
}

interface MonthlyTotal {
  expenses: number;
  income: number;
  net: number;
}

interface FinancialDashboardState {
  expenses: Expense[];
  income: Income[];
  categories: Record<string, CategorySummary>;
  monthlyTotals: Record<string, MonthlyTotal>;
}

const categoryEmojis: Record<string, string> = {
  'Food': '🍔',
  'Transportation': '🚗',
  'Shopping': '🛍️',
  'Entertainment': '🎬',
  'Utilities': '⚡',
  'Healthcare': '🏥',
  'Travel': '✈️',
  'Other': '📦'
};

export const FinancialDashboard: React.FC<ComponentProps<FinancialDashboardState>> = ({ state, onAction }) => {
  const { expenses, income, categories, monthlyTotals } = state;

  // Calculate totals
  const totalExpenses = useMemo(() =>
    expenses.reduce((sum, exp) => sum + exp.amount, 0),
    [expenses]
  );

  const totalIncome = useMemo(() =>
    income.reduce((sum, inc) => sum + inc.amount, 0),
    [income]
  );

  const netBalance = totalIncome - totalExpenses;

  // Get sorted categories
  const sortedCategories = useMemo(() =>
    Object.entries(categories)
      .sort(([, a], [, b]) => b.total - a.total),
    [categories]
  );

  // Get recent expenses (last 5)
  const recentExpenses = useMemo(() =>
    [...expenses]
      .sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime())
      .slice(0, 5),
    [expenses]
  );

  // Calculate percentage for category bars
  const maxCategoryTotal = Math.max(...Object.values(categories).map(c => c.total), 1);

  return (
    <div className="bg-white rounded-lg border border-gray-300 p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-xl font-bold text-gray-900">💰 Financial Dashboard</h2>
        <button
          onClick={() => onAction('add_expense', {})}
          className="px-3 py-1.5 bg-blue-600 text-white text-sm rounded-lg hover:bg-blue-700 transition-colors"
        >
          + Add Expense
        </button>
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
        {/* Total Income */}
        <div className="bg-green-50 border border-green-200 rounded-lg p-4">
          <div className="text-sm text-green-600 font-medium mb-1">Total Income</div>
          <div className="text-2xl font-bold text-green-700">
            ${totalIncome.toFixed(2)}
          </div>
          <div className="text-xs text-green-600 mt-1">{income.length} transaction{income.length !== 1 ? 's' : ''}</div>
        </div>

        {/* Total Expenses */}
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <div className="text-sm text-red-600 font-medium mb-1">Total Expenses</div>
          <div className="text-2xl font-bold text-red-700">
            ${totalExpenses.toFixed(2)}
          </div>
          <div className="text-xs text-red-600 mt-1">{expenses.length} transaction{expenses.length !== 1 ? 's' : ''}</div>
        </div>

        {/* Net Balance */}
        <div className={`${netBalance >= 0 ? 'bg-blue-50 border-blue-200' : 'bg-orange-50 border-orange-200'} border rounded-lg p-4`}>
          <div className={`text-sm font-medium mb-1 ${netBalance >= 0 ? 'text-blue-600' : 'text-orange-600'}`}>
            Net Balance
          </div>
          <div className={`text-2xl font-bold ${netBalance >= 0 ? 'text-blue-700' : 'text-orange-700'}`}>
            ${Math.abs(netBalance).toFixed(2)}
          </div>
          <div className={`text-xs mt-1 ${netBalance >= 0 ? 'text-blue-600' : 'text-orange-600'}`}>
            {netBalance >= 0 ? '✓ Positive' : '⚠ Negative'}
          </div>
        </div>
      </div>

      {expenses.length === 0 && income.length === 0 ? (
        <div className="text-center py-12 text-gray-500">
          <div className="text-4xl mb-2">💵</div>
          <div className="text-sm">No financial data yet</div>
          <div className="text-xs mt-1">Add expenses or income to get started</div>
        </div>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Categories Breakdown */}
          <div>
            <h3 className="text-sm font-bold text-gray-900 mb-3">Spending by Category</h3>
            {sortedCategories.length === 0 ? (
              <div className="text-sm text-gray-500 text-center py-8">No categories yet</div>
            ) : (
              <div className="space-y-3">
                {sortedCategories.map(([category, summary]) => {
                  const percentage = (summary.total / maxCategoryTotal) * 100;

                  return (
                    <div key={category} className="bg-gray-50 rounded-lg p-3 border border-gray-200">
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          <span className="text-lg">{categoryEmojis[category] || '📦'}</span>
                          <span className="text-sm font-semibold text-gray-900">{category}</span>
                        </div>
                        <div className="text-right">
                          <div className="text-sm font-bold text-gray-900">${summary.total.toFixed(2)}</div>
                          <div className="text-xs text-gray-500">{summary.count} item{summary.count !== 1 ? 's' : ''}</div>
                        </div>
                      </div>
                      <div className="w-full bg-gray-200 rounded-full h-2">
                        <div
                          className="bg-blue-600 h-2 rounded-full transition-all duration-300"
                          style={{ width: `${percentage}%` }}
                        />
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Recent Expenses */}
          <div>
            <h3 className="text-sm font-bold text-gray-900 mb-3">Recent Expenses</h3>
            {recentExpenses.length === 0 ? (
              <div className="text-sm text-gray-500 text-center py-8">No expenses yet</div>
            ) : (
              <div className="space-y-2">
                {recentExpenses.map((expense) => (
                  <div key={expense.id} className="bg-gray-50 rounded-lg p-3 border border-gray-200 hover:border-gray-300 transition-colors">
                    <div className="flex items-start justify-between mb-1">
                      <div className="flex items-start gap-2 flex-1">
                        <span className="text-base">{categoryEmojis[expense.category] || '📦'}</span>
                        <div className="flex-1 min-w-0">
                          <div className="text-sm font-semibold text-gray-900 truncate">
                            {expense.description}
                          </div>
                          <div className="text-xs text-gray-500">
                            {expense.category}
                          </div>
                        </div>
                      </div>
                      <div className="text-sm font-bold text-red-600 ml-2">
                        ${expense.amount.toFixed(2)}
                      </div>
                    </div>
                    <div className="flex items-center gap-2 text-xs text-gray-500">
                      <span>📅 {new Date(expense.date).toLocaleDateString()}</span>
                      {expense.source !== 'manual' && (
                        <span className="text-blue-600">📧 From email</span>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Monthly Totals */}
      {Object.keys(monthlyTotals).length > 0 && (
        <div className="mt-6 pt-6 border-t border-gray-200">
          <h3 className="text-sm font-bold text-gray-900 mb-3">Monthly Summary</h3>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            {Object.entries(monthlyTotals)
              .sort(([a], [b]) => b.localeCompare(a))
              .slice(0, 6)
              .map(([month, totals]) => (
                <div key={month} className="bg-gray-50 rounded-lg p-3 border border-gray-200">
                  <div className="text-xs font-semibold text-gray-600 mb-2">
                    {new Date(month + '-01').toLocaleDateString('en-US', { month: 'short', year: 'numeric' })}
                  </div>
                  <div className="space-y-1">
                    <div className="flex justify-between text-xs">
                      <span className="text-gray-600">Income:</span>
                      <span className="font-semibold text-green-600">${totals.income.toFixed(0)}</span>
                    </div>
                    <div className="flex justify-between text-xs">
                      <span className="text-gray-600">Expenses:</span>
                      <span className="font-semibold text-red-600">${totals.expenses.toFixed(0)}</span>
                    </div>
                    <div className="flex justify-between text-xs pt-1 border-t border-gray-300">
                      <span className="text-gray-600">Net:</span>
                      <span className={`font-bold ${totals.net >= 0 ? 'text-blue-600' : 'text-orange-600'}`}>
                        ${totals.net.toFixed(0)}
                      </span>
                    </div>
                  </div>
                </div>
              ))}
          </div>
        </div>
      )}
    </div>
  );
};
