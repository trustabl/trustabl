// ccsdk/ui-state-manager.ts
import { readdir, watch, appendFile, mkdir } from "fs/promises";
import { join } from "path";
import { existsSync } from "fs";
import type { UIStateTemplate, UIStateModule } from "../agent/custom_scripts/types";
import type { DatabaseManager } from "../database/database-manager";

/**
 * UIStateManager handles:
 * - File-based template discovery from agent/custom_scripts/ui-states/
 * - Hot reload when templates change
 * - Database operations for persistent state
 * - JSONL logging for audit trail
 * - Broadcasting state updates to clients
 */
export class UIStateManager {
  private uiStatesDir = join(process.cwd(), "agent/custom_scripts/ui-states");
  private logsDir = join(process.cwd(), "agent/custom_scripts/.logs/ui-states");
  private templates: Map<string, UIStateTemplate<any>> = new Map();
  private updateCallbacks: Set<(stateId: string, data: any) => void> = new Set();

  constructor(private db: DatabaseManager) {
    this.ensureLogsDir();
  }

  /**
   * Ensure logs directory exists
   */
  private async ensureLogsDir() {
    if (!existsSync(this.logsDir)) {
      await mkdir(this.logsDir, { recursive: true });
    }
  }

  /**
   * Load all UI state templates from directory
   */
  async loadAllTemplates(): Promise<UIStateTemplate<any>[]> {
    this.templates.clear();

    try {
      if (!existsSync(this.uiStatesDir)) {
        console.log("UI states directory does not exist yet, will be created on first template");
        await mkdir(this.uiStatesDir, { recursive: true });
        return [];
      }

      const files = await readdir(this.uiStatesDir);

      for (const file of files) {
        if (file.endsWith(".ts") && !file.startsWith("_")) {
          await this.loadTemplate(file);
        }
      }
    } catch (error) {
      console.error("Error loading UI state templates:", error);
    }

    return Array.from(this.templates.values());
  }

  /**
   * Load a single template file
   */
  private async loadTemplate(filename: string) {
    try {
      const filePath = join(this.uiStatesDir, filename);
      // Use dynamic import with cache busting for hot reload
      const module: UIStateModule = await import(`${filePath}?t=${Date.now()}`);

      if (module.config?.id && module.config.initialState !== undefined) {
        this.templates.set(module.config.id, module.config);
        console.log(`✓ Loaded UI state template: ${module.config.id}`);
      } else {
        console.warn(`⚠ Invalid UI state template ${filename}: missing config or initialState`);
      }
    } catch (error) {
      console.error(`✗ Error loading UI state template ${filename}:`, error);
    }
  }

  /**
   * Get template by ID
   */
  getTemplate(id: string): UIStateTemplate<any> | undefined {
    return this.templates.get(id);
  }

  /**
   * Get all templates
   */
  getAllTemplates(): UIStateTemplate<any>[] {
    return Array.from(this.templates.values());
  }

  /**
   * Get UI state by ID from database
   */
  async getState<T = any>(stateId: string): Promise<T | null> {
    try {
      const result = this.db.getUIState(stateId);

      if (!result) {
        // Check if there's a template with initial state
        const template = this.templates.get(stateId);
        if (template) {
          return template.initialState as T;
        }
        return null;
      }

      return result as T;
    } catch (error) {
      console.error(`Error getting UI state ${stateId}:`, error);
      return null;
    }
  }

  /**
   * Set/update UI state in database
   */
  async setState<T = any>(stateId: string, data: T): Promise<void> {
    try {
      // Save to database
      await this.db.setUIState(stateId, data);

      // Log the update
      await this.logStateUpdate(stateId, data);

      // Notify all subscribers
      this.notifyStateUpdate(stateId, data);

      console.log(`✓ UI state updated: ${stateId}`);
    } catch (error) {
      console.error(`Error setting UI state ${stateId}:`, error);
      throw error;
    }
  }

  /**
   * List all UI states
   */
  async listStates(): Promise<Array<{ stateId: string; updatedAt: string }>> {
    try {
      return this.db.listUIStates();
    } catch (error) {
      console.error("Error listing UI states:", error);
      return [];
    }
  }

  /**
   * Delete a UI state
   */
  async deleteState(stateId: string): Promise<void> {
    try {
      await this.db.deleteUIState(stateId);
      console.log(`✓ UI state deleted: ${stateId}`);
    } catch (error) {
      console.error(`Error deleting UI state ${stateId}:`, error);
      throw error;
    }
  }

  /**
   * Subscribe to state updates
   */
  onStateUpdate(callback: (stateId: string, data: any) => void): () => void {
    this.updateCallbacks.add(callback);
    // Return unsubscribe function
    return () => {
      this.updateCallbacks.delete(callback);
    };
  }

  /**
   * Notify all subscribers of state update
   */
  private notifyStateUpdate(stateId: string, data: any): void {
    for (const callback of this.updateCallbacks) {
      try {
        callback(stateId, data);
      } catch (error) {
        console.error("Error in state update callback:", error);
      }
    }
  }

  /**
   * Log state update to JSONL file
   */
  private async logStateUpdate(stateId: string, data: any) {
    try {
      const date = new Date().toISOString().split("T")[0];
      const logFile = join(this.logsDir, `${date}.jsonl`);

      const logEntry = {
        timestamp: new Date().toISOString(),
        stateId,
        action: "update",
        dataSize: JSON.stringify(data).length
      };

      await appendFile(logFile, JSON.stringify(logEntry) + "\n");
    } catch (error) {
      console.error("Failed to log UI state update:", error);
    }
  }

  /**
   * Watch for template file changes
   */
  async watchTemplates(onChange: (templates: UIStateTemplate<any>[]) => void) {
    try {
      if (!existsSync(this.uiStatesDir)) {
        await mkdir(this.uiStatesDir, { recursive: true });
      }

      const watcher = watch(this.uiStatesDir);

      for await (const event of watcher) {
        if (event.filename?.endsWith(".ts")) {
          console.log(`UI state template ${event.eventType}: ${event.filename}`);
          const templates = await this.loadAllTemplates();
          onChange(templates);
        }
      }
    } catch (error) {
      console.error("Error watching UI state templates:", error);
    }
  }

  /**
   * Initialize a state with its template's initial state if it doesn't exist
   */
  async initializeStateIfNeeded(stateId: string): Promise<boolean> {
    try {
      const existing = await this.getState(stateId);
      if (existing !== null) {
        return false; // Already exists
      }

      const template = this.templates.get(stateId);
      if (!template) {
        console.warn(`No template found for state ID: ${stateId}`);
        return false;
      }

      await this.setState(stateId, template.initialState);
      console.log(`✓ Initialized UI state: ${stateId}`);
      return true;
    } catch (error) {
      console.error(`Error initializing state ${stateId}:`, error);
      return false;
    }
  }
}
