package mcp

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/executor"
	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
	"github.com/takeshy/mcp-gatekeeper/internal/policy"
)

const (
	ProtocolVersion = "2024-11-05"
	ServerName      = "mcp-gatekeeper"
	ServerVersion   = "1.0.0"
)

// StdioServer implements the MCP server over stdio
type StdioServer struct {
	plugins     *plugin.Config
	evaluator   *policy.Evaluator
	executor    *executor.Executor
	initialized bool
	reader      *bufio.Reader
	writer      io.Writer
	rootDir     string
	db          *db.DB // Optional database for audit logging
}

// NewStdioServer creates a new stdio MCP server
func NewStdioServer(plugins *plugin.Config, apiKey string, expectedAPIKey string, rootDir string, wasmDir string, database *db.DB) (*StdioServer, error) {
	// Validate API key if expected key is set
	if expectedAPIKey != "" {
		if apiKey == "" {
			return nil, fmt.Errorf("API key required")
		}
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(expectedAPIKey)) != 1 {
			return nil, fmt.Errorf("invalid API key")
		}
	}

	execConfig := &executor.ExecutorConfig{
		Timeout:   executor.DefaultTimeout,
		MaxOutput: executor.DefaultMaxOutput,
		RootDir:   rootDir,
		WasmDir:   wasmDir,
	}

	return &StdioServer{
		plugins:   plugins,
		evaluator: policy.NewEvaluator(),
		executor:  executor.NewExecutor(execConfig),
		reader:    bufio.NewReader(os.Stdin),
		writer:    os.Stdout,
		rootDir:   rootDir,
		db:        database,
	}, nil
}

// Run runs the stdio server
func (s *StdioServer) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		response, err := s.handleMessage(ctx, []byte(line))
		if err != nil {
			// Log error but continue
			fmt.Fprintf(os.Stderr, "error handling message: %v\n", err)
			continue
		}

		if response != nil {
			if err := s.writeResponse(response); err != nil {
				return fmt.Errorf("failed to write response: %w", err)
			}
		}
	}
}

func (s *StdioServer) handleMessage(ctx context.Context, data []byte) (*Response, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return NewErrorResponse(nil, ParseError, "Parse error", err.Error()), nil
	}

	if req.JSONRPC != "2.0" {
		return NewErrorResponse(req.ID, InvalidRequest, "Invalid Request", "jsonrpc must be 2.0"), nil
	}

	// Handle notifications (no id)
	if req.ID == nil || string(req.ID) == "null" {
		if err := s.handleNotification(ctx, &req); err != nil {
			fmt.Fprintf(os.Stderr, "notification error: %v\n", err)
		}
		return nil, nil
	}

	// Handle request
	return s.handleRequest(ctx, &req)
}

func (s *StdioServer) handleNotification(ctx context.Context, req *Request) error {
	switch req.Method {
	case "notifications/initialized":
		s.initialized = true
		return nil
	case "notifications/cancelled":
		// Handle cancellation
		return nil
	default:
		return fmt.Errorf("unknown notification: %s", req.Method)
	}
}

func (s *StdioServer) handleRequest(ctx context.Context, req *Request) (*Response, error) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	case "ping":
		return NewResponse(req.ID, struct{}{}), nil
	default:
		return NewErrorResponse(req.ID, MethodNotFound, "Method not found", req.Method), nil
	}
}

func (s *StdioServer) handleInitialize(req *Request) (*Response, error) {
	caps := ServerCapabilities{
		Tools: &ToolsCapability{
			ListChanged: false,
		},
	}

	// Add resources capability if any tool has UI enabled
	if s.hasUIEnabledTools() {
		caps.Resources = &ResourcesCapability{
			Subscribe:   false,
			ListChanged: false,
		}
	}

	result := &InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    caps,
		ServerInfo: ServerInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}
	return NewResponse(req.ID, result), nil
}

func (s *StdioServer) hasUIEnabledTools() bool {
	for _, t := range s.plugins.ListTools() {
		if t.UIType != "" || t.UITemplate != "" {
			return true
		}
	}
	return false
}

func (s *StdioServer) handleToolsList(req *Request) (*Response, error) {
	// Get tools from plugins
	pluginTools := s.plugins.ListTools()

	tools := make([]Tool, 0, len(pluginTools))
	for _, t := range pluginTools {
		// Filter out tools that are not visible to the model
		if !t.IsVisibleToModel() {
			continue
		}
		tools = append(tools, Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cwd": {
						Type:        "string",
						Description: "Working directory for the command (defaults to root directory)",
					},
					"args": {
						Type:        "array",
						Description: "Command arguments",
						Items:       &Items{Type: "string"},
					},
				},
				Required: []string{},
			},
			Meta: BuildToolMeta(t),
		})
	}
	return NewResponse(req.ID, &ListToolsResult{Tools: tools}), nil
}

func (s *StdioServer) handleToolsCall(ctx context.Context, req *Request) (*Response, error) {
	startTime := time.Now()
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Invalid params: %v\n", err)
		resp := NewErrorResponse(req.ID, InvalidParams, "Invalid params", err.Error())
		s.logAudit(req.Method, params.Name, req.Params, resp, err, startTime)
		return resp, nil
	}

	// Look up tool by name from plugins
	tool := s.plugins.GetTool(params.Name)
	if tool == nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Tool not found: %s\n", params.Name)
		resp := NewErrorResponse(req.ID, MethodNotFound, "Tool not found", params.Name)
		s.logAudit(req.Method, params.Name, req.Params, resp, fmt.Errorf("tool not found: %s", params.Name), startTime)
		return resp, nil
	}

	return s.handleExecute(ctx, req.ID, req.Method, tool, params.Arguments, req.Params, startTime)
}

func (s *StdioServer) handleExecute(ctx context.Context, id json.RawMessage, method string, tool *plugin.Tool, args map[string]interface{}, rawParams json.RawMessage, startTime time.Time) (*Response, error) {
	// Parse arguments
	cwd, _ := args["cwd"].(string)
	var cmdArgs []string
	if argsRaw, ok := args["args"].([]interface{}); ok {
		for _, a := range argsRaw {
			if str, ok := a.(string); ok {
				cmdArgs = append(cmdArgs, str)
			}
		}
	}

	// Default cwd to rootDir if not provided
	if cwd == "" {
		cwd = s.rootDir
	}

	// Evaluate policy (check if user-provided args are allowed)
	decision, err := s.evaluator.EvaluateArgs(tool, cmdArgs)
	if err != nil {
		resp := NewErrorResponse(id, InternalError, "Policy evaluation failed", err.Error())
		s.logAudit(method, tool.Name, rawParams, resp, err, startTime)
		return resp, nil
	}

	if !decision.Allowed {
		fmt.Fprintf(os.Stderr, "[WARN] Arguments denied by policy: %s\n", decision.Reason)
		resp := NewErrorResponse(id, PolicyDenied, "Arguments denied by policy", decision.Reason)
		s.logAudit(method, tool.Name, rawParams, resp, fmt.Errorf("policy denied: %s", decision.Reason), startTime)
		return resp, nil
	}

	// Filter environment variables
	filteredEnvKeys := s.evaluator.FilterEnvKeys(s.plugins.AllowedEnvKeys, getEnvKeys(os.Environ()))
	filteredEnv := filterEnvByKeys(os.Environ(), filteredEnvKeys)

	// Prepend args_prefix if defined (after policy evaluation)
	if len(tool.ArgsPrefix) > 0 {
		cmdArgs = append(tool.ArgsPrefix, cmdArgs...)
	}

	// Execute command using the tool's sandbox setting
	result, err := s.executor.ExecuteWithSandbox(ctx, cwd, tool.Command, cmdArgs, filteredEnv, tool.Sandbox, tool.WasmBinary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Execution failed: %v\n", err)
		resp := NewErrorResponse(id, ExecutionFailed, "Execution failed", err.Error())
		s.logAudit(method, tool.Name, rawParams, resp, err, startTime)
		return resp, nil
	}

	// Return result
	content := []Content{
		{
			Type: "text",
			Text: result.Stdout,
		},
	}

	resp := NewResponse(id, &CallToolResult{
		Content: content,
		IsError: result.ExitCode != 0,
		Metadata: &ResultMetadata{
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
		},
		Meta: BuildResultMeta(tool, result.Stdout),
	})
	s.logAudit(method, tool.Name, rawParams, resp, nil, startTime)
	return resp, nil
}

// logAudit logs an audit entry if database is configured
func (s *StdioServer) logAudit(method string, toolName string, params interface{}, resp *Response, err error, startTime time.Time) {
	if s.db == nil {
		return
	}
	if logErr := s.db.LogAudit(db.AuditModeStdio, method, toolName, params, resp, err, startTime); logErr != nil {
		fmt.Fprintf(os.Stderr, "[WARN] Failed to log audit: %v\n", logErr)
	}
}

func (s *StdioServer) handleResourcesList(req *Request) (*Response, error) {
	// List UI resources for tools that have UI enabled
	pluginTools := s.plugins.ListTools()

	var resources []Resource
	for _, t := range pluginTools {
		if t.UIType != "" || t.UITemplate != "" {
			resources = append(resources, Resource{
				URI:         UIResourceURI(t.Name),
				Name:        fmt.Sprintf("%s UI", t.Name),
				Description: fmt.Sprintf("Interactive UI for %s tool", t.Name),
				MimeType:    "text/html",
				Meta:        BuildResourceMeta(t),
			})
		}
	}

	return NewResponse(req.ID, &ListResourcesResult{Resources: resources}), nil
}

func (s *StdioServer) handleResourcesRead(req *Request) (*Response, error) {
	var params ReadResourceParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid params", err.Error()), nil
	}

	// Parse ui:// URI
	if !strings.HasPrefix(params.URI, "ui://") {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid resource URI", "Only ui:// URIs are supported"), nil
	}

	// Extract tool name and query string
	uriPath := strings.TrimPrefix(params.URI, "ui://")
	parts := strings.SplitN(uriPath, "?", 2)
	pathParts := strings.Split(parts[0], "/")
	if len(pathParts) < 1 {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid resource URI", "Missing tool name"), nil
	}
	toolName := pathParts[0]

	// Get the tool from plugins
	tool := s.plugins.GetTool(toolName)
	if tool == nil {
		return NewErrorResponse(req.ID, MethodNotFound, "Tool not found", toolName), nil
	}

	// Check if tool has UI enabled
	if tool.UIType == "" && tool.UITemplate == "" {
		return NewErrorResponse(req.ID, InvalidParams, "Tool has no UI", toolName), nil
	}

	// Extract data from query string
	var encodedData string
	if len(parts) > 1 {
		queryParts := strings.SplitN(parts[1], "=", 2)
		if len(queryParts) == 2 && queryParts[0] == "data" {
			encodedData = queryParts[1]
		}
	}

	// Generate HTML
	htmlContent, err := GenerateUIHTML(tool, encodedData)
	if err != nil {
		return NewErrorResponse(req.ID, InternalError, "Failed to generate UI", err.Error()), nil
	}

	return NewResponse(req.ID, &ReadResourceResult{
		Contents: []ResourceContent{
			{
				URI:      params.URI,
				MimeType: "text/html",
				Text:     htmlContent,
			},
		},
	}), nil
}

func (s *StdioServer) writeResponse(resp *Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.writer, "%s\n", data)
	return err
}

// getEnvKeys extracts environment variable keys from env list
func getEnvKeys(env []string) []string {
	keys := make([]string, 0, len(env))
	for _, e := range env {
		for i, c := range e {
			if c == '=' {
				keys = append(keys, e[:i])
				break
			}
		}
	}
	return keys
}

// filterEnvByKeys filters environment variables by allowed keys
func filterEnvByKeys(env []string, allowedKeys []string) []string {
	allowedSet := make(map[string]bool)
	for _, key := range allowedKeys {
		allowedSet[key] = true
	}

	var filtered []string
	for _, e := range env {
		for i, c := range e {
			if c == '=' {
				key := e[:i]
				if allowedSet[key] {
					filtered = append(filtered, e)
				}
				break
			}
		}
	}
	return filtered
}
