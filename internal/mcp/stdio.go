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
}

// NewStdioServer creates a new stdio MCP server
func NewStdioServer(plugins *plugin.Config, apiKey string, expectedAPIKey string, rootDir string, wasmDir string) (*StdioServer, error) {
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
	case "ping":
		return NewResponse(req.ID, struct{}{}), nil
	default:
		return NewErrorResponse(req.ID, MethodNotFound, "Method not found", req.Method), nil
	}
}

func (s *StdioServer) handleInitialize(req *Request) (*Response, error) {
	result := &InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{
				ListChanged: false,
			},
		},
		ServerInfo: ServerInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}
	return NewResponse(req.ID, result), nil
}

func (s *StdioServer) handleToolsList(req *Request) (*Response, error) {
	// Get tools from plugins
	pluginTools := s.plugins.ListTools()

	tools := make([]Tool, len(pluginTools))
	for i, t := range pluginTools {
		tools[i] = Tool{
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
		}
	}
	return NewResponse(req.ID, &ListToolsResult{Tools: tools}), nil
}

func (s *StdioServer) handleToolsCall(ctx context.Context, req *Request) (*Response, error) {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Invalid params: %v\n", err)
		return NewErrorResponse(req.ID, InvalidParams, "Invalid params", err.Error()), nil
	}

	// Look up tool by name from plugins
	tool := s.plugins.GetTool(params.Name)
	if tool == nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Tool not found: %s\n", params.Name)
		return NewErrorResponse(req.ID, MethodNotFound, "Tool not found", params.Name), nil
	}

	return s.handleExecute(ctx, req.ID, tool, params.Arguments)
}

func (s *StdioServer) handleExecute(ctx context.Context, id json.RawMessage, tool *plugin.Tool, args map[string]interface{}) (*Response, error) {
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

	// Evaluate policy (check if args are allowed)
	decision, err := s.evaluator.EvaluateArgs(tool, cmdArgs)
	if err != nil {
		return NewErrorResponse(id, InternalError, "Policy evaluation failed", err.Error()), nil
	}

	if !decision.Allowed {
		fmt.Fprintf(os.Stderr, "[WARN] Arguments denied by policy: %s\n", decision.Reason)
		return NewErrorResponse(id, PolicyDenied, "Arguments denied by policy", decision.Reason), nil
	}

	// Filter environment variables
	filteredEnvKeys := s.evaluator.FilterEnvKeys(s.plugins.AllowedEnvKeys, getEnvKeys(os.Environ()))
	filteredEnv := filterEnvByKeys(os.Environ(), filteredEnvKeys)

	// Execute command using the tool's sandbox setting
	result, err := s.executor.ExecuteWithSandbox(ctx, cwd, tool.Command, cmdArgs, filteredEnv, tool.Sandbox, tool.WasmBinary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Execution failed: %v\n", err)
		return NewErrorResponse(id, ExecutionFailed, "Execution failed", err.Error()), nil
	}

	// Return result
	content := []Content{
		{
			Type: "text",
			Text: result.Stdout,
		},
	}

	return NewResponse(id, &CallToolResult{
		Content: content,
		IsError: result.ExitCode != 0,
		Metadata: &ResultMetadata{
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
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
