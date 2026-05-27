// client/components/custom/TaskBoard.tsx
import React from 'react';
import type { ComponentProps } from './ComponentRegistry';

interface Task {
  id: string;
  title: string;
  description: string;
  status: 'todo' | 'in_progress' | 'done';
  priority: 'low' | 'medium' | 'high';
  dueDate?: string;
  createdAt: string;
  updatedAt: string;
  source?: string;
}

interface TaskBoardState {
  tasks: Task[];
  columns: {
    todo: string[];
    in_progress: string[];
    done: string[];
  };
}

const priorityColors = {
  low: 'bg-gray-100 text-gray-700 border-gray-300',
  medium: 'bg-yellow-100 text-yellow-700 border-yellow-300',
  high: 'bg-red-100 text-red-700 border-red-300'
};

const priorityLabels = {
  low: 'Low',
  medium: 'Medium',
  high: 'High'
};

export const TaskBoard: React.FC<ComponentProps<TaskBoardState>> = ({ state, onAction }) => {
  const { tasks, columns } = state;

  // Create task lookup
  const taskMap = new Map(tasks.map(task => [task.id, task]));

  const handleStatusChange = (taskId: string, newStatus: 'todo' | 'in_progress' | 'done') => {
    onAction('update_task_status', {
      taskId,
      status: newStatus
    });
  };

  const renderTask = (taskId: string) => {
    const task = taskMap.get(taskId);
    if (!task) return null;

    return (
      <div
        key={task.id}
        className="bg-white p-3 rounded-lg border border-gray-200 shadow-sm hover:shadow-md transition-shadow"
      >
        <div className="flex items-start justify-between mb-2">
          <h4 className="text-sm font-semibold text-gray-900 flex-1">{task.title}</h4>
          <span className={`text-xs px-2 py-1 rounded-full border ${priorityColors[task.priority]}`}>
            {priorityLabels[task.priority]}
          </span>
        </div>

        {task.description && (
          <p className="text-xs text-gray-600 mb-2 line-clamp-2">{task.description}</p>
        )}

        {task.dueDate && (
          <div className="text-xs text-gray-500 mb-2">
            📅 {new Date(task.dueDate).toLocaleDateString()}
          </div>
        )}

        {task.source && (
          <div className="text-xs text-blue-600 mb-2" title="From email">
            📧 From email
          </div>
        )}

        <div className="flex gap-1 mt-2">
          {task.status !== 'todo' && (
            <button
              onClick={() => handleStatusChange(task.id, 'todo')}
              className="text-xs px-2 py-1 bg-gray-100 hover:bg-gray-200 rounded transition-colors"
            >
              ← To Do
            </button>
          )}
          {task.status !== 'in_progress' && (
            <button
              onClick={() => handleStatusChange(task.id, 'in_progress')}
              className="text-xs px-2 py-1 bg-blue-100 hover:bg-blue-200 text-blue-700 rounded transition-colors"
            >
              {task.status === 'todo' ? '→' : '←'} In Progress
            </button>
          )}
          {task.status !== 'done' && (
            <button
              onClick={() => handleStatusChange(task.id, 'done')}
              className="text-xs px-2 py-1 bg-green-100 hover:bg-green-200 text-green-700 rounded transition-colors"
            >
              → Done
            </button>
          )}
        </div>
      </div>
    );
  };

  const renderColumn = (
    columnId: 'todo' | 'in_progress' | 'done',
    title: string,
    bgColor: string
  ) => {
    const taskIds = columns[columnId];

    return (
      <div className="flex-1 min-w-[250px]">
        <div className={`${bgColor} px-3 py-2 rounded-t-lg border border-b-0`}>
          <h3 className="text-sm font-bold text-gray-900">
            {title}
            <span className="ml-2 text-xs font-normal text-gray-600">
              ({taskIds.length})
            </span>
          </h3>
        </div>
        <div className="bg-gray-50 p-3 rounded-b-lg border border-gray-200 min-h-[200px] space-y-2">
          {taskIds.length === 0 ? (
            <div className="text-xs text-gray-400 text-center py-8">
              No tasks
            </div>
          ) : (
            taskIds.map(renderTask)
          )}
        </div>
      </div>
    );
  };

  return (
    <div className="bg-white rounded-lg border border-gray-300 p-4">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-bold text-gray-900">📋 Task Board</h2>
        <div className="text-xs text-gray-500">
          {tasks.length} total task{tasks.length !== 1 ? 's' : ''}
        </div>
      </div>

      {tasks.length === 0 ? (
        <div className="text-center py-12 text-gray-500">
          <div className="text-4xl mb-2">📝</div>
          <div className="text-sm">No tasks yet</div>
          <div className="text-xs mt-1">Tasks from emails will appear here automatically</div>
        </div>
      ) : (
        <div className="flex gap-4 overflow-x-auto pb-2">
          {renderColumn('todo', '📝 To Do', 'bg-gray-100')}
          {renderColumn('in_progress', '🚀 In Progress', 'bg-blue-100')}
          {renderColumn('done', '✅ Done', 'bg-green-100')}
        </div>
      )}
    </div>
  );
};
