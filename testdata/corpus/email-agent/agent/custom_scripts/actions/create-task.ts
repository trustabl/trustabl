// agent/custom_scripts/actions/create-task.ts
import type { ActionTemplate, ActionContext, ActionResult } from '../types';
import type { TaskBoardState, Task } from '../ui-states/task-board';

export const config: ActionTemplate = {
  id: 'create_task',
  name: 'Create Task',
  description: 'Create a new task and add it to the task board',
  icon: '📝',
  parameterSchema: {
    type: 'object',
    properties: {
      title: {
        type: 'string',
        description: 'Task title'
      },
      description: {
        type: 'string',
        description: 'Task description'
      },
      priority: {
        type: 'string',
        description: 'Task priority',
        enum: ['low', 'medium', 'high'],
        default: 'medium'
      },
      dueDate: {
        type: 'string',
        description: 'Due date (ISO format, optional)'
      },
      emailId: {
        type: 'string',
        description: 'Source email ID (optional)'
      }
    },
    required: ['title', 'description']
  }
};

export async function handler(
  params: {
    title: string;
    description: string;
    priority?: 'low' | 'medium' | 'high';
    dueDate?: string;
    emailId?: string;
  },
  context: ActionContext
): Promise<ActionResult> {
  try {
    const stateId = 'task_board';

    // Get current state (or use initial state)
    let state = await context.uiState.get<TaskBoardState>(stateId);

    if (!state) {
      // Initialize with empty state
      state = {
        tasks: [],
        columns: {
          todo: [],
          in_progress: [],
          done: []
        }
      };
    }

    // Create new task
    const task: Task = {
      id: `task_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      title: params.title,
      description: params.description,
      status: 'todo',
      priority: params.priority || 'medium',
      dueDate: params.dueDate,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      source: params.emailId
    };

    // Add task to state
    state.tasks.push(task);
    state.columns.todo.push(task.id);

    // Save updated state
    await context.uiState.set(stateId, state);

    context.log(`Created task: ${task.title}`);

    return {
      success: true,
      message: `Created task: "${params.title}"`,
      components: [{
        instanceId: `comp_${Date.now()}`,
        componentId: 'task_board',
        stateId
      }]
    };
  } catch (error) {
    context.log(`Error creating task: ${String(error)}`, 'error');
    return {
      success: false,
      message: `Failed to create task: ${(error as Error).message}`
    };
  }
}
