-- Migration: Add imap_uid column to emails table
-- Date: 2025-10-28
-- Description: Adds IMAP UID field to emails table for direct IMAP operations

-- Add the imap_uid column (will be NULL for existing emails)
ALTER TABLE emails ADD COLUMN imap_uid INTEGER;

-- Create index for fast UID lookups
CREATE INDEX IF NOT EXISTS idx_emails_imap_uid ON emails(imap_uid);

-- Display migration status
SELECT 'Migration completed: imap_uid column added to emails table' AS status;
