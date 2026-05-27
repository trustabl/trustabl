// Simplified interfaces for backward compatibility
interface EmailMessage {
  id: string;
  messageId: string;
  from: string;
  to: string;
  subject: string;
  date: Date;
  body: string;
  hasAttachments: boolean;
  isRead: boolean;
  folder: string;
  labels?: string[];
}

interface GetInboxOptions {
  limit?: number;
  includeRead?: boolean;
}

interface SearchCriteria {
  query?: string;
  from?: string | string[];
  to?: string | string[];
  subject?: string;
  dateRange?: {
    start: Date;
    end: Date;
  };
  hasAttachments?: boolean;
  isUnread?: boolean;
  folders?: string[];
  labels?: string[];
  limit?: number;
  gmailQuery?: string;  // Gmail-specific native search syntax (e.g., "from:me (address OR birthday)")
}

class EmailAPI {
  private baseUrl: string;

  constructor(baseUrl: string = 'http://localhost:3000') {
    this.baseUrl = baseUrl;
  }

  async getInbox(options: GetInboxOptions = {}): Promise<EmailMessage[]> {
    const { limit = 50, includeRead = true } = options;

    try {
      const params = new URLSearchParams({
        limit: limit.toString(),
        includeRead: includeRead.toString()
      });

      const response = await fetch(`${this.baseUrl}/api/emails/inbox?${params}`, {
        method: 'GET',
        headers: {
          'Content-Type': 'application/json',
        },
      });

      if (!response.ok) {
        const error = await response.json() as any;
        throw new Error(error.error || 'Failed to fetch inbox');
      }

      const emails = await response.json() as any[];

      // Ensure dates are Date objects
      return emails.map((email: any) => ({
        ...email,
        date: new Date(email.date)
      }));
    } catch (error) {
      console.error('Error fetching inbox:', error);
      throw new Error('Failed to fetch inbox emails');
    }
  }

  async searchEmails(criteria: SearchCriteria & { headersOnly?: boolean }): Promise<EmailMessage[]> {
    try {
      const response = await fetch(`${this.baseUrl}/api/emails/search`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(criteria),
      });

      if (!response.ok) {
        const error = await response.json() as any;
        throw new Error(error.error || 'Failed to search emails');
      }

      const emails = await response.json() as any[];

      // Ensure dates are Date objects
      return emails.map((email: any) => ({
        ...email,
        date: new Date(email.date)
      }));
    } catch (error) {
      console.error('Error searching emails:', error);
      throw new Error('Failed to search emails');
    }
  }

  /**
   * Search emails using Gmail's native query syntax
   * Supports all Gmail operators: OR, has:attachment, from:me, newer_than:, etc.
   * Automatically searches in Sent Mail folder when using from:me
   * @param query - Gmail query string (e.g., 'from:me (address OR phone) newer_than:2y')
   * @param options - Additional options like headersOnly, limit
   * @returns Promise resolving to array of matching email messages
   */
  async searchWithGmailQuery(query: string, options: { headersOnly?: boolean; limit?: number } = {}): Promise<EmailMessage[]> {
    return this.searchEmails({
      gmailQuery: query,
      ...options
    });
  }

  /**
   * Get multiple emails by their IDs
   * @param ids - Array of email IDs to fetch
   * @returns Promise resolving to array of email messages
   */
  async getEmailsByIds(ids: string[]): Promise<EmailMessage[]> {
    try {
      const response = await fetch(`${this.baseUrl}/api/emails/batch`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ ids }),
      });

      if (!response.ok) {
        const error = await response.json() as any;
        throw new Error(error.error || 'Failed to fetch emails');
      }

      const result = await response.json() as any;
      const emails = result.emails || [];

      // Ensure dates are Date objects
      return emails.map((email: any) => ({
        ...email,
        date: new Date(email.date)
      }));
    } catch (error) {
      console.error('Error fetching emails by IDs:', error);
      throw new Error('Failed to fetch emails by IDs');
    }
  }
}

// Export the API class and types
export { EmailAPI, EmailMessage, GetInboxOptions, SearchCriteria };

// Example usage:
/*
const emailAPI = new EmailAPI();

// Get inbox from database
const inbox = await emailAPI.getInbox({
  limit: 20,
  includeRead: false
});

// Search emails with various criteria
const searchResults = await emailAPI.searchEmails({
  query: 'invoice',
  dateRange: {
    start: new Date('2024-01-01'),
    end: new Date()
  },
  hasAttachments: true,
  folders: ['INBOX', '[Gmail]/Sent Mail']
});

// Search for emails from the authenticated user (using 'me' as shorthand)
// This automatically searches in Sent Mail folder
const myEmails = await emailAPI.searchEmails({
  from: 'me',  // Will be replaced with the user's email address on the server
  limit: 20
});

// Use Gmail's native query syntax (recommended for complex searches)
// Automatically searches in Sent Mail when using from:me
const addressEmails = await emailAPI.searchWithGmailQuery(
  'from:me ("address" OR "street" OR "city" OR "state" OR "zip") newer_than:2y',
  { limit: 50 }
);

// Search for unread emails with attachments
const unreadWithAttachments = await emailAPI.searchWithGmailQuery(
  'is:unread has:attachment',
  { headersOnly: true }  // Faster, only fetches headers
);
*/