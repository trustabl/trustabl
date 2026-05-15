const Imap = require("node-imap");
const { simpleParser } = require("mailparser");
import { EmailRecord, Attachment, SearchCriteria } from "./database-manager";

interface ImapConfig {
  user: string;
  password: string;
  host: string;
  port: number;
  tls: boolean;
  tlsOptions?: { servername: string };
  connTimeout?: number;
  authTimeout?: number;
  keepalive?: boolean | { interval: number; idleInterval: number; forceNoop: boolean };
}

export class ImapManager {
  private static instance: ImapManager;
  private imapConfig: ImapConfig;
  private imap: any;
  private isConnected: boolean = false;
  private connectionPromise: Promise<void> | null = null;
  private isIdling: boolean = false;
  private idleInterval: any = null;
  private currentFolder: string = "INBOX";
  private onNewEmailCallback: ((count: number) => void) | null = null;

  private constructor(config?: Partial<ImapConfig>) {
    // Build config from environment or provided config
    const EMAIL = config?.user || process.env.EMAIL_ADDRESS || process.env.EMAIL_USER;
    const PASSWORD = config?.password || process.env.EMAIL_APP_PASSWORD || process.env.EMAIL_PASS;

    console.log('🔧 IMAP Configuration:');
    console.log('   Email:', EMAIL ? `${EMAIL.substring(0, 3)}...@${EMAIL.split('@')[1]}` : 'NOT SET');
    console.log('   Password:', PASSWORD ? '***SET***' : 'NOT SET');
    console.log('   Host:', config?.host || process.env.IMAP_HOST || "imap.gmail.com");

    if (!EMAIL || !PASSWORD) {
      throw new Error(
        "Email credentials not found! Please provide email configuration or set EMAIL_ADDRESS and EMAIL_APP_PASSWORD environment variables"
      );
    }

    this.imapConfig = {
      user: EMAIL,
      password: PASSWORD,
      host: config?.host || process.env.IMAP_HOST || "imap.gmail.com",
      port: config?.port || parseInt(process.env.IMAP_PORT || "993"),
      tls: config?.tls !== undefined ? config.tls : true,
      tlsOptions: config?.tlsOptions || { servername: config?.host || "imap.gmail.com" },
      connTimeout: config?.connTimeout || 30000,  // Reduced from 120s to 30s
      authTimeout: config?.authTimeout || 30000,  // Reduced from 60s to 30s
      keepalive: config?.keepalive !== undefined ? config.keepalive : {
        interval: 10000,  // Send keepalive every 10 seconds
        idleInterval: 300000,  // Use IDLE for 5 minutes
        forceNoop: true  // Force NOOP commands even when IDLE is available
      },
    };

    this.imap = new Imap(this.imapConfig);
  }

  public static getInstance(config?: Partial<ImapConfig>): ImapManager {
    if (!ImapManager.instance) {
      ImapManager.instance = new ImapManager(config);
    }
    return ImapManager.instance;
  }

  private async connect(): Promise<void> {
    if (this.isConnected) return;

    if (this.connectionPromise) {
      return this.connectionPromise;
    }

    this.connectionPromise = new Promise((resolve, reject) => {
      // Set a timeout for connection
      const timeout = setTimeout(() => {
        this.connectionPromise = null;
        this.imap.end();
        reject(new Error('IMAP connection timeout after 30 seconds'));
      }, 30000);

      const onReady = () => {
        clearTimeout(timeout);
        this.isConnected = true;
        this.connectionPromise = null;
        resolve();
      };

      const onError = (err: Error) => {
        console.error('❌ IMAP connection error:', err.message);
        clearTimeout(timeout);
        this.isConnected = false;
        this.connectionPromise = null;
        reject(err);
      };

      const onEnd = () => {
        clearTimeout(timeout);
        this.isConnected = false;
        this.connectionPromise = null;
      };

      this.imap.once("ready", onReady);
      this.imap.once("error", onError);
      this.imap.once("end", onEnd);

      try {
        console.log('🔌 Attempting IMAP connection to', this.imapConfig.host);
        this.imap.connect();
      } catch (err) {
        console.error('❌ IMAP connect error:', err);
        clearTimeout(timeout);
        this.connectionPromise = null;
        reject(err);
      }
    });

    return this.connectionPromise;
  }

  private async ensureConnection(): Promise<void> {
    if (!this.isConnected) {
      console.log("Trying to connect to IMAP server...");
      await this.connect();
    } else {
      console.log("Already connected to IMAP server");
    }
  }

  private openMailbox(mailbox: string, readOnly: boolean = true): Promise<any> {
    return new Promise((resolve, reject) => {
      this.imap.openBox(mailbox, readOnly, (err: Error | null, box: any) => {
        if (err) reject(err);
        else {
          this.currentFolder = mailbox;
          resolve(box);
        }
      });
    });
  }

  // Convert SearchCriteria to IMAP search array
  private buildImapSearchCriteria(criteria: SearchCriteria): any[] {
    const searchCriteria: any[] = [];

    // PRIORITY: If gmailQuery is provided, use Gmail's native search syntax (X-GM-RAW)
    // This allows using Gmail's powerful search operators like OR, has:attachment, etc.
    if (criteria.gmailQuery) {
      console.log('📧 Using Gmail native search syntax:', criteria.gmailQuery);
      searchCriteria.push(['X-GM-RAW', criteria.gmailQuery]);
      return searchCriteria;  // Return early, ignore other criteria when using Gmail syntax
    }

    if (criteria.query) {
      searchCriteria.push(['OR',
        ['SUBJECT', criteria.query],
        ['BODY', criteria.query]
      ]);
    }

    if (criteria.from) {
      const fromAddresses = Array.isArray(criteria.from) ? criteria.from : [criteria.from];
      if (fromAddresses.length === 1) {
        searchCriteria.push(['FROM', fromAddresses[0]]);
      } else {
        const orConditions = fromAddresses.map(addr => ['FROM', addr]);
        searchCriteria.push(['OR', ...orConditions]);
      }
    }

    if (criteria.to) {
      const toAddresses = Array.isArray(criteria.to) ? criteria.to : [criteria.to];
      if (toAddresses.length === 1) {
        searchCriteria.push(['TO', toAddresses[0]]);
      } else {
        const orConditions = toAddresses.map(addr => ['TO', addr]);
        searchCriteria.push(['OR', ...orConditions]);
      }
    }

    if (criteria.subject) {
      searchCriteria.push(['SUBJECT', criteria.subject]);
    }

    if (criteria.dateRange) {
      if (criteria.dateRange.start) {
        searchCriteria.push(['SINCE', criteria.dateRange.start]);
      }
      if (criteria.dateRange.end) {
        searchCriteria.push(['BEFORE', criteria.dateRange.end]);
      }
    }

    if (criteria.hasAttachments) {
      searchCriteria.push(['KEYWORD', 'has:attachment']);
    }

    if (criteria.isUnread) {
      searchCriteria.push('UNSEEN');
    } else if (criteria.isUnread === false) {
      searchCriteria.push('SEEN');
    }

    if (criteria.minSize) {
      searchCriteria.push(['LARGER', criteria.minSize]);
    }

    if (criteria.maxSize) {
      searchCriteria.push(['SMALLER', criteria.maxSize]);
    }

    // Default to ALL if no criteria specified
    if (searchCriteria.length === 0) {
      searchCriteria.push('ALL');
    }

    return searchCriteria;
  }

  private searchMailbox(criteria: any[]): Promise<number[]> {
    return new Promise((resolve, reject) => {
      this.imap.search(criteria, (err: Error | null, results: number[]) => {
        if (err) reject(err);
        else resolve(results);
      });
    });
  }

  private fetchEmail(uid: number, headersOnly: boolean = false): Promise<any> {
    return new Promise((resolve, reject) => {
      const fetchOptions = headersOnly
        ? { bodies: 'HEADER.FIELDS (FROM TO SUBJECT DATE MESSAGE-ID IN-REPLY-TO REFERENCES)', struct: true }
        : { bodies: "" };

      const fetch = this.imap.fetch(uid, fetchOptions);
      let emailData = "";
      let resolved = false;
      let attributes: any = null;

      const safeResolve = (result: any) => {
        if (!resolved) {
          resolved = true;
          resolve(result);
        }
      };

      const safeReject = (error: Error) => {
        if (!resolved) {
          resolved = true;
          reject(error);
        }
      };

      fetch.on("message", (msg: any) => {
        msg.on("body", (stream: any) => {
          stream.on("data", (chunk: Buffer) => {
            // Memory bounds checking (max 50MB per email)
            if (emailData.length + chunk.length > 50 * 1024 * 1024) {
              safeReject(new Error("Email too large (exceeds 50MB limit)"));
              return;
            }
            emailData += chunk.toString("utf8");
          });
        });

        msg.on("attributes", (attrs: any) => {
          attributes = attrs;
        });

        msg.once("end", () => {
          if (headersOnly && attributes) {
            // For headers-only, combine the parsed headers with attributes
            simpleParser(emailData, (err: Error | null, parsed: any) => {
              if (err) safeReject(err);
              else {
                parsed.attributes = attributes;
                safeResolve(parsed);
              }
            });
          } else {
            simpleParser(emailData, (err: Error | null, parsed: any) => {
              if (err) safeReject(err);
              else safeResolve(parsed);
            });
          }
        });
      });

      fetch.once("error", (error: Error) => {
        safeReject(error);
      });

      fetch.once("end", () => {
        if (!emailData && !resolved) {
          safeReject(new Error("No email data received"));
        }
      });
    });
  }

  // Fetch emails in parallel with batching
  private async fetchEmailsBatch(uids: number[], headersOnly: boolean = false, batchSize: number = 20): Promise<Map<number, any>> {
    const results = new Map<number, any>();

    // Use smaller batch size for headers-only (faster, less data)
    const effectiveBatchSize = headersOnly ? 30 : batchSize;

    for (let i = 0; i < uids.length; i += effectiveBatchSize) {
      const batch = uids.slice(i, i + effectiveBatchSize);
      const promises = batch.map(async (uid) => {
        try {
          const parsed = await this.fetchEmail(uid, headersOnly);
          return { uid, parsed };
        } catch (err) {
          console.error(`Error fetching email ${uid}:`, (err as Error).message);
          return { uid, parsed: null };
        }
      });

      const batchResults = await Promise.all(promises);
      for (const { uid, parsed } of batchResults) {
        if (parsed) {
          results.set(uid, parsed);
        }
      }
    }

    return results;
  }

  // Convert parsed email to EmailRecord
  private parseEmailToRecord(parsed: any, uid: number, folder: string): EmailRecord {
    // Extract addresses
    const toAddresses = parsed.to?.value?.map((addr: any) => addr.address).join(", ") || "";
    const ccAddresses = parsed.cc?.value?.map((addr: any) => addr.address).join(", ") || "";
    const bccAddresses = parsed.bcc?.value?.map((addr: any) => addr.address).join(", ") || "";

    // Extract attachments
    const attachments: Attachment[] = [];
    if (parsed.attachments && parsed.attachments.length > 0) {
      for (const att of parsed.attachments) {
        attachments.push({
          filename: att.filename || "unnamed",
          contentType: att.contentType,
          sizeBytes: att.size,
          contentId: att.contentId,
          isInline: att.contentDisposition === "inline",
        });
      }
    }

    return {
      messageId: parsed.messageId || `${uid}-${Date.now()}`,
      threadId: parsed.threadId || parsed.inReplyTo,
      inReplyTo: parsed.inReplyTo,
      emailReferences: Array.isArray(parsed.references) ? parsed.references.join(" ") : parsed.references,
      dateSent: parsed.date || new Date(),
      subject: parsed.subject || "",
      fromAddress: parsed.from?.value?.[0]?.address || "",
      fromName: parsed.from?.value?.[0]?.name || "",
      toAddresses,
      ccAddresses,
      bccAddresses,
      replyTo: parsed.replyTo?.value?.[0]?.address,
      bodyText: parsed.text || "",
      bodyHtml: parsed.html || "",
      snippet: (parsed.text || "").substring(0, 200),
      isRead: false,
      isStarred: false,
      isImportant: false,
      isDraft: false,
      isSent: folder === "Sent" || folder === "[Gmail]/Sent Mail",
      isTrash: folder === "Trash" || folder === "[Gmail]/Trash",
      isSpam: folder === "Spam" || folder === "[Gmail]/Spam",
      sizeBytes: 0,
      hasAttachments: attachments.length > 0,
      attachmentCount: attachments.length,
      folder,
      labels: [],
      rawHeaders: JSON.stringify(parsed.headers),
    };
  }

  // Search emails from IMAP with optimized parallel fetching
  public async searchEmails(criteria: SearchCriteria, headersOnly: boolean = false): Promise<Array<{ email: EmailRecord; attachments: Attachment[] }>> {
    await this.ensureConnection();

    const folders = criteria.folders || [criteria.folder || "INBOX"];
    const allEmails: Array<{ email: EmailRecord; attachments: Attachment[] }> = [];
    const limit = criteria.limit || 30;

    for (const folder of folders) {
      try {
        await this.openMailbox(folder);

        const imapCriteria = this.buildImapSearchCriteria(criteria);
        console.log(`🔍 Searching ${folder} with criteria:`, JSON.stringify(imapCriteria));

        const uids = await this.searchMailbox(imapCriteria);
        console.log(`📊 Found ${uids.length} messages in ${folder}`);

        if (uids.length === 0) {
          continue;
        }

        // Apply limit per folder (reverse to get newest first)
        const limitedUids = uids.slice(-Math.min(limit, uids.length)).reverse();
        console.log(`📥 Fetching ${limitedUids.length} messages in parallel...`);

        // Fetch emails in parallel batches
        const parsedEmails = await this.fetchEmailsBatch(limitedUids, headersOnly, 10);

        // Process fetched emails
        for (const uid of limitedUids) {
          const parsed = parsedEmails.get(uid);
          if (!parsed) continue;

          try {
            const email = this.parseEmailToRecord(parsed, uid, folder);

            // Extract attachments (only if full body was fetched)
            const attachments: Attachment[] = [];
            if (!headersOnly && parsed.attachments && parsed.attachments.length > 0) {
              for (const att of parsed.attachments) {
                attachments.push({
                  filename: att.filename || "unnamed",
                  contentType: att.contentType,
                  sizeBytes: att.size,
                  contentId: att.contentId,
                  isInline: att.contentDisposition === "inline",
                });
              }
            }

            allEmails.push({ email, attachments });

            // Stop if we've reached the overall limit
            if (allEmails.length >= limit) {
              break;
            }
          } catch (err) {
            console.error(`Error processing email ${uid} from ${folder}:`, (err as Error).message);
          }
        }

        // Stop searching folders if we've reached the limit
        if (allEmails.length >= limit) {
          break;
        }
      } catch (err) {
        console.error(`Error searching folder ${folder}:`, (err as Error).message);
      }
    }

    console.log(`✅ Fetched total of ${allEmails.length} emails`);
    return allEmails;
  }

  // Quick search that only fetches headers (much faster)
  public async searchEmailsHeadersOnly(criteria: SearchCriteria): Promise<Array<{ email: EmailRecord; attachments: Attachment[] }>> {
    return this.searchEmails(criteria, true);
  }

  // Sync emails for a specific date range
  public async syncEmails(dateRange: { start: Date; end: Date }, folders?: string[]): Promise<Array<{ email: EmailRecord; attachments: Attachment[] }>> {
    const criteria: SearchCriteria = {
      dateRange,
      folders: folders || ["INBOX", "[Gmail]/Sent Mail", "[Gmail]/All Mail"],
      limit: 1000,
    };

    return this.searchEmails(criteria);
  }

  // Get recent emails
  public async getRecentEmails(days: number = 7, folders?: string[]): Promise<Array<{ email: EmailRecord; attachments: Attachment[] }>> {
    const start = new Date();
    start.setDate(start.getDate() - days);

    return this.syncEmails(
      { start, end: new Date() },
      folders
    );
  }

  // Disconnect from IMAP
  public disconnect(): void {
    if (this.isConnected && this.imap) {
      this.imap.end();
      this.isConnected = false;
    }
  }

  // Force reconnect
  public async reconnect(): Promise<void> {
    this.disconnect();
    await this.connect();
  }

  // Start IDLE monitoring on a specific folder
  public async startIdleMonitoring(folder: string = "INBOX", onNewEmail: (count: number) => void): Promise<void> {
    await this.ensureConnection();

    this.currentFolder = folder;
    this.onNewEmailCallback = onNewEmail;

    console.log(`🔔 Starting IDLE monitoring on folder: ${folder}`);

    // Open the mailbox in read-only mode
    await this.openMailbox(folder);

    // Mark as idling (connection will automatically use IDLE when configured with keepalive)
    this.isIdling = true;

    // Set up mail event listener for new emails
    this.imap.on("mail", (numNewMsgs: number) => {
      console.log(`📬 New email(s) detected: ${numNewMsgs} new message(s)`);
      if (this.onNewEmailCallback) {
        this.onNewEmailCallback(numNewMsgs);
      }
    });

    // Set up update event listener (fires when mailbox changes)
    this.imap.on("update", (seqno: number, info: any) => {
      console.log(`📝 Email updated: seqno ${seqno}`);
    });

    // Set up error handler
    this.imap.on("error", (err: Error) => {
      console.error("❌ IMAP IDLE error:", err.message);
      this.isIdling = false;
      // Attempt to reconnect after error
      setTimeout(() => {
        console.log("🔄 Attempting to reconnect after IDLE error...");
        this.reconnect().then(() => {
          this.startIdleMonitoring(folder, onNewEmail);
        }).catch(console.error);
      }, 5000);
    });

    // Set up close handler
    this.imap.on("close", () => {
      console.log("🔌 IMAP connection closed");
      this.isConnected = false;
      this.isIdling = false;
    });

    console.log("✅ IDLE monitoring active - waiting for new emails...");
  }

  // Stop IDLE monitoring completely
  public stopIdleMonitoring(): void {
    console.log("🛑 Stopping IDLE monitoring...");

    this.isIdling = false;

    if (this.idleInterval) {
      clearInterval(this.idleInterval);
      this.idleInterval = null;
    }

    // Remove event listeners
    this.imap.removeAllListeners("mail");
    this.imap.removeAllListeners("update");
    this.imap.removeAllListeners("error");
    this.imap.removeAllListeners("close");

    this.onNewEmailCallback = null;
  }

  // Check if IDLE is currently active
  public isIdleActive(): boolean {
    return this.isIdling;
  }

  // Email action methods for ListenersManager

  /**
   * Mark an email as read by adding the \Seen flag
   */
  public async markAsRead(uid: number, folder: string = "INBOX"): Promise<void> {
    await this.ensureConnection();
    await this.openMailbox(folder, false); // Open in read-write mode

    return new Promise((resolve, reject) => {
      this.imap.addFlags(uid, ['\\Seen'], (err: Error | null) => {
        if (err) {
          console.error(`Failed to mark email ${uid} as read:`, err.message);
          reject(err);
        } else {
          console.log(`✓ Marked email ${uid} as read`);
          resolve();
        }
      });
    });
  }

  /**
   * Mark an email as unread by removing the \Seen flag
   */
  public async markAsUnread(uid: number, folder: string = "INBOX"): Promise<void> {
    await this.ensureConnection();
    await this.openMailbox(folder, false);

    return new Promise((resolve, reject) => {
      this.imap.delFlags(uid, ['\\Seen'], (err: Error | null) => {
        if (err) {
          console.error(`Failed to mark email ${uid} as unread:`, err.message);
          reject(err);
        } else {
          console.log(`✓ Marked email ${uid} as unread`);
          resolve();
        }
      });
    });
  }

  /**
   * Star an email by adding the \Flagged flag
   */
  public async starEmail(uid: number, folder: string = "INBOX"): Promise<void> {
    await this.ensureConnection();
    await this.openMailbox(folder, false);

    return new Promise((resolve, reject) => {
      this.imap.addFlags(uid, ['\\Flagged'], (err: Error | null) => {
        if (err) {
          console.error(`Failed to star email ${uid}:`, err.message);
          reject(err);
        } else {
          console.log(`✓ Starred email ${uid}`);
          resolve();
        }
      });
    });
  }

  /**
   * Unstar an email by removing the \Flagged flag
   */
  public async unstarEmail(uid: number, folder: string = "INBOX"): Promise<void> {
    await this.ensureConnection();
    await this.openMailbox(folder, false);

    return new Promise((resolve, reject) => {
      this.imap.delFlags(uid, ['\\Flagged'], (err: Error | null) => {
        if (err) {
          console.error(`Failed to unstar email ${uid}:`, err.message);
          reject(err);
        } else {
          console.log(`✓ Unstarred email ${uid}`);
          resolve();
        }
      });
    });
  }

  /**
   * Archive an email by moving it to [Gmail]/All Mail folder
   * This removes it from INBOX while keeping it searchable
   */
  public async archiveEmail(uid: number, folder: string = "INBOX"): Promise<void> {
    await this.ensureConnection();
    await this.openMailbox(folder, false);

    return new Promise((resolve, reject) => {
      this.imap.move(uid, '[Gmail]/All Mail', (err: Error | null) => {
        if (err) {
          console.error(`Failed to archive email ${uid}:`, err.message);
          reject(err);
        } else {
          console.log(`✓ Archived email ${uid} to [Gmail]/All Mail`);
          resolve();
        }
      });
    });
  }

  /**
   * Add a Gmail label to an email using X-GM-LABELS extension
   */
  public async addLabel(uid: number, label: string, folder: string = "INBOX"): Promise<void> {
    await this.ensureConnection();
    await this.openMailbox(folder, false);

    return new Promise((resolve, reject) => {
      // Gmail uses X-GM-LABELS as a special flag
      // Note: This may require Gmail-specific IMAP extensions
      this.imap.addFlags(uid, [`X-GM-LABELS`, label], (err: Error | null) => {
        if (err) {
          console.error(`Failed to add label "${label}" to email ${uid}:`, err.message);
          reject(err);
        } else {
          console.log(`✓ Added label "${label}" to email ${uid}`);
          resolve();
        }
      });
    });
  }

  /**
   * Remove a Gmail label from an email using X-GM-LABELS extension
   */
  public async removeLabel(uid: number, label: string, folder: string = "INBOX"): Promise<void> {
    await this.ensureConnection();
    await this.openMailbox(folder, false);

    return new Promise((resolve, reject) => {
      this.imap.delFlags(uid, [`X-GM-LABELS`, label], (err: Error | null) => {
        if (err) {
          console.error(`Failed to remove label "${label}" from email ${uid}:`, err.message);
          reject(err);
        } else {
          console.log(`✓ Removed label "${label}" from email ${uid}`);
          resolve();
        }
      });
    });
  }
}