package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
)

func newHTTPTestDB(t *testing.T) *db.DB {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "test-mcp-http-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(tmpFile.Name())
	})

	database, err := db.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	return database
}

func TestWWWAuthenticateHeaderWhenOAuthEnabled(t *testing.T) {
	plugins := &plugin.Config{Tools: map[string]*plugin.Tool{}}
	config := &HTTPConfig{
		EnableOAuth:     true,
		DB:              newHTTPTestDB(t),
		RateLimit:       10,
		RateLimitWindow: time.Minute,
	}

	server, err := NewHTTPServer(plugins, config)
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}

	want := `Bearer resource_metadata="http://example.com/.well-known/oauth-protected-resource/mcp"`
	if got := w.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("expected WWW-Authenticate %q, got %q", want, got)
	}
}

func TestInitializeIncludesOAuthExtension(t *testing.T) {
	plugins := &plugin.Config{Tools: map[string]*plugin.Tool{}}
	config := &HTTPConfig{
		EnableOAuth:     true,
		APIKey:          "test-key",
		DB:              newHTTPTestDB(t),
		RateLimit:       10,
		RateLimitWindow: time.Minute,
	}

	server, err := NewHTTPServer(plugins, config)
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected result object, got %T", resp.Result)
	}

	capabilities, ok := result["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected capabilities object, got %T", result["capabilities"])
	}
	extensions, ok := capabilities["extensions"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected extensions object, got %T", capabilities["extensions"])
	}
	if _, ok := extensions["io.modelcontextprotocol/oauth-client-credentials"]; !ok {
		t.Fatalf("expected oauth client credentials extension, got %v", extensions)
	}
}
