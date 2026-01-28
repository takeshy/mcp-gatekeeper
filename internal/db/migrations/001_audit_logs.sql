-- Unified audit logging table for all modes (bridge, http, stdio)
CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mode TEXT NOT NULL,           -- 'bridge', 'http', 'stdio'
    method TEXT NOT NULL,         -- MCP method name (e.g., 'tools/call')
    tool_name TEXT,               -- Tool name if applicable
    params TEXT,                  -- Request params (JSON)
    response TEXT,                -- Response (JSON)
    error TEXT,                   -- Error message if any
    request_size INTEGER,         -- Request size in bytes
    response_size INTEGER,        -- Response size in bytes
    duration_ms INTEGER,          -- Execution duration
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_mode ON audit_logs(mode);
CREATE INDEX IF NOT EXISTS idx_audit_logs_method ON audit_logs(method);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
