package bridge

import (
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(10, time.Minute)

	if limiter.limit != 10 {
		t.Errorf("expected limit 10, got %d", limiter.limit)
	}
	if limiter.window != time.Minute {
		t.Errorf("expected window 1m, got %v", limiter.window)
	}
}

func TestRateLimiterAllow(t *testing.T) {
	limiter := NewRateLimiter(3, time.Minute)

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !limiter.Allow() {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if limiter.Allow() {
		t.Error("4th request should be denied")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	limiter := NewRateLimiter(2, 50*time.Millisecond)

	// First 2 requests allowed
	if !limiter.Allow() {
		t.Error("1st request should be allowed")
	}
	if !limiter.Allow() {
		t.Error("2nd request should be allowed")
	}

	// 3rd request denied
	if limiter.Allow() {
		t.Error("3rd request should be denied")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !limiter.Allow() {
		t.Error("request after window expiry should be allowed")
	}
}

func TestNewServerMissingCommand(t *testing.T) {
	config := &ServerConfig{
		Command: "",
	}

	_, err := NewServer(config)
	if err == nil {
		t.Error("expected error when command is missing")
	}
}

func TestNewServerDefaults(t *testing.T) {
	config := &ServerConfig{
		Command: "echo",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	if server.rateLimiter.limit != 500 {
		t.Errorf("expected default rate limit 500, got %d", server.rateLimiter.limit)
	}
}

func TestNewServerWithAPIKey(t *testing.T) {
	config := &ServerConfig{
		Command: "echo",
		APIKey:  "test-key",
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	if server.apiKey != "test-key" {
		t.Errorf("expected API key 'test-key', got '%s'", server.apiKey)
	}
}
