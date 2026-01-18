package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
)

const (
	// APIKeyLength is the length of generated API keys in bytes (before base64 encoding)
	APIKeyLength = 32
	// BcryptCost is the cost factor for bcrypt hashing
	BcryptCost = 12
)

// Authenticator handles API key authentication
type Authenticator struct {
	db *db.DB
}

// NewAuthenticator creates a new Authenticator
func NewAuthenticator(database *db.DB) *Authenticator {
	return &Authenticator{db: database}
}

// GenerateAPIKey generates a new random API key
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, APIKeyLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// HashAPIKey hashes an API key using bcrypt
func HashAPIKey(apiKey string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(apiKey), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash API key: %w", err)
	}
	return string(hash), nil
}

// VerifyAPIKey verifies an API key against its hash
func VerifyAPIKey(apiKey, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(apiKey))
	return err == nil
}

// CreateAPIKey creates a new API key and stores it in the database
func (a *Authenticator) CreateAPIKey(name string) (apiKey string, keyRecord *db.APIKey, err error) {
	apiKey, err = GenerateAPIKey()
	if err != nil {
		return "", nil, err
	}

	hash, err := HashAPIKey(apiKey)
	if err != nil {
		return "", nil, err
	}

	keyRecord, err = a.db.CreateAPIKey(name, hash)
	if err != nil {
		return "", nil, err
	}

	return apiKey, keyRecord, nil
}

// Authenticate authenticates an API key and returns the associated key record
func (a *Authenticator) Authenticate(apiKey string) (*db.APIKey, error) {
	// Get all active API keys (in production, you might want to optimize this)
	keys, err := a.db.ListAPIKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to list API keys: %w", err)
	}

	for _, key := range keys {
		if key.Status != "active" {
			continue
		}
		if VerifyAPIKey(apiKey, key.KeyHash) {
			// Update last used timestamp
			if err := a.db.UpdateAPIKeyLastUsed(key.ID); err != nil {
				// Log error but don't fail authentication
				fmt.Printf("warning: failed to update last used: %v\n", err)
			}
			return key, nil
		}
	}

	return nil, nil // Not authenticated
}
