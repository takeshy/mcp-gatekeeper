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
	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/version"
)

// Server implements an HTTP bridge to stdio MCP servers
type Server struct {
	client          *Client
	router          chi.Router
	apiKey          string
	rateLimiter     *RateLimiter
	maxResponseSize int
	fileStore       *FileStore
	debug           bool
	db              *db.DB
	mu              sync.RWMutex
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
	MaxResponseSize int  // Max response size in bytes (default 500000)
	Debug           bool // Enable debug logging

	// Database for audit logging (optional)
	DB *db.DB
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

	maxResponseSize := config.MaxResponseSize
	if maxResponseSize == 0 {
		maxResponseSize = 500000 // Default 500KB (~500K tokens)
	}

	// Create file store for externalized files
	fileStore, err := NewFileStore("/tmp/mcp-gatekeeper-files")
	if err != nil {
		return nil, fmt.Errorf("failed to create file store: %w", err)
	}

	s := &Server{
		client:          NewClient(clientConfig),
		apiKey:          config.APIKey,
		rateLimiter:     NewRateLimiter(rateLimit, rateLimitWindow),
		maxResponseSize: maxResponseSize,
		fileStore:       fileStore,
		debug:           config.Debug,
		db:              config.DB,
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

	// File retrieval endpoint
	r.Group(func(r chi.Router) {
		if s.apiKey != "" {
			r.Use(s.authMiddleware)
		}
		r.Get("/files/{key}", s.handleFileGet)
	})

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

// handleFileGet handles file retrieval by key
func (s *Server) handleFileGet(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key == "" {
		s.writeError(w, http.StatusBadRequest, "missing key")
		return
	}

	file, data, err := s.fileStore.Get(key)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "file not found")
		return
	}

	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleMCP handles MCP JSON-RPC requests
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	// Read raw request body
	var rawReq json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
		fmt.Fprintf(os.Stderr, "[bridge] failed to parse request: %v\n", err)
		if s.debug {
			fmt.Fprintf(os.Stderr, "[debug] parse error for request\n")
		}
		resp := &Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		}
		s.writeJSONRPC(w, resp)
		s.logAudit("", "", resp, err, startTime)
		return
	}

	// Parse request to check method first
	var req Request
	if err := json.Unmarshal(rawReq, &req); err != nil {
		resp := &Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		}
		s.writeJSONRPC(w, resp)
		s.logAudit("", string(rawReq), resp, err, startTime)
		return
	}

	// Debug log request
	if s.debug {
		fmt.Fprintf(os.Stderr, "[debug] REQUEST method=%s size=%d\n", req.Method, len(rawReq))
		if len(rawReq) > 1000 {
			fmt.Fprintf(os.Stderr, "[debug] REQUEST body (truncated): %s...\n", string(rawReq[:1000]))
		} else {
			fmt.Fprintf(os.Stderr, "[debug] REQUEST body: %s\n", string(rawReq))
		}
	}

	// Handle initialize locally (don't forward, don't require upstream)
	if req.Method == "initialize" {
		s.handleInitializeWithAudit(w, &req, string(rawReq), startTime)
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
				resp := &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &RPCError{
						Code:    -32603,
						Message: "Upstream not initialized",
					},
				}
				s.writeJSONRPC(w, resp)
				s.logAudit(req.Method, string(rawReq), resp, err, startTime)
				return
			}
		}
		s.mu.Unlock()
	}

	// Forward to upstream
	resp, err := client.Forward(ctx, rawReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bridge] forward error: %v\n", err)
		errResp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Forward error: %v", err),
			},
		}
		s.writeJSONRPC(w, errResp)
		s.logAudit(req.Method, string(rawReq), errResp, err, startTime)
		return
	}

	// Notification (no response)
	if resp == nil {
		w.WriteHeader(http.StatusOK)
		s.logAudit(req.Method, string(rawReq), nil, nil, startTime)
		return
	}

	// Save original response for audit logging (before externalization)
	originalResp := resp

	// Externalize large content from response
	var beforeSize int
	if s.debug && resp != nil {
		beforeJSON, _ := json.Marshal(resp)
		beforeSize = len(beforeJSON)
		// Log upstream response details before externalization
		s.debugLogUpstreamResponse(resp, req.Method)
	}
	resp = s.externalizeLargeContent(resp, r.Host)
	if s.debug && resp != nil {
		afterJSON, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stderr, "[debug] EXTERNALIZE method=%s before=%d after=%d\n", req.Method, beforeSize, len(afterJSON))
	}

	// Debug log response
	if s.debug {
		respJSON, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stderr, "[debug] RESPONSE method=%s size=%d\n", req.Method, len(respJSON))
		s.debugLogResponseSummary(resp, req.Method)
	}

	// Check response size
	respJSON, err := json.Marshal(resp)
	if err != nil {
		errResp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32603,
				Message: "Failed to marshal response",
			},
		}
		s.writeJSONRPC(w, errResp)
		s.logAudit(req.Method, string(rawReq), errResp, err, startTime)
		return
	}

	if len(respJSON) > s.maxResponseSize {
		fmt.Fprintf(os.Stderr, "[bridge] response too large: %d bytes (max: %d)\n", len(respJSON), s.maxResponseSize)
		errResp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Response too large: %d bytes exceeds limit of %d bytes", len(respJSON), s.maxResponseSize),
			},
		}
		s.writeJSONRPC(w, errResp)
		s.logAudit(req.Method, string(rawReq), errResp, fmt.Errorf("response too large"), startTime)
		return
	}

	s.writeJSONRPC(w, resp)
	// Log original response (before externalization) for audit
	s.logAudit(req.Method, string(rawReq), originalResp, nil, startTime)
}

// handleInitializeWithAudit handles initialize requests locally with audit logging
func (s *Server) handleInitializeWithAudit(w http.ResponseWriter, req *Request, params string, startTime time.Time) {
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
	resp := &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
	s.writeJSONRPC(w, resp)
	s.logAudit(req.Method, params, resp, nil, startTime)
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

// logAudit logs an MCP request/response to the database if configured
func (s *Server) logAudit(method string, params string, resp *Response, err error, startTime time.Time) {
	if s.db == nil {
		return
	}

	durationMs := time.Since(startTime).Milliseconds()

	var respStr string
	var errStr string
	var responseSize int64

	if resp != nil {
		respJSON, _ := json.Marshal(resp)
		respStr = string(respJSON)
		responseSize = int64(len(respJSON))
	}

	if err != nil {
		errStr = err.Error()
	} else if resp != nil && resp.Error != nil {
		errStr = resp.Error.Message
	}

	auditLog := &db.BridgeAuditLog{
		Method:       method,
		Params:       params,
		Response:     respStr,
		Error:        errStr,
		RequestSize:  int64(len(params)),
		ResponseSize: responseSize,
		DurationMs:   durationMs,
	}

	if _, logErr := s.db.CreateBridgeAuditLog(auditLog); logErr != nil {
		fmt.Fprintf(os.Stderr, "[bridge] failed to log audit: %v\n", logErr)
	}
}

// MaxContentSize is the maximum size of content to include in response (500KB)
// Content larger than this will be externalized to a file
const MaxContentSize = 500 * 1024

// ExternalFileInfo contains information about an externalized file
type ExternalFileInfo struct {
	Type     string `json:"type"`               // "external_file"
	URL      string `json:"url"`                // HTTP URL to retrieve the file
	MimeType string `json:"mimeType,omitempty"` // e.g., "image/png", "text/plain"
	Size     int    `json:"size"`               // original file size in bytes
}

// externalizeLargeContent replaces large content with file references
func (s *Server) externalizeLargeContent(resp *Response, host string) *Response {
	if resp == nil || resp.Result == nil {
		return resp
	}

	// Parse the result to check for content array (MCP tool call response format)
	var result struct {
		Content []map[string]interface{} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return resp
	}

	if len(result.Content) == 0 {
		return resp
	}

	modified := false
	filteredContent := make([]map[string]interface{}, 0, len(result.Content))

	// First pass: collect all image file paths from text content
	var imageFilePaths []string
	for _, item := range result.Content {
		if itemType, _ := item["type"].(string); itemType == "text" {
			if text, ok := item["text"].(string); ok {
				paths := ExtractMarkdownImagePaths(text)
				imageFilePaths = append(imageFilePaths, paths...)
			}
		}
	}

	for _, item := range result.Content {
		itemType, ok := item["type"].(string)
		if !ok {
			filteredContent = append(filteredContent, item)
			continue
		}

		switch itemType {
		case "image":
			// Try to use full-size file from Markdown link instead of base64 thumbnail
			data, hasData := item["data"].(string)
			mimeType, _ := item["mimeType"].(string)

			// Check if we have a full-size file path
			var usedFullFile bool
			for _, filePath := range imageFilePaths {
				key, detectedMime, size, err := s.fileStore.StoreFile(filePath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[bridge] failed to store file %s: %v\n", filePath, err)
					continue
				}
				if mimeType == "" {
					mimeType = detectedMime
				}
				fmt.Fprintf(os.Stderr, "[bridge] externalized full-size image from file (%d bytes) -> key=%s\n", size, key)

				fileRef := map[string]interface{}{
					"type": "text",
					"text": s.createExternalFileJSON(key, mimeType, size, host),
				}
				filteredContent = append(filteredContent, fileRef)
				modified = true
				usedFullFile = true
				break // Use first matching file
			}

			if usedFullFile {
				continue
			}

			// Fall back to base64 data if no file found
			if hasData && len(data) > MaxContentSize {
				key, detectedMime, err := s.fileStore.StoreBase64(data)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[bridge] failed to store image: %v\n", err)
					filteredContent = append(filteredContent, item)
					continue
				}
				if mimeType == "" {
					mimeType = detectedMime
				}
				fmt.Fprintf(os.Stderr, "[bridge] externalized image from base64 (%d bytes) -> key=%s\n", len(data), key)

				fileRef := map[string]interface{}{
					"type": "text",
					"text": s.createExternalFileJSON(key, mimeType, len(data), host),
				}
				filteredContent = append(filteredContent, fileRef)
				modified = true
				continue
			}

		case "text":
			// Externalize large text content
			text, hasText := item["text"].(string)
			if hasText && len(text) > MaxContentSize {
				// Check if text contains embedded base64 image
				if base64Data, found := ExtractBase64Image(text); found {
					// Store as image
					key, mimeType, err := s.fileStore.StoreBase64(base64Data)
					if err != nil {
						fmt.Fprintf(os.Stderr, "[bridge] failed to store embedded image: %v\n", err)
						filteredContent = append(filteredContent, item)
						continue
					}
					fmt.Fprintf(os.Stderr, "[bridge] externalized embedded image (%d bytes, %s) -> key=%s\n", len(base64Data), mimeType, key)

					item["text"] = s.createExternalFileJSON(key, mimeType, len(base64Data), host)
					modified = true
				} else {
					// Store as text file
					key, err := s.fileStore.Store([]byte(text), "text/plain")
					if err != nil {
						fmt.Fprintf(os.Stderr, "[bridge] failed to store text: %v\n", err)
						filteredContent = append(filteredContent, item)
						continue
					}
					fmt.Fprintf(os.Stderr, "[bridge] externalized text (%d bytes) -> key=%s\n", len(text), key)

					item["text"] = s.createExternalFileJSON(key, "text/plain", len(text), host)
					modified = true
				}
			}

		case "resource":
			// Externalize embedded resources with large content
			if resource, ok := item["resource"].(map[string]interface{}); ok {
				if blob, hasBlob := resource["blob"].(string); hasBlob && len(blob) > MaxContentSize {
					mimeType, _ := resource["mimeType"].(string)
					if mimeType == "" {
						mimeType = "application/octet-stream"
					}
					key, detectedMime, err := s.fileStore.StoreBase64(blob)
					if err != nil {
						fmt.Fprintf(os.Stderr, "[bridge] failed to store resource blob: %v\n", err)
						filteredContent = append(filteredContent, item)
						continue
					}
					if mimeType == "application/octet-stream" {
						mimeType = detectedMime
					}
					fmt.Fprintf(os.Stderr, "[bridge] externalized resource blob (%d bytes) -> key=%s\n", len(blob), key)

					// Replace blob with external file reference
					delete(resource, "blob")
					resource["externalFile"] = s.createExternalFileJSON(key, mimeType, len(blob), host)
					modified = true
				}
				if text, hasText := resource["text"].(string); hasText && len(text) > MaxContentSize {
					key, err := s.fileStore.Store([]byte(text), "text/plain")
					if err != nil {
						fmt.Fprintf(os.Stderr, "[bridge] failed to store resource text: %v\n", err)
						filteredContent = append(filteredContent, item)
						continue
					}
					fmt.Fprintf(os.Stderr, "[bridge] externalized resource text (%d bytes) -> key=%s\n", len(text), key)

					delete(resource, "text")
					resource["externalFile"] = s.createExternalFileJSON(key, "text/plain", len(text), host)
					modified = true
				}
			}
		}

		filteredContent = append(filteredContent, item)
	}

	if !modified {
		return resp
	}

	// Rebuild the result with filtered content
	newResult := map[string]interface{}{
		"content": filteredContent,
	}
	newResultJSON, err := json.Marshal(newResult)
	if err != nil {
		return resp
	}

	return &Response{
		JSONRPC: resp.JSONRPC,
		ID:      resp.ID,
		Result:  newResultJSON,
	}
}

// createExternalFileJSON creates a JSON string for external file info
func (s *Server) createExternalFileJSON(key, mimeType string, size int, host string) string {
	// Determine protocol (assume http for localhost, https otherwise)
	protocol := "https"
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") || strings.HasPrefix(host, "[::1]") {
		protocol = "http"
	}

	info := ExternalFileInfo{
		Type:     "external_file",
		URL:      fmt.Sprintf("%s://%s/files/%s", protocol, host, key),
		MimeType: mimeType,
		Size:     size,
	}
	jsonBytes, _ := json.Marshal(info)
	return string(jsonBytes)
}

// debugLogUpstreamResponse logs detailed upstream response before externalization
func (s *Server) debugLogUpstreamResponse(resp *Response, method string) {
	if resp == nil || resp.Result == nil {
		return
	}

	var result struct {
		Content []map[string]interface{} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return
	}

	fmt.Fprintf(os.Stderr, "[debug] UPSTREAM %s: %d content items\n", method, len(result.Content))
	for i, item := range result.Content {
		itemType, _ := item["type"].(string)
		switch itemType {
		case "text":
			text, _ := item["text"].(string)
			fmt.Fprintf(os.Stderr, "[debug]   [%d] UPSTREAM type=text len=%d\n", i, len(text))
			// Check if it looks like base64
			if len(text) > 100 {
				if base64Data, found := ExtractBase64Image(text); found {
					fmt.Fprintf(os.Stderr, "[debug]       -> contains base64 image, extracted len=%d\n", len(base64Data))
				} else {
					fmt.Fprintf(os.Stderr, "[debug]       -> no base64 image detected, preview: %q\n", text[:min(100, len(text))])
				}
			}
		case "image":
			data, _ := item["data"].(string)
			mimeType, _ := item["mimeType"].(string)
			fmt.Fprintf(os.Stderr, "[debug]   [%d] UPSTREAM type=image mimeType=%s dataLen=%d\n", i, mimeType, len(data))
		default:
			itemJSON, _ := json.Marshal(item)
			if len(itemJSON) > 200 {
				fmt.Fprintf(os.Stderr, "[debug]   [%d] UPSTREAM type=%s preview=%s...\n", i, itemType, string(itemJSON[:200]))
			} else {
				fmt.Fprintf(os.Stderr, "[debug]   [%d] UPSTREAM type=%s content=%s\n", i, itemType, string(itemJSON))
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// debugLogResponseSummary logs a summary of the response without flooding with base64 data
func (s *Server) debugLogResponseSummary(resp *Response, method string) {
	if resp == nil {
		fmt.Fprintf(os.Stderr, "[debug] RESPONSE %s: nil\n", method)
		return
	}

	if resp.Error != nil {
		fmt.Fprintf(os.Stderr, "[debug] RESPONSE %s: error=%s\n", method, resp.Error.Message)
		return
	}

	if resp.Result == nil {
		fmt.Fprintf(os.Stderr, "[debug] RESPONSE %s: no result\n", method)
		return
	}

	// Try to parse as MCP tool call response
	var result struct {
		Content []map[string]interface{} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err == nil && len(result.Content) > 0 {
		fmt.Fprintf(os.Stderr, "[debug] RESPONSE %s: %d content items\n", method, len(result.Content))
		for i, item := range result.Content {
			itemType, _ := item["type"].(string)
			switch itemType {
			case "text":
				text, _ := item["text"].(string)
				if len(text) > 200 {
					fmt.Fprintf(os.Stderr, "[debug]   [%d] type=text len=%d preview=%q...\n", i, len(text), text[:200])
				} else {
					fmt.Fprintf(os.Stderr, "[debug]   [%d] type=text len=%d content=%q\n", i, len(text), text)
				}
			case "image":
				data, _ := item["data"].(string)
				mimeType, _ := item["mimeType"].(string)
				fmt.Fprintf(os.Stderr, "[debug]   [%d] type=image mimeType=%s dataLen=%d\n", i, mimeType, len(data))
			default:
				fmt.Fprintf(os.Stderr, "[debug]   [%d] type=%s\n", i, itemType)
			}
		}
		return
	}

	// Generic result
	if len(resp.Result) > 500 {
		fmt.Fprintf(os.Stderr, "[debug] RESPONSE %s: result_len=%d preview=%s...\n", method, len(resp.Result), string(resp.Result[:500]))
	} else {
		fmt.Fprintf(os.Stderr, "[debug] RESPONSE %s: result=%s\n", method, string(resp.Result))
	}
}
