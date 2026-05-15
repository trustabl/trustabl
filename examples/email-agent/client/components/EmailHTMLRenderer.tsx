import React, { useRef, useEffect, useState } from 'react';

interface EmailHTMLRendererProps {
  html: string;
  className?: string;
}

export function EmailHTMLRenderer({ html, className = '' }: EmailHTMLRendererProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [iframeHeight, setIframeHeight] = useState('400px');

  useEffect(() => {
    if (!iframeRef.current || !html) return;

    const iframe = iframeRef.current;

    const setupIframe = () => {
      const doc = iframe.contentDocument || iframe.contentWindow?.document;

      if (!doc) {
        console.error('Could not access iframe document');
        return;
      }

      // Clear any existing content
      doc.open();

      // Inject a base style to make emails responsive and readable
      const styledHTML = `
        <!DOCTYPE html>
        <html>
          <head>
            <meta charset="UTF-8">
            <meta name="viewport" content="width=device-width, initial-scale=1.0">
            <style>
              body {
                margin: 0;
                padding: 16px;
                font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Helvetica', 'Arial', sans-serif;
                font-size: 14px;
                line-height: 1.5;
                color: #374151;
                overflow-x: hidden;
                word-wrap: break-word;
              }
              img {
                max-width: 100% !important;
                height: auto !important;
              }
              table {
                max-width: 100% !important;
              }
              * {
                max-width: 100%;
                box-sizing: border-box;
              }
              a {
                color: #2563eb;
                text-decoration: underline;
              }
            </style>
          </head>
          <body>
            ${html}
          </body>
        </html>
      `;

      doc.write(styledHTML);
      doc.close();

      // Auto-resize iframe based on content
      const resizeIframe = () => {
        try {
          if (doc.body) {
            const height = Math.max(
              doc.body.scrollHeight,
              doc.body.offsetHeight,
              doc.documentElement.scrollHeight,
              doc.documentElement.offsetHeight
            );
            setIframeHeight(`${height + 20}px`);
          }
        } catch (e) {
          console.error('Error resizing iframe:', e);
        }
      };

      // Multiple resize attempts to handle different loading scenarios
      setTimeout(resizeIframe, 0);
      setTimeout(resizeIframe, 100);
      setTimeout(resizeIframe, 300);
      setTimeout(resizeIframe, 500);

      // Resize when images load
      const images = doc.getElementsByTagName('img');
      Array.from(images).forEach(img => {
        img.addEventListener('load', resizeIframe);
        img.addEventListener('error', resizeIframe);
      });

      // Resize on window resize
      const handleResize = () => resizeIframe();
      window.addEventListener('resize', handleResize);

      return () => {
        window.removeEventListener('resize', handleResize);
      };
    };

    // Small delay to ensure iframe is ready
    const timeoutId = setTimeout(setupIframe, 10);

    return () => {
      clearTimeout(timeoutId);
    };
  }, [html]);

  if (!html) {
    return (
      <div className={`p-4 text-gray-400 italic text-sm ${className}`}>
        No HTML content available
      </div>
    );
  }

  return (
    <iframe
      ref={iframeRef}
      className={`w-full border-0 ${className}`}
      style={{ height: iframeHeight }}
      sandbox="allow-same-origin allow-popups"
      title="Email content"
    />
  );
}
