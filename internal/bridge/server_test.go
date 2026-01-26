package bridge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(10, time.Minute)

	if limiter.limit != 10 {
		t.Errorf("expected limit 10, got %d", limiter.limit)
	}
	if limiter.windowNano != time.Minute.Nanoseconds() {
		t.Errorf("expected window 1m in nanos, got %v", limiter.windowNano)
	}
}

func TestRateLimiterAllow(t *testing.T) {
	limiter := NewRateLimiter(3, time.Minute)

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !limiter.Allow() {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if limiter.Allow() {
		t.Error("4th request should be denied")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	limiter := NewRateLimiter(2, 50*time.Millisecond)

	// First 2 requests allowed
	if !limiter.Allow() {
		t.Error("1st request should be allowed")
	}
	if !limiter.Allow() {
		t.Error("2nd request should be allowed")
	}

	// 3rd request denied
	if limiter.Allow() {
		t.Error("3rd request should be denied")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !limiter.Allow() {
		t.Error("request after window expiry should be allowed")
	}
}

func TestNewServerMissingCommand(t *testing.T) {
	config := &ServerConfig{
		Command: "",
	}

	_, err := NewServer(config)
	if err == nil {
		t.Error("expected error when command is missing")
	}
}

func TestNewServerDefaults(t *testing.T) {
	config := &ServerConfig{
		Command: "echo",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	if server.rateLimiter.limit != 500 {
		t.Errorf("expected default rate limit 500, got %d", server.rateLimiter.limit)
	}
}

func TestNewServerWithAPIKey(t *testing.T) {
	config := &ServerConfig{
		Command: "echo",
		APIKey:  "test-key",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	if server.apiKey != "test-key" {
		t.Errorf("expected API key 'test-key', got '%s'", server.apiKey)
	}
}

func TestHealthEndpoint(t *testing.T) {
	config := &ServerConfig{
		Command: "echo",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["initialized"] != false {
		t.Error("expected initialized to be false before start")
	}
}

func TestAuthMiddleware(t *testing.T) {
	config := &ServerConfig{
		Command: "echo",
		APIKey:  "test-secret-key",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "no auth header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid format",
			authHeader:     "Basic dGVzdA==",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "wrong key",
			authHeader:     "Bearer wrong-key",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "valid key",
			authHeader:     "Bearer test-secret-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "case insensitive bearer",
			authHeader:     "bearer test-secret-key",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
			req := httptest.NewRequest(http.MethodPost, "/mcp", body)
			req.Header.Set("Content-Type", "application/json")
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	config := &ServerConfig{
		Command:         "echo",
		RateLimit:       2,
		RateLimitWindow: time.Minute,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
		req := httptest.NewRequest(http.MethodPost, "/mcp", body)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected status 200, got %d", i+1, w.Code)
		}
	}

	// 3rd request should be rate limited
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", w.Code)
	}
}

func TestMCPParseError(t *testing.T) {
	config := &ServerConfig{
		Command: "echo",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	body := bytes.NewBufferString(`invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for JSON-RPC error, got %d", w.Code)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error == nil {
		t.Error("expected error in response")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("expected error code -32700, got %d", resp.Error.Code)
	}
}

func TestInitializeHandler(t *testing.T) {
	config := &ServerConfig{
		Command: "echo",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}
	if resp.ID == nil {
		t.Error("expected ID in response")
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result["protocolVersion"] == nil {
		t.Error("expected protocolVersion in result")
	}
	if result["serverInfo"] == nil {
		t.Error("expected serverInfo in result")
	}
}
