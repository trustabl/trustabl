// agent/custom_scripts/actions/update-task-status.ts
import type { ActionTemplate, ActionContext, ActionResult } from '../types';
import type { TaskBoardState } from '../ui-states/task-board';

export const config: ActionTemplate = {
  id: 'update_task_status',
  name: 'Update Task Status',
  description: 'Move a task between columns (todo, in_progress, done)',
  icon: '🔄',
  parameterSchema: {
    type: 'object',
    properties: {
      taskId: {
        type: 'string',
        description: 'Task ID to update'
      },
      status: {
        type: 'string',
        description: 'New status for the task',
        enum: ['todo', 'in_progress', 'done']
      }
    },
    required: ['taskId', 'status']
  }
};

export async function handler(
  params: {
    taskId: string;
    status: 'todo' | 'in_progress' | 'done';
  },
  context: ActionContext
): Promise<ActionResult> {
  try {
    const stateId = 'task_board';

    // Get current state
    const state = await context.uiState.get<TaskBoardState>(stateId);

    if (!state) {
      return {
        success: false,
        message: 'Task board not found. Create a task first.'
      };
    }

    // Find the task
    const task = state.tasks.find(t => t.id === params.taskId);

    if (!task) {
      return {
        success: false,
        message: `Task not found: ${params.taskId}`
      };
    }

    const oldStatus = task.status;

    // Remove task ID from old column
    state.columns[oldStatus] = state.columns[oldStatus].filter(id => id !== params.taskId);

    // Add task ID to new column
    if (!state.columns[params.status].includes(params.taskId)) {
      state.columns[params.status].push(params.taskId);
    }

    // Update task status
    task.status = params.status;
    task.updatedAt = new Date().toISOString();

    // Save updated state
    await context.uiState.set(stateId, state);

    context.log(`Moved task "${task.title}" from ${oldStatus} to ${params.status}`);

    return {
      success: true,
      message: `Moved task to ${params.status.replace('_', ' ')}`,
      components: [{
        instanceId: `comp_${Date.now()}`,
        componentId: 'task_board',
        stateId
      }]
    };
  } catch (error) {
    context.log(`Error updating task status: ${String(error)}`, 'error');
    return {
      success: false,
      message: `Failed to update task: ${(error as Error).message}`
    };
  }
}
