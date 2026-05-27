import React from "react";

interface Chat {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
}

interface ChatListProps {
  chats: Chat[];
  selectedChatId: string | null;
  onSelectChat: (chatId: string) => void;
  onNewChat: () => void;
  onDeleteChat: (chatId: string) => void;
}

export function ChatList({
  chats,
  selectedChatId,
  onSelectChat,
  onNewChat,
  onDeleteChat,
}: ChatListProps) {
  return (
    <div className="flex flex-col h-full bg-gray-900 text-white">
      {/* Header */}
      <div className="p-4 border-b border-gray-700">
        <button
          onClick={onNewChat}
          className="w-full flex items-center justify-center gap-2 px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors"
        >
          <span>+</span>
          <span>New Chat</span>
        </button>
      </div>

      {/* Chat list */}
      <div className="flex-1 overflow-y-auto">
        {chats.length === 0 ? (
          <div className="p-4 text-center text-gray-500">
            <p className="text-sm">No chats yet</p>
            <p className="text-xs mt-1">Click "New Chat" to start</p>
          </div>
        ) : (
          <div className="p-2 space-y-1">
            {chats.map((chat) => (
              <div
                key={chat.id}
                className={`group flex items-center gap-2 px-3 py-2 rounded-lg cursor-pointer transition-colors ${
                  selectedChatId === chat.id
                    ? "bg-gray-700"
                    : "hover:bg-gray-800"
                }`}
                onClick={() => onSelectChat(chat.id)}
              >
                <span className="text-gray-400 shrink-0">💬</span>
                <span className="flex-1 truncate text-sm">{chat.title}</span>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onDeleteChat(chat.id);
                  }}
                  className="opacity-0 group-hover:opacity-100 p-1 hover:bg-gray-600 rounded transition-all text-gray-400 hover:text-white"
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="p-4 border-t border-gray-700">
        <p className="text-xs text-gray-500 text-center">
          Simple Chat App
        </p>
      </div>
    </div>
  );
}
