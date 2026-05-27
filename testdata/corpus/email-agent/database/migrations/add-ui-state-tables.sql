-- Migration: Add UI State and Component Instance tables
-- Date: 2025-11-06
-- Description: Adds tables for persistent UI state management and component tracking

-- Table for storing UI state data
CREATE TABLE IF NOT EXISTS ui_states (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    state_id TEXT UNIQUE NOT NULL,        -- Unique identifier for the state (e.g., "financial_dashboard", "task_board")
    data_json TEXT NOT NULL,               -- JSON serialized state data
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Index for fast state lookups
CREATE INDEX IF NOT EXISTS idx_ui_states_state_id ON ui_states(state_id);
CREATE INDEX IF NOT EXISTS idx_ui_states_updated_at ON ui_states(updated_at);

-- Table for tracking component instances
CREATE TABLE IF NOT EXISTS component_instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id TEXT UNIQUE NOT NULL,     -- Unique identifier for this component instance
    component_id TEXT NOT NULL,            -- Component template ID
    state_id TEXT NOT NULL,                -- Which UI state this component uses
    session_id TEXT,                       -- Optional session scope
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for component instance lookups
CREATE INDEX IF NOT EXISTS idx_component_instances_instance_id ON component_instances(instance_id);
CREATE INDEX IF NOT EXISTS idx_component_instances_component_id ON component_instances(component_id);
CREATE INDEX IF NOT EXISTS idx_component_instances_state_id ON component_instances(state_id);
CREATE INDEX IF NOT EXISTS idx_component_instances_session_id ON component_instances(session_id);

-- Trigger to automatically update updated_at timestamp
CREATE TRIGGER IF NOT EXISTS update_ui_states_timestamp
AFTER UPDATE ON ui_states
FOR EACH ROW
BEGIN
    UPDATE ui_states SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- Display migration status
SELECT 'Migration completed: UI state and component instance tables created' AS status;
