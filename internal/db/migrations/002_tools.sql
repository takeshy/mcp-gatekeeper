-- Migration: 002_tools
-- Description: Replace policies with tools-based model

-- Add allowed_env_keys to api_keys
ALTER TABLE api_keys ADD COLUMN allowed_env_keys TEXT DEFAULT '[]';

-- Migrate allowed_env_keys from policies to api_keys
UPDATE api_keys SET allowed_env_keys = (
    SELECT allowed_env_keys FROM policies WHERE policies.api_key_id = api_keys.id
) WHERE EXISTS (
    SELECT 1 FROM policies WHERE policies.api_key_id = api_keys.id
);

-- Create tools table
CREATE TABLE IF NOT EXISTS tools (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    api_key_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    command TEXT NOT NULL,
    allowed_arg_globs TEXT DEFAULT '[]',
    sandbox TEXT NOT NULL DEFAULT 'bubblewrap' CHECK(sandbox IN ('none', 'bubblewrap', 'wasm')),
    wasm_binary TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE,
    UNIQUE(api_key_id, name)
);

-- Create index for faster tool lookups
CREATE INDEX IF NOT EXISTS idx_tools_api_key_id ON tools(api_key_id);

-- Drop policies table
DROP TABLE IF EXISTS policies;

-- Update schema version
INSERT OR REPLACE INTO schema_migrations (version) VALUES (2);
