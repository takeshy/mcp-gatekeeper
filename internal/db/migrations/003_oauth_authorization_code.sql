ALTER TABLE oauth_clients ADD COLUMN redirect_uris TEXT NOT NULL DEFAULT '[]';
ALTER TABLE oauth_clients ADD COLUMN scopes TEXT NOT NULL DEFAULT '[]';

ALTER TABLE oauth_tokens ADD COLUMN subject TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_tokens ADD COLUMN scope TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_tokens ADD COLUMN resource TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code_hash TEXT NOT NULL UNIQUE,
    oauth_client_id INTEGER NOT NULL,
    redirect_uri TEXT NOT NULL,
    subject TEXT NOT NULL,
    scope TEXT NOT NULL,
    resource TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (oauth_client_id) REFERENCES oauth_clients(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_oauth_authorization_codes_hash
    ON oauth_authorization_codes (code_hash);
