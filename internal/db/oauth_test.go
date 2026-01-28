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

	// Test CreateOAuthClient
	clientSecret, err := db.CreateOAuthClient("test-client")
	if err != nil {
		t.Fatalf("CreateOAuthClient failed: %v", err)
	}
	if clientSecret == "" {
		t.Fatal("Expected non-empty client secret")
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

	// Test ValidateClientCredentials with correct secret
	validated, err := db.ValidateClientCredentials("test-client", clientSecret)
	if err != nil {
		t.Fatalf("ValidateClientCredentials failed: %v", err)
	}
	if validated == nil {
		t.Fatal("Expected credentials to be valid")
	}

	// Test ValidateClientCredentials with wrong secret
	validated, err = db.ValidateClientCredentials("test-client", "wrong-secret")
	if err != nil {
		t.Fatalf("ValidateClientCredentials failed: %v", err)
	}
	if validated != nil {
		t.Fatal("Expected invalid credentials")
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

	// Test that revoked client cannot authenticate
	validated, err = db.ValidateClientCredentials("test-client", clientSecret)
	if err != nil {
		t.Fatalf("ValidateClientCredentials failed: %v", err)
	}
	if validated != nil {
		t.Fatal("Expected revoked client to fail validation")
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
	_, err = db.CreateOAuthClient("token-test-client")
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
	_, err = db.CreateOAuthClient("cleanup-test-client")
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
