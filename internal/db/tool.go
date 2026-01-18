package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SandboxType represents the sandbox mode for a tool
type SandboxType string

const (
	SandboxTypeNone       SandboxType = "none"
	SandboxTypeBubblewrap SandboxType = "bubblewrap"
	SandboxTypeWasm       SandboxType = "wasm"
)

// Tool represents a tool record
type Tool struct {
	ID              int64
	APIKeyID        int64
	Name            string
	Description     string
	Command         string
	AllowedArgGlobs []string
	Sandbox         SandboxType
	WasmBinary      string
	CreatedAt       time.Time
}

// CreateTool creates a new tool for an API key
func (db *DB) CreateTool(tool *Tool) (*Tool, error) {
	allowedArgGlobsJSON, err := json.Marshal(tool.AllowedArgGlobs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal allowed_arg_globs: %w", err)
	}

	result, err := db.Exec(
		`INSERT INTO tools (api_key_id, name, description, command, allowed_arg_globs, sandbox, wasm_binary)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tool.APIKeyID, tool.Name, tool.Description, tool.Command,
		string(allowedArgGlobsJSON), string(tool.Sandbox), tool.WasmBinary,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tool: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get tool ID: %w", err)
	}

	return db.GetToolByID(id)
}

// GetToolByID retrieves a tool by ID
func (db *DB) GetToolByID(id int64) (*Tool, error) {
	var (
		allowedArgGlobsJSON string
		sandbox             string
	)

	tool := &Tool{}
	err := db.QueryRow(`
		SELECT id, api_key_id, name, description, command, allowed_arg_globs, sandbox, wasm_binary, created_at
		FROM tools WHERE id = ?
	`, id).Scan(
		&tool.ID, &tool.APIKeyID, &tool.Name, &tool.Description, &tool.Command,
		&allowedArgGlobsJSON, &sandbox, &tool.WasmBinary, &tool.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tool: %w", err)
	}

	tool.Sandbox = SandboxType(sandbox)

	if err := json.Unmarshal([]byte(allowedArgGlobsJSON), &tool.AllowedArgGlobs); err != nil {
		return nil, fmt.Errorf("failed to parse allowed_arg_globs: %w", err)
	}

	return tool, nil
}

// GetToolByAPIKeyAndName retrieves a tool by API key ID and name
func (db *DB) GetToolByAPIKeyAndName(apiKeyID int64, name string) (*Tool, error) {
	var (
		allowedArgGlobsJSON string
		sandbox             string
	)

	tool := &Tool{}
	err := db.QueryRow(`
		SELECT id, api_key_id, name, description, command, allowed_arg_globs, sandbox, wasm_binary, created_at
		FROM tools WHERE api_key_id = ? AND name = ?
	`, apiKeyID, name).Scan(
		&tool.ID, &tool.APIKeyID, &tool.Name, &tool.Description, &tool.Command,
		&allowedArgGlobsJSON, &sandbox, &tool.WasmBinary, &tool.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tool: %w", err)
	}

	tool.Sandbox = SandboxType(sandbox)

	if err := json.Unmarshal([]byte(allowedArgGlobsJSON), &tool.AllowedArgGlobs); err != nil {
		return nil, fmt.Errorf("failed to parse allowed_arg_globs: %w", err)
	}

	return tool, nil
}

// ListToolsByAPIKeyID retrieves all tools for an API key
func (db *DB) ListToolsByAPIKeyID(apiKeyID int64) ([]*Tool, error) {
	rows, err := db.Query(`
		SELECT id, api_key_id, name, description, command, allowed_arg_globs, sandbox, wasm_binary, created_at
		FROM tools WHERE api_key_id = ? ORDER BY name
	`, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	defer rows.Close()

	var tools []*Tool
	for rows.Next() {
		var (
			allowedArgGlobsJSON string
			sandbox             string
		)
		tool := &Tool{}
		err := rows.Scan(
			&tool.ID, &tool.APIKeyID, &tool.Name, &tool.Description, &tool.Command,
			&allowedArgGlobsJSON, &sandbox, &tool.WasmBinary, &tool.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tool: %w", err)
		}

		tool.Sandbox = SandboxType(sandbox)

		if err := json.Unmarshal([]byte(allowedArgGlobsJSON), &tool.AllowedArgGlobs); err != nil {
			return nil, fmt.Errorf("failed to parse allowed_arg_globs: %w", err)
		}

		tools = append(tools, tool)
	}
	return tools, rows.Err()
}

// UpdateTool updates a tool
func (db *DB) UpdateTool(tool *Tool) error {
	allowedArgGlobsJSON, err := json.Marshal(tool.AllowedArgGlobs)
	if err != nil {
		return fmt.Errorf("failed to marshal allowed_arg_globs: %w", err)
	}

	_, err = db.Exec(`
		UPDATE tools SET
			name = ?,
			description = ?,
			command = ?,
			allowed_arg_globs = ?,
			sandbox = ?,
			wasm_binary = ?
		WHERE id = ?
	`, tool.Name, tool.Description, tool.Command,
		string(allowedArgGlobsJSON), string(tool.Sandbox), tool.WasmBinary, tool.ID)
	if err != nil {
		return fmt.Errorf("failed to update tool: %w", err)
	}
	return nil
}

// DeleteTool deletes a tool
func (db *DB) DeleteTool(id int64) error {
	_, err := db.Exec("DELETE FROM tools WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete tool: %w", err)
	}
	return nil
}

// DeleteToolsByAPIKeyID deletes all tools for an API key
func (db *DB) DeleteToolsByAPIKeyID(apiKeyID int64) error {
	_, err := db.Exec("DELETE FROM tools WHERE api_key_id = ?", apiKeyID)
	if err != nil {
		return fmt.Errorf("failed to delete tools: %w", err)
	}
	return nil
}
