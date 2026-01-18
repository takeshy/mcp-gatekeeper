package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Precedence defines policy evaluation order
type Precedence string

const (
	PrecedenceDenyOverrides  Precedence = "deny_overrides"
	PrecedenceAllowOverrides Precedence = "allow_overrides"
)

// Policy represents a policy record
type Policy struct {
	ID             int64
	APIKeyID       int64
	Precedence     Precedence
	AllowedCwdGlobs []string
	AllowedCmdGlobs []string
	DeniedCmdGlobs  []string
	AllowedEnvKeys  []string
}

// CreatePolicy creates a new policy for an API key
func (db *DB) CreatePolicy(apiKeyID int64, precedence Precedence) (*Policy, error) {
	result, err := db.Exec(
		"INSERT INTO policies (api_key_id, precedence) VALUES (?, ?)",
		apiKeyID, string(precedence),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create policy: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get policy ID: %w", err)
	}

	return db.GetPolicyByID(id)
}

// GetPolicyByID retrieves a policy by ID
func (db *DB) GetPolicyByID(id int64) (*Policy, error) {
	var (
		allowedCwdGlobsJSON string
		allowedCmdGlobsJSON string
		deniedCmdGlobsJSON  string
		allowedEnvKeysJSON  string
		precedence          string
	)

	policy := &Policy{}
	err := db.QueryRow(`
		SELECT id, api_key_id, precedence, allowed_cwd_globs, allowed_cmd_globs, denied_cmd_globs, allowed_env_keys
		FROM policies WHERE id = ?
	`, id).Scan(
		&policy.ID, &policy.APIKeyID, &precedence,
		&allowedCwdGlobsJSON, &allowedCmdGlobsJSON, &deniedCmdGlobsJSON, &allowedEnvKeysJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy: %w", err)
	}

	policy.Precedence = Precedence(precedence)

	// Parse JSON arrays
	if err := json.Unmarshal([]byte(allowedCwdGlobsJSON), &policy.AllowedCwdGlobs); err != nil {
		return nil, fmt.Errorf("failed to parse allowed_cwd_globs: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedCmdGlobsJSON), &policy.AllowedCmdGlobs); err != nil {
		return nil, fmt.Errorf("failed to parse allowed_cmd_globs: %w", err)
	}
	if err := json.Unmarshal([]byte(deniedCmdGlobsJSON), &policy.DeniedCmdGlobs); err != nil {
		return nil, fmt.Errorf("failed to parse denied_cmd_globs: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedEnvKeysJSON), &policy.AllowedEnvKeys); err != nil {
		return nil, fmt.Errorf("failed to parse allowed_env_keys: %w", err)
	}

	return policy, nil
}

// GetPolicyByAPIKeyID retrieves a policy by API key ID
func (db *DB) GetPolicyByAPIKeyID(apiKeyID int64) (*Policy, error) {
	var (
		allowedCwdGlobsJSON string
		allowedCmdGlobsJSON string
		deniedCmdGlobsJSON  string
		allowedEnvKeysJSON  string
		precedence          string
	)

	policy := &Policy{}
	err := db.QueryRow(`
		SELECT id, api_key_id, precedence, allowed_cwd_globs, allowed_cmd_globs, denied_cmd_globs, allowed_env_keys
		FROM policies WHERE api_key_id = ?
	`, apiKeyID).Scan(
		&policy.ID, &policy.APIKeyID, &precedence,
		&allowedCwdGlobsJSON, &allowedCmdGlobsJSON, &deniedCmdGlobsJSON, &allowedEnvKeysJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy: %w", err)
	}

	policy.Precedence = Precedence(precedence)

	// Parse JSON arrays
	if err := json.Unmarshal([]byte(allowedCwdGlobsJSON), &policy.AllowedCwdGlobs); err != nil {
		return nil, fmt.Errorf("failed to parse allowed_cwd_globs: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedCmdGlobsJSON), &policy.AllowedCmdGlobs); err != nil {
		return nil, fmt.Errorf("failed to parse allowed_cmd_globs: %w", err)
	}
	if err := json.Unmarshal([]byte(deniedCmdGlobsJSON), &policy.DeniedCmdGlobs); err != nil {
		return nil, fmt.Errorf("failed to parse denied_cmd_globs: %w", err)
	}
	if err := json.Unmarshal([]byte(allowedEnvKeysJSON), &policy.AllowedEnvKeys); err != nil {
		return nil, fmt.Errorf("failed to parse allowed_env_keys: %w", err)
	}

	return policy, nil
}

// UpdatePolicy updates a policy
func (db *DB) UpdatePolicy(policy *Policy) error {
	allowedCwdGlobsJSON, err := json.Marshal(policy.AllowedCwdGlobs)
	if err != nil {
		return fmt.Errorf("failed to marshal allowed_cwd_globs: %w", err)
	}
	allowedCmdGlobsJSON, err := json.Marshal(policy.AllowedCmdGlobs)
	if err != nil {
		return fmt.Errorf("failed to marshal allowed_cmd_globs: %w", err)
	}
	deniedCmdGlobsJSON, err := json.Marshal(policy.DeniedCmdGlobs)
	if err != nil {
		return fmt.Errorf("failed to marshal denied_cmd_globs: %w", err)
	}
	allowedEnvKeysJSON, err := json.Marshal(policy.AllowedEnvKeys)
	if err != nil {
		return fmt.Errorf("failed to marshal allowed_env_keys: %w", err)
	}

	_, err = db.Exec(`
		UPDATE policies SET
			precedence = ?,
			allowed_cwd_globs = ?,
			allowed_cmd_globs = ?,
			denied_cmd_globs = ?,
			allowed_env_keys = ?
		WHERE id = ?
	`, string(policy.Precedence), string(allowedCwdGlobsJSON), string(allowedCmdGlobsJSON),
		string(deniedCmdGlobsJSON), string(allowedEnvKeysJSON), policy.ID)
	if err != nil {
		return fmt.Errorf("failed to update policy: %w", err)
	}
	return nil
}

// DeletePolicy deletes a policy
func (db *DB) DeletePolicy(id int64) error {
	_, err := db.Exec("DELETE FROM policies WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete policy: %w", err)
	}
	return nil
}
