package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Decision represents the allow/deny decision
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// AuditLog represents an audit log record
type AuditLog struct {
	ID                int64
	APIKeyID          int64
	RequestedCwd      string
	RequestedCmd      string
	RequestedArgs     []string
	NormalizedCwd     string
	NormalizedCmdline string
	Decision          Decision
	MatchedRules      []string
	Stdout            string
	Stderr            string
	ExitCode          sql.NullInt64
	DurationMs        sql.NullInt64
	CreatedAt         time.Time
}

// CreateAuditLog creates a new audit log entry
func (db *DB) CreateAuditLog(log *AuditLog) (*AuditLog, error) {
	requestedArgsJSON, err := json.Marshal(log.RequestedArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal requested_args: %w", err)
	}
	matchedRulesJSON, err := json.Marshal(log.MatchedRules)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal matched_rules: %w", err)
	}

	result, err := db.Exec(`
		INSERT INTO audit_logs (
			api_key_id, requested_cwd, requested_cmd, requested_args,
			normalized_cwd, normalized_cmdline, decision, matched_rules,
			stdout, stderr, exit_code, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		log.APIKeyID, log.RequestedCwd, log.RequestedCmd, string(requestedArgsJSON),
		log.NormalizedCwd, log.NormalizedCmdline, string(log.Decision), string(matchedRulesJSON),
		log.Stdout, log.Stderr, log.ExitCode, log.DurationMs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit log: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get audit log ID: %w", err)
	}

	log.ID = id
	return log, nil
}

// GetAuditLogByID retrieves an audit log by ID
func (db *DB) GetAuditLogByID(id int64) (*AuditLog, error) {
	var (
		requestedArgsJSON string
		matchedRulesJSON  string
		decision          string
	)

	log := &AuditLog{}
	err := db.QueryRow(`
		SELECT id, api_key_id, requested_cwd, requested_cmd, requested_args,
			normalized_cwd, normalized_cmdline, decision, matched_rules,
			stdout, stderr, exit_code, duration_ms, created_at
		FROM audit_logs WHERE id = ?
	`, id).Scan(
		&log.ID, &log.APIKeyID, &log.RequestedCwd, &log.RequestedCmd, &requestedArgsJSON,
		&log.NormalizedCwd, &log.NormalizedCmdline, &decision, &matchedRulesJSON,
		&log.Stdout, &log.Stderr, &log.ExitCode, &log.DurationMs, &log.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get audit log: %w", err)
	}

	log.Decision = Decision(decision)

	if err := json.Unmarshal([]byte(requestedArgsJSON), &log.RequestedArgs); err != nil {
		return nil, fmt.Errorf("failed to parse requested_args: %w", err)
	}
	if err := json.Unmarshal([]byte(matchedRulesJSON), &log.MatchedRules); err != nil {
		return nil, fmt.Errorf("failed to parse matched_rules: %w", err)
	}

	return log, nil
}

// ListAuditLogs retrieves audit logs with pagination
func (db *DB) ListAuditLogs(limit, offset int) ([]*AuditLog, error) {
	rows, err := db.Query(`
		SELECT id, api_key_id, requested_cwd, requested_cmd, requested_args,
			normalized_cwd, normalized_cmdline, decision, matched_rules,
			stdout, stderr, exit_code, duration_ms, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var (
			requestedArgsJSON string
			matchedRulesJSON  string
			decision          string
		)
		log := &AuditLog{}
		err := rows.Scan(
			&log.ID, &log.APIKeyID, &log.RequestedCwd, &log.RequestedCmd, &requestedArgsJSON,
			&log.NormalizedCwd, &log.NormalizedCmdline, &decision, &matchedRulesJSON,
			&log.Stdout, &log.Stderr, &log.ExitCode, &log.DurationMs, &log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		log.Decision = Decision(decision)

		if err := json.Unmarshal([]byte(requestedArgsJSON), &log.RequestedArgs); err != nil {
			return nil, fmt.Errorf("failed to parse requested_args: %w", err)
		}
		if err := json.Unmarshal([]byte(matchedRulesJSON), &log.MatchedRules); err != nil {
			return nil, fmt.Errorf("failed to parse matched_rules: %w", err)
		}

		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// ListAuditLogsByAPIKey retrieves audit logs for a specific API key
func (db *DB) ListAuditLogsByAPIKey(apiKeyID int64, limit, offset int) ([]*AuditLog, error) {
	rows, err := db.Query(`
		SELECT id, api_key_id, requested_cwd, requested_cmd, requested_args,
			normalized_cwd, normalized_cmdline, decision, matched_rules,
			stdout, stderr, exit_code, duration_ms, created_at
		FROM audit_logs WHERE api_key_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?
	`, apiKeyID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var (
			requestedArgsJSON string
			matchedRulesJSON  string
			decision          string
		)
		log := &AuditLog{}
		err := rows.Scan(
			&log.ID, &log.APIKeyID, &log.RequestedCwd, &log.RequestedCmd, &requestedArgsJSON,
			&log.NormalizedCwd, &log.NormalizedCmdline, &decision, &matchedRulesJSON,
			&log.Stdout, &log.Stderr, &log.ExitCode, &log.DurationMs, &log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		log.Decision = Decision(decision)

		if err := json.Unmarshal([]byte(requestedArgsJSON), &log.RequestedArgs); err != nil {
			return nil, fmt.Errorf("failed to parse requested_args: %w", err)
		}
		if err := json.Unmarshal([]byte(matchedRulesJSON), &log.MatchedRules); err != nil {
			return nil, fmt.Errorf("failed to parse matched_rules: %w", err)
		}

		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// CountAuditLogs returns the total count of audit logs
func (db *DB) CountAuditLogs() (int64, error) {
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM audit_logs").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count audit logs: %w", err)
	}
	return count, nil
}

// CountAuditLogsByAPIKey returns the count of audit logs for a specific API key
func (db *DB) CountAuditLogsByAPIKey(apiKeyID int64) (int64, error) {
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE api_key_id = ?", apiKeyID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count audit logs: %w", err)
	}
	return count, nil
}

// UpdateAuditLogResult updates the execution result of an audit log
func (db *DB) UpdateAuditLogResult(id int64, stdout, stderr string, exitCode int, durationMs int64) error {
	_, err := db.Exec(`
		UPDATE audit_logs SET stdout = ?, stderr = ?, exit_code = ?, duration_ms = ?
		WHERE id = ?
	`, stdout, stderr, exitCode, durationMs, id)
	if err != nil {
		return fmt.Errorf("failed to update audit log result: %w", err)
	}
	return nil
}
