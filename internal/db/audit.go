package db

import (
	"encoding/json"
	"time"
)

// AuditMode represents the server mode for audit logging
type AuditMode string

const (
	AuditModeBridge AuditMode = "bridge"
	AuditModeHTTP   AuditMode = "http"
	AuditModeStdio  AuditMode = "stdio"
)

// AuditEntry represents a single audit log entry
type AuditEntry struct {
	ID           int64
	Mode         AuditMode
	Method       string
	ToolName     string
	Params       string
	Response     string
	Error        string
	RequestSize  int
	ResponseSize int
	DurationMs   int64
	CreatedAt    time.Time
}

// LogAudit creates an audit log entry
func (d *DB) LogAudit(mode AuditMode, method string, toolName string, params interface{}, response interface{}, err error, startTime time.Time) error {
	duration := time.Since(startTime).Milliseconds()

	var paramsJSON string
	var responseJSON string
	var errorStr string
	var requestSize int
	var responseSize int

	if params != nil {
		if data, marshalErr := json.Marshal(params); marshalErr == nil {
			paramsJSON = string(data)
			requestSize = len(data)
		}
	}

	if response != nil {
		if data, marshalErr := json.Marshal(response); marshalErr == nil {
			responseJSON = string(data)
			responseSize = len(data)
		}
	}

	if err != nil {
		errorStr = err.Error()
	}

	_, execErr := d.db.Exec(`
		INSERT INTO audit_logs (mode, method, tool_name, params, response, error, request_size, response_size, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, string(mode), method, toolName, paramsJSON, responseJSON, errorStr, requestSize, responseSize, duration)

	return execErr
}

// ListAuditLogs retrieves audit logs with optional filtering
func (d *DB) ListAuditLogs(mode AuditMode, limit int, offset int) ([]*AuditEntry, error) {
	query := `
		SELECT id, mode, method, tool_name, params, response, error, request_size, response_size, duration_ms, created_at
		FROM audit_logs
	`
	var args []interface{}

	if mode != "" {
		query += " WHERE mode = ?"
		args = append(args, string(mode))
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		entry := &AuditEntry{}
		var toolName, params, response, errorStr *string
		if err := rows.Scan(
			&entry.ID,
			&entry.Mode,
			&entry.Method,
			&toolName,
			&params,
			&response,
			&errorStr,
			&entry.RequestSize,
			&entry.ResponseSize,
			&entry.DurationMs,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		if toolName != nil {
			entry.ToolName = *toolName
		}
		if params != nil {
			entry.Params = *params
		}
		if response != nil {
			entry.Response = *response
		}
		if errorStr != nil {
			entry.Error = *errorStr
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// GetAuditStats returns statistics about audit logs
func (d *DB) GetAuditStats() (map[AuditMode]int64, error) {
	rows, err := d.db.Query(`
		SELECT mode, COUNT(*) as count
		FROM audit_logs
		GROUP BY mode
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[AuditMode]int64)
	for rows.Next() {
		var mode string
		var count int64
		if err := rows.Scan(&mode, &count); err != nil {
			return nil, err
		}
		stats[AuditMode(mode)] = count
	}

	return stats, nil
}
