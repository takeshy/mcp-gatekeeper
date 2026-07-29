package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Token expiration durations
const (
	AccessTokenExpiration     = 1 * time.Hour
	AuthorizationCodeLifetime = 5 * time.Minute
)

// OAuthClient represents an OAuth client
type OAuthClient struct {
	ID           int64
	ClientID     string
	Status       string
	CreatedAt    time.Time
	RevokedAt    *time.Time
	RedirectURIs []string
	Scopes       []string
}

// OAuthAuthorizationCode is the data atomically consumed during a code exchange.
type OAuthAuthorizationCode struct {
	ClientID      int64
	RedirectURI   string
	Subject       string
	Scope         string
	Resource      string
	CodeChallenge string
}

// OAuthAccessToken describes the authorization bound to an access token.
type OAuthAccessToken struct {
	Client   *OAuthClient
	Subject  string
	Scope    string
	Resource string
}

// OAuthToken represents an OAuth token pair
type OAuthToken struct {
	ID               int64
	OAuthClientID    int64
	AccessTokenHash  string
	RefreshTokenHash string
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

// generateSecureToken generates a cryptographically secure random token
func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// hashToken creates a SHA256 hash of a token for storage
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// RegisterOAuthPublicClient registers a pre-configured Authorization Code client.
func (d *DB) RegisterOAuthPublicClient(clientID string, redirectURIs, scopes []string) error {
	if strings.TrimSpace(clientID) == "" || len(redirectURIs) == 0 {
		return fmt.Errorf("client ID and at least one redirect URI are required")
	}
	for _, redirectURI := range redirectURIs {
		parsed, err := url.Parse(redirectURI)
		if err != nil || parsed.Host == "" || parsed.Fragment != "" {
			return fmt.Errorf("invalid redirect URI %q", redirectURI)
		}
		if parsed.User != nil {
			return fmt.Errorf("redirect URI must not contain user information: %q", redirectURI)
		}
		host := parsed.Hostname()
		loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
			return fmt.Errorf("redirect URI must use HTTPS or HTTP loopback: %q", redirectURI)
		}
	}
	for _, scope := range scopes {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n") {
			return fmt.Errorf("invalid scope %q", scope)
		}
	}
	if !containsString(scopes, "mcp") {
		return fmt.Errorf("OAuth client scopes must include %q", "mcp")
	}
	redirectJSON, err := json.Marshal(redirectURIs)
	if err != nil {
		return err
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	// The legacy column remains for migration compatibility but public clients
	// never authenticate with this unusable random value.
	placeholder, err := generateSecureToken(32)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`
		INSERT INTO oauth_clients (client_id, client_secret_hash, status, redirect_uris, scopes)
		VALUES (?, ?, 'active', ?, ?)
	`, clientID, placeholder, string(redirectJSON), string(scopesJSON))
	if err != nil {
		return fmt.Errorf("failed to register OAuth client: %w", err)
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func scanOAuthClient(scanner interface{ Scan(...interface{}) error }) (*OAuthClient, error) {
	client := &OAuthClient{}
	var revokedAt sql.NullTime
	var legacySecretHash, redirectJSON, scopesJSON string
	if err := scanner.Scan(&client.ID, &client.ClientID, &legacySecretHash, &client.Status, &client.CreatedAt, &revokedAt, &redirectJSON, &scopesJSON); err != nil {
		return nil, err
	}
	if revokedAt.Valid {
		client.RevokedAt = &revokedAt.Time
	}
	if err := json.Unmarshal([]byte(redirectJSON), &client.RedirectURIs); err != nil {
		return nil, fmt.Errorf("invalid redirect URI data: %w", err)
	}
	if err := json.Unmarshal([]byte(scopesJSON), &client.Scopes); err != nil {
		return nil, fmt.Errorf("invalid scope data: %w", err)
	}
	return client, nil
}

// GetOAuthClient retrieves an OAuth client by client_id
func (d *DB) GetOAuthClient(clientID string) (*OAuthClient, error) {
	row := d.db.QueryRow(`
		SELECT id, client_id, client_secret_hash, status, created_at, revoked_at, redirect_uris, scopes
		FROM oauth_clients
		WHERE client_id = ?
	`, clientID)

	client, err := scanOAuthClient(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OAuth client: %w", err)
	}

	return client, nil
}

// GetOAuthClientByID retrieves an OAuth client by internal ID
func (d *DB) GetOAuthClientByID(id int64) (*OAuthClient, error) {
	row := d.db.QueryRow(`
		SELECT id, client_id, client_secret_hash, status, created_at, revoked_at, redirect_uris, scopes
		FROM oauth_clients
		WHERE id = ?
	`, id)

	client, err := scanOAuthClient(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OAuth client: %w", err)
	}

	return client, nil
}

// ListOAuthClients retrieves all OAuth clients
func (d *DB) ListOAuthClients() ([]*OAuthClient, error) {
	rows, err := d.db.Query(`
		SELECT id, client_id, client_secret_hash, status, created_at, revoked_at, redirect_uris, scopes
		FROM oauth_clients
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list OAuth clients: %w", err)
	}
	defer rows.Close()

	var clients []*OAuthClient
	for rows.Next() {
		client, err := scanOAuthClient(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan OAuth client: %w", err)
		}
		clients = append(clients, client)
	}

	return clients, nil
}

// RevokeOAuthClient revokes an OAuth client
func (d *DB) RevokeOAuthClient(id int64) error {
	result, err := d.db.Exec(`
		UPDATE oauth_clients
		SET status = 'revoked', revoked_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'active'
	`, id)
	if err != nil {
		return fmt.Errorf("failed to revoke OAuth client: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("client not found or already revoked")
	}

	// Also delete all tokens for this client
	_, err = d.db.Exec(`DELETE FROM oauth_tokens WHERE oauth_client_id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete tokens: %w", err)
	}
	if _, err := d.db.Exec(`DELETE FROM oauth_authorization_codes WHERE oauth_client_id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete authorization codes: %w", err)
	}

	return nil
}

// DeleteOAuthClient permanently deletes an OAuth client
func (d *DB) DeleteOAuthClient(id int64) error {
	result, err := d.db.Exec(`DELETE FROM oauth_clients WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete OAuth client: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("client not found")
	}

	return nil
}

// CreateToken creates a new OAuth token pair
func (d *DB) CreateToken(clientID int64) (accessToken, refreshToken string, err error) {
	return d.CreateAuthorizedToken(clientID, "", "", "")
}

// CreateAuthorizedToken creates a resource- and scope-bound token pair.
func (d *DB) CreateAuthorizedToken(clientID int64, subject, scope, resource string) (accessToken, refreshToken string, err error) {
	// Generate tokens
	accessToken, err = generateSecureToken(32)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err = generateSecureToken(32)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// Hash tokens for storage
	accessTokenHash := hashToken(accessToken)
	refreshTokenHash := hashToken(refreshToken)

	expiresAt := time.Now().Add(AccessTokenExpiration)

	_, err = d.db.Exec(`
		INSERT INTO oauth_tokens (oauth_client_id, access_token_hash, refresh_token_hash, expires_at, subject, scope, resource)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, clientID, accessTokenHash, refreshTokenHash, expiresAt, subject, scope, resource)
	if err != nil {
		return "", "", fmt.Errorf("failed to insert token: %w", err)
	}

	return accessToken, refreshToken, nil
}

// CreateAuthorizationCode creates a short-lived, single-use authorization code.
func (d *DB) CreateAuthorizationCode(clientID int64, redirectURI, subject, scope, resource, codeChallenge string) (string, error) {
	code, err := generateSecureToken(32)
	if err != nil {
		return "", err
	}
	_, err = d.db.Exec(`
		INSERT INTO oauth_authorization_codes
		(code_hash, oauth_client_id, redirect_uri, subject, scope, resource, code_challenge, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, hashToken(code), clientID, redirectURI, subject, scope, resource, codeChallenge, time.Now().Add(AuthorizationCodeLifetime))
	if err != nil {
		return "", fmt.Errorf("failed to create authorization code: %w", err)
	}
	return code, nil
}

// ConsumeAuthorizationCode atomically marks a code used and returns its binding.
func (d *DB) ConsumeAuthorizationCode(code string, clientID int64, redirectURI, codeChallenge string) (*OAuthAuthorizationCode, error) {
	row := d.db.QueryRow(`
		UPDATE oauth_authorization_codes
		SET used_at = CURRENT_TIMESTAMP
		WHERE code_hash = ? AND oauth_client_id = ? AND redirect_uri = ? AND code_challenge = ?
		  AND used_at IS NULL AND expires_at > CURRENT_TIMESTAMP
		RETURNING oauth_client_id, redirect_uri, subject, scope, resource, code_challenge
	`, hashToken(code), clientID, redirectURI, codeChallenge)
	result := &OAuthAuthorizationCode{}
	if err := row.Scan(&result.ClientID, &result.RedirectURI, &result.Subject, &result.Scope, &result.Resource, &result.CodeChallenge); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to consume authorization code: %w", err)
	}
	return result, nil
}

// ValidateAccessToken validates an access token and returns the associated client
func (d *DB) ValidateAccessToken(token string) (*OAuthClient, error) {
	authorization, err := d.ValidateAuthorizedAccessToken(token)
	if err != nil || authorization == nil {
		return nil, err
	}
	return authorization.Client, nil
}

// ValidateAuthorizedAccessToken validates a token and returns its authorization context.
func (d *DB) ValidateAuthorizedAccessToken(token string) (*OAuthAccessToken, error) {
	tokenHash := hashToken(token)

	row := d.db.QueryRow(`
		SELECT t.oauth_client_id, t.expires_at, t.subject, t.scope, t.resource
		FROM oauth_tokens t
		WHERE t.access_token_hash = ?
	`, tokenHash)

	var clientID int64
	var expiresAt time.Time
	var subject, scope, resource string
	if err := row.Scan(&clientID, &expiresAt, &subject, &scope, &resource); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to validate access token: %w", err)
	}

	// Check if token is expired
	if time.Now().After(expiresAt) {
		return nil, nil
	}

	// Get the client and check if it's still active
	client, err := d.GetOAuthClientByID(clientID)
	if err != nil {
		return nil, err
	}
	if client == nil || client.Status != "active" {
		return nil, nil
	}

	return &OAuthAccessToken{Client: client, Subject: subject, Scope: scope, Resource: resource}, nil
}

// RefreshToken exchanges a refresh token for new access and refresh tokens
func (d *DB) RefreshToken(refreshToken string, clientID int64) (newAccessToken, newRefreshToken string, err error) {
	return d.RefreshAuthorizedToken(refreshToken, clientID, "")
}

// RefreshAuthorizedToken rotates a refresh token bound to the requested resource.
func (d *DB) RefreshAuthorizedToken(refreshToken string, clientID int64, expectedResource string) (newAccessToken, newRefreshToken string, err error) {
	refreshTokenHash := hashToken(refreshToken)
	tx, err := d.db.Begin()
	if err != nil {
		return "", "", fmt.Errorf("failed to begin refresh transaction: %w", err)
	}
	defer tx.Rollback()

	// Read the authorization and active-client state in the same transaction
	// that rotates the token, preventing concurrent reuse.
	row := tx.QueryRow(`
		SELECT t.id, t.oauth_client_id, t.subject, t.scope, t.resource
		FROM oauth_tokens t
		JOIN oauth_clients c ON c.id = t.oauth_client_id
		WHERE t.refresh_token_hash = ? AND t.oauth_client_id = ? AND t.resource = ?
		  AND c.status = 'active'
	`, refreshTokenHash, clientID, expectedResource)

	var tokenID, tokenClientID int64
	var subject, scope, resource string
	if err := row.Scan(&tokenID, &tokenClientID, &subject, &scope, &resource); err != nil {
		if err == sql.ErrNoRows {
			return "", "", fmt.Errorf("invalid refresh token")
		}
		return "", "", fmt.Errorf("failed to get refresh token: %w", err)
	}

	newAccessToken, err = generateSecureToken(32)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate access token: %w", err)
	}
	newRefreshToken, err = generateSecureToken(32)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate refresh token: %w", err)
	}

	result, err := tx.Exec(`DELETE FROM oauth_tokens WHERE id = ? AND refresh_token_hash = ?`, tokenID, refreshTokenHash)
	if err != nil {
		return "", "", fmt.Errorf("failed to delete old token: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return "", "", fmt.Errorf("refresh token was already used")
	}

	_, err = tx.Exec(`
		INSERT INTO oauth_tokens (oauth_client_id, access_token_hash, refresh_token_hash, expires_at, subject, scope, resource)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, tokenClientID, hashToken(newAccessToken), hashToken(newRefreshToken), time.Now().Add(AccessTokenExpiration), subject, scope, resource)
	if err != nil {
		return "", "", fmt.Errorf("failed to insert refreshed token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("failed to commit refresh transaction: %w", err)
	}
	return newAccessToken, newRefreshToken, nil
}

// CleanupExpiredTokens removes expired or consumed authorization codes.
// Token rows are retained because they contain independently valid refresh tokens.
func (d *DB) CleanupExpiredTokens() error {
	// oauth_tokens rows also contain refresh tokens, which remain valid after
	// their paired access token expires. Expired access tokens are rejected by
	// ValidateAuthorizedAccessToken; the row is removed when the refresh token
	// is rotated or its client is revoked.
	if _, err := d.db.Exec(`DELETE FROM oauth_authorization_codes WHERE expires_at <= CURRENT_TIMESTAMP OR used_at IS NOT NULL`); err != nil {
		return fmt.Errorf("failed to cleanup authorization codes: %w", err)
	}

	return nil
}
