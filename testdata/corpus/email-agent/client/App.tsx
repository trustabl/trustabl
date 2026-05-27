import React, { useState, useEffect, useMemo } from "react";
import { ChatInterface } from "./components/ChatInterface";
import { InboxView } from "./components/InboxView";
import { EmailViewer } from "./components/EmailViewer";
import { TabNavigation } from "./components/TabNavigation";
import { TaskBoardView } from "./components/views/TaskBoardView";
import { FinancialDashboardView } from "./components/views/FinancialDashboardView";
import { useWebSocket } from "./hooks/useWebSocket";
import { useComponentTabs } from "./hooks/useComponentTabs";
import { ScreenshotModeProvider } from "./context/ScreenshotModeContext";

const App: React.FC = () => {
  const [emails, setEmails] = useState([]);
  const [messages, setMessages] = useState([]);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [selectedEmail, setSelectedEmail] = useState<any | null>(null);
  const [activeTab, setActiveTab] = useState<string>('inbox');

  // Get component tabs from registry
  const componentTabs = useComponentTabs();

  // Build full tabs list: Inbox (hardcoded) + auto-discovered components
  const tabs = useMemo(() => {
    return [
      { id: 'inbox', name: 'Inbox' },
      ...componentTabs
    ];
  }, [componentTabs]);

  // Single WebSocket connection for all components
  const { isConnected, sendMessage, ws } = useWebSocket({
    url: 'ws://localhost:3000/ws',
    onMessage: (message) => {
      switch (message.type) {
        case 'inbox_update':
          setEmails(message.emails || []);
          break;
        case 'connected':
          console.log('Connected to server:', message.message);
          break;
        case 'session':
        case 'session_info':
          setSessionId(message.sessionId);
          break;
        case 'assistant_message':
          const assistantMsg = {
            id: Date.now().toString() + '-assistant',
            type: 'assistant',
            content: [{ type: 'text', text: message.content }],
            timestamp: new Date().toISOString(),
          };
          setMessages(prev => [...prev, assistantMsg]);
          setIsLoading(false);
          break;
        case 'tool_use':
          const toolMsg = {
            id: Date.now().toString() + '-tool',
            type: 'assistant',
            content: [{
              type: 'tool_use',
              id: message.toolId || Date.now().toString(),
              name: message.toolName,
              input: message.toolInput || {}
            }],
            timestamp: new Date().toISOString(),
          };
          setMessages(prev => [...prev, toolMsg]);
          break;
        case 'result':
          if (message.success) {
            console.log('Query completed successfully', message);
          } else {
            console.error('Query failed:', message.error);
          }
          setIsLoading(false);
          break;
        case 'error':
          console.error('Server error:', message.error);
          const errorMessage = {
            id: Date.now().toString(),
            type: 'assistant',
            content: [{ type: 'text', text: `Error: ${message.error}` }],
            timestamp: new Date().toISOString(),
          };
          setMessages(prev => [...prev, errorMessage]);
          setIsLoading(false);
          break;
      }
    },
  });

  // Render active view based on selected tab
  const renderActiveView = () => {
    switch (activeTab) {
      case 'inbox':
        return (
          <InboxView
            emails={emails}
            onEmailSelect={setSelectedEmail}
            selectedEmailId={selectedEmail?.id}
          />
        );
      case 'task_board':
        return <TaskBoardView ws={ws} />;
      case 'financial_dashboard':
        return <FinancialDashboardView ws={ws} />;
      default:
        return (
          <div className="h-full flex items-center justify-center bg-gray-50">
            <div className="text-gray-500">Unknown view: {activeTab}</div>
          </div>
        );
    }
  };

  return (
    <ScreenshotModeProvider>
      <div className="flex h-screen bg-white">
        {/* Main content area with tabs */}
        <div className="flex-1 flex flex-col">
          {/* Tab Navigation */}
          <TabNavigation
            tabs={tabs}
            activeTab={activeTab}
            onTabChange={setActiveTab}
          />

          {/* Active View */}
          <div className="flex-1 overflow-hidden">
            {renderActiveView()}
          </div>
        </div>

        {/* Email Viewer - Overlay when email selected */}
        {selectedEmail && (
          <EmailViewer
            email={selectedEmail}
            onClose={() => setSelectedEmail(null)}
          />
        )}

        {/* Chat Interface - Always visible on right */}
        <div className="w-96 border-l border-gray-200">
          <ChatInterface
            isConnected={isConnected}
            sendMessage={sendMessage}
            messages={messages}
            setMessages={setMessages}
            sessionId={sessionId}
            isLoading={isLoading}
            setIsLoading={setIsLoading}
            ws={ws}
          />
        </div>
      </div>
    </ScreenshotModeProvider>
  );
};

export default App;
