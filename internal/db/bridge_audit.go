package db

import (
	"database/sql"
	"fmt"
	"time"
)

// BridgeAuditLog represents a bridge audit log record
type BridgeAuditLog struct {
	ID           int64
	Method       string
	Params       string
	Response     string
	Error        string
	RequestSize  int64
	ResponseSize int64
	DurationMs   int64
	CreatedAt    time.Time
}

// MaxLogFieldSize is the maximum size for params/response fields (100KB)
const MaxLogFieldSize = 100 * 1024

// truncateField truncates a string to MaxLogFieldSize
func truncateField(s string) string {
	if len(s) > MaxLogFieldSize {
		return s[:MaxLogFieldSize] + "...(truncated)"
	}
	return s
}

// CreateBridgeAuditLog creates a new bridge audit log entry
func (db *DB) CreateBridgeAuditLog(log *BridgeAuditLog) (*BridgeAuditLog, error) {
	// Truncate large fields to prevent DB bloat
	params := truncateField(log.Params)
	response := truncateField(log.Response)

	result, err := db.Exec(`
		INSERT INTO bridge_audit_logs (
			method, params, response, error, request_size, response_size, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		log.Method, params, response, log.Error,
		log.RequestSize, log.ResponseSize, log.DurationMs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge audit log: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get bridge audit log ID: %w", err)
	}

	log.ID = id
	return log, nil
}

// GetBridgeAuditLogByID retrieves a bridge audit log by ID
func (db *DB) GetBridgeAuditLogByID(id int64) (*BridgeAuditLog, error) {
	log := &BridgeAuditLog{}
	var params, response, errMsg sql.NullString
	var requestSize, responseSize, durationMs sql.NullInt64

	err := db.QueryRow(`
		SELECT id, method, params, response, error, request_size, response_size, duration_ms, created_at
		FROM bridge_audit_logs WHERE id = ?
	`, id).Scan(
		&log.ID, &log.Method, &params, &response, &errMsg,
		&requestSize, &responseSize, &durationMs, &log.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get bridge audit log: %w", err)
	}

	if params.Valid {
		log.Params = params.String
	}
	if response.Valid {
		log.Response = response.String
	}
	if errMsg.Valid {
		log.Error = errMsg.String
	}
	if requestSize.Valid {
		log.RequestSize = requestSize.Int64
	}
	if responseSize.Valid {
		log.ResponseSize = responseSize.Int64
	}
	if durationMs.Valid {
		log.DurationMs = durationMs.Int64
	}

	return log, nil
}

// ListBridgeAuditLogs retrieves bridge audit logs with pagination
func (db *DB) ListBridgeAuditLogs(limit, offset int) ([]*BridgeAuditLog, error) {
	rows, err := db.Query(`
		SELECT id, method, params, response, error, request_size, response_size, duration_ms, created_at
		FROM bridge_audit_logs ORDER BY created_at DESC LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list bridge audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*BridgeAuditLog
	for rows.Next() {
		log := &BridgeAuditLog{}
		var params, response, errMsg sql.NullString
		var requestSize, responseSize, durationMs sql.NullInt64

		err := rows.Scan(
			&log.ID, &log.Method, &params, &response, &errMsg,
			&requestSize, &responseSize, &durationMs, &log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan bridge audit log: %w", err)
		}

		if params.Valid {
			log.Params = params.String
		}
		if response.Valid {
			log.Response = response.String
		}
		if errMsg.Valid {
			log.Error = errMsg.String
		}
		if requestSize.Valid {
			log.RequestSize = requestSize.Int64
		}
		if responseSize.Valid {
			log.ResponseSize = responseSize.Int64
		}
		if durationMs.Valid {
			log.DurationMs = durationMs.Int64
		}

		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// ListBridgeAuditLogsByMethod retrieves bridge audit logs for a specific method
func (db *DB) ListBridgeAuditLogsByMethod(method string, limit, offset int) ([]*BridgeAuditLog, error) {
	rows, err := db.Query(`
		SELECT id, method, params, response, error, request_size, response_size, duration_ms, created_at
		FROM bridge_audit_logs WHERE method = ? ORDER BY created_at DESC LIMIT ? OFFSET ?
	`, method, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list bridge audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*BridgeAuditLog
	for rows.Next() {
		log := &BridgeAuditLog{}
		var params, response, errMsg sql.NullString
		var requestSize, responseSize, durationMs sql.NullInt64

		err := rows.Scan(
			&log.ID, &log.Method, &params, &response, &errMsg,
			&requestSize, &responseSize, &durationMs, &log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan bridge audit log: %w", err)
		}

		if params.Valid {
			log.Params = params.String
		}
		if response.Valid {
			log.Response = response.String
		}
		if errMsg.Valid {
			log.Error = errMsg.String
		}
		if requestSize.Valid {
			log.RequestSize = requestSize.Int64
		}
		if responseSize.Valid {
			log.ResponseSize = responseSize.Int64
		}
		if durationMs.Valid {
			log.DurationMs = durationMs.Int64
		}

		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// CountBridgeAuditLogs returns the total count of bridge audit logs
func (db *DB) CountBridgeAuditLogs() (int64, error) {
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM bridge_audit_logs").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count bridge audit logs: %w", err)
	}
	return count, nil
}
