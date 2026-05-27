// client/hooks/useComponentTabs.tsx
import { useMemo } from 'react';
import { getRegisteredComponentIds, getComponent } from '../components/custom/ComponentRegistry';

interface ComponentTab {
  id: string;
  name: string;
  stateId: string;
}

/**
 * Hook to get available component tabs from ComponentRegistry
 * Returns a list of tabs that can be rendered in the UI
 */
export function useComponentTabs(): ComponentTab[] {
  const tabs = useMemo(() => {
    const componentIds = getRegisteredComponentIds();

    return componentIds.map(id => {
      // For now, derive name from ID
      // Format: 'task_board' -> 'Task Board'
      const name = id
        .split('_')
        .map(word => word.charAt(0).toUpperCase() + word.slice(1))
        .join(' ');

      return {
        id,
        name,
        stateId: id, // Assumes component ID matches state ID
      };
    });
  }, []);

  return tabs;
}
