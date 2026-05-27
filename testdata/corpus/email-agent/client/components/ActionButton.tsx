// client/components/ActionButton.tsx
import React from 'react';

interface ActionInstance {
  instanceId: string;
  templateId: string;
  label: string;
  description?: string;
  params: Record<string, any>;
  style?: "primary" | "secondary" | "danger";
  sessionId: string;
  createdAt: string;
}

interface ActionButtonProps {
  action: ActionInstance;
  onExecute: (instanceId: string) => void;
  loading?: boolean;
}

export function ActionButton({ action, onExecute, loading }: ActionButtonProps) {
  const styleClass = {
    primary: "bg-blue-600 hover:bg-blue-700 text-white",
    secondary: "bg-gray-200 hover:bg-gray-300 text-gray-800",
    danger: "bg-red-600 hover:bg-red-700 text-white"
  }[action.style || "primary"];

  return (
    <button
      onClick={() => onExecute(action.instanceId)}
      disabled={loading}
      className={`
        px-3 py-1.5 rounded-md text-sm font-medium
        transition-colors duration-200
        disabled:opacity-50 disabled:cursor-not-allowed
        ${styleClass}
      `}
      title={action.description}
    >
      {loading ? "..." : action.label}
    </button>
  );
}
