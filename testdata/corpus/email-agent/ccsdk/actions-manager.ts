// ccsdk/actions-manager.ts
import { readdir, watch, appendFile, mkdir } from "fs/promises";
import { join } from "path";
import { existsSync } from "fs";
import type {
  ActionTemplate,
  ActionInstance,
  ActionContext,
  ActionResult,
  ActionLogEntry
} from "../agent/custom_scripts/types";

export class ActionsManager {
  private actionsDir = join(process.cwd(), "agent/custom_scripts/actions");
  private logsDir = join(process.cwd(), "agent/custom_scripts/.logs/actions");
  private templates: Map<string, {
    config: ActionTemplate;
    handler: Function;
  }> = new Map();
  private instances: Map<string, ActionInstance> = new Map();

  constructor() {
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
   * Load all action templates from directory
   */
  async loadAllTemplates(): Promise<ActionTemplate[]> {
    this.templates.clear();

    try {
      if (!existsSync(this.actionsDir)) {
        console.log("Actions directory does not exist yet, skipping template loading");
        return [];
      }

      const files = await readdir(this.actionsDir);

      for (const file of files) {
        if (file.endsWith(".ts") && !file.startsWith("_")) {
          await this.loadTemplate(file);
        }
      }
    } catch (error) {
      console.error("Error loading action templates:", error);
    }

    return Array.from(this.templates.values()).map(t => t.config);
  }

  /**
   * Load a single template file
   */
  private async loadTemplate(filename: string) {
    try {
      const filePath = join(this.actionsDir, filename);
      // Use dynamic import with cache busting for hot reload
      const module = await import(`${filePath}?t=${Date.now()}`);

      if (module.config?.id && typeof module.handler === "function") {
        this.templates.set(module.config.id, {
          config: module.config,
          handler: module.handler
        });
        console.log(`✓ Loaded action template: ${module.config.id}`);
      } else {
        console.warn(`⚠ Invalid action template ${filename}: missing config or handler`);
      }
    } catch (error) {
      console.error(`✗ Error loading template ${filename}:`, error);
    }
  }

  /**
   * Get template by ID
   */
  getTemplate(id: string): ActionTemplate | undefined {
    return this.templates.get(id)?.config;
  }

  /**
   * Get all templates
   */
  getAllTemplates(): ActionTemplate[] {
    return Array.from(this.templates.values()).map(t => t.config);
  }

  /**
   * Register an action instance created by agent
   */
  registerInstance(instance: ActionInstance): void {
    this.instances.set(instance.instanceId, instance);
  }

  /**
   * Get action instance by ID
   */
  getInstance(instanceId: string): ActionInstance | undefined {
    return this.instances.get(instanceId);
  }

  /**
   * Execute an action instance
   */
  async executeAction(
    instanceId: string,
    context: ActionContext
  ): Promise<ActionResult> {
    const startTime = Date.now();
    const instance = this.instances.get(instanceId);

    if (!instance) {
      return {
        success: false,
        message: "Action instance not found"
      };
    }

    const template = this.templates.get(instance.templateId);

    if (!template) {
      return {
        success: false,
        message: `Template "${instance.templateId}" not found`
      };
    }

    let result: ActionResult;
    let error: string | undefined;

    try {
      // Execute handler
      context.log(`Executing action: ${instance.label}`);
      result = await template.handler(instance.params, context);
    } catch (err: any) {
      error = err.message;
      result = {
        success: false,
        message: `Action failed: ${error}`
      };
      context.log(`Action failed: ${error}`, "error");
    }

    const duration = Date.now() - startTime;

    // Log execution
    await this.logExecution({
      timestamp: new Date().toISOString(),
      instanceId: instance.instanceId,
      templateId: instance.templateId,
      sessionId: instance.sessionId,
      params: instance.params,
      result,
      duration,
      error
    });

    return result;
  }

  /**
   * Log action execution to JSONL file
   */
  private async logExecution(entry: ActionLogEntry) {
    try {
      const date = new Date().toISOString().split("T")[0];
      const logFile = join(this.logsDir, `${date}.jsonl`);
      await appendFile(logFile, JSON.stringify(entry) + "\n");
    } catch (error) {
      console.error("Failed to log action execution:", error);
    }
  }

  /**
   * Watch for template file changes
   */
  async watchTemplates(onChange: (templates: ActionTemplate[]) => void) {
    try {
      if (!existsSync(this.actionsDir)) {
        console.log("Actions directory does not exist, skipping watch");
        return;
      }

      const watcher = watch(this.actionsDir);

      for await (const event of watcher) {
        if (event.filename?.endsWith(".ts")) {
          console.log(`Action template ${event.eventType}: ${event.filename}`);
          const templates = await this.loadAllTemplates();
          onChange(templates);
        }
      }
    } catch (error) {
      console.error("Error watching action templates:", error);
    }
  }

  /**
   * Get execution logs for a template
   */
  async getTemplateLogs(templateId: string, limit: number = 50): Promise<ActionLogEntry[]> {
    // TODO: Implementation would read JSONL files and filter by templateId
    // Similar to ListenersManager.getListenerLogs()
    return [];
  }

  /**
   * Clean up old action instances (optional)
   */
  pruneInstances(maxAge: number = 3600000) { // 1 hour default
    const now = Date.now();
    for (const [id, instance] of this.instances) {
      const age = now - new Date(instance.createdAt).getTime();
      if (age > maxAge) {
        this.instances.delete(id);
      }
    }
  }
}
