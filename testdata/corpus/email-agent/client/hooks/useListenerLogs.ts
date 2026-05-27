// client/hooks/useListenerLogs.ts
import { useState, useEffect, useRef } from "react";

export interface ListenerLogEntry {
  timestamp: string;
  emailId: string;
  emailSubject: string;
  emailFrom: string;
  executed: boolean;
  reason: string;
  actions?: string[];
  executionTimeMs: number;
  error?: string;
}

interface UseListenerLogsOptions {
  listenerId: string;
  limit?: number;
}

export function useListenerLogs({ listenerId, limit = 20 }: UseListenerLogsOptions) {
  const [logs, setLogs] = useState<ListenerLogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const wsRef = useRef<WebSocket | null>(null);

  // Fetch initial logs
  useEffect(() => {
    const fetchLogs = async () => {
      try {
        setLoading(true);
        const response = await fetch(
          `http://localhost:3000/api/listener/${encodeURIComponent(listenerId)}/logs?limit=${limit}`
        );

        if (!response.ok) {
          throw new Error(`Failed to fetch logs: ${response.statusText}`);
        }

        const data = await response.json() as { logs: ListenerLogEntry[] };
        setLogs(data.logs || []);
        setError(null);
      } catch (err) {
        console.error("Failed to fetch listener logs:", err);
        setError(err instanceof Error ? err.message : "Failed to fetch logs");
      } finally {
        setLoading(false);
      }
    };

    fetchLogs();
  }, [listenerId, limit]);

  // Subscribe to WebSocket for real-time updates
  useEffect(() => {
    const ws = new WebSocket("ws://localhost:3000/ws");
    wsRef.current = ws;

    ws.onopen = () => {
      console.log("[useListenerLogs] WebSocket connected");
    };

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);

        if (message.type === "listener_log") {
          const logEntry = message.log;

          // Only add logs for this specific listener
          if (logEntry.listenerId === listenerId) {
            setLogs((prevLogs) => {
              // Add new log at the beginning (newest first)
              const newLogs = [
                {
                  timestamp: logEntry.timestamp,
                  emailId: logEntry.emailId,
                  emailSubject: logEntry.emailSubject,
                  emailFrom: logEntry.emailFrom,
                  executed: logEntry.executed,
                  reason: logEntry.reason,
                  actions: logEntry.actions,
                  executionTimeMs: logEntry.executionTimeMs,
                  error: logEntry.error,
                },
                ...prevLogs,
              ];

              // Keep only the most recent 'limit' logs
              return newLogs.slice(0, limit);
            });
          }
        }
      } catch (err) {
        console.error("[useListenerLogs] Failed to parse WebSocket message:", err);
      }
    };

    ws.onerror = (err) => {
      console.error("[useListenerLogs] WebSocket error:", err);
    };

    ws.onclose = () => {
      console.log("[useListenerLogs] WebSocket disconnected");
    };

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [listenerId, limit]);

  return {
    logs,
    loading,
    error,
  };
}
