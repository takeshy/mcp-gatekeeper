package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/version"
)

// Client manages communication with an external stdio MCP server
type Client struct {
	command   string
	args      []string
	env       []string
	workDir   string
	timeout   time.Duration
	maxOutput int

	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Reader
	stderr      *bufio.Reader
	initialized atomic.Bool
	requestID   atomic.Int64
	pending     map[string]chan *Response // Key is the raw JSON ID string
	pendingMu   sync.Mutex
	done        chan struct{}
	closeOnce   sync.Once
}

// Request represents a JSON-RPC 2.0 request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// ClientConfig holds client configuration
type ClientConfig struct {
	Command   string
	Args      []string
	Env       []string
	WorkDir   string
	Timeout   time.Duration
	MaxOutput int
}

// DefaultClientConfig returns default configuration
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Timeout:   30 * time.Second,
		MaxOutput: 1024 * 1024, // 1MB
	}
}

// NewClient creates a new bridge client
func NewClient(config *ClientConfig) *Client {
	if config == nil {
		config = DefaultClientConfig()
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxOutput == 0 {
		config.MaxOutput = 1024 * 1024
	}

	return &Client{
		command:   config.Command,
		args:      config.Args,
		env:       config.Env,
		workDir:   config.WorkDir,
		timeout:   config.Timeout,
		maxOutput: config.MaxOutput,
		pending:   make(map[string]chan *Response),
		done:      make(chan struct{}),
	}
}

// Start starts the external MCP server process
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil {
		return fmt.Errorf("client already started")
	}

	cmd := exec.CommandContext(ctx, c.command, c.args...)
	if c.workDir != "" {
		cmd.Dir = c.workDir
	}
	if len(c.env) > 0 {
		cmd.Env = append(os.Environ(), c.env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return fmt.Errorf("failed to start process: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	c.stderr = bufio.NewReader(stderr)

	// Start response reader goroutine
	go c.readResponses()

	// Start stderr reader goroutine
	go c.readStderr()

	return nil
}

// readResponses reads responses from stdout
func (c *Client) readResponses() {
	defer c.Close()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		line, err := c.stdout.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "[bridge] stdout read error: %v\n", err)
			}
			return
		}

		if line == "" || line == "\n" {
			continue
		}

		// Try to parse as a generic message to check if it's a request or response
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id,omitempty"`
			Method  string          `json:"method,omitempty"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   json.RawMessage `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			fmt.Fprintf(os.Stderr, "[bridge] failed to parse message: %v\n", err)
			continue
		}

		// Check if this is a request from upstream (has method)
		if msg.Method != "" {
			c.handleUpstreamRequest(msg.ID, msg.Method, line)
			continue
		}

		// This is a response
		if msg.ID != nil && string(msg.ID) != "null" {
			idKey := string(msg.ID)
			c.pendingMu.Lock()
			if ch, ok := c.pending[idKey]; ok {
				var resp Response
				json.Unmarshal([]byte(line), &resp)
				ch <- &resp
				delete(c.pending, idKey)
			}
			c.pendingMu.Unlock()
		}
	}
}

// readStderr reads stderr output
func (c *Client) readStderr() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		line, err := c.stderr.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "[bridge] stderr read error: %v\n", err)
			}
			return
		}

		if line != "" {
			fmt.Fprintf(os.Stderr, "[upstream] %s", line)
		}
	}
}

// Initialize sends the initialize request to the upstream server
func (c *Client) Initialize(ctx context.Context) (*Response, error) {
	params := map[string]interface{}{
		"protocolVersion": version.MCPProtocolVersion,
		"capabilities": map[string]interface{}{
			"roots": map[string]interface{}{
				"listChanged": false,
			},
		},
		"clientInfo": map[string]interface{}{
			"name":    "mcp-gatekeeper-bridge",
			"version": version.Version,
		},
	}

	resp, err := c.Call(ctx, "initialize", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("initialize failed: %s", resp.Error.Message)
	}

	c.initialized.Store(true)

	// Send initialized notification
	c.Notify("notifications/initialized", nil)

	return resp, nil
}

// Call sends a request and waits for response
func (c *Client) Call(ctx context.Context, method string, params interface{}) (*Response, error) {
	c.mu.Lock()
	if c.cmd == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("client not started")
	}
	c.mu.Unlock()

	id := c.requestID.Add(1)

	var paramsJSON json.RawMessage
	if params != nil {
		var err error
		paramsJSON, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	idJSON, _ := json.Marshal(id)
	idKey := string(idJSON) // Use raw JSON as key
	req := Request{
		JSONRPC: "2.0",
		ID:      idJSON,
		Method:  method,
		Params:  paramsJSON,
	}

	// Create response channel
	respCh := make(chan *Response, 1)
	c.pendingMu.Lock()
	c.pending[idKey] = respCh
	c.pendingMu.Unlock()

	// Clean up on exit
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, idKey)
		c.pendingMu.Unlock()
	}()

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	c.mu.Lock()
	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Wait for response with timeout
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	case <-time.After(c.timeout):
		return nil, fmt.Errorf("request timeout")
	case <-c.done:
		return nil, fmt.Errorf("client closed")
	}
}

// Notify sends a notification (no response expected)
func (c *Client) Notify(method string, params interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd == nil {
		return fmt.Errorf("client not started")
	}

	var paramsJSON json.RawMessage
	if params != nil {
		var err error
		paramsJSON, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}

// Forward forwards a raw JSON-RPC request to the upstream server
func (c *Client) Forward(ctx context.Context, rawRequest []byte) (*Response, error) {
	var req Request
	if err := json.Unmarshal(rawRequest, &req); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	// Handle notifications (no id)
	if req.ID == nil || string(req.ID) == "null" {
		if err := c.Notify(req.Method, req.Params); err != nil {
			return nil, err
		}
		return nil, nil
	}

	// Parse params to interface
	var params interface{}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("failed to parse params: %w", err)
		}
	}

	return c.Call(ctx, req.Method, params)
}

// IsInitialized returns whether the client has been initialized
func (c *Client) IsInitialized() bool {
	return c.initialized.Load()
}

// handleUpstreamRequest handles requests from upstream server (MCP bidirectional communication)
func (c *Client) handleUpstreamRequest(id json.RawMessage, method string, raw string) {
	var result interface{}

	switch method {
	case "roots/list":
		// Return empty roots list
		result = map[string]interface{}{
			"roots": []interface{}{},
		}
	case "sampling/createMessage":
		// Not supported, return error
		c.sendErrorResponse(id, -32601, "Method not supported")
		return
	default:
		// Unknown method, return error
		c.sendErrorResponse(id, -32601, "Method not found")
		return
	}

	c.sendResultResponse(id, result)
}

func (c *Client) sendResultResponse(id json.RawMessage, result interface{}) {
	resultJSON, _ := json.Marshal(result)
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultJSON,
	}
	data, _ := json.Marshal(resp)
	c.mu.Lock()
	fmt.Fprintf(c.stdin, "%s\n", data)
	c.mu.Unlock()
}

func (c *Client) sendErrorResponse(id json.RawMessage, code int, message string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	data, _ := json.Marshal(resp)
	c.mu.Lock()
	fmt.Fprintf(c.stdin, "%s\n", data)
	c.mu.Unlock()
}

// Close closes the client and terminates the process
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.done)

		c.mu.Lock()
		defer c.mu.Unlock()

		if c.stdin != nil {
			c.stdin.Close()
		}

		if c.cmd != nil && c.cmd.Process != nil {
			// Give the process time to exit gracefully
			done := make(chan error, 1)
			go func() {
				done <- c.cmd.Wait()
			}()

			select {
			case <-done:
				// Process exited
			case <-time.After(5 * time.Second):
				// Force kill
				c.cmd.Process.Kill()
				<-done
			}
		}

		// Cancel all pending requests
		c.pendingMu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = make(map[string]chan *Response)
		c.pendingMu.Unlock()
	})

	return err
}
