import * as path from "path";

const DB_FILENAME = "emails.db";
// Always use the database directory relative to this config file
const DB_DIR = path.resolve(__dirname, ".");

export const getDatabasePath = (): string => {
  return path.join(DB_DIR, DB_FILENAME);
};

export const DATABASE_PATH = getDatabasePath();