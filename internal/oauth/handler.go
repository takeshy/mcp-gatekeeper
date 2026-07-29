package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/version"
)

var authorizePage = template.Must(template.New("authorize").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Authorize MCP Gatekeeper</title></head><body>
<main><h1>Authorize MCP Gatekeeper</h1>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}
<p>Client <strong>{{.ClientID}}</strong> requests access to <code>{{.Resource}}</code>.</p>
<p>Scopes: <code>{{.Scope}}</code></p>
<form method="post" action="/oauth/authorize">
{{range $key, $value := .Hidden}}<input type="hidden" name="{{$key}}" value="{{$value}}">{{end}}
<label>Username <input name="username" autocomplete="username" required autofocus></label><br>
<label>Password <input type="password" name="password" autocomplete="current-password" required></label><br>
<button type="submit" name="decision" value="approve">Sign in and authorize</button>
<button type="submit" name="decision" value="deny" formnovalidate>Deny</button>
</form></main></body></html>`))

type authorizationRequest struct {
	ClientID    string
	RedirectURI string
	Scope       string
	State       string
	Resource    string
	Challenge   string
}

type authorizePageData struct {
	ClientID string
	Scope    string
	Resource string
	Error    string
	Hidden   map[string]string
}

// Handler handles OAuth endpoints
type Handler struct {
	db            *db.DB
	issuer        string
	resource      string
	htpasswdPath  string
	router        chi.Router
	loginMu       sync.Mutex
	loginAttempts map[string][]time.Time
	lastLoginGC   time.Time
}

// Config configures the embedded Authorization Code server.
type Config struct {
	Issuer       string
	Resource     string
	HTPasswdPath string
}

const maxLoginAttemptBuckets = 10000

// NewHandler creates a new OAuth handler
func NewHandler(database *db.DB, issuer string) *Handler {
	return NewHandlerWithConfig(database, Config{Issuer: issuer})
}

// NewHandlerWithConfig creates an OAuth handler with interactive login support.
func NewHandlerWithConfig(database *db.DB, config Config) *Handler {
	h := &Handler{
		db:            database,
		issuer:        config.Issuer,
		resource:      config.Resource,
		htpasswdPath:  config.HTPasswdPath,
		loginAttempts: make(map[string][]time.Time),
		lastLoginGC:   time.Now(),
	}
	h.setupRoutes()
	return h
}

func (h *Handler) setupRoutes() {
	r := chi.NewRouter()

	// OAuth 2.0 endpoints
	r.Get("/oauth/authorize", h.handleAuthorize)
	r.Post("/oauth/authorize", h.handleAuthorize)
	r.Post("/oauth/token", h.handleToken)

	// Well-known discovery endpoints
	r.Get("/.well-known/oauth-authorization-server", h.handleOAuthMetadata)
	r.Get("/.well-known/openid-configuration", h.handleOpenIDConfiguration)
	r.Get("/.well-known/oauth-protected-resource", h.handleProtectedResourceMetadata)
	r.Get("/.well-known/oauth-protected-resource/*", h.handleProtectedResourceMetadata)

	h.router = r
}

// Router returns the OAuth router
func (h *Handler) Router() chi.Router {
	return h.router
}

// TokenResponse represents an OAuth token response
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ErrorResponse represents an OAuth error response
type ErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// OAuthMetadata represents OAuth server metadata
type OAuthMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
}

// ProtectedResourceMetadata represents protected resource metadata (RFC 9728)
type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`
}

// handleToken handles the OAuth token endpoint
func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "failed to parse form")
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.handleAuthorizationCodeGrant(w, r)
	case "refresh_token":
		h.handleRefreshTokenGrant(w, r)
	default:
		h.writeError(w, http.StatusBadRequest, "unsupported_grant_type", "only 'authorization_code' and 'refresh_token' grant types are supported")
	}
}

func (h *Handler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	request, client, err := h.validateAuthorizationRequest(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if r.Method == http.MethodPost && r.FormValue("decision") == "deny" {
		h.redirectAuthorizationResult(w, r, request, "", "access_denied")
		return
	}

	loginError := ""
	status := http.StatusOK
	if r.Method == http.MethodPost {
		if !h.allowLoginAttempt(r.RemoteAddr) {
			h.writeError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many login attempts")
			return
		}
		if h.htpasswdPath == "" {
			h.writeError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "htpasswd login is not configured")
			return
		}
		ok, authErr := authenticateHTPasswd(h.htpasswdPath, r.FormValue("username"), r.FormValue("password"))
		if authErr != nil {
			h.writeError(w, http.StatusInternalServerError, "server_error", "failed to validate login")
			return
		}
		if ok {
			code, codeErr := h.db.CreateAuthorizationCode(client.ID, request.RedirectURI, r.FormValue("username"), request.Scope, request.Resource, request.Challenge)
			if codeErr != nil {
				h.writeError(w, http.StatusInternalServerError, "server_error", "failed to create authorization code")
				return
			}
			h.redirectAuthorizationResult(w, r, request, code, "")
			return
		}
		loginError = "Invalid username or password"
		status = http.StatusUnauthorized
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	data := authorizePageData{
		ClientID: request.ClientID,
		Scope:    request.Scope,
		Resource: request.Resource,
		Error:    loginError,
		Hidden: map[string]string{
			"response_type":         "code",
			"client_id":             request.ClientID,
			"redirect_uri":          request.RedirectURI,
			"scope":                 request.Scope,
			"state":                 request.State,
			"resource":              request.Resource,
			"code_challenge":        request.Challenge,
			"code_challenge_method": "S256",
		},
	}
	w.WriteHeader(status)
	if err := authorizePage.Execute(w, data); err != nil {
		return
	}
}

func (h *Handler) allowLoginAttempt(remoteAddr string) bool {
	h.loginMu.Lock()
	defer h.loginMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	if now.Sub(h.lastLoginGC) >= time.Minute {
		for attemptKey, attempts := range h.loginAttempts {
			keep := attempts[:0]
			for _, attempt := range attempts {
				if attempt.After(cutoff) {
					keep = append(keep, attempt)
				}
			}
			if len(keep) == 0 {
				delete(h.loginAttempts, attemptKey)
			} else {
				h.loginAttempts[attemptKey] = keep
			}
		}
		h.lastLoginGC = now
	}
	key := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		key = host
	}
	attempts, exists := h.loginAttempts[key]
	if !exists && len(h.loginAttempts) >= maxLoginAttemptBuckets {
		return false
	}
	valid := attempts[:0]
	for _, attempt := range attempts {
		if attempt.After(cutoff) {
			valid = append(valid, attempt)
		}
	}
	if len(valid) >= 10 {
		h.loginAttempts[key] = valid
		return false
	}
	h.loginAttempts[key] = append(valid, now)
	return true
}

func (h *Handler) validateAuthorizationRequest(r *http.Request) (*authorizationRequest, *db.OAuthClient, error) {
	if err := r.ParseForm(); err != nil {
		return nil, nil, fmt.Errorf("invalid form")
	}
	request := &authorizationRequest{
		ClientID:    r.FormValue("client_id"),
		RedirectURI: r.FormValue("redirect_uri"),
		Scope:       strings.Join(strings.Fields(r.FormValue("scope")), " "),
		State:       r.FormValue("state"),
		Resource:    strings.TrimSuffix(r.FormValue("resource"), "/"),
		Challenge:   r.FormValue("code_challenge"),
	}
	if r.FormValue("response_type") != "code" || request.ClientID == "" || request.RedirectURI == "" || request.State == "" {
		return nil, nil, fmt.Errorf("response_type=code, client_id, redirect_uri, and state are required")
	}
	if r.FormValue("code_challenge_method") != "S256" || !validPKCEChallenge(request.Challenge) {
		return nil, nil, fmt.Errorf("PKCE S256 code challenge is required")
	}
	client, err := h.db.GetOAuthClient(request.ClientID)
	if err != nil || client == nil || client.Status != "active" {
		return nil, nil, fmt.Errorf("unknown or inactive client")
	}
	if !contains(client.RedirectURIs, request.RedirectURI) {
		return nil, nil, fmt.Errorf("redirect_uri is not registered")
	}
	expectedResource := h.protectedResourceURL(r)
	if request.Resource == "" || request.Resource != expectedResource {
		return nil, nil, fmt.Errorf("resource must be %s", expectedResource)
	}
	requestedScopes := strings.Fields(request.Scope)
	if !contains(requestedScopes, "mcp") {
		return nil, nil, fmt.Errorf("scope must include %q", "mcp")
	}
	for _, scope := range requestedScopes {
		if !contains(client.Scopes, scope) {
			return nil, nil, fmt.Errorf("scope %q is not registered", scope)
		}
	}
	return request, client, nil
}

func (h *Handler) redirectAuthorizationResult(w http.ResponseWriter, r *http.Request, request *authorizationRequest, code, errorCode string) {
	target, _ := url.Parse(request.RedirectURI)
	query := target.Query()
	if code != "" {
		query.Set("code", code)
	} else {
		query.Set("error", errorCode)
	}
	query.Set("state", request.State)
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func validPKCEChallenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil
}

func pkceChallenge(verifier string) string {
	if !validPKCEVerifier(verifier) {
		return ""
	}
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func validPKCEVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, char := range verifier {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("-._~", char) {
			continue
		}
		return false
	}
	return true
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (h *Handler) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	clientID := r.FormValue("client_id")
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	verifier := r.FormValue("code_verifier")
	if clientID == "" || code == "" || redirectURI == "" || verifier == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "client_id, code, redirect_uri, and code_verifier are required")
		return
	}
	client, err := h.db.GetOAuthClient(clientID)
	if err != nil || client == nil || client.Status != "active" {
		h.writeError(w, http.StatusUnauthorized, "invalid_client", "unknown or inactive client")
		return
	}
	challenge := pkceChallenge(verifier)
	if challenge == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_grant", "invalid code verifier")
		return
	}
	authorization, err := h.db.ConsumeAuthorizationCode(code, client.ID, redirectURI, challenge)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "failed to consume authorization code")
		return
	}
	if authorization == nil {
		h.writeError(w, http.StatusBadRequest, "invalid_grant", "invalid or expired authorization code")
		return
	}
	accessToken, refreshToken, err := h.db.CreateAuthorizedToken(client.ID, authorization.Subject, authorization.Scope, authorization.Resource)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "failed to create tokens")
		return
	}
	h.writeToken(w, accessToken, refreshToken, authorization.Scope)
}

func (h *Handler) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")
	resource := strings.TrimSuffix(r.FormValue("resource"), "/")

	if refreshToken == "" || resource == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "refresh_token and resource are required")
		return
	}

	if clientID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "missing client_id")
		return
	}

	client, err := h.db.GetOAuthClient(clientID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "failed to validate credentials")
		return
	}
	if client == nil || client.Status != "active" {
		h.writeError(w, http.StatusUnauthorized, "invalid_client", "unknown or inactive client")
		return
	}

	// Refresh tokens
	newAccessToken, newRefreshToken, err := h.db.RefreshAuthorizedToken(refreshToken, client.ID, resource)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	h.writeToken(w, newAccessToken, newRefreshToken, "")
}

func (h *Handler) writeToken(w http.ResponseWriter, accessToken, refreshToken, scope string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(db.AccessTokenExpiration.Seconds()),
		RefreshToken: refreshToken,
		Scope:        scope,
	})
}

// handleOAuthMetadata returns OAuth 2.0 server metadata
func (h *Handler) handleOAuthMetadata(w http.ResponseWriter, r *http.Request) {
	metadata := h.buildMetadata(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

// handleOpenIDConfiguration returns OpenID Connect discovery document
func (h *Handler) handleOpenIDConfiguration(w http.ResponseWriter, r *http.Request) {
	metadata := h.buildMetadata(r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

// handleProtectedResourceMetadata returns OAuth 2.0 protected resource metadata
func (h *Handler) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	baseURL := h.requestBaseURL(r)
	resource := h.protectedResourceURL(r)
	if strings.TrimSpace(h.resource) == "" && strings.TrimSpace(h.issuer) == "" {
		suffix := strings.TrimPrefix(r.URL.Path, "/.well-known/oauth-protected-resource")
		if suffix != "" && suffix != "/" {
			resource = baseURL + suffix
		}
	}

	metadata := ProtectedResourceMetadata{
		Resource:             resource,
		AuthorizationServers: []string{h.authorizationServerBaseURL(r)},
		ScopesSupported:      h.supportedScopes(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

func (h *Handler) buildMetadata(r *http.Request) OAuthMetadata {
	// Determine base URL from request or configured issuer
	baseURL := h.authorizationServerBaseURL(r)

	return OAuthMetadata{
		Issuer:                            baseURL,
		AuthorizationEndpoint:             baseURL + "/oauth/authorize",
		TokenEndpoint:                     baseURL + "/oauth/token",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		ScopesSupported:                   h.supportedScopes(),
	}
}

func (h *Handler) supportedScopes() []string {
	clients, err := h.db.ListOAuthClients()
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var scopes []string
	for _, client := range clients {
		for _, scope := range client.Scopes {
			if !seen[scope] {
				seen[scope] = true
				scopes = append(scopes, scope)
			}
		}
	}
	return scopes
}

func (h *Handler) writeError(w http.ResponseWriter, status int, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:            errorCode,
		ErrorDescription: description,
	})
}

func (h *Handler) requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}

func (h *Handler) authorizationServerBaseURL(r *http.Request) string {
	baseURL := strings.TrimSuffix(h.issuer, "/")
	if baseURL == "" {
		baseURL = strings.TrimSuffix(h.requestBaseURL(r), "/")
	}
	return baseURL
}

func (h *Handler) protectedResourceURL(r *http.Request) string {
	if resource := strings.TrimSuffix(strings.TrimSpace(h.resource), "/"); resource != "" {
		return resource
	}
	return h.authorizationServerBaseURL(r) + "/mcp"
}

// ValidateAccessToken validates an access token from the Authorization header
func (h *Handler) ValidateAccessToken(r *http.Request) (*db.OAuthClient, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, nil
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return nil, nil
	}

	authorization, err := h.db.ValidateAuthorizedAccessToken(parts[1])
	if err != nil || authorization == nil {
		return nil, err
	}
	expectedResource := h.protectedResourceURL(r)
	if authorization.Resource != expectedResource {
		return nil, nil
	}
	if !contains(strings.Fields(authorization.Scope), "mcp") {
		return nil, nil
	}
	return authorization.Client, nil
}

// Version returns the OAuth handler version (for logging)
func (h *Handler) Version() string {
	return version.Version
}
