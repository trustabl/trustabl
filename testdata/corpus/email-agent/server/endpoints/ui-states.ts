// server/endpoints/ui-states.ts
import type { UIStateManager } from "../../ccsdk/ui-state-manager";
import type { ComponentManager } from "../../ccsdk/component-manager";

const corsHeaders = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
  'Access-Control-Allow-Headers': 'Content-Type',
};

/**
 * GET /api/ui-state/:stateId
 * Fetch UI state by ID
 */
export async function handleGetUIState(
  req: Request,
  uiStateManager: UIStateManager
): Promise<Response> {
  try {
    const url = new URL(req.url);
    const pathParts = url.pathname.split('/');
    const stateId = pathParts[pathParts.length - 1];

    if (!stateId) {
      return new Response(JSON.stringify({
        error: 'State ID is required'
      }), {
        status: 400,
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    const state = await uiStateManager.getState(stateId);

    if (state === null) {
      return new Response(JSON.stringify({
        error: 'State not found'
      }), {
        status: 404,
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    return new Response(JSON.stringify({
      stateId,
      data: state
    }), {
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  } catch (error) {
    console.error('Error fetching UI state:', error);
    return new Response(JSON.stringify({
      error: 'Failed to fetch UI state',
      details: error instanceof Error ? error.message : 'Unknown error'
    }), {
      status: 500,
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  }
}

/**
 * PUT /api/ui-state/:stateId
 * Update UI state
 */
export async function handleSetUIState(
  req: Request,
  uiStateManager: UIStateManager
): Promise<Response> {
  try {
    const url = new URL(req.url);
    const pathParts = url.pathname.split('/');
    const stateId = pathParts[pathParts.length - 1];

    if (!stateId) {
      return new Response(JSON.stringify({
        error: 'State ID is required'
      }), {
        status: 400,
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    const body = await req.json();

    if (!body.data) {
      return new Response(JSON.stringify({
        error: 'Data is required in request body'
      }), {
        status: 400,
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    await uiStateManager.setState(stateId, body.data);

    return new Response(JSON.stringify({
      success: true,
      stateId,
      message: 'State updated successfully'
    }), {
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  } catch (error) {
    console.error('Error updating UI state:', error);
    return new Response(JSON.stringify({
      error: 'Failed to update UI state',
      details: error instanceof Error ? error.message : 'Unknown error'
    }), {
      status: 500,
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  }
}

/**
 * GET /api/ui-states
 * List all UI states
 */
export async function handleListUIStates(
  req: Request,
  uiStateManager: UIStateManager
): Promise<Response> {
  try {
    const states = await uiStateManager.listStates();

    return new Response(JSON.stringify({
      states
    }), {
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  } catch (error) {
    console.error('Error listing UI states:', error);
    return new Response(JSON.stringify({
      error: 'Failed to list UI states',
      details: error instanceof Error ? error.message : 'Unknown error'
    }), {
      status: 500,
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  }
}

/**
 * GET /api/ui-state-templates
 * List all UI state templates
 */
export async function handleListUIStateTemplates(
  req: Request,
  uiStateManager: UIStateManager
): Promise<Response> {
  try {
    const templates = uiStateManager.getAllTemplates();

    return new Response(JSON.stringify({
      templates
    }), {
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  } catch (error) {
    console.error('Error listing UI state templates:', error);
    return new Response(JSON.stringify({
      error: 'Failed to list UI state templates',
      details: error instanceof Error ? error.message : 'Unknown error'
    }), {
      status: 500,
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  }
}

/**
 * GET /api/component-templates
 * List all component templates
 */
export async function handleListComponentTemplates(
  req: Request,
  componentManager: ComponentManager
): Promise<Response> {
  try {
    const templates = componentManager.getAllTemplates();

    return new Response(JSON.stringify({
      templates
    }), {
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  } catch (error) {
    console.error('Error listing component templates:', error);
    return new Response(JSON.stringify({
      error: 'Failed to list component templates',
      details: error instanceof Error ? error.message : 'Unknown error'
    }), {
      status: 500,
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  }
}

/**
 * DELETE /api/ui-state/:stateId
 * Delete UI state
 */
export async function handleDeleteUIState(
  req: Request,
  uiStateManager: UIStateManager
): Promise<Response> {
  try {
    const url = new URL(req.url);
    const pathParts = url.pathname.split('/');
    const stateId = pathParts[pathParts.length - 1];

    if (!stateId) {
      return new Response(JSON.stringify({
        error: 'State ID is required'
      }), {
        status: 400,
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    await uiStateManager.deleteState(stateId);

    return new Response(JSON.stringify({
      success: true,
      stateId,
      message: 'State deleted successfully'
    }), {
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  } catch (error) {
    console.error('Error deleting UI state:', error);
    return new Response(JSON.stringify({
      error: 'Failed to delete UI state',
      details: error instanceof Error ? error.message : 'Unknown error'
    }), {
      status: 500,
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  }
}
