import React, { useState, useEffect } from 'react';
import { Mail, Calendar, User, Paperclip, AlertCircle, Reply, Send, X } from 'lucide-react';
import { EmailHTMLRenderer } from './EmailHTMLRenderer';

interface EmailData {
  id: number;
  subject: string;
  from_address: string;
  from_name: string;
  date_sent: string;
  body_text: string;
  body_html?: string;
  snippet: string;
  is_read: boolean;
  is_starred: boolean;
  has_attachments: boolean;
  attachment_count: number;
  folder: string;
  recipients?: {
    to: string[];
    cc?: string[];
    bcc?: string[];
  };
}

interface EmailDisplayProps {
  emailId: string;
  compact?: boolean;
}

export function EmailDisplay({ emailId, compact = false }: EmailDisplayProps) {
  const [email, setEmail] = useState<EmailData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(!compact);
  const [showDraftResponse, setShowDraftResponse] = useState(false);
  const [draftContent, setDraftContent] = useState('');
  const [sendingDraft, setSendingDraft] = useState(false);

  useEffect(() => {
    fetchEmailData();
  }, [emailId]);

  const fetchEmailData = async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await fetch(`/api/email/${encodeURIComponent(emailId)}`);
      if (!response.ok) {
        throw new Error(`Failed to fetch email: ${response.statusText}`);
      }

      const data = await response.json();
      setEmail(data);
    } catch (err) {
      console.error('Error fetching email:', err);
      setError(err instanceof Error ? err.message : 'Failed to load email');
    } finally {
      setLoading(false);
    }
  };

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
    
    if (diffHours < 24) {
      return date.toLocaleTimeString('en-US', { 
        hour: 'numeric', 
        minute: '2-digit',
        hour12: true 
      });
    } else if (diffHours < 48) {
      return 'Yesterday';
    } else if (diffHours < 168) { // Less than a week
      return date.toLocaleDateString('en-US', { weekday: 'long' });
    } else {
      return date.toLocaleDateString('en-US', { 
        month: 'short', 
        day: 'numeric',
        year: date.getFullYear() !== now.getFullYear() ? 'numeric' : undefined
      });
    }
  };

  const handleSendDraft = async () => {
    if (!draftContent.trim() || !email) return;
    
    setSendingDraft(true);
    try {
      // Send the draft response to the server
      const response = await fetch('/api/email/send', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          to: email.from_address,
          subject: `Re: ${email.subject}`,
          body: draftContent,
          inReplyTo: emailId,
        }),
      });

      if (!response.ok) {
        throw new Error('Failed to send email');
      }

      // Clear draft and close the response UI
      setDraftContent('');
      setShowDraftResponse(false);
      
      // Show success feedback (you can enhance this with a toast notification)
      console.log('Email sent successfully');
    } catch (err) {
      console.error('Error sending email:', err);
      // You can add error handling UI here
    } finally {
      setSendingDraft(false);
    }
  };

  if (loading) {
    return (
      <div className="border border-gray-200 p-3 bg-gray-50 animate-pulse">
        <div className="h-3 bg-gray-200 w-3/4 mb-2"></div>
        <div className="h-2 bg-gray-200 w-1/2 mb-2"></div>
        <div className="h-2 bg-gray-200 w-full"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="border border-gray-300 p-3 bg-gray-50">
        <div className="flex items-center text-gray-600">
          <AlertCircle className="w-3 h-3 mr-2" />
          <span className="text-xs">{error}</span>
        </div>
      </div>
    );
  }

  if (!email) {
    return null;
  }

  return (
    <div className="border border-gray-200 bg-white">
      <div 
        className="p-3 cursor-pointer"
        onClick={() => compact && setExpanded(!expanded)}
      >
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-sm font-semibold text-gray-900">
                {email.subject || '(No Subject)'}
              </span>
              {email.is_starred && (
                <span className="text-xs">★</span>
              )}
              {email.has_attachments && (
                <Paperclip className="w-3 h-3 text-gray-400" />
              )}
            </div>
            
            <div className="flex items-center gap-3 text-xs text-gray-600">
              <span className="font-medium">{email.from_name || email.from_address}</span>
              <span>•</span>
              <span>{formatDate(email.date_sent)}</span>
              <span>•</span>
              <span className="px-1 py-0.5 bg-gray-100 text-xs uppercase">
                {email.folder}
              </span>
            </div>
          </div>
          
          {compact && (
            <button 
              className="text-gray-400 hover:text-gray-900 font-mono text-xs"
              onClick={(e) => {
                e.stopPropagation();
                setExpanded(!expanded);
              }}
            >
              {expanded ? '[-]' : '[+]'}
            </button>
          )}
        </div>

        {(!compact || expanded) && (
          <>
            <div className="border-t border-gray-200 pt-2 mt-2">
              {email.recipients?.to && (
                <div className="text-xs text-gray-600 mb-1">
                  <span className="font-semibold uppercase">To:</span> {email.recipients.to.join(', ')}
                </div>
              )}
              {email.recipients?.cc && email.recipients.cc.length > 0 && (
                <div className="text-xs text-gray-600 mb-1">
                  <span className="font-semibold uppercase">Cc:</span> {email.recipients.cc.join(', ')}
                </div>
              )}
            </div>

            <div className="mt-2 bg-white border border-gray-200">
              {email.body_html ? (
                <EmailHTMLRenderer html={email.body_html} className="min-h-[400px]" />
              ) : email.body_text ? (
                <div className="p-2 whitespace-pre-wrap font-mono text-xs">{email.body_text}</div>
              ) : (
                <div className="p-2 text-gray-400 italic text-xs">No content available</div>
              )}
            </div>

            {email.has_attachments && (
              <div className="mt-2 pt-2 border-t border-gray-200">
                <div className="flex items-center gap-2 text-xs text-gray-600">
                  <Paperclip className="w-3 h-3" />
                  <span className="uppercase">{email.attachment_count} attachment{email.attachment_count !== 1 ? 's' : ''}</span>
                </div>
              </div>
            )}

            {/* Reply Button */}
            {!showDraftResponse && (
              <div className="mt-2 pt-2 border-t border-gray-200">
                <button
                  onClick={() => setShowDraftResponse(true)}
                  className="flex items-center gap-2 px-3 py-1.5 text-xs font-semibold uppercase tracking-wider bg-gray-900 text-white hover:bg-white hover:text-gray-900 border border-gray-900 transition-colors"
                >
                  <Reply className="w-3 h-3" />
                  Reply
                </button>
              </div>
            )}

            {/* Draft Response Section */}
            {showDraftResponse && (
              <div className="mt-2 pt-2 border-t border-gray-200">
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <h4 className="text-xs font-semibold uppercase tracking-wider text-gray-700">Draft Response</h4>
                    <button
                      onClick={() => {
                        setShowDraftResponse(false);
                        setDraftContent('');
                      }}
                      className="text-gray-400 hover:text-gray-900 font-mono text-xs"
                    >
                      [X]
                    </button>
                  </div>
                  
                  <div className="space-y-1">
                    <div className="text-xs text-gray-600">
                      <span className="font-semibold uppercase">To:</span> {email.from_address}
                    </div>
                    <div className="text-xs text-gray-600">
                      <span className="font-semibold uppercase">Subject:</span> Re: {email.subject}
                    </div>
                  </div>

                  <textarea
                    value={draftContent}
                    onChange={(e) => setDraftContent(e.target.value)}
                    placeholder="Type your response here..."
                    className="w-full min-h-[120px] p-2 border border-gray-300 focus:border-gray-900 focus:outline-none text-xs font-mono"
                    disabled={sendingDraft}
                  />

                  <div className="flex items-center gap-2">
                    <button
                      onClick={handleSendDraft}
                      disabled={!draftContent.trim() || sendingDraft}
                      className="flex items-center gap-1 px-3 py-1.5 text-xs font-semibold uppercase tracking-wider bg-gray-900 text-white hover:bg-white hover:text-gray-900 border border-gray-900 disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                    >
                      <Send className="w-3 h-3" />
                      {sendingDraft ? 'Sending...' : 'Send'}
                    </button>
                    <button
                      onClick={() => {
                        setShowDraftResponse(false);
                        setDraftContent('');
                      }}
                      className="px-3 py-1.5 text-xs text-gray-600 hover:text-gray-900 uppercase"
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}