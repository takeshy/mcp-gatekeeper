package db

import (
	"database/sql"
	"fmt"
	"time"
)

// APIKey represents an API key record
type APIKey struct {
	ID         int64
	Name       string
	KeyHash    string
	Status     string
	CreatedAt  time.Time
	RevokedAt  sql.NullTime
	LastUsedAt sql.NullTime
}

// CreateAPIKey creates a new API key
func (db *DB) CreateAPIKey(name, keyHash string) (*APIKey, error) {
	result, err := db.Exec(
		"INSERT INTO api_keys (name, key_hash) VALUES (?, ?)",
		name, keyHash,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get API key ID: %w", err)
	}

	return db.GetAPIKeyByID(id)
}

// GetAPIKeyByID retrieves an API key by ID
func (db *DB) GetAPIKeyByID(id int64) (*APIKey, error) {
	key := &APIKey{}
	err := db.QueryRow(`
		SELECT id, name, key_hash, status, created_at, revoked_at, last_used_at
		FROM api_keys WHERE id = ?
	`, id).Scan(
		&key.ID, &key.Name, &key.KeyHash, &key.Status,
		&key.CreatedAt, &key.RevokedAt, &key.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	return key, nil
}

// GetAPIKeyByHash retrieves an API key by its hash
func (db *DB) GetAPIKeyByHash(keyHash string) (*APIKey, error) {
	key := &APIKey{}
	err := db.QueryRow(`
		SELECT id, name, key_hash, status, created_at, revoked_at, last_used_at
		FROM api_keys WHERE key_hash = ?
	`, keyHash).Scan(
		&key.ID, &key.Name, &key.KeyHash, &key.Status,
		&key.CreatedAt, &key.RevokedAt, &key.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	return key, nil
}

// ListAPIKeys retrieves all API keys
func (db *DB) ListAPIKeys() ([]*APIKey, error) {
	rows, err := db.Query(`
		SELECT id, name, key_hash, status, created_at, revoked_at, last_used_at
		FROM api_keys ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list API keys: %w", err)
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		key := &APIKey{}
		err := rows.Scan(
			&key.ID, &key.Name, &key.KeyHash, &key.Status,
			&key.CreatedAt, &key.RevokedAt, &key.LastUsedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan API key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// RevokeAPIKey revokes an API key
func (db *DB) RevokeAPIKey(id int64) error {
	_, err := db.Exec(
		"UPDATE api_keys SET status = 'revoked', revoked_at = CURRENT_TIMESTAMP WHERE id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to revoke API key: %w", err)
	}
	return nil
}

// UpdateAPIKeyLastUsed updates the last_used_at timestamp
func (db *DB) UpdateAPIKeyLastUsed(id int64) error {
	_, err := db.Exec(
		"UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to update last used: %w", err)
	}
	return nil
}

// DeleteAPIKey deletes an API key (and its policy via CASCADE)
func (db *DB) DeleteAPIKey(id int64) error {
	_, err := db.Exec("DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}
	return nil
}
