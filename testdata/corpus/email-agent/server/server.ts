import "dotenv/config";
import { WebSocketHandler } from "../ccsdk/websocket-handler";
import type { WSClient } from "../ccsdk/types";
import { Database } from "bun:sqlite";
import { EmailSyncService } from "../database/email-sync";
import { DATABASE_PATH } from "../database/config";
import { DatabaseManager } from "../database/database-manager";
import { ImapManager } from "../database/imap-manager";
import { ListenersManager } from "../ccsdk/listeners-manager";
import { ActionsManager } from "../ccsdk/actions-manager";
import { UIStateManager } from "../ccsdk/ui-state-manager";
import { ComponentManager } from "../ccsdk/component-manager";
import {
  handleSyncEndpoint,
  handleSyncStatusEndpoint,
  handleInboxEndpoint,
  handleSearchEndpoint,
  handleEmailDetailsEndpoint,
  handleBatchEmailsEndpoint,
  handleListenerDetailsEndpoint,
  handleGetUIState,
  handleSetUIState,
  handleListUIStates,
  handleListUIStateTemplates,
  handleListComponentTemplates,
  handleDeleteUIState
} from "./endpoints";

// Initialize managers
const dbManager = DatabaseManager.getInstance();
const imapManager = ImapManager.getInstance();
const actionsManager = new ActionsManager();
const uiStateManager = new UIStateManager(dbManager);
const componentManager = new ComponentManager(dbManager);

const wsHandler = new WebSocketHandler(
  DATABASE_PATH,
  actionsManager,
  uiStateManager,
  componentManager
);
const db = new Database(DATABASE_PATH);

db.run(`
  CREATE TABLE IF NOT EXISTS sync_metadata (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_time TEXT NOT NULL,
    emails_synced INTEGER DEFAULT 0,
    emails_skipped INTEGER DEFAULT 0,
    sync_type TEXT DEFAULT 'manual'
  )
`);

// Initialize Listeners Manager with IMAP, Database, and UIState dependencies
const listenersManager = new ListenersManager(
  (notification) => {
    // Notification callback - will be used when listeners execute
    console.log('[Server] Listener notification:', notification);
    // TODO: Broadcast to WebSocket clients when listeners execute
  },
  imapManager,
  dbManager,
  (log) => {
    // Log broadcast callback - broadcasts listener logs via WebSocket
    wsHandler.broadcastListenerLog(log);
  },
  uiStateManager
);

// Initialize EmailSyncService with listenersManager
const syncService = new EmailSyncService(DATABASE_PATH, listenersManager);

// Initialize listeners, actions, and IDLE monitoring asynchronously
(async () => {
  // Load all listeners at startup
  await listenersManager.loadAllListeners();

  // Start watching for listener file changes
  listenersManager.watchListeners((listeners) => {
    console.log(`[Server] Listeners reloaded: ${listeners.length} active listener(s)`);
  }).catch((error) => {
    console.error('[Server] Failed to start listener file watcher:', error);
  });

  // Load all action templates at startup
  const templates = await actionsManager.loadAllTemplates();
  console.log(`✅ Loaded ${templates.length} action template(s)`);

  // Start watching for action template file changes
  actionsManager.watchTemplates((templates) => {
    console.log(`[Server] Action templates reloaded: ${templates.length} template(s)`);
  }).catch((error) => {
    console.error('[Server] Failed to start action templates watcher:', error);
  });

  // Load all UI state templates at startup
  const uiStateTemplates = await uiStateManager.loadAllTemplates();
  console.log(`✅ Loaded ${uiStateTemplates.length} UI state template(s)`);

  // Start watching for UI state template file changes
  uiStateManager.watchTemplates((templates) => {
    console.log(`[Server] UI state templates reloaded: ${templates.length} template(s)`);
  }).catch((error) => {
    console.error('[Server] Failed to start UI state templates watcher:', error);
  });

  // Load all component templates at startup
  const componentTemplates = await componentManager.loadAllTemplates();
  console.log(`✅ Loaded ${componentTemplates.length} component template(s)`);

  // Start watching for component template file changes
  componentManager.watchTemplates((templates) => {
    console.log(`[Server] Component templates reloaded: ${templates.length} template(s)`);
  }).catch((error) => {
    console.error('[Server] Failed to start component templates watcher:', error);
  });

  // Start IDLE monitoring for live email notifications
  try {
    await imapManager.startIdleMonitoring("INBOX", async (count: number) => {
      console.log(`[Server] IDLE: ${count} new email(s) detected`);
      await syncService.handleIdleNewEmails(count, "INBOX");
    });
    console.log("✅ IDLE monitoring started successfully");
  } catch (error) {
    console.error('❌ Failed to start IDLE monitoring:', error);
    console.log('ℹ️  Server will continue without IDLE monitoring. You can still sync manually.');
  }
})();

const corsHeaders = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
  'Access-Control-Allow-Headers': 'Content-Type',
};

const server = Bun.serve({
  port: 3000,
  idleTimeout: 120,

  websocket: {
    open(ws: WSClient) {
      wsHandler.onOpen(ws);
    },

    message(ws: WSClient, message: string) {
      wsHandler.onMessage(ws, message);
    },

    close(ws: WSClient) {
      wsHandler.onClose(ws);
    }
  },

  async fetch(req: Request, server: any) {
    const url = new URL(req.url);

    if (req.method === 'OPTIONS') {
      return new Response(null, { headers: corsHeaders });
    }

    if (url.pathname === '/ws') {
      const upgraded = server.upgrade(req, { data: { sessionId: '' } });
      if (!upgraded) {
        return new Response('WebSocket upgrade failed', { status: 400 });
      }
      return;
    }

    if (url.pathname === '/') {
      const file = Bun.file('./client/index.html');
      return new Response(file, {
        headers: {
          'Content-Type': 'text/html',
        },
      });
    }

    if (url.pathname.startsWith('/client/') && url.pathname.endsWith('.css')) {
      const filePath = `.${url.pathname}`;
      const file = Bun.file(filePath);

      if (await file.exists()) {
        try {
          const cssContent = await file.text();

          const postcss = require('postcss');
          const tailwindcss = require('@tailwindcss/postcss');
          const autoprefixer = require('autoprefixer');

          const result = await postcss([
            tailwindcss(),
            autoprefixer,
          ]).process(cssContent, {
            from: filePath,
            to: undefined
          });

          return new Response(result.css, {
            headers: {
              'Content-Type': 'text/css',
            },
          });
        } catch (error) {
          console.error('CSS processing error:', error);
          return new Response('CSS processing failed', { status: 500 });
        }
      }
    }

    if (url.pathname.startsWith('/client/') && (url.pathname.endsWith('.tsx') || url.pathname.endsWith('.ts'))) {
      const filePath = `.${url.pathname}`;
      const file = Bun.file(filePath);

      if (await file.exists()) {
        try {
          const transpiled = await Bun.build({
            entrypoints: [filePath],
            target: 'browser',
            format: 'esm',
          });

          if (transpiled.success) {
            const jsCode = await transpiled.outputs[0].text();
            return new Response(jsCode, {
              headers: {
                'Content-Type': 'application/javascript',
              },
            });
          }
        } catch (error) {
          console.error('Transpilation error:', error);
          return new Response('Transpilation failed', { status: 500 });
        }
      }
    }

    if (url.pathname === '/api/sync' && req.method === 'POST') {
      return handleSyncEndpoint(req);
    }

    if (url.pathname === '/api/sync/status' && req.method === 'GET') {
      return handleSyncStatusEndpoint(req);
    }

    if (url.pathname === '/api/emails/inbox' && req.method === 'GET') {
      return handleInboxEndpoint(req);
    }

    if (url.pathname === '/api/emails/search' && req.method === 'POST') {
      return handleSearchEndpoint(req);
    }

    if (url.pathname.startsWith('/api/email/') && req.method === 'GET') {
      const emailId = decodeURIComponent(url.pathname.split('/').pop()!);
      return handleEmailDetailsEndpoint(req, emailId);
    }

    if (url.pathname === '/api/emails/batch' && req.method === 'POST') {
      return handleBatchEmailsEndpoint(req);
    }

    // Listener logs endpoint - MUST come before generic listener details endpoint
    if (url.pathname.match(/^\/api\/listener\/[^/]+\/logs$/) && req.method === 'GET') {
      const pathParts = url.pathname.split('/');
      const listenerId = decodeURIComponent(pathParts[3]);
      const limit = parseInt(url.searchParams.get('limit') || '50');

      const logWriter = listenersManager.getLogWriter();
      const logs = await logWriter.readLogs(listenerId, limit);

      return new Response(JSON.stringify({ logs }), {
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    if (url.pathname.startsWith('/api/listener/') && req.method === 'GET') {
      const filename = decodeURIComponent(url.pathname.split('/').pop()!);
      return handleListenerDetailsEndpoint(req, filename);
    }

    if (url.pathname === '/api/listeners' && req.method === 'GET') {
      const listeners = listenersManager.getAllListeners();
      const stats = listenersManager.getStats();
      return new Response(JSON.stringify({
        listeners,
        stats
      }), {
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    if (url.pathname === '/api/chat' && req.method === 'POST') {
      return new Response(JSON.stringify({
        error: 'Please use WebSocket connection at /ws for chat'
      }), {
        status: 400,
        headers: {
          'Content-Type': 'application/json',
          ...corsHeaders,
        },
      });
    }

    // UI State endpoints
    if (url.pathname.startsWith('/api/ui-state/') && req.method === 'GET') {
      return handleGetUIState(req, uiStateManager);
    }

    if (url.pathname.startsWith('/api/ui-state/') && req.method === 'PUT') {
      return handleSetUIState(req, uiStateManager);
    }

    if (url.pathname.startsWith('/api/ui-state/') && req.method === 'DELETE') {
      return handleDeleteUIState(req, uiStateManager);
    }

    if (url.pathname === '/api/ui-states' && req.method === 'GET') {
      return handleListUIStates(req, uiStateManager);
    }

    if (url.pathname === '/api/ui-state-templates' && req.method === 'GET') {
      return handleListUIStateTemplates(req, uiStateManager);
    }

    if (url.pathname === '/api/component-templates' && req.method === 'GET') {
      return handleListComponentTemplates(req, componentManager);
    }

    return new Response('Not Found', { status: 404 });
  },
});

console.log(`Server running at http://localhost:${server.port}`);
console.log('WebSocket endpoint available at ws://localhost:3000/ws');
console.log('Visit http://localhost:3000 to view the email chat interface');