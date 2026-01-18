package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/takeshy/mcp-gatekeeper/internal/auth"
	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/executor"
	"github.com/takeshy/mcp-gatekeeper/internal/policy"
)

const (
	ProtocolVersion = "2024-11-05"
	ServerName      = "mcp-gatekeeper"
	ServerVersion   = "1.0.0"
)

// StdioServer implements the MCP server over stdio
type StdioServer struct {
	db          *db.DB
	auth        *auth.Authenticator
	evaluator   *policy.Evaluator
	executor    *executor.Executor
	initialized bool
	apiKey      *db.APIKey
	reader      *bufio.Reader
	writer      io.Writer
	rootDir     string
}

// NewStdioServer creates a new stdio MCP server
func NewStdioServer(database *db.DB, apiKeyStr string, rootDir string) (*StdioServer, error) {
	authenticator := auth.NewAuthenticator(database)

	// Authenticate API key
	apiKey, err := authenticator.Authenticate(apiKeyStr)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	if apiKey == nil {
		return nil, fmt.Errorf("invalid API key")
	}

	execConfig := &executor.ExecutorConfig{
		Timeout:   executor.DefaultTimeout,
		MaxOutput: executor.DefaultMaxOutput,
		RootDir:   rootDir,
	}

	return &StdioServer{
		db:        database,
		auth:      authenticator,
		evaluator: policy.NewEvaluator(),
		executor:  executor.NewExecutor(execConfig),
		apiKey:    apiKey,
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
	// Get tools from database for this API key
	dbTools, err := s.db.ListToolsByAPIKeyID(s.apiKey.ID)
	if err != nil {
		return NewErrorResponse(req.ID, InternalError, "Failed to list tools", err.Error()), nil
	}

	tools := make([]Tool, len(dbTools))
	for i, t := range dbTools {
		tools[i] = Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"cwd": {
						Type:        "string",
						Description: "Working directory for the command",
					},
					"args": {
						Type:        "array",
						Description: "Command arguments",
						Items:       &Items{Type: "string"},
					},
				},
				Required: []string{"cwd"},
			},
		}
	}
	return NewResponse(req.ID, &ListToolsResult{Tools: tools}), nil
}

func (s *StdioServer) handleToolsCall(ctx context.Context, req *Request) (*Response, error) {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid params", err.Error()), nil
	}

	// Look up tool by name for this API key
	tool, err := s.db.GetToolByAPIKeyAndName(s.apiKey.ID, params.Name)
	if err != nil {
		return NewErrorResponse(req.ID, InternalError, "Failed to get tool", err.Error()), nil
	}
	if tool == nil {
		return NewErrorResponse(req.ID, MethodNotFound, "Tool not found", params.Name), nil
	}

	return s.handleExecute(ctx, req.ID, tool, params.Arguments)
}

func (s *StdioServer) handleExecute(ctx context.Context, id json.RawMessage, tool *db.Tool, args map[string]interface{}) (*Response, error) {
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

	if cwd == "" {
		return NewErrorResponse(id, InvalidParams, "cwd is required", nil), nil
	}

	// Evaluate policy (check if args are allowed)
	decision, err := s.evaluator.EvaluateArgs(tool, cmdArgs)
	if err != nil {
		return NewErrorResponse(id, InternalError, "Policy evaluation failed", err.Error()), nil
	}

	// Create audit log
	auditLog := &db.AuditLog{
		APIKeyID:          s.apiKey.ID,
		RequestedCwd:      cwd,
		RequestedCmd:      tool.Command,
		RequestedArgs:     cmdArgs,
		NormalizedCwd:     cwd,
		NormalizedCmdline: tool.Command + " " + strings.Join(cmdArgs, " "),
		MatchedRules:      decision.MatchedRules,
	}

	if !decision.Allowed {
		auditLog.Decision = db.DecisionDeny
		s.db.CreateAuditLog(auditLog)
		return NewErrorResponse(id, PolicyDenied, "Arguments denied by policy", decision.Reason), nil
	}

	auditLog.Decision = db.DecisionAllow

	// Filter environment variables
	filteredEnvKeys := s.evaluator.FilterEnvKeys(s.apiKey.AllowedEnvKeys, getEnvKeys(os.Environ()))
	filteredEnv := filterEnvByKeys(os.Environ(), filteredEnvKeys)

	// Execute command using the tool's sandbox setting
	result, err := s.executor.ExecuteWithSandbox(ctx, cwd, tool.Command, cmdArgs, filteredEnv, tool.Sandbox, tool.WasmBinary)
	if err != nil {
		auditLog.Stderr = err.Error()
		s.db.CreateAuditLog(auditLog)
		return NewErrorResponse(id, ExecutionFailed, "Execution failed", err.Error()), nil
	}

	// Update audit log with result
	auditLog.Stdout = result.Stdout
	auditLog.Stderr = result.Stderr
	auditLog.ExitCode.Int64 = int64(result.ExitCode)
	auditLog.ExitCode.Valid = true
	auditLog.DurationMs.Int64 = result.DurationMs
	auditLog.DurationMs.Valid = true
	s.db.CreateAuditLog(auditLog)

	// Return result
	execResult := &ExecuteResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}

	content := []Content{
		{
			Type: "text",
			Text: fmt.Sprintf("Exit code: %d\n\nStdout:\n%s\n\nStderr:\n%s",
				execResult.ExitCode, execResult.Stdout, execResult.Stderr),
		},
	}

	return NewResponse(id, &CallToolResult{
		Content: content,
		IsError: result.ExitCode != 0,
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
