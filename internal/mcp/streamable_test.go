package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/mcp/session"
	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
)

func setupStreamableHandler(t *testing.T) (*StreamableHandler, *HTTPServer) {
	plugins := &plugin.Config{}
	config := &HTTPConfig{
		RateLimit:       100,
		RateLimitWindow: time.Minute,
		RootDir:         "/tmp",
	}

	httpServer, err := NewHTTPServer(plugins, config)
	if err != nil {
		t.Fatalf("failed to create HTTP server: %v", err)
	}

	handler := NewStreamableHandler(httpServer, 30*time.Minute)
	return handler, httpServer
}

func TestStreamableHandler_Initialize(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// Create initialize request
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params: json.RawMessage(`{
			"protocolVersion": "2025-06-18",
			"capabilities": {},
			"clientInfo": {"name": "test", "version": "1.0"}
		}`),
	}

	body, _ := json.Marshal(initReq)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	w := httptest.NewRecorder()
	handler.HandlePost(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Check session ID header
	sessionID := w.Header().Get(HeaderMcpSessionID)
	if sessionID == "" {
		t.Error("expected Mcp-Session-Id header")
	}

	// Parse response
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error != nil {
		t.Errorf("expected no error, got %v", resp.Error)
	}

	// Check protocol version in result
	resultJSON, _ := json.Marshal(resp.Result)
	var result InitializeResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.ProtocolVersion != StreamableProtocolVersion {
		t.Errorf("expected protocol version %s, got %s", StreamableProtocolVersion, result.ProtocolVersion)
	}
}

func TestStreamableHandler_ToolsList(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)
	if sessionID == "" {
		t.Fatal("expected session ID from initialize")
	}

	// Now request tools/list with the session
	listReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	}

	listBody, _ := json.Marshal(listReq)
	listReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(listBody))
	listReqHTTP.Header.Set("Content-Type", "application/json")
	listReqHTTP.Header.Set("Accept", "application/json")
	listReqHTTP.Header.Set(HeaderMcpSessionID, sessionID)
	listReqHTTP.Header.Set(HeaderMCPProtocolVersion, StreamableProtocolVersion)

	listW := httptest.NewRecorder()
	handler.HandlePost(listW, listReqHTTP)

	if listW.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, listW.Code)
	}

	var resp Response
	if err := json.Unmarshal(listW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error != nil {
		t.Errorf("expected no error, got %v", resp.Error)
	}
}

func TestStreamableHandler_MissingSessionID(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// Request tools/list without session ID
	listReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	}

	body, _ := json.Marshal(listReq)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	w := httptest.NewRecorder()
	handler.HandlePost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestStreamableHandler_InvalidSessionID(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// Request tools/list with invalid session ID
	listReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	}

	body, _ := json.Marshal(listReq)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(HeaderMcpSessionID, "invalid-session-id")

	w := httptest.NewRecorder()
	handler.HandlePost(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestStreamableHandler_Notification(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)

	// Send notification (no ID)
	notifReq := Request{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}

	notifBody, _ := json.Marshal(notifReq)
	notifReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(notifBody))
	notifReqHTTP.Header.Set("Content-Type", "application/json")
	notifReqHTTP.Header.Set("Accept", "application/json")
	notifReqHTTP.Header.Set(HeaderMcpSessionID, sessionID)

	notifW := httptest.NewRecorder()
	handler.HandlePost(notifW, notifReqHTTP)

	// Notifications should return 202 Accepted
	if notifW.Code != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, notifW.Code)
	}

	// Body should be empty
	if notifW.Body.Len() > 0 {
		t.Errorf("expected empty body for notification, got %s", notifW.Body.String())
	}
}

func TestStreamableHandler_Delete(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)

	// Delete the session
	deleteReq := httptest.NewRequest("DELETE", "/mcp", nil)
	deleteReq.Header.Set(HeaderMcpSessionID, sessionID)

	deleteW := httptest.NewRecorder()
	handler.HandleDelete(deleteW, deleteReq)

	if deleteW.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, deleteW.Code)
	}

	// Try to use the deleted session
	listReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	}

	listBody, _ := json.Marshal(listReq)
	listReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(listBody))
	listReqHTTP.Header.Set("Content-Type", "application/json")
	listReqHTTP.Header.Set("Accept", "application/json")
	listReqHTTP.Header.Set(HeaderMcpSessionID, sessionID)

	listW := httptest.NewRecorder()
	handler.HandlePost(listW, listReqHTTP)

	if listW.Code != http.StatusNotFound {
		t.Errorf("expected status %d for deleted session, got %d", http.StatusNotFound, listW.Code)
	}
}

func TestStreamableHandler_Delete_MissingSessionID(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	req := httptest.NewRequest("DELETE", "/mcp", nil)

	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestStreamableHandler_Delete_InvalidSessionID(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	req := httptest.NewRequest("DELETE", "/mcp", nil)
	req.Header.Set(HeaderMcpSessionID, "invalid-session-id")

	w := httptest.NewRecorder()
	handler.HandleDelete(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestStreamableHandler_Delete_WrongProtocolVersion(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)

	// Try to delete with wrong protocol version
	deleteReq := httptest.NewRequest("DELETE", "/mcp", nil)
	deleteReq.Header.Set(HeaderMcpSessionID, sessionID)
	deleteReq.Header.Set(HeaderMCPProtocolVersion, "2024-11-05") // Wrong version

	deleteW := httptest.NewRecorder()
	handler.HandleDelete(deleteW, deleteReq)

	if deleteW.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, deleteW.Code)
	}

	// Session should still exist
	_, ok := handler.sessionManager.Get(sessionID)
	if !ok {
		t.Error("session should not have been deleted")
	}
}

func TestStreamableHandler_Get_RequiresSSEAccept(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// Initialize to get session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)

	// Try GET without SSE Accept header
	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set(HeaderMcpSessionID, sessionID)
	req.Header.Set("Accept", "application/json") // Wrong accept type

	w := httptest.NewRecorder()
	handler.HandleGet(w, req)

	if w.Code != http.StatusNotAcceptable {
		t.Errorf("expected status %d, got %d", http.StatusNotAcceptable, w.Code)
	}
}

func TestStreamableHandler_Get_MissingSessionID(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")

	w := httptest.NewRecorder()
	handler.HandleGet(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestStreamableHandler_Ping(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)

	// Send ping
	pingReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "ping",
	}

	pingBody, _ := json.Marshal(pingReq)
	pingReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(pingBody))
	pingReqHTTP.Header.Set("Content-Type", "application/json")
	pingReqHTTP.Header.Set("Accept", "application/json")
	pingReqHTTP.Header.Set(HeaderMcpSessionID, sessionID)

	pingW := httptest.NewRecorder()
	handler.HandlePost(pingW, pingReqHTTP)

	if pingW.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, pingW.Code)
	}

	var resp Response
	if err := json.Unmarshal(pingW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error != nil {
		t.Errorf("expected no error, got %v", resp.Error)
	}
}

func TestStreamableHandler_SSE_Broadcast(t *testing.T) {
	handler, _ := setupStreamableHandler(t)
	handler.heartbeatInterval = time.Hour // Disable heartbeat for this test

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)
	if sessionID == "" {
		t.Fatal("expected session ID from initialize")
	}

	// Get the session to broadcast an event
	sess, ok := handler.sessionManager.Get(sessionID)
	if !ok {
		t.Fatal("session not found")
	}

	// Create a context that we can cancel
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Use a buffered response writer
	sseW := &bufferResponseWriter{
		header: make(http.Header),
		buf:    &bytes.Buffer{},
	}

	sseReq := httptest.NewRequest("GET", "/mcp", nil).WithContext(ctx)
	sseReq.Header.Set("Accept", "text/event-stream")
	sseReq.Header.Set(HeaderMcpSessionID, sessionID)
	sseReq.Header.Set(HeaderMCPProtocolVersion, StreamableProtocolVersion)

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleGet(sseW, sseReq)
	}()

	// Wait a bit for SSE handler to start
	time.Sleep(50 * time.Millisecond)

	// Broadcast an event
	sess.Broadcast(&session.SSEEvent{
		ID:   "test-1",
		Data: map[string]string{"message": "hello"},
	})

	// Wait for context to be cancelled
	<-done

	output := sseW.buf.String()
	if !strings.Contains(output, "event: message") {
		t.Errorf("expected 'event: message' in SSE output, got: %s", output)
	}
	if !strings.Contains(output, "id: test-1") {
		t.Errorf("expected 'id: test-1' in SSE output, got: %s", output)
	}
	if !strings.Contains(output, `"message":"hello"`) {
		t.Errorf("expected message data in SSE output, got: %s", output)
	}
}

func TestStreamableHandler_SSE_Heartbeat(t *testing.T) {
	handler, _ := setupStreamableHandler(t)
	handler.heartbeatInterval = 50 * time.Millisecond

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)

	// Create a context that cancels after enough time for a heartbeat
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	sseW := &bufferResponseWriter{
		header: make(http.Header),
		buf:    &bytes.Buffer{},
	}

	sseReq := httptest.NewRequest("GET", "/mcp", nil).WithContext(ctx)
	sseReq.Header.Set("Accept", "text/event-stream")
	sseReq.Header.Set(HeaderMcpSessionID, sessionID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleGet(sseW, sseReq)
	}()

	<-done

	output := sseW.buf.String()
	if !strings.Contains(output, ": heartbeat") {
		t.Errorf("expected heartbeat comment in SSE output, got: %s", output)
	}
}

// bufferResponseWriter is a custom ResponseWriter that writes to a buffer
type bufferResponseWriter struct {
	header http.Header
	buf    *bytes.Buffer
	status int
}

func (w *bufferResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *bufferResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *bufferResponseWriter) Flush() {
	// no-op for buffer
}

func TestStreamableHandler_AcceptsJSON(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	tests := []struct {
		accept   string
		expected bool
	}{
		{"application/json", true},
		{"application/json, text/event-stream", true},
		{"text/html", false},
		{"*/*", true},
		{"", true},
		{"APPLICATION/JSON", true},
	}

	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			got := handler.acceptsJSON(tt.accept)
			if got != tt.expected {
				t.Errorf("acceptsJSON(%q) = %v, want %v", tt.accept, got, tt.expected)
			}
		})
	}
}

func TestStreamableHandler_AcceptsSSE(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	tests := []struct {
		accept   string
		expected bool
	}{
		{"text/event-stream", true},
		{"application/json, text/event-stream", true},
		{"text/html", false},
		{"*/*", false},
		{"", false},
		{"TEXT/EVENT-STREAM", true},
	}

	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			got := handler.acceptsSSE(tt.accept)
			if got != tt.expected {
				t.Errorf("acceptsSSE(%q) = %v, want %v", tt.accept, got, tt.expected)
			}
		})
	}
}

func TestStreamableHandler_Initialize_UnsupportedProtocolVersion(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// Create initialize request with unsupported protocol version
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params: json.RawMessage(`{
			"protocolVersion": "2024-11-05",
			"capabilities": {},
			"clientInfo": {"name": "test", "version": "1.0"}
		}`),
	}

	body, _ := json.Marshal(initReq)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	w := httptest.NewRecorder()
	handler.HandlePost(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Parse response - should be an error
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error == nil {
		t.Error("expected error for unsupported protocol version")
	}

	if resp.Error.Code != InvalidRequest {
		t.Errorf("expected error code %d, got %d", InvalidRequest, resp.Error.Code)
	}

	if !strings.Contains(resp.Error.Message, "Unsupported protocol version") {
		t.Errorf("expected error message about protocol version, got %s", resp.Error.Message)
	}
}

func TestStreamableHandler_WrongProtocolVersionHeader(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)
	if sessionID == "" {
		t.Fatal("expected session ID from initialize")
	}

	// Now request tools/list with wrong protocol version header
	listReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	}

	listBody, _ := json.Marshal(listReq)
	listReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(listBody))
	listReqHTTP.Header.Set("Content-Type", "application/json")
	listReqHTTP.Header.Set("Accept", "application/json")
	listReqHTTP.Header.Set(HeaderMcpSessionID, sessionID)
	listReqHTTP.Header.Set(HeaderMCPProtocolVersion, "2024-11-05") // Wrong version

	listW := httptest.NewRecorder()
	handler.HandlePost(listW, listReqHTTP)

	if listW.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, listW.Code)
	}
}

func TestStreamableHandler_MethodNotFound(t *testing.T) {
	handler, _ := setupStreamableHandler(t)

	// First, initialize to get a session
	initReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}`),
	}

	initBody, _ := json.Marshal(initReq)
	initReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(initBody))
	initReqHTTP.Header.Set("Content-Type", "application/json")
	initReqHTTP.Header.Set("Accept", "application/json")

	initW := httptest.NewRecorder()
	handler.HandlePost(initW, initReqHTTP)

	sessionID := initW.Header().Get(HeaderMcpSessionID)

	// Send unknown method
	unknownReq := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "unknown/method",
	}

	unknownBody, _ := json.Marshal(unknownReq)
	unknownReqHTTP := httptest.NewRequest("POST", "/mcp", bytes.NewReader(unknownBody))
	unknownReqHTTP.Header.Set("Content-Type", "application/json")
	unknownReqHTTP.Header.Set("Accept", "application/json")
	unknownReqHTTP.Header.Set(HeaderMcpSessionID, sessionID)

	unknownW := httptest.NewRecorder()
	handler.HandlePost(unknownW, unknownReqHTTP)

	if unknownW.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, unknownW.Code)
	}

	var resp Response
	if err := json.Unmarshal(unknownW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}

	if resp.Error.Code != MethodNotFound {
		t.Errorf("expected error code %d, got %d", MethodNotFound, resp.Error.Code)
	}

	if !strings.Contains(resp.Error.Data.(string), "unknown/method") {
		t.Errorf("expected error to contain method name, got %v", resp.Error.Data)
	}
}
