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

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/executor"
	"github.com/takeshy/mcp-gatekeeper/internal/oauth"
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
	expectedAPIKey string         // Expected API key for authentication
	db             *db.DB         // Optional database for audit logging
	oauthHandler   *oauth.Handler // Optional OAuth handler
}

// HTTPConfig holds HTTP server configuration
type HTTPConfig struct {
	RateLimit       int
	RateLimitWindow time.Duration
	RootDir         string
	WasmDir         string
	APIKey          string // Expected API key for authentication (optional)
	DB              *db.DB // Optional database for audit logging
	EnableOAuth     bool   // Enable OAuth authentication (requires DB)
	OAuthIssuer     string // OAuth issuer URL (optional, auto-detected if empty)
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
		db:             config.DB,
	}

	// Initialize OAuth handler if enabled and DB is available
	if config.EnableOAuth && config.DB != nil {
		s.oauthHandler = oauth.NewHandler(config.DB, config.OAuthIssuer)
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

	// OAuth endpoints (if enabled)
	if s.oauthHandler != nil {
		r.Mount("/", s.oauthHandler.Router())
	}

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

// authMiddleware handles API key and OAuth token authentication
func (s *HTTPServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If no API key is configured and OAuth is not enabled, skip authentication
		if s.expectedAPIKey == "" && s.oauthHandler == nil {
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
			s.maybeSetWWWAuthenticate(w, r)
			s.writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		// Parse Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			s.maybeSetWWWAuthenticate(w, r)
			s.writeError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}
		token := parts[1]

		// Try API key authentication first (if configured)
		authenticated := false
		if s.expectedAPIKey != "" {
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.expectedAPIKey)) == 1 {
				authenticated = true
			}
		}

		// Try OAuth token authentication (if enabled and API key didn't match)
		if !authenticated && s.oauthHandler != nil {
			client, err := s.oauthHandler.ValidateAccessToken(r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[WARN] OAuth token validation error: %v\n", err)
			}
			if client != nil {
				authenticated = true
			}
		}

		if !authenticated {
			s.maybeSetWWWAuthenticate(w, r)
			s.writeError(w, http.StatusUnauthorized, "invalid credentials")
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
	case "resources/list":
		resp = s.handleMCPResourcesList(&req)
	case "resources/read":
		resp = s.handleMCPResourcesRead(&req)
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
	caps := ServerCapabilities{
		Tools: &ToolsCapability{
			ListChanged: false,
		},
	}

	// Add resources capability if any tool has UI enabled
	if s.hasUIEnabledTools() {
		caps.Resources = &ResourcesCapability{
			Subscribe:   false,
			ListChanged: false,
		}
	}

	if s.oauthHandler != nil {
		caps.Extensions = map[string]map[string]interface{}{
			"io.modelcontextprotocol/oauth-client-credentials": {},
		}
	}

	result := &InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    caps,
		ServerInfo: ServerInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}
	return NewResponse(req.ID, result)
}

func (s *HTTPServer) maybeSetWWWAuthenticate(w http.ResponseWriter, r *http.Request) {
	if s.oauthHandler == nil {
		return
	}

	baseURL := s.requestBaseURL(r)
	metadataURL := baseURL + "/.well-known/oauth-protected-resource" + r.URL.Path
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s"`, metadataURL))
}

func (s *HTTPServer) requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}

func (s *HTTPServer) hasUIEnabledTools() bool {
	for _, t := range s.plugins.ListTools() {
		if t.UIType != "" || t.UITemplate != "" {
			return true
		}
	}
	return false
}

func (s *HTTPServer) handleMCPToolsList(req *Request) *Response {
	pluginTools := s.plugins.ListTools()

	tools := make([]Tool, 0, len(pluginTools))
	for _, t := range pluginTools {
		// Filter out tools that are not visible to the model
		if !t.IsVisibleToModel() {
			continue
		}

		// Build properties based on tool configuration
		props := map[string]Property{}
		// Only include cwd property if the tool allows changing working directory
		if !t.FixedCwd {
			props["cwd"] = Property{
				Type:        "string",
				Description: "Working directory for the command (defaults to root directory)",
			}
		}
		// Only include args property if the tool accepts arguments
		// A tool with allowed_arg_globs = [""] means it accepts no arguments
		noArgs := len(t.AllowedArgGlobs) == 1 && t.AllowedArgGlobs[0] == ""
		if !noArgs {
			props["args"] = Property{
				Type:        "array",
				Description: "Command arguments",
				Items:       &Items{Type: "string"},
			}
		}

		tools = append(tools, Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: InputSchema{
				Type:       "object",
				Properties: props,
				Required:   []string{},
			},
			Meta: BuildToolMeta(t),
		})
	}
	return NewResponse(req.ID, &ListToolsResult{Tools: tools})
}

func (s *HTTPServer) handleMCPToolsCall(ctx context.Context, req *Request) *Response {
	startTime := time.Now()
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		resp := NewErrorResponse(req.ID, InvalidParams, "Invalid params", err.Error())
		s.logAudit(req.Method, params.Name, req.Params, resp, err, startTime)
		return resp
	}

	// Look up tool by name from plugins
	tool := s.plugins.GetTool(params.Name)
	if tool == nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Tool not found: %s\n", params.Name)
		resp := NewErrorResponse(req.ID, MethodNotFound, "Tool not found", params.Name)
		s.logAudit(req.Method, params.Name, req.Params, resp, fmt.Errorf("tool not found: %s", params.Name), startTime)
		return resp
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

	// Evaluate policy (check if user-provided args are allowed)
	decision, err := s.evaluator.EvaluateArgs(tool, cmdArgs)
	if err != nil {
		resp := NewErrorResponse(req.ID, InternalError, "Policy evaluation failed", err.Error())
		s.logAudit(req.Method, params.Name, req.Params, resp, err, startTime)
		return resp
	}

	if !decision.Allowed {
		fmt.Fprintf(os.Stderr, "[WARN] Arguments denied by policy: %s\n", decision.Reason)
		resp := NewErrorResponse(req.ID, PolicyDenied, "Arguments denied by policy", decision.Reason)
		s.logAudit(req.Method, params.Name, req.Params, resp, fmt.Errorf("policy denied: %s", decision.Reason), startTime)
		return resp
	}

	// Filter environment variables
	filteredEnvKeys := s.evaluator.FilterEnvKeys(s.plugins.AllowedEnvKeys, getEnvKeys(os.Environ()))
	filteredEnv := filterEnvByKeys(os.Environ(), filteredEnvKeys)

	// Prepend args_prefix if defined (after policy evaluation)
	if len(tool.ArgsPrefix) > 0 {
		cmdArgs = append(tool.ArgsPrefix, cmdArgs...)
	}

	// Execute command using the tool's sandbox setting
	result, err := s.executor.ExecuteWithSandbox(ctx, cwd, tool.Command, cmdArgs, filteredEnv, tool.Sandbox, tool.WasmBinary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Execution failed: %v\n", err)
		resp := NewErrorResponse(req.ID, ExecutionFailed, "Execution failed", err.Error())
		s.logAudit(req.Method, params.Name, req.Params, resp, err, startTime)
		return resp
	}

	// Return MCP-formatted result
	content := []Content{
		{
			Type: "text",
			Text: result.Stdout,
		},
	}

	resp := NewResponse(req.ID, &CallToolResult{
		Content: content,
		IsError: result.ExitCode != 0,
		Metadata: &ResultMetadata{
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
		},
		Meta: BuildResultMeta(tool, result.Stdout),
	})
	s.logAudit(req.Method, params.Name, req.Params, resp, nil, startTime)
	return resp
}

func (s *HTTPServer) handleMCPResourcesList(req *Request) *Response {
	// List UI resources for tools that have UI enabled
	pluginTools := s.plugins.ListTools()

	var resources []Resource
	for _, t := range pluginTools {
		if t.UIType != "" || t.UITemplate != "" {
			resources = append(resources, Resource{
				URI:         UIResourceURI(t.Name),
				Name:        fmt.Sprintf("%s UI", t.Name),
				Description: fmt.Sprintf("Interactive UI for %s tool", t.Name),
				MimeType:    "text/html",
				Meta:        BuildResourceMeta(t),
			})
		}
	}

	return NewResponse(req.ID, &ListResourcesResult{Resources: resources})
}

func (s *HTTPServer) handleMCPResourcesRead(req *Request) *Response {
	var params ReadResourceParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid params", err.Error())
	}

	// Parse ui:// URI
	if !strings.HasPrefix(params.URI, "ui://") {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid resource URI", "Only ui:// URIs are supported")
	}

	// Extract tool name and query string
	uriPath := strings.TrimPrefix(params.URI, "ui://")
	parts := strings.SplitN(uriPath, "?", 2)
	pathParts := strings.Split(parts[0], "/")
	if len(pathParts) < 1 {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid resource URI", "Missing tool name")
	}
	toolName := pathParts[0]

	// Get the tool from plugins
	tool := s.plugins.GetTool(toolName)
	if tool == nil {
		return NewErrorResponse(req.ID, MethodNotFound, "Tool not found", toolName)
	}

	// Check if tool has UI enabled
	if tool.UIType == "" && tool.UITemplate == "" {
		return NewErrorResponse(req.ID, InvalidParams, "Tool has no UI", toolName)
	}

	// Extract data from query string
	var encodedData string
	if len(parts) > 1 {
		queryParts := strings.SplitN(parts[1], "=", 2)
		if len(queryParts) == 2 && queryParts[0] == "data" {
			encodedData = queryParts[1]
		}
	}

	// Generate HTML
	htmlContent, err := GenerateUIHTML(tool, encodedData)
	if err != nil {
		return NewErrorResponse(req.ID, InternalError, "Failed to generate UI", err.Error())
	}

	return NewResponse(req.ID, &ReadResourceResult{
		Contents: []ResourceContent{
			{
				URI:      params.URI,
				MimeType: "text/html",
				Text:     htmlContent,
			},
		},
	})
}

// logAudit logs an audit entry if database is configured
func (s *HTTPServer) logAudit(method string, toolName string, params interface{}, resp *Response, err error, startTime time.Time) {
	if s.db == nil {
		return
	}
	if logErr := s.db.LogAudit(db.AuditModeHTTP, method, toolName, params, resp, err, startTime); logErr != nil {
		fmt.Fprintf(os.Stderr, "[WARN] Failed to log audit: %v\n", logErr)
	}
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
