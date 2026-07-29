package db

import (
	"os"
	"testing"
	"time"
)

func TestOAuthClientCRUD(t *testing.T) {
	// Create temp database
	tmpFile, err := os.CreateTemp("", "test-oauth-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Test RegisterOAuthPublicClient
	err = db.RegisterOAuthPublicClient(
		"test-client",
		[]string{"https://client.example/callback"},
		[]string{"mcp"},
	)
	if err != nil {
		t.Fatalf("RegisterOAuthPublicClient failed: %v", err)
	}

	// Test GetOAuthClient
	client, err := db.GetOAuthClient("test-client")
	if err != nil {
		t.Fatalf("GetOAuthClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("Expected client to exist")
	}
	if client.ClientID != "test-client" {
		t.Errorf("Expected client_id 'test-client', got '%s'", client.ClientID)
	}
	if client.Status != "active" {
		t.Errorf("Expected status 'active', got '%s'", client.Status)
	}
	if len(client.RedirectURIs) != 1 || client.RedirectURIs[0] != "https://client.example/callback" {
		t.Fatalf("Unexpected redirect URIs: %v", client.RedirectURIs)
	}
	if len(client.Scopes) != 1 || client.Scopes[0] != "mcp" {
		t.Fatalf("Unexpected scopes: %v", client.Scopes)
	}

	// Test ListOAuthClients
	clients, err := db.ListOAuthClients()
	if err != nil {
		t.Fatalf("ListOAuthClients failed: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("Expected 1 client, got %d", len(clients))
	}

	// Test RevokeOAuthClient
	err = db.RevokeOAuthClient(client.ID)
	if err != nil {
		t.Fatalf("RevokeOAuthClient failed: %v", err)
	}

	client, err = db.GetOAuthClient("test-client")
	if err != nil {
		t.Fatalf("GetOAuthClient failed: %v", err)
	}
	if client.Status != "revoked" {
		t.Errorf("Expected status 'revoked', got '%s'", client.Status)
	}

	// Test DeleteOAuthClient
	err = db.DeleteOAuthClient(client.ID)
	if err != nil {
		t.Fatalf("DeleteOAuthClient failed: %v", err)
	}

	client, err = db.GetOAuthClient("test-client")
	if err != nil {
		t.Fatalf("GetOAuthClient failed: %v", err)
	}
	if client != nil {
		t.Fatal("Expected client to be deleted")
	}
}

func TestOAuthTokenFlow(t *testing.T) {
	// Create temp database
	tmpFile, err := os.CreateTemp("", "test-oauth-token-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create client
	err = db.RegisterOAuthPublicClient(
		"token-test-client",
		[]string{"https://client.example/callback"},
		[]string{"mcp"},
	)
	if err != nil {
		t.Fatal(err)
	}

	client, err := db.GetOAuthClient("token-test-client")
	if err != nil {
		t.Fatal(err)
	}

	// Test CreateToken
	accessToken, refreshToken, err := db.CreateToken(client.ID)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if accessToken == "" {
		t.Fatal("Expected non-empty access token")
	}
	if refreshToken == "" {
		t.Fatal("Expected non-empty refresh token")
	}

	// Test ValidateAccessToken
	validated, err := db.ValidateAccessToken(accessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if validated == nil {
		t.Fatal("Expected access token to be valid")
	}
	if validated.ClientID != "token-test-client" {
		t.Errorf("Expected client_id 'token-test-client', got '%s'", validated.ClientID)
	}

	// Test ValidateAccessToken with invalid token
	validated, err = db.ValidateAccessToken("invalid-token")
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if validated != nil {
		t.Fatal("Expected invalid token to fail validation")
	}

	// Test RefreshToken
	newAccessToken, newRefreshToken, err := db.RefreshToken(refreshToken, client.ID)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if newAccessToken == "" {
		t.Fatal("Expected non-empty new access token")
	}
	if newRefreshToken == "" {
		t.Fatal("Expected non-empty new refresh token")
	}

	// Old access token should be invalid
	validated, err = db.ValidateAccessToken(accessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if validated != nil {
		t.Fatal("Expected old access token to be invalid after refresh")
	}

	// New access token should be valid
	validated, err = db.ValidateAccessToken(newAccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if validated == nil {
		t.Fatal("Expected new access token to be valid")
	}
}

func TestOAuthCleanup(t *testing.T) {
	// Create temp database
	tmpFile, err := os.CreateTemp("", "test-oauth-cleanup-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	db, err := Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create client
	err = db.RegisterOAuthPublicClient(
		"cleanup-test-client",
		[]string{"https://client.example/callback"},
		[]string{"mcp"},
	)
	if err != nil {
		t.Fatal(err)
	}

	client, err := db.GetOAuthClient("cleanup-test-client")
	if err != nil {
		t.Fatal(err)
	}

	// Create token
	_, _, err = db.CreateToken(client.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Test CleanupExpiredTokens (should not remove non-expired tokens)
	err = db.CleanupExpiredTokens()
	if err != nil {
		t.Fatalf("CleanupExpiredTokens failed: %v", err)
	}

	if AccessTokenExpiration != 1*time.Hour {
		t.Errorf("Expected AccessTokenExpiration to be 1 hour, got %v", AccessTokenExpiration)
	}
}

func TestOAuthCleanupPreservesRefreshTokenAfterAccessExpiry(t *testing.T) {
	database, err := Open(t.TempDir() + "/oauth-cleanup.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.RegisterOAuthPublicClient("refresh-client", []string{"https://client.example/callback"}, []string{"mcp"}); err != nil {
		t.Fatal(err)
	}
	client, err := database.GetOAuthClient("refresh-client")
	if err != nil {
		t.Fatal(err)
	}
	_, refreshToken, err := database.CreateToken(client.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`UPDATE oauth_tokens SET expires_at = ? WHERE oauth_client_id = ?`, time.Now().Add(-time.Minute), client.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.CleanupExpiredTokens(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.RefreshToken(refreshToken, client.ID); err != nil {
		t.Fatalf("refresh token was removed with its expired access token: %v", err)
	}
}
