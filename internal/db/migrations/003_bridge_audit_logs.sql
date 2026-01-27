-- Bridge audit logs for MCP request/response logging
CREATE TABLE IF NOT EXISTS bridge_audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    method TEXT NOT NULL,
    params TEXT,
    response TEXT,
    error TEXT,
    request_size INTEGER,
    response_size INTEGER,
    duration_ms INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bridge_audit_logs_method ON bridge_audit_logs(method);
CREATE INDEX IF NOT EXISTS idx_bridge_audit_logs_created_at ON bridge_audit_logs(created_at);
