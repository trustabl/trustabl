import { useState, useEffect } from 'react';

export interface ListenerConfig {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  event: string;
}

export interface ListenerStats {
  total: number;
  enabled: number;
  byEvent: Record<string, number>;
}

export interface UseListenersReturn {
  listeners: ListenerConfig[];
  stats: ListenerStats;
  loading: boolean;
  error: string | null;
  refetch: () => void;
}

export function useListeners(pollInterval?: number): UseListenersReturn {
  const [listeners, setListeners] = useState<ListenerConfig[]>([]);
  const [stats, setStats] = useState<ListenerStats>({ total: 0, enabled: 0, byEvent: {} });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchListeners = async () => {
    try {
      const response = await fetch('/api/listeners');
      if (!response.ok) {
        throw new Error(`Failed to fetch listeners: ${response.statusText}`);
      }
      const data = (await response.json()) as {
        listeners?: ListenerConfig[];
        stats?: ListenerStats;
      };
      setListeners(data.listeners || []);
      setStats(data.stats || { total: 0, enabled: 0, byEvent: {} });
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
      console.error('Error fetching listeners:', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchListeners();

    // Set up polling if interval is provided
    if (pollInterval && pollInterval > 0) {
      const intervalId = setInterval(fetchListeners, pollInterval);
      return () => clearInterval(intervalId);
    }
  }, [pollInterval]);

  return {
    listeners,
    stats,
    loading,
    error,
    refetch: fetchListeners,
  };
}
