package auth

import (
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	key1, err := GenerateAPIKey()
	if err != nil {
		t.Errorf("GenerateAPIKey() error = %v", err)
		return
	}

	key2, err := GenerateAPIKey()
	if err != nil {
		t.Errorf("GenerateAPIKey() error = %v", err)
		return
	}

	// Keys should be different
	if key1 == key2 {
		t.Errorf("GenerateAPIKey() generated same key twice")
	}

	// Keys should have reasonable length
	if len(key1) < 20 {
		t.Errorf("GenerateAPIKey() key too short: %d", len(key1))
	}
}

func TestHashAPIKey(t *testing.T) {
	key := "test-api-key-12345"

	hash1, err := HashAPIKey(key)
	if err != nil {
		t.Errorf("HashAPIKey() error = %v", err)
		return
	}

	hash2, err := HashAPIKey(key)
	if err != nil {
		t.Errorf("HashAPIKey() error = %v", err)
		return
	}

	// Same key should produce different hashes (bcrypt salt)
	if hash1 == hash2 {
		t.Errorf("HashAPIKey() produced same hash for same key (no salt?)")
	}

	// Hash should be different from key
	if hash1 == key {
		t.Errorf("HashAPIKey() hash equals key")
	}
}

func TestVerifyAPIKey(t *testing.T) {
	key := "test-api-key-12345"
	hash, err := HashAPIKey(key)
	if err != nil {
		t.Errorf("HashAPIKey() error = %v", err)
		return
	}

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{
			name: "correct key",
			key:  key,
			want: true,
		},
		{
			name: "wrong key",
			key:  "wrong-key",
			want: false,
		},
		{
			name: "empty key",
			key:  "",
			want: false,
		},
		{
			name: "similar key",
			key:  key + "x",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifyAPIKey(tt.key, hash)
			if got != tt.want {
				t.Errorf("VerifyAPIKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
