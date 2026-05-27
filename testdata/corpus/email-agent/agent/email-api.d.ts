/**
 * Email API Interface Definitions
 * This file defines the complete interface for the centralized Email API.
 * The AI should reference this file to understand available email operations.
 */

/**
 * Represents a single email message
 */
export interface EmailMessage {
  /** Unique identifier for the email */
  id: string;
  /** RFC 822 message ID */
  messageId: string;
  /** Sender email address */
  from: string;
  /** Recipient email address */
  to: string;
  /** Email subject line */
  subject: string;
  /** Date when the email was sent/received */
  date: Date;
  /** Email body content (text or HTML) */
  body: string;
  /** Whether the email has attachments */
  hasAttachments: boolean;
  /** Whether the email has been read */
  isRead: boolean;
  /** Folder/mailbox where the email is stored */
  folder: string;
  /** Optional labels/tags associated with the email */
  labels?: string[];
}

/**
 * Options for retrieving inbox emails from the local database cache
 * Note: Only recent inbox emails are stored in the database
 */
export interface GetInboxOptions {
  /** Maximum number of emails to return (default: 50) */
  limit?: number;
  /** Number of emails to skip for pagination (default: 0) */
  offset?: number;
  /** Whether to include read emails (default: true) */
  includeRead?: boolean;
  /** How to sort the results (default: 'date') */
  sortBy?: 'date' | 'sender' | 'priority';
}

/**
 * Search criteria for finding emails directly via IMAP
 * Note: All searches are performed on the IMAP server, not in the local database
 */
export interface SearchCriteria {
  /** General text query to search in subject and body */
  query?: string;
  /** Filter by sender email address(es) */
  from?: string | string[];
  /** Filter by recipient email address(es) */
  to?: string | string[];
  /** Filter by subject line text */
  subject?: string;
  /** Filter by date range */
  dateRange?: {
    /** Start date (inclusive) */
    start: Date;
    /** End date (inclusive) */
    end: Date;
  };
  /** Filter to only emails with attachments */
  hasAttachments?: boolean;
  /** Filter to only unread emails */
  isUnread?: boolean;
  /** Search within specific folders/mailboxes */
  folders?: string[];
  /** Filter by email labels */
  labels?: string[];
  /** Minimum email size in bytes */
  minSize?: number;
  /** Maximum email size in bytes */
  maxSize?: number;
  /**
   * Gmail-specific native search syntax using X-GM-RAW
   * Supports all Gmail search operators like OR, has:attachment, is:unread, etc.
   * Example: "from:me (address OR birthday OR phone)"
   * If provided, this takes priority over other search criteria
   */
  gmailQuery?: string;
}


/**
 * Main Email API class providing centralized email operations
 * Uses default configuration from environment or config files
 */
export declare class EmailAPI {
  /**
   * Creates a new EmailAPI instance with default configuration
   */
  constructor();

  /**
   * Initialize the API by setting up database and connections
   * Must be called before using other methods
   * @returns Promise that resolves when initialization is complete
   */
  init(): Promise<void>;

  /**
   * Retrieve recent inbox emails from the local database cache
   * Fast retrieval of cached inbox emails without IMAP connection
   * Note: Only returns emails previously synced to the database via sync()
   * @param options - Filtering and pagination options
   * @returns Promise resolving to array of email messages
   * @example
   * const emails = await api.getInbox({ limit: 20, includeRead: false });
   */
  getInbox(options?: GetInboxOptions): Promise<EmailMessage[]>;

  /**
   * Search emails using IMAP server with advanced criteria
   * Always performs real-time search directly on the mail server
   * Results are NOT cached in the local database
   * @param criteria - Search filters and parameters
   * @returns Promise resolving to array of matching email messages
   * @example
   * const results = await api.searchEmails({
   *   query: 'invoice',
   *   dateRange: { start: new Date('2024-01-01'), end: new Date() },
   *   hasAttachments: true
   * });
   */
  searchEmails(criteria: SearchCriteria): Promise<EmailMessage[]>;

  /**
   * Search emails using Gmail's native query syntax
   * Supports all Gmail operators: OR, has:attachment, from:me, newer_than:, etc.
   * Automatically searches in Sent Mail folder when using from:me
   * @param query - Gmail query string (e.g., 'from:me (address OR phone) newer_than:2y')
   * @param options - Additional options like headersOnly and limit
   * @returns Promise resolving to array of matching email messages
   * @example
   * const results = await api.searchWithGmailQuery(
   *   'from:me ("address" OR "city") newer_than:2y',
   *   { limit: 50 }
   * );
   */
  searchWithGmailQuery(query: string, options?: { headersOnly?: boolean; limit?: number }): Promise<EmailMessage[]>;

  /**
   * Synchronize recent inbox emails from IMAP server to local database
   * Only syncs emails from the INBOX folder for the specified time period
   * @param days - Number of days of emails to sync (default: 7)
   * @returns Promise that resolves when sync is complete
   */
  sync(days?: number): Promise<void>;

  /**
   * Close all connections and clean up resources
   * Should be called when done using the API
   * @returns Promise that resolves when cleanup is complete
   */
  close(): Promise<void>;
}

/**
 * Usage Example:
 *
 * ```typescript
 * import { EmailAPI } from './email-api';
 *
 * const api = new EmailAPI();
 *
 * await api.init();
 *
 * // Sync recent inbox emails to local cache
 * await api.sync(7); // Last 7 days
 *
 * // Get recent unread emails from local cache
 * const inbox = await api.getInbox({
 *   limit: 10,
 *   includeRead: false
 * });
 *
 * // Search for specific emails directly on IMAP server
 * const searchResults = await api.searchEmails({
 *   from: 'boss@company.com',
 *   dateRange: {
 *     start: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000),
 *     end: new Date()
 *   }
 * });
 *
 * // Clean up when done
 * await api.close();
 * ```
 */