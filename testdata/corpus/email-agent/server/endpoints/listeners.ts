import { join } from "path";
import { readFile } from "fs/promises";

const corsHeaders = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
  'Access-Control-Allow-Headers': 'Content-Type',
};

export async function handleListenerDetailsEndpoint(req: Request, filename: string): Promise<Response> {
  if (!filename) {
    return new Response(JSON.stringify({ error: 'Invalid listener filename' }), {
      status: 400,
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  }

  // Ensure filename ends with .ts
  if (!filename.endsWith('.ts')) {
    filename = `${filename}.ts`;
  }

  // Security: prevent directory traversal
  if (filename.includes('..') || filename.includes('/') || filename.includes('\\')) {
    return new Response(JSON.stringify({ error: 'Invalid filename' }), {
      status: 400,
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  }

  try {
    const listenersDir = join(process.cwd(), "agent/custom_scripts/listeners");
    const filePath = join(listenersDir, filename);

    // Read the file content
    const code = await readFile(filePath, 'utf-8');

    // Import the module to get the config
    const module = await import(`${filePath}?t=${Date.now()}`);

    if (!module.config || !module.handler) {
      return new Response(JSON.stringify({
        error: 'Invalid listener file: missing config or handler'
      }), {
        status: 400,
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    const listenerData = {
      config: {
        id: module.config.id,
        name: module.config.name,
        description: module.config.description,
        enabled: module.config.enabled,
        event: module.config.event,
      },
      filename,
      code,
    };

    return new Response(JSON.stringify(listenerData), {
      headers: {
        'Content-Type': 'application/json',
        ...corsHeaders,
      },
    });
  } catch (error) {
    console.error('Error fetching listener details:', error);

    // Check if file not found
    if ((error as any).code === 'ENOENT') {
      return new Response(JSON.stringify({
        error: 'Listener file not found',
        filename
      }), {
        status: 404,
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    return new Response(JSON.stringify({
      error: 'Failed to fetch listener details',
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
