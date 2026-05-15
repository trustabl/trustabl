// ccsdk/log-writer.ts

import * as fs from "fs/promises";
import * as path from "path";

/**
 * Log entry structure stored in JSONL files
 */
export interface ListenerLogEntry {
  timestamp: string;
  emailId: string;
  emailSubject: string;
  emailFrom: string;
  executed: boolean;
  reason: string;
  actions?: string[];
  executionTimeMs: number;
  error?: string;
}

/**
 * Utility class for writing listener logs to JSONL files
 * Each listener gets its own log file in agent/custom_scripts/listeners/.logs/{listener-id}.jsonl
 */
export class LogWriter {
  private logsDir: string;

  constructor(listenersDir: string) {
    this.logsDir = path.join(listenersDir, ".logs");
  }

  /**
   * Ensure the logs directory exists
   */
  private async ensureLogsDir(): Promise<void> {
    try {
      await fs.mkdir(this.logsDir, { recursive: true });
    } catch (error) {
      console.error("[LogWriter] Failed to create logs directory:", error);
    }
  }

  /**
   * Append a log entry to the listener's JSONL file
   */
  async appendLog(listenerId: string, entry: ListenerLogEntry): Promise<void> {
    try {
      await this.ensureLogsDir();

      const logFile = path.join(this.logsDir, `${listenerId}.jsonl`);
      const logLine = JSON.stringify(entry) + "\n";

      await fs.appendFile(logFile, logLine, "utf-8");
    } catch (error) {
      console.error(
        `[LogWriter] Failed to write log for listener ${listenerId}:`,
        error
      );
    }
  }

  /**
   * Read the last N log entries for a listener
   */
  async readLogs(
    listenerId: string,
    limit: number = 50
  ): Promise<ListenerLogEntry[]> {
    try {
      const logFile = path.join(this.logsDir, `${listenerId}.jsonl`);

      // Check if file exists
      try {
        await fs.access(logFile);
      } catch {
        return []; // File doesn't exist yet
      }

      const content = await fs.readFile(logFile, "utf-8");
      const lines = content.trim().split("\n").filter((line) => line.length > 0);

      // Parse JSON lines and return last N entries (newest first)
      const entries = lines
        .map((line) => {
          try {
            return JSON.parse(line) as ListenerLogEntry;
          } catch {
            return null;
          }
        })
        .filter((entry): entry is ListenerLogEntry => entry !== null);

      // Return last N entries in reverse order (newest first)
      return entries.slice(-limit).reverse();
    } catch (error) {
      console.error(
        `[LogWriter] Failed to read logs for listener ${listenerId}:`,
        error
      );
      return [];
    }
  }

  /**
   * Get all log entries across all listeners (for activity feed)
   */
  async readAllLogs(limit: number = 100): Promise<
    Array<ListenerLogEntry & { listenerId: string }>
  > {
    try {
      await this.ensureLogsDir();

      const files = await fs.readdir(this.logsDir);
      const jsonlFiles = files.filter((f) => f.endsWith(".jsonl"));

      const allEntries: Array<ListenerLogEntry & { listenerId: string }> = [];

      for (const file of jsonlFiles) {
        const listenerId = file.replace(".jsonl", "");
        const entries = await this.readLogs(listenerId, limit);

        allEntries.push(
          ...entries.map((entry) => ({
            ...entry,
            listenerId,
          }))
        );
      }

      // Sort by timestamp (newest first) and limit
      return allEntries
        .sort(
          (a, b) =>
            new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
        )
        .slice(0, limit);
    } catch (error) {
      console.error("[LogWriter] Failed to read all logs:", error);
      return [];
    }
  }
}
