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

func TestResponseWithStringID(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"string-id-123"`),
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

	// Verify the ID is preserved as-is
	if string(parsed.ID) != `"string-id-123"` {
		t.Errorf("expected ID '\"string-id-123\"', got '%s'", string(parsed.ID))
	}
}

func TestForwardNotification(t *testing.T) {
	client := NewClient(&ClientConfig{
		Command: "echo",
	})

	// Forward a notification (no ID) without starting should fail
	rawReq := []byte(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	_, err := client.Forward(context.Background(), rawReq)
	if err == nil {
		t.Error("expected error when forwarding without starting")
	}
}

func TestForwardParseError(t *testing.T) {
	client := NewClient(&ClientConfig{
		Command: "echo",
	})

	rawReq := []byte(`invalid json`)
	_, err := client.Forward(context.Background(), rawReq)
	if err == nil {
		t.Error("expected error when forwarding invalid JSON")
	}
}

func TestNotifyNotStarted(t *testing.T) {
	client := NewClient(&ClientConfig{
		Command: "echo",
	})

	err := client.Notify("test/method", nil)
	if err == nil {
		t.Error("expected error when notifying without starting")
	}
}

func TestClientAlreadyStarted(t *testing.T) {
	client := NewClient(&ClientConfig{
		Command: "sleep",
		Args:    []string{"10"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	// Try to start again
	err := client.Start(ctx)
	if err == nil {
		t.Error("expected error when starting already started client")
	}
}

func TestIsInitialized(t *testing.T) {
	client := NewClient(&ClientConfig{
		Command: "echo",
	})

	if client.IsInitialized() {
		t.Error("expected IsInitialized to be false before initialization")
	}
}

func TestClientClose(t *testing.T) {
	client := NewClient(&ClientConfig{
		Command: "sleep",
		Args:    []string{"10"},
	})

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	// Close should terminate the process
	if err := client.Close(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	// Double close should be safe
	if err := client.Close(); err != nil {
		t.Errorf("unexpected error on double close: %v", err)
	}
}
