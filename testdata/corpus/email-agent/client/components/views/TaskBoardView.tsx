// client/components/views/TaskBoardView.tsx
import React, { useEffect, useState } from 'react';
import { TaskBoard } from '../custom/TaskBoard';
import { useUIState } from '../../hooks/useUIState';

interface TaskBoardViewProps {
  ws: WebSocket | null;
}

export function TaskBoardView({ ws }: TaskBoardViewProps) {
  const { getState, fetchState } = useUIState(ws);
  const [state, setState] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const stateId = 'task_board';

  // Fetch initial state
  useEffect(() => {
    const loadState = async () => {
      try {
        setLoading(true);
        const data = await fetchState(stateId);

        if (data) {
          setState(data);
        } else {
          // Use initial state if no data exists
          setState({
            tasks: [],
            columns: {
              todo: [],
              in_progress: [],
              done: []
            }
          });
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load task board');
      } finally {
        setLoading(false);
      }
    };

    loadState();
  }, [stateId, fetchState]);

  // Subscribe to state updates
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

  // Handle actions from component
  const handleAction = (actionId: string, params: Record<string, any>) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        type: 'execute_action',
        templateId: actionId,
        params
      }));
    }
  };

  if (loading) {
    return (
      <div className="h-full flex items-center justify-center bg-gray-50">
        <div className="flex items-center space-x-3">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
          <span className="text-gray-600">Loading task board...</span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="h-full flex items-center justify-center bg-gray-50">
        <div className="text-center p-8">
          <div className="text-red-600 text-lg mb-2">⚠️ Error Loading Task Board</div>
          <div className="text-gray-600 text-sm">{error}</div>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full overflow-auto bg-gray-50 p-6">
      <TaskBoard state={state} onAction={handleAction} />
    </div>
  );
}
