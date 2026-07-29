package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/oauth.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func newAuthorizationCodeHandler(t *testing.T) (*Handler, *db.DB) {
	t.Helper()
	database := newTestDB(t)
	if err := database.RegisterOAuthPublicClient("mcp-client", []string{"https://client.example/callback"}, []string{"mcp"}); err != nil {
		t.Fatalf("register client: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	passwordFile := t.TempDir() + "/.htpasswd"
	if err := os.WriteFile(passwordFile, []byte("alice:"+string(hash)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return NewHandlerWithConfig(database, Config{HTPasswdPath: passwordFile}), database
}

func TestAuthorizationCodePKCEFlow(t *testing.T) {
	handler, _ := newAuthorizationCodeHandler(t)
	verifier := strings.Repeat("a", 43)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])

	form := url.Values{
		"response_type":         {"code"},
		"client_id":             {"mcp-client"},
		"redirect_uri":          {"https://client.example/callback"},
		"scope":                 {"mcp"},
		"state":                 {"state-123"},
		"resource":              {"http://example.com/mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"username":              {"alice"},
		"password":              {"correct-password"},
		"decision":              {"approve"},
	}
	req := httptest.NewRequest(http.MethodPost, "http://example.com/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Router().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("authorize status=%d body=%s", w.Code, w.Body.String())
	}
	redirect, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if redirect.Query().Get("state") != "state-123" || redirect.Query().Get("code") == "" {
		t.Fatalf("unexpected redirect: %s", redirect)
	}

	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {"mcp-client"},
		"code":          {redirect.Query().Get("code")},
		"redirect_uri":  {"https://client.example/callback"},
		"code_verifier": {verifier},
	}
	wrongVerifierForm := url.Values{}
	for key, values := range tokenForm {
		wrongVerifierForm[key] = append([]string(nil), values...)
	}
	wrongVerifierForm.Set("code_verifier", strings.Repeat("b", 43))
	wrongVerifierReq := httptest.NewRequest(http.MethodPost, "http://example.com/oauth/token", strings.NewReader(wrongVerifierForm.Encode()))
	wrongVerifierReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wrongVerifierW := httptest.NewRecorder()
	handler.Router().ServeHTTP(wrongVerifierW, wrongVerifierReq)
	if wrongVerifierW.Code != http.StatusBadRequest {
		t.Fatalf("wrong verifier status=%d body=%s", wrongVerifierW.Code, wrongVerifierW.Body.String())
	}

	tokenReq := httptest.NewRequest(http.MethodPost, "http://example.com/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenW := httptest.NewRecorder()
	handler.Router().ServeHTTP(tokenW, tokenReq)
	if tokenW.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", tokenW.Code, tokenW.Body.String())
	}
	var token TokenResponse
	if err := json.Unmarshal(tokenW.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.Scope != "mcp" {
		t.Fatalf("unexpected token: %+v", token)
	}
	mcpReq := httptest.NewRequest(http.MethodPost, "http://example.com/mcp", nil)
	mcpReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	client, err := handler.ValidateAccessToken(mcpReq)
	if err != nil || client == nil || client.ClientID != "mcp-client" {
		t.Fatalf("access token validation failed: client=%+v err=%v", client, err)
	}
	wrongResourceReq := httptest.NewRequest(http.MethodPost, "http://other.example/mcp", nil)
	wrongResourceReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	client, err = handler.ValidateAccessToken(wrongResourceReq)
	if err != nil || client != nil {
		t.Fatalf("token accepted for wrong resource: client=%+v err=%v", client, err)
	}

	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {"mcp-client"},
		"refresh_token": {token.RefreshToken},
		"resource":      {"http://example.com/mcp"},
	}
	refreshReq := httptest.NewRequest(http.MethodPost, "http://example.com/oauth/token", strings.NewReader(refreshForm.Encode()))
	refreshReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refreshW := httptest.NewRecorder()
	handler.Router().ServeHTTP(refreshW, refreshReq)
	if refreshW.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshW.Code, refreshW.Body.String())
	}
	var refreshed TokenResponse
	if err := json.Unmarshal(refreshW.Body.Bytes(), &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" || refreshed.RefreshToken == token.RefreshToken {
		t.Fatalf("unexpected refreshed token: %+v", refreshed)
	}
	client, err = handler.ValidateAccessToken(mcpReq)
	if err != nil || client != nil {
		t.Fatalf("old access token remained valid after refresh: client=%+v err=%v", client, err)
	}
	mcpReq.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	client, err = handler.ValidateAccessToken(mcpReq)
	if err != nil || client == nil {
		t.Fatalf("refreshed access token validation failed: client=%+v err=%v", client, err)
	}

	replayW := httptest.NewRecorder()
	replayReq := httptest.NewRequest(http.MethodPost, "http://example.com/oauth/token", strings.NewReader(tokenForm.Encode()))
	replayReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.Router().ServeHTTP(replayW, replayReq)
	if replayW.Code != http.StatusBadRequest {
		t.Fatalf("authorization code replay status=%d", replayW.Code)
	}
}

func TestClientCredentialsIsUnsupported(t *testing.T) {
	handler, _ := newAuthorizationCodeHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=client_credentials"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Router().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unsupported_grant_type") {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
}

func TestOAuthMetadataAdvertisesAuthorizationCodePKCE(t *testing.T) {
	handler, _ := newAuthorizationCodeHandler(t)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.Router().ServeHTTP(w, req)
	var metadata OAuthMetadata
	if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.AuthorizationEndpoint != "http://example.com/oauth/authorize" || !contains(metadata.GrantTypesSupported, "authorization_code") || contains(metadata.GrantTypesSupported, "client_credentials") {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	if !contains(metadata.CodeChallengeMethodsSupported, "S256") {
		t.Fatalf("PKCE S256 not advertised: %+v", metadata)
	}
}

func TestProtectedResourceMetadata(t *testing.T) {
	handler, _ := newAuthorizationCodeHandler(t)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()
	handler.Router().ServeHTTP(w, req)
	var metadata ProtectedResourceMetadata
	if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Resource != "http://example.com/mcp" || len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != "http://example.com" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
}

func TestConfiguredIssuerProvidesStableProtectedResource(t *testing.T) {
	handler, database := newAuthorizationCodeHandler(t)
	handler.issuer = "https://public.example"

	verifier := strings.Repeat("a", 43)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {"mcp-client"},
		"redirect_uri":          {"https://client.example/callback"},
		"scope":                 {"mcp"},
		"state":                 {"state-123"},
		"resource":              {"https://public.example/mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "http://internal:8080/oauth/authorize?"+query.Encode(), nil)
	w := httptest.NewRecorder()
	handler.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", w.Code, w.Body.String())
	}

	client, err := database.GetOAuthClient("mcp-client")
	if err != nil {
		t.Fatal(err)
	}
	accessToken, _, err := database.CreateAuthorizedToken(client.ID, "alice", "mcp", "https://public.example/mcp")
	if err != nil {
		t.Fatal(err)
	}
	mcpReq := httptest.NewRequest(http.MethodPost, "http://internal:8080/mcp", nil)
	mcpReq.Header.Set("Authorization", "Bearer "+accessToken)
	validated, err := handler.ValidateAccessToken(mcpReq)
	if err != nil || validated == nil {
		t.Fatalf("configured issuer token was rejected: client=%+v err=%v", validated, err)
	}
}

func TestLoginAttemptBucketsAreGarbageCollected(t *testing.T) {
	handler, _ := newAuthorizationCodeHandler(t)
	handler.loginAttempts["stale"] = []time.Time{time.Now().Add(-2 * time.Minute)}
	handler.lastLoginGC = time.Now().Add(-2 * time.Minute)

	if !handler.allowLoginAttempt("192.0.2.1:1234") {
		t.Fatal("fresh login attempt was unexpectedly rejected")
	}
	if _, exists := handler.loginAttempts["stale"]; exists {
		t.Fatal("stale login-attempt bucket was not removed")
	}
}
