import { v4 as uuidv4 } from "uuid";
import type { Chat, ChatMessage } from "./types.js";

// Simple in-memory store for chats
class ChatStore {
  private chats: Map<string, Chat> = new Map();
  private messages: Map<string, ChatMessage[]> = new Map();

  createChat(title?: string): Chat {
    const id = uuidv4();
    const now = new Date().toISOString();
    const chat: Chat = {
      id,
      title: title || "New Chat",
      createdAt: now,
      updatedAt: now,
    };
    this.chats.set(id, chat);
    this.messages.set(id, []);
    return chat;
  }

  getChat(id: string): Chat | undefined {
    return this.chats.get(id);
  }

  getAllChats(): Chat[] {
    return Array.from(this.chats.values()).sort(
      (a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime()
    );
  }

  updateChatTitle(id: string, title: string): Chat | undefined {
    const chat = this.chats.get(id);
    if (chat) {
      chat.title = title;
      chat.updatedAt = new Date().toISOString();
    }
    return chat;
  }

  deleteChat(id: string): boolean {
    this.messages.delete(id);
    return this.chats.delete(id);
  }

  addMessage(chatId: string, message: Omit<ChatMessage, "id" | "chatId" | "timestamp">): ChatMessage {
    const messages = this.messages.get(chatId);
    if (!messages) {
      throw new Error(`Chat ${chatId} not found`);
    }

    const newMessage: ChatMessage = {
      id: uuidv4(),
      chatId,
      timestamp: new Date().toISOString(),
      ...message,
    };
    messages.push(newMessage);

    // Update chat's updatedAt
    const chat = this.chats.get(chatId);
    if (chat) {
      chat.updatedAt = newMessage.timestamp;

      // Auto-generate title from first user message if still "New Chat"
      if (chat.title === "New Chat" && message.role === "user") {
        chat.title = message.content.slice(0, 50) + (message.content.length > 50 ? "..." : "");
      }
    }

    return newMessage;
  }

  getMessages(chatId: string): ChatMessage[] {
    return this.messages.get(chatId) || [];
  }
}

// Singleton instance
export const chatStore = new ChatStore();
