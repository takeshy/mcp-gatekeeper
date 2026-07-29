package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
)

func newStdioTestServer(t *testing.T) *StdioServer {
	t.Helper()
	server, err := NewStdioServer(&plugin.Config{Tools: map[string]*plugin.Tool{}}, "", "", t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("NewStdioServer: %v", err)
	}
	return server
}

func TestStdioStatelessDiscover(t *testing.T) {
	server := newStdioTestServer(t)
	req := &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"discover"`),
		Method:  "server/discover",
		Params:  json.RawMessage(`{` + statelessMetaJSON + `}`),
	}
	resp, err := server.handleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(*DiscoverResult)
	if !ok || result.ResultType != "complete" || len(result.SupportedVersions) == 0 {
		t.Fatalf("unexpected discovery result: %#v", resp.Result)
	}
}

func TestStdioStatelessRequestRequiresMetadata(t *testing.T) {
	server := newStdioTestServer(t)
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list", Params: json.RawMessage(`{}`)}
	resp, err := server.handleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != UnsupportedProtocolVersionCode {
		t.Fatalf("expected metadata error, got %+v", resp.Error)
	}
}

func TestStdioLegacyInitializeEnablesLegacyRequests(t *testing.T) {
	server := newStdioTestServer(t)
	initialize := &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}`),
	}
	if resp, err := server.handleRequest(context.Background(), initialize); err != nil || resp.Error != nil {
		t.Fatalf("legacy initialize failed: resp=%+v err=%v", resp, err)
	}
	list := &Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/list", Params: json.RawMessage(`{}`)}
	resp, err := server.handleRequest(context.Background(), list)
	if err != nil || resp.Error != nil {
		t.Fatalf("legacy tools/list failed: resp=%+v err=%v", resp, err)
	}
}
