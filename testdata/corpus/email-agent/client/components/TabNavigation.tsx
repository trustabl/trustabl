// client/components/TabNavigation.tsx
import React from 'react';
import { Mail } from 'lucide-react';

interface Tab {
  id: string;
  name: string;
}

interface TabNavigationProps {
  tabs: Tab[];
  activeTab: string;
  onTabChange: (tabId: string) => void;
}

export function TabNavigation({ tabs, activeTab, onTabChange }: TabNavigationProps) {
  return (
    <div className="border-b border-gray-200 bg-white">
      <div className="flex space-x-1 px-4 pt-3">
        {tabs.map((tab) => {
          const isActive = activeTab === tab.id;

          return (
            <button
              key={tab.id}
              onClick={() => onTabChange(tab.id)}
              className={`
                px-4 py-2 text-sm font-medium rounded-t-lg transition-colors
                ${isActive
                  ? 'bg-white text-blue-600 border-t border-l border-r border-gray-200'
                  : 'text-gray-600 hover:text-gray-900 hover:bg-gray-50'
                }
              `}
              style={isActive ? { borderBottom: '2px solid white', marginBottom: '-1px' } : {}}
            >
              <div className="flex items-center gap-2">
                {tab.id === 'inbox' && <Mail className="w-4 h-4" />}
                <span>{tab.name}</span>
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
