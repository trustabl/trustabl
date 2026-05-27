// ccsdk/component-manager.ts
import { readdir, watch, mkdir } from "fs/promises";
import { join } from "path";
import { existsSync } from "fs";
import type { ComponentTemplate, ComponentInstance, ComponentModule } from "../agent/custom_scripts/types";
import type { DatabaseManager } from "../database/database-manager";

/**
 * ComponentManager handles:
 * - File-based template discovery from agent/custom_scripts/components/
 * - Hot reload when templates change
 * - Component instance registration
 * - Instance lifecycle management
 */
export class ComponentManager {
  private componentsDir = join(process.cwd(), "agent/custom_scripts/components");
  private templates: Map<string, ComponentTemplate> = new Map();
  private instances: Map<string, ComponentInstance> = new Map();

  constructor(private db: DatabaseManager) {}

  /**
   * Load all component templates from directory
   */
  async loadAllTemplates(): Promise<ComponentTemplate[]> {
    this.templates.clear();

    try {
      if (!existsSync(this.componentsDir)) {
        console.log("Components directory does not exist yet, will be created on first template");
        await mkdir(this.componentsDir, { recursive: true });
        return [];
      }

      const files = await readdir(this.componentsDir);

      for (const file of files) {
        if ((file.endsWith(".ts") || file.endsWith(".tsx")) && !file.startsWith("_")) {
          await this.loadTemplate(file);
        }
      }
    } catch (error) {
      console.error("Error loading component templates:", error);
    }

    return Array.from(this.templates.values());
  }

  /**
   * Load a single template file
   */
  private async loadTemplate(filename: string) {
    try {
      const filePath = join(this.componentsDir, filename);
      // Use dynamic import with cache busting for hot reload
      const module: ComponentModule = await import(`${filePath}?t=${Date.now()}`);

      if (module.config?.id && module.config.stateId) {
        this.templates.set(module.config.id, module.config);
        console.log(`✓ Loaded component template: ${module.config.id}`);
      } else {
        console.warn(`⚠ Invalid component template ${filename}: missing config or stateId`);
      }
    } catch (error) {
      console.error(`✗ Error loading component template ${filename}:`, error);
    }
  }

  /**
   * Get template by ID
   */
  getTemplate(id: string): ComponentTemplate | undefined {
    return this.templates.get(id);
  }

  /**
   * Get all templates
   */
  getAllTemplates(): ComponentTemplate[] {
    return Array.from(this.templates.values());
  }

  /**
   * Register a component instance
   */
  registerInstance(instance: ComponentInstance): void {
    // Store in memory
    this.instances.set(instance.instanceId, instance);

    // Store in database
    try {
      this.db.registerComponentInstance({
        instanceId: instance.instanceId,
        componentId: instance.componentId,
        stateId: instance.stateId,
        sessionId: instance.sessionId
      });
      console.log(`✓ Registered component instance: ${instance.componentId} (${instance.instanceId})`);
    } catch (error) {
      console.error(`Error registering component instance:`, error);
    }
  }

  /**
   * Get component instance by ID
   */
  getInstance(instanceId: string): ComponentInstance | undefined {
    return this.instances.get(instanceId);
  }

  /**
   * Get all instances for a session
   */
  getInstancesBySession(sessionId: string): ComponentInstance[] {
    try {
      return this.db.getComponentInstancesBySession(sessionId);
    } catch (error) {
      console.error(`Error getting component instances for session:`, error);
      return [];
    }
  }

  /**
   * Watch for template file changes
   */
  async watchTemplates(onChange: (templates: ComponentTemplate[]) => void) {
    try {
      if (!existsSync(this.componentsDir)) {
        await mkdir(this.componentsDir, { recursive: true });
      }

      const watcher = watch(this.componentsDir);

      for await (const event of watcher) {
        if (event.filename?.endsWith(".ts") || event.filename?.endsWith(".tsx")) {
          console.log(`Component template ${event.eventType}: ${event.filename}`);
          const templates = await this.loadAllTemplates();
          onChange(templates);
        }
      }
    } catch (error) {
      console.error("Error watching component templates:", error);
    }
  }

  /**
   * Prune old component instances
   */
  pruneOldInstances(daysOld: number = 7): void {
    try {
      this.db.pruneOldComponentInstances(daysOld);
      console.log(`✓ Pruned component instances older than ${daysOld} days`);
    } catch (error) {
      console.error("Error pruning old component instances:", error);
    }
  }

  /**
   * Get component data (template + state)
   */
  async getComponentData(instanceId: string, uiStateManager: any): Promise<{
    template: ComponentTemplate;
    state: any;
    instance: ComponentInstance;
  } | null> {
    const instance = this.instances.get(instanceId);

    if (!instance) {
      console.warn(`Component instance not found: ${instanceId}`);
      return null;
    }

    const template = this.templates.get(instance.componentId);

    if (!template) {
      console.warn(`Component template not found: ${instance.componentId}`);
      return null;
    }

    try {
      const state = await uiStateManager.getState(instance.stateId);

      return {
        template,
        state,
        instance
      };
    } catch (error) {
      console.error(`Error getting component data:`, error);
      return null;
    }
  }
}
