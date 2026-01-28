package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/version"
)

// Handler handles OAuth endpoints
type Handler struct {
	db     *db.DB
	issuer string
	router chi.Router
}

// NewHandler creates a new OAuth handler
func NewHandler(database *db.DB, issuer string) *Handler {
	h := &Handler{
		db:     database,
		issuer: issuer,
	}
	h.setupRoutes()
	return h
}

func (h *Handler) setupRoutes() {
	r := chi.NewRouter()

	// OAuth 2.0 endpoints
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
}

// ProtectedResourceMetadata represents protected resource metadata (RFC 9728)
type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
}

// handleToken handles the OAuth token endpoint
func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "failed to parse form")
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "client_credentials":
		h.handleClientCredentialsGrant(w, r)
	case "refresh_token":
		h.handleRefreshTokenGrant(w, r)
	default:
		h.writeError(w, http.StatusBadRequest, "unsupported_grant_type", "only 'client_credentials' and 'refresh_token' grant types are supported")
	}
}

func (h *Handler) handleClientCredentialsGrant(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret, err := h.parseClientCredentials(r)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
		return
	}

	if clientID == "" || clientSecret == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "missing client credentials")
		return
	}

	// Validate client credentials
	client, err := h.db.ValidateClientCredentials(clientID, clientSecret)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "failed to validate credentials")
		return
	}
	if client == nil {
		h.writeError(w, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
		return
	}

	accessToken, refreshToken, err := h.db.CreateToken(client.ID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "failed to create tokens")
		return
	}

	// Return token response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(db.AccessTokenExpiration.Seconds()),
		RefreshToken: refreshToken,
	})
}

func (h *Handler) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	clientID, clientSecret, err := h.parseClientCredentials(r)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
		return
	}

	if refreshToken == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "missing refresh_token")
		return
	}

	if clientID == "" || clientSecret == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "missing client credentials")
		return
	}

	// Validate client credentials
	client, err := h.db.ValidateClientCredentials(clientID, clientSecret)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "server_error", "failed to validate credentials")
		return
	}
	if client == nil {
		h.writeError(w, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
		return
	}

	// Refresh tokens
	newAccessToken, newRefreshToken, err := h.db.RefreshToken(refreshToken, client.ID)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	// Return token response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  newAccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(db.AccessTokenExpiration.Seconds()),
		RefreshToken: newRefreshToken,
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
	resource := baseURL + "/mcp"
	suffix := strings.TrimPrefix(r.URL.Path, "/.well-known/oauth-protected-resource")
	if suffix != "" && suffix != "/" {
		resource = baseURL + suffix
	}

	metadata := ProtectedResourceMetadata{
		Resource:             resource,
		AuthorizationServers: []string{h.authorizationServerBaseURL(r)},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

func (h *Handler) buildMetadata(r *http.Request) OAuthMetadata {
	// Determine base URL from request or configured issuer
	baseURL := h.authorizationServerBaseURL(r)

	return OAuthMetadata{
		Issuer:                            baseURL,
		TokenEndpoint:                     baseURL + "/oauth/token",
		GrantTypesSupported:               []string{"client_credentials", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:            errorCode,
		ErrorDescription: description,
	})
}

func (h *Handler) parseClientCredentials(r *http.Request) (string, string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "basic") {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
			if err != nil {
				return "", "", fmt.Errorf("invalid basic auth")
			}
			creds := strings.SplitN(string(decoded), ":", 2)
			if len(creds) != 2 {
				return "", "", fmt.Errorf("invalid basic auth")
			}
			return creds[0], creds[1], nil
		}
	}

	return r.FormValue("client_id"), r.FormValue("client_secret"), nil
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

	return h.db.ValidateAccessToken(parts[1])
}

// Version returns the OAuth handler version (for logging)
func (h *Handler) Version() string {
	return version.Version
}
