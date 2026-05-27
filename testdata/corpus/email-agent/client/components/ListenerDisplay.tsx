import React, { useState, useEffect } from 'react';
import { AlertCircle, FileCode, Activity, Clock } from 'lucide-react';

interface ListenerConfig {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  event: string;
}

interface ListenerData {
  config: ListenerConfig;
  filename: string;
  code: string;
}

interface ListenerDisplayProps {
  listenerId: string; // This will be the filename
  compact?: boolean;
}

export function ListenerDisplay({ listenerId, compact = false }: ListenerDisplayProps) {
  const [listener, setListener] = useState<ListenerData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(!compact);
  const [showCode, setShowCode] = useState(false);

  useEffect(() => {
    fetchListenerData();
  }, [listenerId]);

  const fetchListenerData = async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await fetch(`/api/listener/${encodeURIComponent(listenerId)}`);
      if (!response.ok) {
        throw new Error(`Failed to fetch listener: ${response.statusText}`);
      }

      const data = await response.json();
      setListener(data);
    } catch (err) {
      console.error('Error fetching listener:', err);
      setError(err instanceof Error ? err.message : 'Failed to load listener');
    } finally {
      setLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="border border-gray-200 p-3 bg-gray-50 animate-pulse">
        <div className="h-3 bg-gray-200 w-3/4 mb-2"></div>
        <div className="h-2 bg-gray-200 w-1/2 mb-2"></div>
        <div className="h-2 bg-gray-200 w-full"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="border border-gray-300 p-3 bg-gray-50">
        <div className="flex items-center text-gray-600">
          <AlertCircle className="w-3 h-3 mr-2" />
          <span className="text-xs">{error}</span>
        </div>
      </div>
    );
  }

  if (!listener) {
    return null;
  }

  return (
    <div className="border border-gray-200 bg-white">
      <div
        className="p-3 cursor-pointer"
        onClick={() => compact && setExpanded(!expanded)}
      >
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-1">
              <FileCode className="w-3 h-3 text-gray-600" />
              <span className="text-sm font-semibold text-gray-900">
                {listener.config.name}
              </span>
              <span className={`text-xs px-1.5 py-0.5 uppercase ${
                listener.config.enabled
                  ? 'bg-green-100 text-green-800'
                  : 'bg-gray-100 text-gray-600'
              }`}>
                {listener.config.enabled ? 'Enabled' : 'Disabled'}
              </span>
            </div>

            <div className="flex items-center gap-3 text-xs text-gray-600">
              <span className="flex items-center gap-1">
                <Activity className="w-3 h-3" />
                {listener.config.event}
              </span>
              <span>•</span>
              <span className="font-mono">{listener.filename}</span>
            </div>
          </div>

          {compact && (
            <button
              className="text-gray-400 hover:text-gray-900 font-mono text-xs"
              onClick={(e) => {
                e.stopPropagation();
                setExpanded(!expanded);
              }}
            >
              {expanded ? '[-]' : '[+]'}
            </button>
          )}
        </div>

        {(!compact || expanded) && (
          <>
            <div className="border-t border-gray-200 pt-2 mt-2">
              <div className="text-xs text-gray-700 mb-2">
                {listener.config.description}
              </div>

              <div className="text-xs text-gray-600">
                <span className="font-semibold uppercase">ID:</span> {listener.config.id}
              </div>
            </div>

            {/* Code Section */}
            <div className="mt-2 pt-2 border-t border-gray-200">
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setShowCode(!showCode);
                }}
                className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-gray-700 hover:text-gray-900"
              >
                <FileCode className="w-3 h-3" />
                {showCode ? 'Hide Code' : 'Show Code'}
                <span className="font-mono">{showCode ? '[-]' : '[+]'}</span>
              </button>

              {showCode && (
                <div className="mt-2 bg-gray-50 border border-gray-200 overflow-auto max-h-96">
                  <pre className="p-3 text-xs font-mono whitespace-pre overflow-x-auto">
                    <code>{listener.code}</code>
                  </pre>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
