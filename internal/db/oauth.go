package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Token expiration durations
const (
	AccessTokenExpiration = 1 * time.Hour
)

// OAuthClient represents an OAuth client
type OAuthClient struct {
	ID               int64
	ClientID         string
	ClientSecretHash string
	Status           string
	CreatedAt        time.Time
	RevokedAt        *time.Time
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

// CreateOAuthClient creates a new OAuth client and returns the generated client secret
func (d *DB) CreateOAuthClient(clientID string) (clientSecret string, err error) {
	// Generate a secure client secret
	clientSecret, err = generateSecureToken(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate client secret: %w", err)
	}

	// Hash the client secret for storage
	hashedSecret, err := bcrypt.GenerateFromPassword([]byte(clientSecret), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash client secret: %w", err)
	}

	_, err = d.db.Exec(`
		INSERT INTO oauth_clients (client_id, client_secret_hash, status)
		VALUES (?, ?, 'active')
	`, clientID, string(hashedSecret))
	if err != nil {
		return "", fmt.Errorf("failed to insert OAuth client: %w", err)
	}

	return clientSecret, nil
}

// GetOAuthClient retrieves an OAuth client by client_id
func (d *DB) GetOAuthClient(clientID string) (*OAuthClient, error) {
	row := d.db.QueryRow(`
		SELECT id, client_id, client_secret_hash, status, created_at, revoked_at
		FROM oauth_clients
		WHERE client_id = ?
	`, clientID)

	client := &OAuthClient{}
	var revokedAt sql.NullTime
	if err := row.Scan(&client.ID, &client.ClientID, &client.ClientSecretHash, &client.Status, &client.CreatedAt, &revokedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OAuth client: %w", err)
	}

	if revokedAt.Valid {
		client.RevokedAt = &revokedAt.Time
	}

	return client, nil
}

// GetOAuthClientByID retrieves an OAuth client by internal ID
func (d *DB) GetOAuthClientByID(id int64) (*OAuthClient, error) {
	row := d.db.QueryRow(`
		SELECT id, client_id, client_secret_hash, status, created_at, revoked_at
		FROM oauth_clients
		WHERE id = ?
	`, id)

	client := &OAuthClient{}
	var revokedAt sql.NullTime
	if err := row.Scan(&client.ID, &client.ClientID, &client.ClientSecretHash, &client.Status, &client.CreatedAt, &revokedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OAuth client: %w", err)
	}

	if revokedAt.Valid {
		client.RevokedAt = &revokedAt.Time
	}

	return client, nil
}

// ListOAuthClients retrieves all OAuth clients
func (d *DB) ListOAuthClients() ([]*OAuthClient, error) {
	rows, err := d.db.Query(`
		SELECT id, client_id, client_secret_hash, status, created_at, revoked_at
		FROM oauth_clients
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list OAuth clients: %w", err)
	}
	defer rows.Close()

	var clients []*OAuthClient
	for rows.Next() {
		client := &OAuthClient{}
		var revokedAt sql.NullTime
		if err := rows.Scan(&client.ID, &client.ClientID, &client.ClientSecretHash, &client.Status, &client.CreatedAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("failed to scan OAuth client: %w", err)
		}
		if revokedAt.Valid {
			client.RevokedAt = &revokedAt.Time
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

// ValidateClientCredentials validates OAuth client credentials
func (d *DB) ValidateClientCredentials(clientID, clientSecret string) (*OAuthClient, error) {
	client, err := d.GetOAuthClient(clientID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, nil
	}

	if client.Status != "active" {
		return nil, nil
	}

	if err := bcrypt.CompareHashAndPassword([]byte(client.ClientSecretHash), []byte(clientSecret)); err != nil {
		return nil, nil
	}

	return client, nil
}

// CreateToken creates a new OAuth token pair
func (d *DB) CreateToken(clientID int64) (accessToken, refreshToken string, err error) {
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
		INSERT INTO oauth_tokens (oauth_client_id, access_token_hash, refresh_token_hash, expires_at)
		VALUES (?, ?, ?, ?)
	`, clientID, accessTokenHash, refreshTokenHash, expiresAt)
	if err != nil {
		return "", "", fmt.Errorf("failed to insert token: %w", err)
	}

	return accessToken, refreshToken, nil
}

// ValidateAccessToken validates an access token and returns the associated client
func (d *DB) ValidateAccessToken(token string) (*OAuthClient, error) {
	tokenHash := hashToken(token)

	row := d.db.QueryRow(`
		SELECT t.oauth_client_id, t.expires_at
		FROM oauth_tokens t
		WHERE t.access_token_hash = ?
	`, tokenHash)

	var clientID int64
	var expiresAt time.Time
	if err := row.Scan(&clientID, &expiresAt); err != nil {
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

	return client, nil
}

// RefreshToken exchanges a refresh token for new access and refresh tokens
func (d *DB) RefreshToken(refreshToken string, clientID int64) (newAccessToken, newRefreshToken string, err error) {
	refreshTokenHash := hashToken(refreshToken)

	// Get the token record
	row := d.db.QueryRow(`
		SELECT id, oauth_client_id
		FROM oauth_tokens
		WHERE refresh_token_hash = ? AND oauth_client_id = ?
	`, refreshTokenHash, clientID)

	var tokenID, tokenClientID int64
	if err := row.Scan(&tokenID, &tokenClientID); err != nil {
		if err == sql.ErrNoRows {
			return "", "", fmt.Errorf("invalid refresh token")
		}
		return "", "", fmt.Errorf("failed to get refresh token: %w", err)
	}

	// Check if client is still active
	client, err := d.GetOAuthClientByID(tokenClientID)
	if err != nil {
		return "", "", err
	}
	if client == nil || client.Status != "active" {
		return "", "", fmt.Errorf("client is inactive or revoked")
	}

	// Delete the old token
	_, err = d.db.Exec(`DELETE FROM oauth_tokens WHERE id = ?`, tokenID)
	if err != nil {
		return "", "", fmt.Errorf("failed to delete old token: %w", err)
	}

	// Create new token pair
	return d.CreateToken(tokenClientID)
}

// CleanupExpiredTokens removes expired authorization codes and tokens
func (d *DB) CleanupExpiredTokens() error {
	// Delete expired access tokens
	_, err := d.db.Exec(`DELETE FROM oauth_tokens WHERE expires_at <= CURRENT_TIMESTAMP`)
	if err != nil {
		return fmt.Errorf("failed to cleanup tokens: %w", err)
	}

	return nil
}
