package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/version"
)

// StreamableProtocolVersion is the MCP Streamable HTTP protocol version
const StreamableProtocolVersion = "2025-06-18"

// HTTP headers for MCP Streamable HTTP
const (
	HeaderMcpSessionID       = "Mcp-Session-Id"
	HeaderMCPProtocolVersion = "MCP-Protocol-Version"
)

// StreamableHandler handles MCP Streamable HTTP requests for bridge mode
type StreamableHandler struct {
	server            *Server
	sessionManager    *SessionManager
	heartbeatInterval time.Duration
}

// NewStreamableHandler creates a new StreamableHandler for bridge mode
func NewStreamableHandler(server *Server, sessionTTL time.Duration, clientConfig *ClientConfig) *StreamableHandler {
	return &StreamableHandler{
		server:            server,
		sessionManager:    NewSessionManager(sessionTTL, clientConfig),
		heartbeatInterval: 30 * time.Second,
	}
}

// StartCleanup starts the session cleanup goroutine
func (h *StreamableHandler) StartCleanup(ctx context.Context) {
	h.sessionManager.StartCleanup(ctx)
}

// Stop stops the session manager and closes all sessions
func (h *StreamableHandler) Stop() {
	h.sessionManager.Stop()
	h.sessionManager.CloseAll()
}

// HandlePost handles POST /mcp requests
func (h *StreamableHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	// Check Accept header
	accept := r.Header.Get("Accept")
	if !h.acceptsJSON(accept) {
		h.writeHTTPError(w, http.StatusNotAcceptable, "Accept header must include application/json")
		return
	}

	// Parse JSON-RPC request
	var rawReq json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
		fmt.Fprintf(os.Stderr, "[bridge] failed to parse request: %v\n", err)
		resp := &Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		}
		h.writeJSONRPC(w, resp)
		h.server.logAudit("", "", resp, err, startTime)
		return
	}

	var req Request
	if err := json.Unmarshal(rawReq, &req); err != nil {
		resp := &Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		}
		h.writeJSONRPC(w, resp)
		h.server.logAudit("", string(rawReq), resp, err, startTime)
		return
	}

	if req.JSONRPC != "2.0" {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32600,
				Message: "Invalid Request",
			},
		}
		h.writeJSONRPC(w, resp)
		return
	}

	// Debug log
	if h.server.debug {
		fmt.Fprintf(os.Stderr, "[debug] REQUEST method=%s size=%d\n", req.Method, len(rawReq))
	}

	// Handle initialize request - creates a new session with new upstream
	if req.Method == "initialize" {
		h.handleInitialize(w, r, &req, rawReq, startTime)
		return
	}

	// For all other requests, require a valid session
	sessionID := r.Header.Get(HeaderMcpSessionID)
	if sessionID == "" {
		h.writeHTTPError(w, http.StatusBadRequest, "missing Mcp-Session-Id header")
		return
	}

	session, ok := h.sessionManager.Get(sessionID)
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

	// Touch session
	h.sessionManager.Touch(sessionID)

	// Handle notifications - forward to upstream
	if req.ID == nil || string(req.ID) == "null" {
		// Forward notification to upstream (no response expected)
		_, err := session.Client.Forward(ctx, rawReq)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[bridge] notification forward error: %v\n", err)
		}
		h.server.logAudit(req.Method, string(rawReq), nil, err, startTime)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Forward to session's upstream client
	resp, err := session.Client.Forward(ctx, rawReq)
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
		h.writeJSONRPC(w, errResp)
		h.server.logAudit(req.Method, string(rawReq), errResp, err, startTime)
		return
	}

	// Notification (no response)
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		h.server.logAudit(req.Method, string(rawReq), nil, nil, startTime)
		return
	}

	// Externalize large content
	originalResp := resp
	resp = h.server.externalizeLargeContent(resp, r.Host)

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
		h.writeJSONRPC(w, errResp)
		h.server.logAudit(req.Method, string(rawReq), errResp, err, startTime)
		return
	}

	if len(respJSON) > h.server.maxResponseSize {
		fmt.Fprintf(os.Stderr, "[bridge] response too large: %d bytes (max: %d)\n", len(respJSON), h.server.maxResponseSize)
		errResp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Response too large: %d bytes exceeds limit of %d bytes", len(respJSON), h.server.maxResponseSize),
			},
		}
		h.writeJSONRPC(w, errResp)
		h.server.logAudit(req.Method, string(rawReq), errResp, fmt.Errorf("response too large"), startTime)
		return
	}

	h.writeJSONRPC(w, resp)
	h.server.logAudit(req.Method, string(rawReq), originalResp, nil, startTime)
}

// handleInitialize handles the initialize request - creates new session with upstream
func (h *StreamableHandler) handleInitialize(w http.ResponseWriter, r *http.Request, req *Request, rawReq json.RawMessage, startTime time.Time) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
		h.writeJSONRPC(w, resp)
		h.server.logAudit(req.Method, string(rawReq), resp, err, startTime)
		return
	}

	// Validate protocol version
	if params.ProtocolVersion != StreamableProtocolVersion {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32600,
				Message: fmt.Sprintf("Unsupported protocol version: expected %s, got %s", StreamableProtocolVersion, params.ProtocolVersion),
			},
		}
		h.writeJSONRPC(w, resp)
		h.server.logAudit(req.Method, string(rawReq), resp, fmt.Errorf("unsupported protocol version"), startTime)
		return
	}

	// Create new session with its own upstream
	session, err := h.sessionManager.Create(r.Context())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bridge] failed to create session: %v\n", err)
		resp := &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32603,
				Message: fmt.Sprintf("Failed to create session: %v", err),
			},
		}
		h.writeJSONRPC(w, resp)
		h.server.logAudit(req.Method, string(rawReq), resp, err, startTime)
		return
	}

	fmt.Fprintf(os.Stderr, "[bridge] created session %s with new upstream\n", session.ID)

	// Build response
	capabilities := map[string]interface{}{
		"tools": map[string]interface{}{
			"listChanged": false,
		},
	}
	if h.server.oauthHandler != nil {
		capabilities["extensions"] = map[string]interface{}{
			"io.modelcontextprotocol/oauth-client-credentials": map[string]interface{}{},
		}
	}

	result := map[string]interface{}{
		"protocolVersion": StreamableProtocolVersion,
		"capabilities":    capabilities,
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

	// Set session ID header
	w.Header().Set(HeaderMcpSessionID, session.ID)
	h.writeJSONRPC(w, resp)
	h.server.logAudit(req.Method, string(rawReq), resp, nil, startTime)
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

	session, ok := h.sessionManager.Get(sessionID)
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
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeHTTPError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set(HeaderMcpSessionID, sessionID)

	// Create event channel
	eventCh := make(chan *SSEEvent, 100)
	session.AddSSEChannel(eventCh)
	defer session.RemoveSSEChannel(eventCh)

	h.sessionManager.Touch(sessionID)

	ctx := r.Context()
	heartbeatTicker := time.NewTicker(h.heartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatTicker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
			h.sessionManager.Touch(sessionID)
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			data, _ := json.Marshal(event.Data)
			fmt.Fprintf(w, "event: message\n")
			if event.ID != "" {
				fmt.Fprintf(w, "id: %s\n", event.ID)
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
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

	// Validate MCP-Protocol-Version header
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

	fmt.Fprintf(os.Stderr, "[bridge] deleted session %s\n", sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// acceptsJSON checks if Accept header includes application/json
func (h *StreamableHandler) acceptsJSON(accept string) bool {
	if accept == "" {
		return true
	}
	accept = strings.ToLower(accept)
	return strings.Contains(accept, "application/json") || strings.Contains(accept, "*/*")
}

// acceptsSSE checks if Accept header includes text/event-stream
func (h *StreamableHandler) acceptsSSE(accept string) bool {
	if accept == "" {
		return false
	}
	accept = strings.ToLower(accept)
	return strings.Contains(accept, "text/event-stream")
}

func (h *StreamableHandler) writeJSONRPC(w http.ResponseWriter, resp *Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *StreamableHandler) writeHTTPError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
