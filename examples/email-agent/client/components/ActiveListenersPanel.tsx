import React, { useState } from 'react';
import { Headphones, AlertCircle, ChevronDown, ChevronRight, Activity, CheckCircle, Circle, Clock } from 'lucide-react';
import { useListeners } from '../hooks/useListeners';
import { ListenerDisplay } from './ListenerDisplay';
import { useListenerLogs } from '../hooks/useListenerLogs';

export function ActiveListenersPanel() {
  const { listeners, stats, loading, error } = useListeners(30000); // Poll every 30 seconds
  const [expandedListener, setExpandedListener] = useState<string | null>(null);

  if (loading) {
    return (
      <div className="border-b border-gray-200 bg-gray-50 px-4 py-2">
        <div className="flex items-center text-xs text-gray-500">
          <Headphones className="w-4 h-4 mr-2 animate-pulse" />
          <span>Loading listeners...</span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="border-b border-gray-200 bg-red-50 px-4 py-2">
        <div className="flex items-center text-xs text-red-600">
          <AlertCircle className="w-4 h-4 mr-2" />
          <span>Error loading listeners: {error}</span>
        </div>
      </div>
    );
  }

  // Only show panel if there are active listeners
  if (listeners.length === 0) {
    return null;
  }

  return (
    <div className="border-b border-gray-200 bg-gradient-to-r from-blue-50 to-indigo-50">
      {/* Header */}
      <div className="px-4 py-2 flex items-center justify-between">
        <div className="flex items-center">
          <Headphones className="w-4 h-4 mr-2 text-blue-600" />
          <span className="text-xs font-semibold text-gray-700 uppercase tracking-wide">
            {stats.enabled} Active Listener{stats.enabled !== 1 ? 's' : ''}
          </span>
        </div>
      </div>

      {/* Listener List */}
      <div className="px-4 pb-2 space-y-1">
        {listeners.map((listener) => (
          <div key={listener.id} className="bg-white rounded border border-gray-200">
            {/* Listener Summary */}
            <button
              onClick={() => setExpandedListener(
                expandedListener === listener.id ? null : listener.id
              )}
              className="w-full px-3 py-2 flex items-center justify-between hover:bg-gray-50 transition-colors"
            >
              <div className="flex items-center gap-2 text-left">
                {expandedListener === listener.id ? (
                  <ChevronDown className="w-3 h-3 text-gray-400" />
                ) : (
                  <ChevronRight className="w-3 h-3 text-gray-400" />
                )}
                <span className="text-xs font-medium text-gray-900">
                  {listener.name}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs px-2 py-0.5 bg-blue-100 text-blue-700 rounded-full font-medium">
                  {listener.event}
                </span>
              </div>
            </button>

            {/* Expanded Listener Details */}
            {expandedListener === listener.id && (
              <div className="border-t border-gray-200 p-2">
                <ListenerDisplay listenerId={listener.id} compact={false} />

                {/* Activity Log */}
                <div className="mt-3 pt-3 border-t border-gray-200">
                  <ListenerActivityLog listenerId={listener.id} />
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function ListenerActivityLog({ listenerId }: { listenerId: string }) {
  const { logs, loading, error } = useListenerLogs({ listenerId, limit: 15 });

  if (loading) {
    return (
      <div className="flex items-center text-xs text-gray-500">
        <Activity className="w-3 h-3 mr-2 animate-pulse" />
        <span>Loading activity log...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center text-xs text-red-600">
        <AlertCircle className="w-3 h-3 mr-2" />
        <span>Error loading logs: {error}</span>
      </div>
    );
  }

  if (logs.length === 0) {
    return (
      <div className="text-xs text-gray-500 italic">
        No activity yet. This listener will log entries as emails are received.
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center mb-2">
        <Activity className="w-3 h-3 mr-1.5 text-gray-600" />
        <span className="text-xs font-semibold text-gray-700 uppercase tracking-wide">
          Activity Log
        </span>
      </div>

      <div className="space-y-1.5 max-h-64 overflow-y-auto">
        {logs.map((log, index) => {
          const timestamp = new Date(log.timestamp);
          const timeStr = timestamp.toLocaleTimeString('en-US', {
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            hour12: false
          });

          return (
            <div
              key={`${log.timestamp}-${index}`}
              className={`text-xs p-2 rounded border ${
                log.error
                  ? 'bg-red-50 border-red-200'
                  : log.executed
                  ? 'bg-green-50 border-green-200'
                  : 'bg-gray-50 border-gray-200'
              }`}
            >
              <div className="flex items-start gap-2">
                {/* Status Icon */}
                <div className="flex-shrink-0 mt-0.5">
                  {log.error ? (
                    <AlertCircle className="w-3 h-3 text-red-600" />
                  ) : log.executed ? (
                    <CheckCircle className="w-3 h-3 text-green-600" />
                  ) : (
                    <Circle className="w-3 h-3 text-gray-400" />
                  )}
                </div>

                {/* Content */}
                <div className="flex-1 min-w-0">
                  <div className="flex items-baseline gap-2 mb-1">
                    <span className="font-mono text-gray-500 text-[10px]">{timeStr}</span>
                    <span className="font-medium text-gray-700 truncate">
                      {log.emailSubject}
                    </span>
                  </div>

                  <div className={`${log.error ? 'text-red-700' : log.executed ? 'text-green-700' : 'text-gray-600'}`}>
                    {log.reason}
                  </div>

                  {log.actions && log.actions.length > 0 && (
                    <div className="mt-1 flex flex-wrap gap-1">
                      {log.actions.map((action, i) => (
                        <span
                          key={i}
                          className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-blue-100 text-blue-700"
                        >
                          {action}
                        </span>
                      ))}
                    </div>
                  )}

                  {/* Execution time */}
                  <div className="flex items-center gap-1 mt-1 text-[10px] text-gray-500">
                    <Clock className="w-2.5 h-2.5" />
                    <span>{log.executionTimeMs}ms</span>
                  </div>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
