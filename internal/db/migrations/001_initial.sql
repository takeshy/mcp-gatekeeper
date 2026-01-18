-- Migration: 001_initial
-- Description: Initial schema for MCP Gatekeeper

-- api_keys table
CREATE TABLE IF NOT EXISTS api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'revoked')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    revoked_at DATETIME,
    last_used_at DATETIME
);

-- policies table
CREATE TABLE IF NOT EXISTS policies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    api_key_id INTEGER NOT NULL UNIQUE,
    precedence TEXT NOT NULL DEFAULT 'deny_overrides' CHECK(precedence IN ('deny_overrides', 'allow_overrides')),
    allowed_cwd_globs TEXT DEFAULT '[]',
    allowed_cmd_globs TEXT DEFAULT '[]',
    denied_cmd_globs TEXT DEFAULT '[]',
    allowed_env_keys TEXT DEFAULT '[]',
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE
);

-- audit_logs table
CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    api_key_id INTEGER NOT NULL,
    requested_cwd TEXT,
    requested_cmd TEXT,
    requested_args TEXT,
    normalized_cwd TEXT,
    normalized_cmdline TEXT,
    decision TEXT NOT NULL CHECK(decision IN ('allow', 'deny')),
    matched_rules TEXT,
    stdout TEXT,
    stderr TEXT,
    exit_code INTEGER,
    duration_ms INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_audit_logs_api_key_id ON audit_logs(api_key_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_migrations (version) VALUES (1);
