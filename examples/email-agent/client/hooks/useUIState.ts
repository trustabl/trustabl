// client/hooks/useUIState.ts
import { useState, useEffect, useCallback, useRef } from 'react';

interface UIStateCache {
  [stateId: string]: any;
}

/**
 * Hook to subscribe to UI state updates via WebSocket
 * Maintains a local cache and automatically updates when state changes
 */
export function useUIState(ws: WebSocket | null) {
  const [uiStates, setUIStates] = useState<UIStateCache>({});
  const [templates, setTemplates] = useState<any[]>([]);
  const subscribersRef = useRef<Set<(stateId: string, data: any) => void>>(new Set());

  useEffect(() => {
    if (!ws) return;

    const handleMessage = (event: MessageEvent) => {
      try {
        const message = JSON.parse(event.data);

        // Handle UI state update broadcasts
        if (message.type === 'ui_state_update') {
          const { stateId, data } = message;

          setUIStates(prev => ({
            ...prev,
            [stateId]: data
          }));

          // Notify all subscribers
          subscribersRef.current.forEach(callback => {
            callback(stateId, data);
          });
        }

        // Handle initial UI state templates
        if (message.type === 'ui_state_templates') {
          setTemplates(message.templates || []);
        }
      } catch (error) {
        console.error('Error handling UI state message:', error);
      }
    };

    ws.addEventListener('message', handleMessage);

    return () => {
      ws.removeEventListener('message', handleMessage);
    };
  }, [ws]);

  /**
   * Get UI state by ID
   */
  const getState = useCallback(<T = any>(stateId: string): T | null => {
    return uiStates[stateId] || null;
  }, [uiStates]);

  /**
   * Update UI state (sends to server via HTTP)
   */
  const setState = useCallback(async <T = any>(stateId: string, data: T): Promise<void> => {
    try {
      const response = await fetch(`/api/ui-state/${stateId}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ data }),
      });

      if (!response.ok) {
        throw new Error('Failed to update UI state');
      }

      // Optimistically update local cache
      setUIStates(prev => ({
        ...prev,
        [stateId]: data
      }));
    } catch (error) {
      console.error('Error updating UI state:', error);
      throw error;
    }
  }, []);

  /**
   * Fetch UI state from server
   */
  const fetchState = useCallback(async <T = any>(stateId: string): Promise<T | null> => {
    try {
      const response = await fetch(`/api/ui-state/${stateId}`);

      if (response.status === 404) {
        return null;
      }

      if (!response.ok) {
        throw new Error('Failed to fetch UI state');
      }

      const result = await response.json();

      // Update local cache
      setUIStates(prev => ({
        ...prev,
        [stateId]: result.data
      }));

      return result.data;
    } catch (error) {
      console.error('Error fetching UI state:', error);
      return null;
    }
  }, []);

  /**
   * Subscribe to state changes for a specific state ID
   */
  const subscribe = useCallback((callback: (stateId: string, data: any) => void) => {
    subscribersRef.current.add(callback);

    // Return unsubscribe function
    return () => {
      subscribersRef.current.delete(callback);
    };
  }, []);

  /**
   * Delete UI state
   */
  const deleteState = useCallback(async (stateId: string): Promise<void> => {
    try {
      const response = await fetch(`/api/ui-state/${stateId}`, {
        method: 'DELETE',
      });

      if (!response.ok) {
        throw new Error('Failed to delete UI state');
      }

      // Remove from local cache
      setUIStates(prev => {
        const newStates = { ...prev };
        delete newStates[stateId];
        return newStates;
      });
    } catch (error) {
      console.error('Error deleting UI state:', error);
      throw error;
    }
  }, []);

  return {
    // State data
    uiStates,
    templates,

    // Methods
    getState,
    setState,
    fetchState,
    deleteState,
    subscribe,
  };
}
