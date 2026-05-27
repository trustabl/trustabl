import React, { useState, useEffect } from 'react';
import { X } from 'lucide-react';
import { useScreenshotMode } from '../context/ScreenshotModeContext';
import { EmailHTMLRenderer } from './EmailHTMLRenderer';
import {
  getPlaceholderEmail,
  getPlaceholderName,
  getPlaceholderSubject,
  getPlaceholderBodyText
} from '../utils/placeholders';

interface Email {
  id: number;
  message_id: string;
  subject: string;
  from_address: string;
  from_name?: string;
  to_address?: string;
  date_sent: string;
  body_text?: string;
  body_html?: string;
  snippet?: string;
  is_read: boolean;
  is_starred: boolean;
  has_attachments: boolean;
  folder?: string;
}

interface EmailViewerProps {
  email: Email | null;
  onClose: () => void;
}

export function EmailViewer({ email, onClose }: EmailViewerProps) {
  const { isScreenshotMode } = useScreenshotMode();
  const [fullEmail, setFullEmail] = useState<Email | null>(null);
  const [loading, setLoading] = useState(false);

  // Fetch full email details when email changes
  useEffect(() => {
    if (!email) {
      setFullEmail(null);
      return;
    }

    // If email already has body_html or body_text, use it directly
    if (email.body_html || email.body_text) {
      setFullEmail(email);
      return;
    }

    // Otherwise fetch the full email
    const fetchFullEmail = async () => {
      setLoading(true);
      try {
        const response = await fetch(`/api/email/${encodeURIComponent(email.message_id)}`);
        if (response.ok) {
          const data = await response.json();
          setFullEmail(data);
        } else {
          // Fallback to the summary email if fetch fails
          setFullEmail(email);
        }
      } catch (error) {
        console.error('Error fetching full email:', error);
        setFullEmail(email);
      } finally {
        setLoading(false);
      }
    };

    fetchFullEmail();
  }, [email]);

  // Use fullEmail for rendering
  const displayEmail = fullEmail || email;

  if (!displayEmail) {
    return null;
  }

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleString('en-US', {
      weekday: 'short',
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black bg-opacity-30 z-40"
        onClick={onClose}
      />

      {/* Email Viewer Modal */}
      <div className="fixed inset-0 flex items-center justify-center z-50 p-8">
        <div className="w-full max-w-4xl h-full bg-white rounded-lg shadow-2xl flex flex-col border border-gray-300">
          {/* Header */}
          <div className="px-6 py-4 border-b border-gray-200">
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <h2 className="text-lg font-semibold mb-2">
              {isScreenshotMode ? getPlaceholderSubject(0) : (displayEmail.subject || '(No subject)')}
            </h2>
            <div className="space-y-1">
              <div className="flex items-center gap-2 text-sm">
                <span className="text-gray-500">From:</span>
                <span className="font-medium">
                  {isScreenshotMode
                    ? `${getPlaceholderName(0)} <${getPlaceholderEmail(0)}>`
                    : (displayEmail.from_name ? `${displayEmail.from_name} <${displayEmail.from_address}>` : displayEmail.from_address)}
                </span>
              </div>
              {displayEmail.to_address && (
                <div className="flex items-center gap-2 text-sm">
                  <span className="text-gray-500">To:</span>
                  <span>
                    {isScreenshotMode ? getPlaceholderEmail(1) : displayEmail.to_address}
                  </span>
                </div>
              )}
              <div className="flex items-center gap-2 text-sm text-gray-500">
                {isScreenshotMode ? 'Monday, Jan 1, 2024, 10:00 AM' : formatDate(displayEmail.date_sent)}
              </div>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1 hover:bg-gray-100 rounded transition-colors"
            title="Close"
          >
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>
      </div>

          {/* Email Body */}
          <div className="flex-1 overflow-y-auto">
        {isScreenshotMode ? (
          <pre className="whitespace-pre-wrap font-sans text-sm text-gray-900 p-6">
            {getPlaceholderBodyText()}
          </pre>
        ) : loading ? (
          <div className="flex items-center justify-center p-6">
            <div className="text-sm text-gray-500">Loading email content...</div>
          </div>
        ) : displayEmail.body_html ? (
          <EmailHTMLRenderer html={displayEmail.body_html} className="min-h-[600px]" />
        ) : (
          <pre className="whitespace-pre-wrap font-sans text-sm text-gray-900 p-6">
            {displayEmail.body_text || displayEmail.snippet || 'No content available'}
          </pre>
        )}
          </div>
        </div>
      </div>
    </>
  );
}