package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/mcp/session"
	"github.com/takeshy/mcp-gatekeeper/internal/mcp/sse"
)

// MCP Streamable HTTP protocol version
const StreamableProtocolVersion = "2025-06-18"

// HTTP headers for MCP Streamable HTTP
const (
	HeaderMcpSessionID       = "Mcp-Session-Id"
	HeaderMCPProtocolVersion = "MCP-Protocol-Version"
)

// StreamableHandler handles MCP Streamable HTTP requests
type StreamableHandler struct {
	httpServer     *HTTPServer
	sessionManager *session.Manager
	heartbeatInterval time.Duration
}

// NewStreamableHandler creates a new StreamableHandler
func NewStreamableHandler(httpServer *HTTPServer, sessionTTL time.Duration) *StreamableHandler {
	return &StreamableHandler{
		httpServer:        httpServer,
		sessionManager:    session.NewManager(sessionTTL),
		heartbeatInterval: 30 * time.Second,
	}
}

// StartCleanup starts the session cleanup goroutine
func (h *StreamableHandler) StartCleanup(ctx context.Context) {
	h.sessionManager.StartCleanup(ctx)
}

// Stop stops the session manager
func (h *StreamableHandler) Stop() {
	h.sessionManager.Stop()
}

// HandlePost handles POST /mcp requests
func (h *StreamableHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	// Check Accept header - must accept both application/json and text/event-stream
	accept := r.Header.Get("Accept")
	if !h.acceptsJSON(accept) {
		h.writeHTTPError(w, http.StatusNotAcceptable, "Accept header must include application/json")
		return
	}

	// Parse JSON-RPC request
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to parse JSON-RPC request: %v\n", err)
		h.writeJSONRPC(w, nil, NewErrorResponse(nil, ParseError, "Parse error", err.Error()))
		return
	}

	if req.JSONRPC != "2.0" {
		h.writeJSONRPC(w, nil, NewErrorResponse(req.ID, InvalidRequest, "Invalid Request", "jsonrpc must be 2.0"))
		return
	}

	// Handle initialize request - creates a new session
	if req.Method == "initialize" {
		h.handleInitialize(w, r, &req)
		return
	}

	// For all other requests, require a valid session
	sessionID := r.Header.Get(HeaderMcpSessionID)
	if sessionID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "missing Mcp-Session-Id header")
		return
	}

	sess, ok := h.sessionManager.Get(sessionID)
	if !ok {
		h.writeHTTPError(w, http.StatusNotFound, "session not found")
		return
	}

	// Validate MCP-Protocol-Version header for non-initialize requests
	protocolVersion := r.Header.Get(HeaderMCPProtocolVersion)
	if protocolVersion != "" && protocolVersion != StreamableProtocolVersion {
		h.writeHTTPError(w, http.StatusBadRequest,
			fmt.Sprintf("unsupported protocol version: expected %s, got %s", StreamableProtocolVersion, protocolVersion))
		return
	}

	// Touch session to update last activity
	h.sessionManager.Touch(sessionID)

	// Handle notifications (no id or null id) - return 202 Accepted
	if req.ID == nil || string(req.ID) == "null" {
		h.handleNotification(&req, sess)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Handle regular requests
	var resp *Response
	switch req.Method {
	case "tools/list":
		resp = h.httpServer.handleMCPToolsList(&req)
	case "tools/call":
		resp = h.httpServer.handleMCPToolsCall(r.Context(), &req)
	case "resources/list":
		resp = h.httpServer.handleMCPResourcesList(&req)
	case "resources/read":
		resp = h.httpServer.handleMCPResourcesRead(&req)
	case "ping":
		resp = NewResponse(req.ID, struct{}{})
	default:
		resp = NewErrorResponse(req.ID, MethodNotFound, "Method not found", req.Method)
	}

	h.writeJSONRPC(w, sess, resp)
}

// handleInitialize handles the initialize request
func (h *StreamableHandler) handleInitialize(w http.ResponseWriter, r *http.Request, req *Request) {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		h.writeJSONRPC(w, nil, NewErrorResponse(req.ID, InvalidParams, "Invalid params", err.Error()))
		return
	}

	// Validate client protocol version
	if params.ProtocolVersion != StreamableProtocolVersion {
		h.writeJSONRPC(w, nil, NewErrorResponse(req.ID, InvalidRequest, "Unsupported protocol version",
			fmt.Sprintf("expected %s, got %s", StreamableProtocolVersion, params.ProtocolVersion)))
		return
	}

	// Create a new session
	sess := h.sessionManager.Create()

	// Build capabilities
	caps := ServerCapabilities{
		Tools: &ToolsCapability{
			ListChanged: false,
		},
	}

	// Add resources capability if any tool has UI enabled
	if h.httpServer.hasUIEnabledTools() {
		caps.Resources = &ResourcesCapability{
			Subscribe:   false,
			ListChanged: false,
		}
	}

	if h.httpServer.oauthHandler != nil {
		caps.Extensions = map[string]map[string]interface{}{
			"io.modelcontextprotocol/oauth-client-credentials": {},
		}
	}

	result := &InitializeResult{
		ProtocolVersion: StreamableProtocolVersion,
		Capabilities:    caps,
		ServerInfo: ServerInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}

	resp := NewResponse(req.ID, result)

	// Set session ID header
	w.Header().Set(HeaderMcpSessionID, sess.ID)
	h.writeJSONRPC(w, sess, resp)
}

// handleNotification handles MCP notifications
func (h *StreamableHandler) handleNotification(req *Request, sess *session.Session) {
	switch req.Method {
	case "notifications/initialized":
		// Client is initialized, nothing to do
	case "notifications/cancelled":
		// Request cancellation, nothing to do for now
	default:
		fmt.Fprintf(os.Stderr, "[WARN] Unknown notification: %s\n", req.Method)
	}
}

// HandleGet handles GET /mcp requests (SSE stream)
func (h *StreamableHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	// Check Accept header
	accept := r.Header.Get("Accept")
	if !h.acceptsSSE(accept) {
		h.writeHTTPError(w, http.StatusNotAcceptable, "Accept header must include text/event-stream")
		return
	}

	// Require session ID
	sessionID := r.Header.Get(HeaderMcpSessionID)
	if sessionID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "missing Mcp-Session-Id header")
		return
	}

	sess, ok := h.sessionManager.Get(sessionID)
	if !ok {
		h.writeHTTPError(w, http.StatusNotFound, "session not found")
		return
	}

	// Validate MCP-Protocol-Version header
	protocolVersion := r.Header.Get(HeaderMCPProtocolVersion)
	if protocolVersion != "" && protocolVersion != StreamableProtocolVersion {
		h.writeHTTPError(w, http.StatusBadRequest,
			fmt.Sprintf("unsupported protocol version: expected %s, got %s", StreamableProtocolVersion, protocolVersion))
		return
	}

	// Create SSE writer
	sseWriter, err := sse.NewWriter(w)
	if err != nil {
		h.writeHTTPError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Set session ID header in response
	w.Header().Set(HeaderMcpSessionID, sessionID)

	// Create event channel for this connection
	eventCh := make(chan *session.SSEEvent, 100)
	sess.AddSSEChannel(eventCh)
	defer sess.RemoveSSEChannel(eventCh)

	// Touch session
	h.sessionManager.Touch(sessionID)

	// Get request context
	ctx := r.Context()

	// Start heartbeat ticker
	heartbeatTicker := time.NewTicker(h.heartbeatInterval)
	defer heartbeatTicker.Stop()

	// Stream events
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatTicker.C:
			if err := sseWriter.WriteComment("heartbeat"); err != nil {
				return
			}
			h.sessionManager.Touch(sessionID)
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed, session ended
				return
			}
			if err := sseWriter.WriteEvent("message", event.ID, event.Data); err != nil {
				return
			}
			h.sessionManager.Touch(sessionID)
		}
	}
}

// HandleDelete handles DELETE /mcp requests (terminate session)
func (h *StreamableHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(HeaderMcpSessionID)
	if sessionID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "missing Mcp-Session-Id header")
		return
	}

	// Validate MCP-Protocol-Version header if present
	protocolVersion := r.Header.Get(HeaderMCPProtocolVersion)
	if protocolVersion != "" && protocolVersion != StreamableProtocolVersion {
		h.writeHTTPError(w, http.StatusBadRequest,
			fmt.Sprintf("unsupported protocol version: expected %s, got %s", StreamableProtocolVersion, protocolVersion))
		return
	}

	if !h.sessionManager.Delete(sessionID) {
		h.writeHTTPError(w, http.StatusNotFound, "session not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// acceptsJSON checks if the Accept header includes application/json
func (h *StreamableHandler) acceptsJSON(accept string) bool {
	if accept == "" {
		return true // Default to accepting JSON
	}
	accept = strings.ToLower(accept)
	return strings.Contains(accept, "application/json") ||
		strings.Contains(accept, "*/*")
}

// acceptsSSE checks if the Accept header includes text/event-stream
func (h *StreamableHandler) acceptsSSE(accept string) bool {
	if accept == "" {
		return false
	}
	accept = strings.ToLower(accept)
	return strings.Contains(accept, "text/event-stream")
}

// writeJSONRPC writes a JSON-RPC response
func (h *StreamableHandler) writeJSONRPC(w http.ResponseWriter, sess *session.Session, resp *Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// writeHTTPError writes an HTTP error response
func (h *StreamableHandler) writeHTTPError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
