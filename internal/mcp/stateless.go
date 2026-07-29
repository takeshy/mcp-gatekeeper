package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/takeshy/mcp-gatekeeper/internal/version"
)

const (
	HeaderMCPMethod = "Mcp-Method"
	HeaderMCPName   = "Mcp-Name"

	HeaderMismatchCode             = -32020
	MissingRequiredCapabilityCode  = -32021
	UnsupportedProtocolVersionCode = -32022
)

// validateStatelessRequest checks the transport headers and per-request metadata
// required by MCP 2026-07-28.
func validateStatelessRequest(r *http.Request, req *Request) error {
	if method := r.Header.Get(HeaderMCPMethod); method == "" {
		return fmt.Errorf("missing %s header", HeaderMCPMethod)
	} else if method != req.Method {
		return fmt.Errorf("%s header %q does not match request method %q", HeaderMCPMethod, method, req.Method)
	}

	if field := statelessNameField(req.Method); field != "" {
		var params map[string]interface{}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fmt.Errorf("invalid params for %s validation: %w", HeaderMCPName, err)
		}
		name, ok := params[field].(string)
		if !ok || name == "" {
			return fmt.Errorf("request params.%s must be a non-empty string", field)
		}
		if headerName := r.Header.Get(HeaderMCPName); headerName == "" {
			return fmt.Errorf("missing %s header", HeaderMCPName)
		} else if headerName != name {
			return fmt.Errorf("%s header %q does not match params.%s %q", HeaderMCPName, headerName, field, name)
		}
	}

	return validateStatelessMetadata(req)
}

func statelessNameField(method string) string {
	switch method {
	case "tools/call", "prompts/get":
		return "name"
	case "resources/read":
		return "uri"
	default:
		return ""
	}
}

func validateStatelessMetadata(req *Request) error {
	var params RequestParamsMeta
	if len(req.Params) == 0 {
		return fmt.Errorf("missing params._meta")
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return fmt.Errorf("invalid request params: %w", err)
	}
	if params.Meta == nil {
		return fmt.Errorf("missing params._meta")
	}
	if params.Meta.ProtocolVersion != version.MCPProtocolVersion {
		return fmt.Errorf("unsupported protocol version %q", params.Meta.ProtocolVersion)
	}
	if params.Meta.ClientInfo != nil && (params.Meta.ClientInfo.Name == "" || params.Meta.ClientInfo.Version == "") {
		return fmt.Errorf("params._meta clientInfo must include name and version")
	}
	if params.Meta.ClientCapabilities == nil {
		return fmt.Errorf("missing params._meta clientCapabilities")
	}
	return nil
}

func discoverResult(hasResources bool, extensions map[string]map[string]interface{}) *DiscoverResult {
	caps := ServerCapabilities{
		Tools:      &ToolsCapability{ListChanged: false},
		Extensions: extensions,
	}
	if hasResources {
		caps.Resources = &ResourcesCapability{Subscribe: false, ListChanged: false}
	}
	return &DiscoverResult{
		SupportedVersions: append([]string(nil), version.SupportedMCPProtocolVersions...),
		Capabilities:      caps,
	}
}

func decorateStatelessResult(resp *Response, method string) {
	if resp == nil || resp.Error != nil || resp.Result == nil {
		return
	}
	data, err := json.Marshal(resp.Result)
	if err != nil {
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return
	}
	result["resultType"] = "complete"
	meta, _ := result["_meta"].(map[string]interface{})
	if meta == nil {
		meta = make(map[string]interface{})
	}
	meta["io.modelcontextprotocol/serverInfo"] = map[string]interface{}{
		"name": ServerName, "version": ServerVersion,
	}
	result["_meta"] = meta
	switch method {
	case "server/discover", "tools/list", "resources/list", "resources/read", "resources/templates/list", "prompts/list":
		result["ttlMs"] = 0
		result["cacheScope"] = "private"
	}
	resp.Result = result
}
