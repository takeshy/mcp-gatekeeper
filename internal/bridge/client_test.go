package bridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	config := &ClientConfig{
		Command:   "echo",
		Args:      []string{"hello"},
		Timeout:   10 * time.Second,
		MaxOutput: 1024,
	}

	client := NewClient(config)

	if client.command != "echo" {
		t.Errorf("expected command 'echo', got '%s'", client.command)
	}
	if len(client.args) != 1 || client.args[0] != "hello" {
		t.Errorf("expected args ['hello'], got %v", client.args)
	}
	if client.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", client.timeout)
	}
	if client.maxOutput != 1024 {
		t.Errorf("expected maxOutput 1024, got %d", client.maxOutput)
	}
}

func TestDefaultClientConfig(t *testing.T) {
	config := DefaultClientConfig()

	if config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", config.Timeout)
	}
	if config.MaxOutput != 1024*1024 {
		t.Errorf("expected default maxOutput 1MB, got %d", config.MaxOutput)
	}
}

func TestClientNotStarted(t *testing.T) {
	client := NewClient(&ClientConfig{
		Command: "echo",
	})

	ctx := context.Background()
	_, err := client.Call(ctx, "test", nil)
	if err == nil {
		t.Error("expected error when calling without starting")
	}
}

func TestRequestMarshal(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "test/method",
		Params:  json.RawMessage(`{"key":"value"}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	var parsed Request
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if parsed.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got '%s'", parsed.JSONRPC)
	}
	if parsed.Method != "test/method" {
		t.Errorf("expected method 'test/method', got '%s'", parsed.Method)
	}
}

func TestResponseMarshal(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Result:  json.RawMessage(`{"status":"ok"}`),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var parsed Response
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if parsed.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got '%s'", parsed.JSONRPC)
	}
}

func TestResponseWithError(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Error: &RPCError{
			Code:    -32600,
			Message: "Invalid Request",
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var parsed Response
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if parsed.Error == nil {
		t.Fatal("expected error in response")
	}
	if parsed.Error.Code != -32600 {
		t.Errorf("expected error code -32600, got %d", parsed.Error.Code)
	}
	if parsed.Error.Message != "Invalid Request" {
		t.Errorf("expected error message 'Invalid Request', got '%s'", parsed.Error.Message)
	}
}
