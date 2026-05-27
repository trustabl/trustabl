// client/components/message/ComponentRenderer.tsx
import React, { useEffect, useState } from 'react';
import { getComponent } from '../custom/ComponentRegistry';

interface ComponentRendererProps {
  instanceId: string;
  componentId: string;
  stateId: string;
  ws: WebSocket | null;
  onAction?: (actionId: string, params: Record<string, any>) => void;
}

/**
 * ComponentRenderer
 * Fetches UI state and renders the appropriate component
 */
export const ComponentRenderer: React.FC<ComponentRendererProps> = ({
  instanceId,
  componentId,
  stateId,
  ws,
  onAction
}) => {
  const [state, setState] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Fetch initial state
  useEffect(() => {
    const fetchState = async () => {
      try {
        setLoading(true);
        const response = await fetch(`/api/ui-state/${stateId}`);

        if (!response.ok) {
          throw new Error('Failed to fetch UI state');
        }

        const result = await response.json();
        setState(result.data);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      } finally {
        setLoading(false);
      }
    };

    fetchState();
  }, [stateId]);

  // Subscribe to state updates via WebSocket
  useEffect(() => {
    if (!ws) return;

    const handleMessage = (event: MessageEvent) => {
      try {
        const message = JSON.parse(event.data);

        if (message.type === 'ui_state_update' && message.stateId === stateId) {
          setState(message.data);
        }
      } catch (error) {
        console.error('Error handling state update:', error);
      }
    };

    ws.addEventListener('message', handleMessage);

    return () => {
      ws.removeEventListener('message', handleMessage);
    };
  }, [ws, stateId]);

  // Handle action triggers from component
  const handleAction = (actionId: string, params: Record<string, any>) => {
    if (onAction) {
      onAction(actionId, params);
    }

    // Also send via WebSocket if available
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        type: 'component_action',
        instanceId,
        actionId,
        params
      }));
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center p-4 bg-gray-50 rounded-lg border border-gray-200">
        <div className="flex items-center space-x-2">
          <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-blue-600"></div>
          <span className="text-sm text-gray-600">Loading component...</span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4 bg-red-50 rounded-lg border border-red-200">
        <div className="flex items-center space-x-2">
          <svg className="h-5 w-5 text-red-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span className="text-sm text-red-800">Error loading component: {error}</span>
        </div>
      </div>
    );
  }

  if (!state) {
    return (
      <div className="p-4 bg-yellow-50 rounded-lg border border-yellow-200">
        <span className="text-sm text-yellow-800">No state data available</span>
      </div>
    );
  }

  // Get the component from registry
  const Component = getComponent(componentId);

  if (!Component) {
    return (
      <div className="p-4 bg-yellow-50 rounded-lg border border-yellow-200">
        <span className="text-sm text-yellow-800">
          Component "{componentId}" not found in registry
        </span>
      </div>
    );
  }

  // Render the component with state and action handler
  return (
    <div className="my-4">
      <Component state={state} onAction={handleAction} />
    </div>
  );
};
