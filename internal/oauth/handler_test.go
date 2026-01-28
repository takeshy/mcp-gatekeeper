package oauth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "test-oauth-handler-*.db")
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

func TestTokenEndpointClientSecretBasic(t *testing.T) {
	database := newTestDB(t)
	clientSecret, err := database.CreateOAuthClient("basic-client")
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}

	handler := NewHandler(database, "")
	server := httptest.NewServer(handler.Router())
	defer server.Close()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequest(http.MethodPost, server.URL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cred := base64.StdEncoding.EncodeToString([]byte("basic-client:" + clientSecret))
	req.Header.Set("Authorization", "Basic "+cred)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
		t.Fatalf("expected access and refresh tokens, got %+v", tokenResp)
	}
	if tokenResp.TokenType != "Bearer" {
		t.Fatalf("expected token_type Bearer, got %q", tokenResp.TokenType)
	}
}

func TestOAuthMetadataAuthMethodsIncludesBasic(t *testing.T) {
	handler := NewHandler(newTestDB(t), "")
	server := httptest.NewServer(handler.Router())
	defer server.Close()

	resp, err := http.Get(server.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatalf("get metadata: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var metadata OAuthMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	if !contains(metadata.TokenEndpointAuthMethodsSupported, "client_secret_basic") {
		t.Fatalf("expected client_secret_basic in auth methods, got %v", metadata.TokenEndpointAuthMethodsSupported)
	}
	if !contains(metadata.TokenEndpointAuthMethodsSupported, "client_secret_post") {
		t.Fatalf("expected client_secret_post in auth methods, got %v", metadata.TokenEndpointAuthMethodsSupported)
	}
}

func TestProtectedResourceMetadata(t *testing.T) {
	handler := NewHandler(newTestDB(t), "")
	server := httptest.NewServer(handler.Router())
	defer server.Close()

	resp, err := http.Get(server.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("get metadata: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var metadata ProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	if metadata.Resource != server.URL+"/mcp" {
		t.Fatalf("expected resource %q, got %q", server.URL+"/mcp", metadata.Resource)
	}
	if len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != server.URL {
		t.Fatalf("expected authorization server %q, got %v", server.URL, metadata.AuthorizationServers)
	}
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
