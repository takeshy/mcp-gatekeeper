package bridge

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
)

// Server implements an HTTP bridge to stdio MCP servers
type Server struct {
	client      *Client
	router      chi.Router
	apiKey      string
	rateLimiter *RateLimiter
	mu          sync.RWMutex
}

// ServerConfig holds server configuration
type ServerConfig struct {
	// Upstream MCP server command
	Command string
	Args    []string
	Env     []string
	WorkDir string

	// Bridge settings
	APIKey          string
	Timeout         time.Duration
	RateLimit       int
	RateLimitWindow time.Duration
}

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

// NewServer creates a new bridge server
func NewServer(config *ServerConfig) (*Server, error) {
	if config.Command == "" {
		return nil, fmt.Errorf("upstream command is required")
	}

	clientConfig := &ClientConfig{
		Command:   config.Command,
		Args:      config.Args,
		Env:       config.Env,
		WorkDir:   config.WorkDir,
		Timeout:   config.Timeout,
		MaxOutput: 1024 * 1024,
	}

	if clientConfig.Timeout == 0 {
		clientConfig.Timeout = 30 * time.Second
	}

	rateLimit := config.RateLimit
	if rateLimit == 0 {
		rateLimit = 500
	}

	rateLimitWindow := config.RateLimitWindow
	if rateLimitWindow == 0 {
		rateLimitWindow = time.Minute
	}

	s := &Server{
		client:      NewClient(clientConfig),
		apiKey:      config.APIKey,
		rateLimiter: NewRateLimiter(rateLimit, rateLimitWindow),
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health check
	r.Get("/health", s.handleHealth)

	// MCP JSON-RPC endpoint
	r.Group(func(r chi.Router) {
		if s.apiKey != "" {
			r.Use(s.authMiddleware)
		}
		r.Use(s.rateLimitMiddleware)
		r.Post("/mcp", s.handleMCP)
	})

	s.router = r
}

// Start initializes the upstream connection
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start upstream: %w", err)
	}

	// Initialize the upstream MCP server
	resp, err := s.client.Initialize(ctx)
	if err != nil {
		s.client.Close()
		return fmt.Errorf("failed to initialize upstream: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[bridge] upstream initialized: %s\n", string(resp.Result))
	return nil
}

// Handler returns the HTTP handler
func (s *Server) Handler() http.Handler {
	return s.router
}

// Close closes the bridge server
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client.Close()
}

// authMiddleware handles API key authentication
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			s.writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			s.writeError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}

		if parts[1] != s.apiKey {
			s.writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware handles rate limiting
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.rateLimiter.Allow() {
			s.writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	initialized := s.client.IsInitialized()
	s.mu.RUnlock()

	status := "ok"
	if !initialized {
		status = "upstream_not_initialized"
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      status,
		"initialized": initialized,
	})
}

// handleMCP handles MCP JSON-RPC requests
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read raw request body
	var rawReq json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
		fmt.Fprintf(os.Stderr, "[bridge] failed to parse request: %v\n", err)
		s.writeJSONRPC(w, &Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		})
		return
	}

	// Check if upstream is initialized
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if !client.IsInitialized() {
		// Try to auto-initialize
		s.mu.Lock()
		if !client.IsInitialized() {
			if _, err := client.Initialize(ctx); err != nil {
				s.mu.Unlock()
				fmt.Fprintf(os.Stderr, "[bridge] failed to initialize upstream: %v\n", err)
				s.writeJSONRPC(w, &Response{
					JSONRPC: "2.0",
					Error: &RPCError{
						Code:    -32603,
						Message: "Upstream not initialized",
					},
				})
				return
			}
		}
		s.mu.Unlock()
	}

	// Parse request to check method
	var req Request
	if err := json.Unmarshal(rawReq, &req); err != nil {
		s.writeJSONRPC(w, &Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		})
		return
	}

	// Handle initialize locally (don't forward)
	if req.Method == "initialize" {
		s.handleInitialize(w, &req)
		return
	}

	// Forward to upstream
	resp, err := client.Forward(ctx, rawReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bridge] forward error: %v\n", err)
		s.writeJSONRPC(w, &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Forward error: %v", err),
			},
		})
		return
	}

	// Notification (no response)
	if resp == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.writeJSONRPC(w, resp)
}

// handleInitialize handles initialize requests locally
func (s *Server) handleInitialize(w http.ResponseWriter, req *Request) {
	// Return bridge server info, forwarding upstream capabilities
	result := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]interface{}{
			"name":    "mcp-gatekeeper-bridge",
			"version": "1.0.0",
		},
	}

	resultJSON, _ := json.Marshal(result)
	s.writeJSONRPC(w, &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	})
}

func (s *Server) writeJSONRPC(w http.ResponseWriter, resp *Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}
