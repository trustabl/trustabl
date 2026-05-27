// agent/custom_scripts/ui-states/task-board.ts
import type { UIStateTemplate } from '../types';

/**
 * Task Board State
 * Manages tasks organized in a Kanban-style board
 */

export interface Task {
  id: string;
  title: string;
  description: string;
  status: 'todo' | 'in_progress' | 'done';
  priority: 'low' | 'medium' | 'high';
  dueDate?: string;
  createdAt: string;
  updatedAt: string;
  source?: string; // Email ID if extracted from email
}

export interface TaskBoardState {
  tasks: Task[];
  columns: {
    todo: string[];      // Task IDs in todo column
    in_progress: string[]; // Task IDs in in_progress column
    done: string[];        // Task IDs in done column
  };
}

export const config: UIStateTemplate<TaskBoardState> = {
  id: 'task_board',
  name: 'Task Board',
  description: 'Kanban-style task board for managing tasks extracted from emails',
  initialState: {
    tasks: [],
    columns: {
      todo: [],
      in_progress: [],
      done: []
    }
  }
};
