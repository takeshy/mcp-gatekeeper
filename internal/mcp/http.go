package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/takeshy/mcp-gatekeeper/internal/auth"
	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/executor"
	"github.com/takeshy/mcp-gatekeeper/internal/policy"
)

// RateLimiter implements a simple rate limiter
type RateLimiter struct {
	mu       sync.Mutex
	requests map[int64][]time.Time
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[int64][]time.Time),
		limit:    limit,
		window:   window,
	}
}

// Allow checks if a request is allowed for the given API key ID
func (r *RateLimiter) Allow(apiKeyID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-r.window)

	// Clean old requests
	times := r.requests[apiKeyID]
	var valid []time.Time
	for _, t := range times {
		if t.After(windowStart) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= r.limit {
		r.requests[apiKeyID] = valid
		return false
	}

	r.requests[apiKeyID] = append(valid, now)
	return true
}

// HTTPServer implements the HTTP API server
type HTTPServer struct {
	db          *db.DB
	auth        *auth.Authenticator
	evaluator   *policy.Evaluator
	executor    *executor.Executor
	rateLimiter *RateLimiter
	router      chi.Router
}

// HTTPConfig holds HTTP server configuration
type HTTPConfig struct {
	RateLimit       int
	RateLimitWindow time.Duration
	RootDir         string
}

// DefaultHTTPConfig returns the default HTTP configuration
func DefaultHTTPConfig() *HTTPConfig {
	return &HTTPConfig{
		RateLimit:       500,
		RateLimitWindow: time.Minute,
	}
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(database *db.DB, config *HTTPConfig) *HTTPServer {
	if config == nil {
		config = DefaultHTTPConfig()
	}

	execConfig := &executor.ExecutorConfig{
		Timeout:   executor.DefaultTimeout,
		MaxOutput: executor.DefaultMaxOutput,
		RootDir:   config.RootDir,
	}

	s := &HTTPServer{
		db:          database,
		auth:        auth.NewAuthenticator(database),
		evaluator:   policy.NewEvaluator(),
		executor:    executor.NewExecutor(execConfig),
		rateLimiter: NewRateLimiter(config.RateLimit, config.RateLimitWindow),
	}

	s.setupRoutes()
	return s
}

func (s *HTTPServer) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health check
	r.Get("/health", s.handleHealth)

	// API v1 routes
	r.Route("/v1", func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/tools", s.handleListTools)
		r.Post("/tools/{toolName}", s.handleToolCall)
	})

	s.router = r
}

// Handler returns the HTTP handler
func (s *HTTPServer) Handler() http.Handler {
	return s.router
}

// authMiddleware handles API key authentication
func (s *HTTPServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get API key from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			s.writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		// Parse Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			s.writeError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}
		apiKeyStr := parts[1]

		// Authenticate
		apiKey, err := s.auth.Authenticate(apiKeyStr)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "authentication error")
			return
		}
		if apiKey == nil {
			s.writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}

		// Check rate limit
		if !s.rateLimiter.Allow(apiKey.ID) {
			s.writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		// Store in context
		ctx := r.Context()
		ctx = contextWithAPIKey(ctx, apiKey)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ToolListResponse represents the list tools response
type ToolListResponse struct {
	Tools []ToolInfo `json:"tools"`
}

// ToolInfo represents tool information in the list response
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Sandbox     string `json:"sandbox"`
}

func (s *HTTPServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	apiKey := apiKeyFromContext(ctx)

	tools, err := s.db.ListToolsByAPIKeyID(apiKey.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list tools: %v", err))
		return
	}

	response := ToolListResponse{
		Tools: make([]ToolInfo, len(tools)),
	}
	for i, t := range tools {
		response.Tools[i] = ToolInfo{
			Name:        t.Name,
			Description: t.Description,
			Command:     t.Command,
			Sandbox:     string(t.Sandbox),
		}
	}

	s.writeJSON(w, http.StatusOK, response)
}

// ToolCallRequest represents the tool call request
type ToolCallRequest struct {
	Cwd  string   `json:"cwd"`
	Args []string `json:"args,omitempty"`
}

// ToolCallResponse represents the tool call response
type ToolCallResponse struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
}

func (s *HTTPServer) handleToolCall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	apiKey := apiKeyFromContext(ctx)
	toolName := chi.URLParam(r, "toolName")

	// Get tool
	tool, err := s.db.GetToolByAPIKeyAndName(apiKey.ID, toolName)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get tool: %v", err))
		return
	}
	if tool == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("tool not found: %s", toolName))
		return
	}

	var req ToolCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Cwd == "" {
		s.writeError(w, http.StatusBadRequest, "cwd is required")
		return
	}

	// Evaluate policy
	decision, err := s.evaluator.EvaluateArgs(tool, req.Args)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("policy evaluation failed: %v", err))
		return
	}

	// Create audit log
	auditLog := &db.AuditLog{
		APIKeyID:          apiKey.ID,
		RequestedCwd:      req.Cwd,
		RequestedCmd:      tool.Command,
		RequestedArgs:     req.Args,
		NormalizedCwd:     req.Cwd,
		NormalizedCmdline: tool.Command + " " + strings.Join(req.Args, " "),
		MatchedRules:      decision.MatchedRules,
	}

	if !decision.Allowed {
		auditLog.Decision = db.DecisionDeny
		s.db.CreateAuditLog(auditLog)
		s.writeError(w, http.StatusForbidden, fmt.Sprintf("arguments denied by policy: %s", decision.Reason))
		return
	}

	auditLog.Decision = db.DecisionAllow

	// Filter environment variables
	filteredEnvKeys := s.evaluator.FilterEnvKeys(apiKey.AllowedEnvKeys, getEnvKeys(os.Environ()))
	filteredEnv := filterEnvByKeys(os.Environ(), filteredEnvKeys)

	// Execute command using the tool's sandbox setting
	result, err := s.executor.ExecuteWithSandbox(ctx, req.Cwd, tool.Command, req.Args, filteredEnv, tool.Sandbox, tool.WasmBinary)
	if err != nil {
		auditLog.Stderr = err.Error()
		s.db.CreateAuditLog(auditLog)
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("execution failed: %v", err))
		return
	}

	// Update audit log with result
	auditLog.Stdout = result.Stdout
	auditLog.Stderr = result.Stderr
	auditLog.ExitCode.Int64 = int64(result.ExitCode)
	auditLog.ExitCode.Valid = true
	auditLog.DurationMs.Int64 = result.DurationMs
	auditLog.DurationMs.Valid = true
	s.db.CreateAuditLog(auditLog)

	// Return response
	resp := &ToolCallResponse{
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		DurationMs: result.DurationMs,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *HTTPServer) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

// Context helpers
type contextKey string

const (
	apiKeyContextKey contextKey = "apiKey"
)

func contextWithAPIKey(ctx context.Context, apiKey *db.APIKey) context.Context {
	return context.WithValue(ctx, apiKeyContextKey, apiKey)
}

func apiKeyFromContext(ctx context.Context) *db.APIKey {
	if v := ctx.Value(apiKeyContextKey); v != nil {
		return v.(*db.APIKey)
	}
	return nil
}
