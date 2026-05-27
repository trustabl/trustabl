// client/components/custom/ComponentRegistry.ts
import type { ComponentType } from 'react';

/**
 * Component Registry
 * Maps component IDs to React components
 * Components must implement the ComponentProps interface
 */

export interface ComponentProps<T = any> {
  state: T;
  onAction: (actionId: string, params: Record<string, any>) => void;
}

// Import components
import { TaskBoard } from './TaskBoard';
import { FinancialDashboard } from './FinancialDashboard';

/**
 * Registry of all custom components
 * Add your components here to make them available
 */
const componentRegistry: Record<string, ComponentType<ComponentProps<any>>> = {
  'task_board': TaskBoard,
  'financial_dashboard': FinancialDashboard,
};

/**
 * Register a component
 */
export function registerComponent(id: string, component: ComponentType<ComponentProps<any>>) {
  componentRegistry[id] = component;
}

/**
 * Get a component by ID
 */
export function getComponent(id: string): ComponentType<ComponentProps<any>> | null {
  return componentRegistry[id] || null;
}

/**
 * Get all registered component IDs
 */
export function getRegisteredComponentIds(): string[] {
  return Object.keys(componentRegistry);
}

/**
 * Check if a component is registered
 */
export function isComponentRegistered(id: string): boolean {
  return id in componentRegistry;
}
