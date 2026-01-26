package bridge

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
	"github.com/takeshy/mcp-gatekeeper/internal/version"
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

// RateLimiter implements a sliding window rate limiter using a ring buffer
type RateLimiter struct {
	mu         sync.Mutex
	timestamps []int64 // Ring buffer of unix nano timestamps
	head       int     // Next write position
	count      int     // Current number of valid entries
	limit      int
	windowNano int64
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		timestamps: make([]int64, limit),
		limit:      limit,
		windowNano: window.Nanoseconds(),
	}
}

// Allow checks if a request is allowed using a sliding window algorithm
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixNano()
	windowStart := now - r.windowNano

	// Count valid (non-expired) entries
	validCount := 0
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + r.limit) % r.limit
		if r.timestamps[idx] > windowStart {
			validCount++
		}
	}

	if validCount >= r.limit {
		return false
	}

	// Add new timestamp to ring buffer
	r.timestamps[r.head] = now
	r.head = (r.head + 1) % r.limit
	if r.count < r.limit {
		r.count++
	}

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

		if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.apiKey)) != 1 {
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

	// Parse request to check method first
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

	// Handle initialize locally (don't forward, don't require upstream)
	if req.Method == "initialize" {
		s.handleInitialize(w, &req)
		return
	}

	// Check if upstream is initialized for other methods
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
					ID:      req.ID,
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
		"protocolVersion": version.MCPProtocolVersion,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]interface{}{
			"name":    "mcp-gatekeeper-bridge",
			"version": version.Version,
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
