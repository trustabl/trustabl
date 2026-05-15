import { EmailSearcher } from "./email-search";
import { EmailDatabase, EmailRecord, Recipient, Attachment } from "./email-db";
import { simpleParser } from "mailparser";
import { DATABASE_PATH } from "./config";
import { ListenersManager } from "../ccsdk/listeners-manager";
import type { Email } from "../agent/custom_scripts/types";

interface SyncOptions {
  folder?: string;
  since?: Date;
  before?: Date;  // Sync emails before this date
  limit?: number;
  markAsRead?: boolean;
  from?: string;  // Filter by sender email/domain
  to?: string;    // Filter by recipient
  subject?: string;  // Filter by subject (partial match)
  hasAttachments?: boolean;  // Only emails with/without attachments
  unreadOnly?: boolean;  // Only unread emails
  starredOnly?: boolean;  // Only starred/flagged emails
  searchText?: string;  // General text search in IMAP
  excludeFolders?: string[];  // Folders to exclude from sync
  sizeMin?: number;  // Minimum email size in bytes
  sizeMax?: number;  // Maximum email size in bytes
}

export class EmailSyncService {
  private emailSearcher: EmailSearcher;
  private database: EmailDatabase;
  private listenersManager?: ListenersManager;

  constructor(dbPath: string = DATABASE_PATH, listenersManager?: ListenersManager) {
    this.emailSearcher = new EmailSearcher();
    this.database = new EmailDatabase(dbPath);
    this.listenersManager = listenersManager;
  }

  // Parse email addresses from string (handles "Name <email@domain.com>" format)
  private parseEmailAddress(addressString: string): { address: string; name?: string }[] {
    if (!addressString) return [];
    
    const addresses: { address: string; name?: string }[] = [];
    const parts = addressString.split(",").map(s => s.trim());
    
    for (const part of parts) {
      const match = part.match(/^(.+?)\s*<(.+?)>$/);
      if (match) {
        addresses.push({ name: match[1].trim(), address: match[2].toLowerCase() });
      } else if (part.includes("@")) {
        addresses.push({ address: part.toLowerCase() });
      }
    }
    
    return addresses;
  }

  // Extract recipients from parsed email
  private extractRecipients(parsedEmail: any, emailId: number): Recipient[] {
    const recipients: Recipient[] = [];
    
    // Process TO recipients
    if (parsedEmail.to) {
      const toAddresses = Array.isArray(parsedEmail.to) 
        ? parsedEmail.to 
        : [parsedEmail.to];
      
      for (const to of toAddresses) {
        if (to.value) {
          for (const addr of to.value) {
            recipients.push({
              email_id: emailId,
              type: "to",
              address: addr.address?.toLowerCase() || "",
              name: addr.name
            });
          }
        } else if (typeof to === "string") {
          const parsed = this.parseEmailAddress(to);
          for (const addr of parsed) {
            recipients.push({
              email_id: emailId,
              type: "to",
              ...addr
            });
          }
        }
      }
    }
    
    // Process CC recipients
    if (parsedEmail.cc) {
      const ccAddresses = Array.isArray(parsedEmail.cc) 
        ? parsedEmail.cc 
        : [parsedEmail.cc];
      
      for (const cc of ccAddresses) {
        if (cc.value) {
          for (const addr of cc.value) {
            recipients.push({
              email_id: emailId,
              type: "cc",
              address: addr.address?.toLowerCase() || "",
              name: addr.name
            });
          }
        } else if (typeof cc === "string") {
          const parsed = this.parseEmailAddress(cc);
          for (const addr of parsed) {
            recipients.push({
              email_id: emailId,
              type: "cc",
              ...addr
            });
          }
        }
      }
    }
    
    // Process BCC recipients (rarely available in received emails)
    if (parsedEmail.bcc) {
      const bccAddresses = Array.isArray(parsedEmail.bcc) 
        ? parsedEmail.bcc 
        : [parsedEmail.bcc];
      
      for (const bcc of bccAddresses) {
        if (bcc.value) {
          for (const addr of bcc.value) {
            recipients.push({
              email_id: emailId,
              type: "bcc",
              address: addr.address?.toLowerCase() || "",
              name: addr.name
            });
          }
        } else if (typeof bcc === "string") {
          const parsed = this.parseEmailAddress(bcc);
          for (const addr of parsed) {
            recipients.push({
              email_id: emailId,
              type: "bcc",
              ...addr
            });
          }
        }
      }
    }
    
    return recipients;
  }

  // Extract attachments from parsed email
  private extractAttachments(parsedEmail: any, emailId: number): Attachment[] {
    const attachments: Attachment[] = [];
    
    if (parsedEmail.attachments && Array.isArray(parsedEmail.attachments)) {
      for (const att of parsedEmail.attachments) {
        attachments.push({
          email_id: emailId,
          filename: att.filename || "unnamed",
          content_type: att.contentType,
          size_bytes: att.size || 0,
          content_id: att.contentId,
          is_inline: att.contentDisposition === "inline"
        });
      }
    }
    
    return attachments;
  }

  // Sync emails from IMAP to database
  async syncEmails(options: SyncOptions = {}): Promise<{
    synced: number;
    skipped: number;
    errors: number;
  }> {
    const stats = { synced: 0, skipped: 0, errors: 0 };
    
    try {
      console.log("🔄 Starting email sync...");
      await this.emailSearcher.connect();
      
      // Open the specified folder or default to INBOX
      if (options.folder && options.folder !== "INBOX") {
        console.log(`📂 Opening folder: ${options.folder}`);
        await this.emailSearcher.openFolder(options.folder);
      } else {
        await this.emailSearcher.openInbox();
      }
      
      // Build search criteria
      const criteria: any[] = [];
      
      // Date range filters
      if (options.since) {
        criteria.push(["SINCE", options.since]);
      } else {
        // Default to last 30 days
        const thirtyDaysAgo = new Date();
        thirtyDaysAgo.setDate(thirtyDaysAgo.getDate() - 30);
        criteria.push(["SINCE", thirtyDaysAgo]);
      }
      
      if (options.before) {
        criteria.push(["BEFORE", options.before]);
      }
      
      // Sender filter
      if (options.from) {
        criteria.push(["FROM", options.from]);
      }
      
      // Recipient filter
      if (options.to) {
        criteria.push(["TO", options.to]);
      }
      
      // Subject filter
      if (options.subject) {
        criteria.push(["SUBJECT", options.subject]);
      }
      
      // Read status filter
      if (options.unreadOnly) {
        criteria.push("UNSEEN");
      }
      
      // Starred/Flagged filter
      if (options.starredOnly) {
        criteria.push("FLAGGED");
      }
      
      // Text search
      if (options.searchText) {
        criteria.push(["TEXT", options.searchText]);
      }
      
      // Size filters
      if (options.sizeMin) {
        criteria.push(["LARGER", options.sizeMin]);
      }
      
      if (options.sizeMax) {
        criteria.push(["SMALLER", options.sizeMax]);
      }
      
      const uids = await this.emailSearcher.searchEmails(criteria);
      console.log(`📧 Found ${uids.length} emails to process`);
      
      const limit = options.limit || uids.length;
      const uidsToProcess = uids.slice(0, limit);
      
      for (let i = 0; i < uidsToProcess.length; i++) {
        const uid = uidsToProcess[i];
        
        try {
          // Fetch full email
          const rawEmail = await this.emailSearcher.fetchEmail(uid);
          
          // Check if email already exists
          const existing = this.database.db.prepare(
            "SELECT id FROM emails WHERE message_id = ?"
          ).get(rawEmail.messageId);
          
          if (existing) {
            stats.skipped++;
            continue;
          }
          
          // Post-fetch filtering for attachments
          if (options.hasAttachments !== undefined) {
            const hasAttachments = (rawEmail.attachments?.length || 0) > 0;
            if (options.hasAttachments !== hasAttachments) {
              stats.skipped++;
              continue;
            }
          }
          
          // Parse email addresses
          const fromParsed = this.parseEmailAddress(rawEmail.from?.text || "");
          const fromAddress = fromParsed[0]?.address || "unknown@unknown.com";
          const fromName = fromParsed[0]?.name;
          
          // Create email record
          const emailRecord: EmailRecord = {
            message_id: rawEmail.messageId || `${uid}-${Date.now()}`,
            imap_uid: uid,
            thread_id: rawEmail.threadId ||
              (typeof rawEmail.references === 'string' ? rawEmail.references.split(" ")[0] :
               Array.isArray(rawEmail.references) ? rawEmail.references[0] : null),
            in_reply_to: rawEmail.inReplyTo,
            email_references: Array.isArray(rawEmail.references) 
              ? rawEmail.references.join(" ")
              : rawEmail.references,
            date_sent: rawEmail.date ? rawEmail.date.toISOString() : new Date().toISOString(),
            subject: rawEmail.subject,
            from_address: fromAddress,
            from_name: fromName,
            reply_to: rawEmail.replyTo?.text,
            body_text: rawEmail.text,
            body_html: rawEmail.html,
            snippet: rawEmail.text?.substring(0, 200),
            is_read: options.markAsRead || false,
            folder: options.folder || "INBOX",
            has_attachments: (rawEmail.attachments?.length || 0) > 0,
            attachment_count: rawEmail.attachments?.length || 0,
            raw_headers: JSON.stringify(rawEmail.headers || {})
          };
          
          // Extract recipients and attachments (using placeholder emailId = 0)
          const recipients = this.extractRecipients(rawEmail, 0);
          const attachments = this.extractAttachments(rawEmail, 0);
          
          // Insert into database
          this.database.insertEmail(emailRecord, recipients, attachments);
          stats.synced++;

          // Emit event to listeners
          if (this.listenersManager) {
            const emailForListener: Email = {
              messageId: emailRecord.message_id,
              from: emailRecord.from_address,
              to: recipients.filter(r => r.type === 'to').map(r => r.address).join(', '),
              subject: emailRecord.subject || '',
              body: emailRecord.body_text || '',
              date: emailRecord.date_sent.toString(),
              isRead: emailRecord.is_read || false,
              hasAttachments: emailRecord.has_attachments || false,
              labels: emailRecord.labels ? JSON.parse(emailRecord.labels) : undefined,
              uid: emailRecord.imap_uid
            };

            await this.listenersManager.checkEvent('email_received', emailForListener);
          }

          if ((i + 1) % 10 === 0) {
            console.log(`Progress: ${i + 1}/${uidsToProcess.length} emails processed`);
          }
          
        } catch (error) {
          console.error(`Error processing email ${uid}:`, error);
          stats.errors++;
        }
      }
      
    } catch (error) {
      console.error("Sync error:", error);
      throw error;
    } finally {
      this.emailSearcher.disconnect();
    }
    
    console.log(`✅ Sync complete: ${stats.synced} synced, ${stats.skipped} skipped, ${stats.errors} errors`);
    return stats;
  }

  // Sync only new emails since last sync
  async syncNewEmails(): Promise<{ synced: number; skipped: number; errors: number }> {
    // Get the most recent email date from database
    const mostRecent = this.database.db.prepare(
      "SELECT MAX(date_sent) as latest FROM emails"
    ).get() as { latest: string };

    const since = mostRecent?.latest ? new Date(mostRecent.latest) : undefined;

    return this.syncEmails({ since });
  }

  // Handle new emails arriving during IDLE monitoring
  async handleIdleNewEmails(count: number, folder: string = "INBOX"): Promise<void> {
    console.log(`📬 Handling ${count} new email(s) from IDLE notification in folder: ${folder}`);

    try {
      // Sync the most recent emails (with a small buffer in case of timing issues)
      const result = await this.syncEmails({
        folder,
        limit: count + 5, // Add buffer for potential race conditions
        since: new Date(Date.now() - 60000) // Look back 1 minute to catch any timing issues
      });

      console.log(`✅ IDLE sync complete: ${result.synced} new email(s) synced`);
    } catch (error) {
      console.error("❌ Error syncing emails from IDLE:", error);
    }
  }

  close(): void {
    this.database.close();
  }
}