import React, { useState, useRef, useEffect } from 'react';
import { MessageRenderer } from './message/MessageRenderer';
import { Message } from './message/types';
import { Send, Wifi, WifiOff, RefreshCw } from 'lucide-react';

interface ChatInterfaceProps {
  isConnected: boolean;
  sendMessage: (message: any) => void;
  messages: Message[];
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>;
  sessionId: string | null;
  isLoading: boolean;
  setIsLoading: React.Dispatch<React.SetStateAction<boolean>>;
  ws: WebSocket | null;
}

export function ChatInterface({ isConnected, sendMessage, messages, setMessages, sessionId, isLoading, setIsLoading, ws }: ChatInterfaceProps) {
  const [inputValue, setInputValue] = useState('');
  const [syncStatus, setSyncStatus] = useState<{
    isSyncing: boolean;
    lastSync: string | null;
    emailCount: number;
  }>({ isSyncing: false, lastSync: null, emailCount: 0 });
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const hasSyncedRef = useRef(false);
  
  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages]);
  
  // Function to sync emails
  const syncEmails = async () => {
    if (hasSyncedRef.current) return;
    
    try {
      // First check if sync is needed
      const statusResponse = await fetch('http://localhost:3000/api/sync/status');
      const statusData = await statusResponse.json();
      
      setSyncStatus(prev => ({
        ...prev,
        lastSync: statusData.lastSync,
        emailCount: statusData.emailCount,
      }));
      
      // Only sync if needed or if we have no emails
      if (statusData.needsSync || statusData.emailCount === 0) {
        setSyncStatus(prev => ({ ...prev, isSyncing: true }));
        
        const syncResponse = await fetch('http://localhost:3000/api/sync', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
        });
        
        const syncData = await syncResponse.json();
        
        if (syncData.success) {
          console.log(`Synced ${syncData.synced} emails, skipped ${syncData.skipped}`);
          setSyncStatus({
            isSyncing: false,
            lastSync: syncData.syncDate,
            emailCount: syncData.totalEmails,
          });
        } else if (syncData.message === 'Already synced recently') {
          console.log('Already synced recently');
          setSyncStatus(prev => ({ ...prev, isSyncing: false }));
        } else {
          console.error('Sync failed:', syncData.error);
          setSyncStatus(prev => ({ ...prev, isSyncing: false }));
        }
      } else {
        console.log('No sync needed - emails are up to date');
      }
      
      hasSyncedRef.current = true;
    } catch (error) {
      console.error('Failed to sync emails:', error);
      setSyncStatus(prev => ({ ...prev, isSyncing: false }));
    }
  };
  
  // Sync emails when app loads
  useEffect(() => {
    syncEmails();
  }, []);
  
  // Poll sync status periodically to check if background sync is complete
  useEffect(() => {
    const interval = setInterval(async () => {
      if (syncStatus.isSyncing) {
        try {
          const statusResponse = await fetch('http://localhost:3000/api/sync/status');
          const statusData = await statusResponse.json();
          
          // Check if sync is complete by comparing email count or last sync time
          if (statusData.emailCount !== syncStatus.emailCount) {
            setSyncStatus({
              isSyncing: false,
              lastSync: statusData.lastSync,
              emailCount: statusData.emailCount,
            });
          }
        } catch (error) {
          console.error('Failed to check sync status:', error);
        }
      }
    }, 2000); // Check every 2 seconds
    
    return () => clearInterval(interval);
  }, [syncStatus.isSyncing, syncStatus.emailCount]);
  
  
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inputValue.trim() || isLoading || !isConnected) return;

    const userMessage: Message = {
      id: Date.now().toString(),
      type: 'user',
      content: inputValue,
      timestamp: new Date().toISOString(),
    };

    setMessages(prev => [...prev, userMessage]);
    setInputValue('');
    setIsLoading(true);

    // Send message through WebSocket
    sendMessage({
      type: 'chat',
      content: inputValue,
      sessionId,
    });
  };

  const handleExecuteAction = (instanceId: string) => {
    console.log('Executing action:', instanceId);
    // Send execute_action message through WebSocket
    sendMessage({
      type: 'execute_action',
      instanceId,
      sessionId,
    });
  };

  // No longer need email click handlers as React Markdown handles it
  
  return (
    <div className="flex flex-col h-screen bg-white">
      <div className="flex-1 overflow-y-auto p-3">
        <div className="max-w-5xl mx-auto">
          <div className="flex items-center justify-between mb-3 pb-3 border-b border-gray-200">
            <h1 className="text-lg font-semibold uppercase tracking-wider">Email Agent</h1>
            <div className="flex items-center gap-4">
              {syncStatus.isSyncing ? (
                <div className="flex items-center gap-2 px-2 py-1 bg-blue-50 rounded">
                  <RefreshCw className="w-3 h-3 text-blue-600 animate-spin" />
                  <span className="text-xs text-blue-600 uppercase font-medium">Syncing...</span>
                </div>
              ) : (
                <button
                  onClick={() => {
                    hasSyncedRef.current = false;
                    syncEmails();
                  }}
                  className="p-1 hover:bg-gray-100 rounded transition-colors"
                  title="Sync emails"
                >
                  <RefreshCw className="w-3 h-3 text-gray-500 hover:text-gray-700" />
                </button>
              )}
              <div className="flex items-center gap-1.5 pl-3 border-l border-gray-200">
                {isConnected ? (
                  <>
                    <Wifi className="w-3 h-3 text-green-600" />
                    <span className="text-xs text-green-600 uppercase font-medium">Online</span>
                  </>
                ) : (
                  <>
                    <WifiOff className="w-3 h-3 text-gray-400" />
                    <span className="text-xs text-gray-400 uppercase font-medium">Offline</span>
                  </>
                )}
              </div>
            </div>
          </div>
          
          {messages.length === 0 ? (
            <div className="text-center text-gray-400 mt-12">
              <p className="text-sm uppercase tracking-wider">Start a conversation</p>
              <p className="mt-2 text-xs">"Show me emails from last week" • "Find emails about meetings"</p>
            </div>
          ) : (
            <div className="space-y-2">
              {messages.map((msg) => (
                <MessageRenderer key={msg.id} message={msg} onExecuteAction={handleExecuteAction} ws={ws} />
              ))}
              {isLoading && (
                <MessageRenderer
                  ws={ws}
                  message={{
                    id: 'loading',
                    type: 'assistant',
                    content: [{ type: 'text', text: 'Processing...' }],
                    timestamp: new Date().toISOString(),
                  }}
                />
              )}
            </div>
          )}
          
          <div ref={messagesEndRef} />
        </div>
      </div>
      
      <div className="border-t border-gray-200 bg-white p-3">
        <form onSubmit={handleSubmit} className="max-w-5xl mx-auto">
          <div className="flex gap-2">
            <input
              type="text"
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
              placeholder={isConnected ? "Ask about your emails..." : "Waiting for connection..."}
              className="flex-1 px-3 py-2 text-sm border border-gray-300 focus:border-gray-900 focus:outline-none"
              disabled={isLoading || !isConnected}
            />
            <button
              type="submit"
              disabled={isLoading || !inputValue.trim() || !isConnected}
              className="px-4 py-2 text-xs font-semibold uppercase tracking-wider bg-gray-900 text-white hover:bg-white hover:text-gray-900 border border-gray-900 disabled:opacity-30 disabled:cursor-not-allowed flex items-center gap-2 transition-colors"
            >
              <Send size={14} />
              Send
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}