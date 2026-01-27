package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/takeshy/mcp-gatekeeper/internal/executor"
	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
	"github.com/takeshy/mcp-gatekeeper/internal/policy"
)

// RateLimiter implements a simple rate limiter
type RateLimiter struct {
	mu       sync.Mutex
	requests []time.Time
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make([]time.Time, 0),
		limit:    limit,
		window:   window,
	}
}

// Allow checks if a request is allowed
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-r.window)

	// Clean old requests
	var valid []time.Time
	for _, t := range r.requests {
		if t.After(windowStart) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= r.limit {
		r.requests = valid
		return false
	}

	r.requests = append(valid, now)
	return true
}

// HTTPServer implements the HTTP API server
type HTTPServer struct {
	plugins        *plugin.Config
	evaluator      *policy.Evaluator
	executor       *executor.Executor
	rateLimiter    *RateLimiter
	router         chi.Router
	rootDir        string
	expectedAPIKey string // Expected API key for authentication
}

// HTTPConfig holds HTTP server configuration
type HTTPConfig struct {
	RateLimit       int
	RateLimitWindow time.Duration
	RootDir         string
	WasmDir         string
	APIKey          string // Expected API key for authentication (optional)
}

// DefaultHTTPConfig returns the default HTTP configuration
func DefaultHTTPConfig() *HTTPConfig {
	return &HTTPConfig{
		RateLimit:       500,
		RateLimitWindow: time.Minute,
	}
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(plugins *plugin.Config, config *HTTPConfig) (*HTTPServer, error) {
	if config == nil {
		config = DefaultHTTPConfig()
	}

	execConfig := &executor.ExecutorConfig{
		Timeout:   executor.DefaultTimeout,
		MaxOutput: executor.DefaultMaxOutput,
		RootDir:   config.RootDir,
		WasmDir:   config.WasmDir,
	}

	s := &HTTPServer{
		plugins:        plugins,
		evaluator:      policy.NewEvaluator(),
		executor:       executor.NewExecutor(execConfig),
		rateLimiter:    NewRateLimiter(config.RateLimit, config.RateLimitWindow),
		rootDir:        config.RootDir,
		expectedAPIKey: config.APIKey,
	}

	s.setupRoutes()
	return s, nil
}

func (s *HTTPServer) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health check
	r.Get("/health", s.handleHealth)

	// MCP JSON-RPC endpoint (with auth)
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Post("/mcp", s.handleMCP)
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
		// If no API key is configured, skip authentication
		if s.expectedAPIKey == "" {
			// Still check rate limit
			if !s.rateLimiter.Allow() {
				s.writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
			return
		}

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
		apiKey := parts[1]

		// Validate API key using constant-time comparison
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(s.expectedAPIKey)) != 1 {
			s.writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}

		// Check rate limit
		if !s.rateLimiter.Allow() {
			s.writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMCP handles MCP JSON-RPC requests
func (s *HTTPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to parse JSON-RPC request: %v\n", err)
		s.writeJSONRPC(w, NewErrorResponse(nil, ParseError, "Parse error", err.Error()))
		return
	}

	if req.JSONRPC != "2.0" {
		s.writeJSONRPC(w, NewErrorResponse(req.ID, InvalidRequest, "Invalid Request", "jsonrpc must be 2.0"))
		return
	}

	// Handle notifications (no id or null id) - don't send response
	if req.ID == nil || string(req.ID) == "null" {
		s.handleMCPNotification(&req)
		w.WriteHeader(http.StatusOK)
		return
	}

	var resp *Response
	switch req.Method {
	case "initialize":
		resp = s.handleMCPInitialize(&req)
	case "tools/list":
		resp = s.handleMCPToolsList(&req)
	case "tools/call":
		resp = s.handleMCPToolsCall(r.Context(), &req)
	case "ping":
		resp = NewResponse(req.ID, struct{}{})
	default:
		resp = NewErrorResponse(req.ID, MethodNotFound, "Method not found", req.Method)
	}

	s.writeJSONRPC(w, resp)
}

// handleMCPNotification handles MCP notifications (no response expected)
func (s *HTTPServer) handleMCPNotification(req *Request) {
	switch req.Method {
	case "notifications/initialized":
		// Client is initialized, nothing to do
	case "notifications/cancelled":
		// Request cancellation, nothing to do for now
	default:
		fmt.Fprintf(os.Stderr, "[WARN] Unknown notification: %s\n", req.Method)
	}
}

func (s *HTTPServer) handleMCPInitialize(req *Request) *Response {
	result := &InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{
				ListChanged: false,
			},
		},
		ServerInfo: ServerInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}
	return NewResponse(req.ID, result)
}

func (s *HTTPServer) handleMCPToolsList(req *Request) *Response {
	pluginTools := s.plugins.ListTools()

	tools := make([]Tool, len(pluginTools))
	for i, t := range pluginTools {
		tools[i] = Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cwd": {
						Type:        "string",
						Description: "Working directory for the command (defaults to root directory)",
					},
					"args": {
						Type:        "array",
						Description: "Command arguments",
						Items:       &Items{Type: "string"},
					},
				},
				Required: []string{},
			},
		}
	}
	return NewResponse(req.ID, &ListToolsResult{Tools: tools})
}

func (s *HTTPServer) handleMCPToolsCall(ctx context.Context, req *Request) *Response {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid params", err.Error())
	}

	// Look up tool by name from plugins
	tool := s.plugins.GetTool(params.Name)
	if tool == nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Tool not found: %s\n", params.Name)
		return NewErrorResponse(req.ID, MethodNotFound, "Tool not found", params.Name)
	}

	// Parse arguments
	cwd, _ := params.Arguments["cwd"].(string)
	var cmdArgs []string
	if argsRaw, ok := params.Arguments["args"].([]interface{}); ok {
		for _, a := range argsRaw {
			if str, ok := a.(string); ok {
				cmdArgs = append(cmdArgs, str)
			}
		}
	}

	// Default cwd to rootDir if not provided
	if cwd == "" {
		cwd = s.rootDir
	}

	// Evaluate policy (check if args are allowed)
	decision, err := s.evaluator.EvaluateArgs(tool, cmdArgs)
	if err != nil {
		return NewErrorResponse(req.ID, InternalError, "Policy evaluation failed", err.Error())
	}

	if !decision.Allowed {
		fmt.Fprintf(os.Stderr, "[WARN] Arguments denied by policy: %s\n", decision.Reason)
		return NewErrorResponse(req.ID, PolicyDenied, "Arguments denied by policy", decision.Reason)
	}

	// Filter environment variables
	filteredEnvKeys := s.evaluator.FilterEnvKeys(s.plugins.AllowedEnvKeys, getEnvKeys(os.Environ()))
	filteredEnv := filterEnvByKeys(os.Environ(), filteredEnvKeys)

	// Execute command using the tool's sandbox setting
	result, err := s.executor.ExecuteWithSandbox(ctx, cwd, tool.Command, cmdArgs, filteredEnv, tool.Sandbox, tool.WasmBinary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Execution failed: %v\n", err)
		return NewErrorResponse(req.ID, ExecutionFailed, "Execution failed", err.Error())
	}

	// Return MCP-formatted result
	content := []Content{
		{
			Type: "text",
			Text: result.Stdout,
		},
	}

	return NewResponse(req.ID, &CallToolResult{
		Content: content,
		IsError: result.ExitCode != 0,
		Metadata: &ResultMetadata{
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
		},
	})
}

func (s *HTTPServer) writeJSONRPC(w http.ResponseWriter, resp *Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (s *HTTPServer) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *HTTPServer) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}
