# Database Migrations

This directory contains SQL migration files for evolving the email database schema.

## Running Migrations

### Option 1: Manual Migration (SQLite CLI)

```bash
sqlite3 database/emails.db < database/migrations/add-imap-uid.sql
```

### Option 2: Using Bun

```bash
bun run -e "
import { Database } from 'bun:sqlite';
import { readFileSync } from 'fs';

const db = new Database('database/emails.db');
const migration = readFileSync('database/migrations/add-imap-uid.sql', 'utf-8');
db.exec(migration);
console.log('Migration complete!');
"
```

### Option 3: Programmatic (TypeScript)

```typescript
import { Database } from "bun:sqlite";
import { readFileSync } from "fs";

const db = new Database("database/emails.db");
const migration = readFileSync("database/migrations/add-imap-uid.sql", "utf-8");
db.exec(migration);
console.log("Migration complete!");
```

## Available Migrations

### add-imap-uid.sql
- **Date**: 2025-10-28
- **Description**: Adds `imap_uid` column to the `emails` table for storing IMAP server UIDs
- **Purpose**: Enables direct IMAP operations (star, archive, mark as read) without searching
- **Impact**: Existing emails will have NULL for `imap_uid`, new emails will have UIDs

## Migration Notes

- Migrations are **additive** - they don't delete data
- Existing emails will have `NULL` for `imap_uid`
- New emails synced after migration will have UIDs populated
- The index on `imap_uid` improves lookup performance for IMAP operations
