package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
)

const (
	// DefaultTimeout is the default command execution timeout
	DefaultTimeout = 30 * time.Second
	// DefaultMaxOutput is the default maximum output size in bytes
	DefaultMaxOutput = 1024 * 1024 // 1MB
)

// ExecutorConfig holds executor configuration
type ExecutorConfig struct {
	Timeout   time.Duration
	MaxOutput int
	RootDir   string // If set, restricts execution to this directory (jail/sandbox)
	WasmDir   string // Optional: directory containing WASM binaries (mounted as /.wasm)
}

// DefaultConfig returns the default executor configuration
func DefaultConfig() *ExecutorConfig {
	return &ExecutorConfig{
		Timeout:   DefaultTimeout,
		MaxOutput: DefaultMaxOutput,
	}
}

// ExecuteResult represents the result of command execution
type ExecuteResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMs int64
	TimedOut   bool
}

// Executor executes commands with timeout and output limits
type Executor struct {
	config       *ExecutorConfig
	sandbox      *Sandbox
	wasmExecutor *WasmExecutor
}

// NewExecutor creates a new Executor
func NewExecutor(config *ExecutorConfig) *Executor {
	if config == nil {
		config = DefaultConfig()
	}

	e := &Executor{config: config}

	// Initialize sandbox if root directory is set
	if config.RootDir != "" {
		// Initialize sandbox to check bwrap availability
		sandboxConfig := &SandboxConfig{
			Mode:    SandboxAuto,
			RootDir: config.RootDir,
		}

		sandbox, err := NewSandbox(sandboxConfig)
		if err != nil {
			// Log error but continue with basic path validation
			fmt.Fprintf(os.Stderr, "WARNING: Failed to initialize sandbox: %v\n", err)
		} else {
			e.sandbox = sandbox
		}

		// Initialize WASM executor
		e.wasmExecutor = NewWasmExecutor(config.RootDir, config.WasmDir)
	}

	return e
}

// Execute executes a command with the given parameters
func (e *Executor) Execute(ctx context.Context, cwd, cmd string, args []string, env []string) (*ExecuteResult, error) {
	result := &ExecuteResult{}
	startTime := time.Now()

	var actualCmd string
	var actualArgs []string

	// Use sandbox if available
	if e.sandbox != nil {
		var err error
		actualCmd, actualArgs, err = e.sandbox.WrapCommand(cwd, cmd, args)
		if err != nil {
			return nil, fmt.Errorf("sandbox validation failed: %w", err)
		}
	} else if e.config.RootDir != "" {
		// Fallback to basic path validation if sandbox not initialized
		if err := e.validatePath(cwd); err != nil {
			return nil, fmt.Errorf("cwd validation failed: %w", err)
		}
		actualCmd = cmd
		actualArgs = args
	} else {
		actualCmd = cmd
		actualArgs = args
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Create command
	execCmd := exec.CommandContext(execCtx, actualCmd, actualArgs...)
	// Only set Dir if not using bwrap (bwrap sets it via --chdir)
	if e.sandbox == nil || e.sandbox.Mode() != SandboxBwrap {
		execCmd.Dir = cwd
	}
	if len(env) > 0 {
		execCmd.Env = env
	}

	// Capture output with limits
	var stdout, stderr limitedBuffer
	stdout.maxSize = e.config.MaxOutput
	stderr.maxSize = e.config.MaxOutput
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	// Execute
	err := execCmd.Run()

	// Calculate duration
	result.DurationMs = time.Since(startTime).Milliseconds()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	// Handle exit code and errors
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
			result.ExitCode = -1
			result.Stderr = fmt.Sprintf("%s\n[execution timed out after %v]", result.Stderr, e.config.Timeout)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	// Add truncation notice if output was limited
	if stdout.truncated {
		result.Stdout += fmt.Sprintf("\n[output truncated, exceeded %d bytes]", e.config.MaxOutput)
	}
	if stderr.truncated {
		result.Stderr += fmt.Sprintf("\n[output truncated, exceeded %d bytes]", e.config.MaxOutput)
	}

	return result, nil
}

// ExecuteWithEnvFilter executes a command with filtered environment variables
func (e *Executor) ExecuteWithEnvFilter(ctx context.Context, cwd, cmd string, args []string, baseEnv []string, allowedKeys []string) (*ExecuteResult, error) {
	var filteredEnv []string

	if len(allowedKeys) == 0 {
		// No filter - use base env
		filteredEnv = baseEnv
	} else {
		// Filter environment variables
		filteredEnv = filterEnv(baseEnv, allowedKeys)
	}

	return e.Execute(ctx, cwd, cmd, args, filteredEnv)
}

// filterEnv filters environment variables by allowed keys
func filterEnv(env []string, allowedKeys []string) []string {
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

// limitedBuffer is a bytes.Buffer with a maximum size limit
type limitedBuffer struct {
	buf       bytes.Buffer
	maxSize   int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (n int, err error) {
	if b.truncated {
		return len(p), nil // Discard but report success
	}

	remaining := b.maxSize - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}

	if len(p) > remaining {
		b.truncated = true
		return b.buf.Write(p[:remaining])
	}

	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

// validatePath checks if a path is within the configured RootDir
func (e *Executor) validatePath(path string) error {
	if e.config.RootDir == "" {
		return nil
	}

	// Resolve to absolute paths
	rootAbs, err := filepath.Abs(e.config.RootDir)
	if err != nil {
		return fmt.Errorf("failed to resolve root dir: %w", err)
	}

	// Evaluate symlinks for root
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("failed to evaluate root dir symlinks: %w", err)
	}

	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Evaluate symlinks for path
	pathReal, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		// Path might not exist yet, check parent
		pathReal = pathAbs
	}

	// Check if path is within root
	if !IsPathWithinRoot(rootReal, pathReal) {
		return fmt.Errorf("path %q is outside root directory %q", path, e.config.RootDir)
	}

	return nil
}

// IsPathWithinRoot checks if a path is within or equal to the root directory
func IsPathWithinRoot(root, path string) bool {
	// Clean and normalize paths
	root = filepath.Clean(root)
	path = filepath.Clean(path)

	// Ensure root ends with separator for proper prefix matching
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root = root + string(filepath.Separator)
	}

	// Path is within root if it equals root or starts with root
	return path+"/" == root || strings.HasPrefix(path+string(filepath.Separator), root)
}

// GetRootDir returns the configured root directory
func (e *Executor) GetRootDir() string {
	return e.config.RootDir
}

// GetSandboxMode returns the effective sandbox mode
func (e *Executor) GetSandboxMode() SandboxMode {
	if e.sandbox != nil {
		return e.sandbox.Mode()
	}
	return SandboxNone
}

// IsSandboxed returns true if commands are sandboxed with bwrap
func (e *Executor) IsSandboxed() bool {
	return e.sandbox != nil && e.sandbox.Mode() == SandboxBwrap
}

// ExecuteWithSandbox executes a command using the specified sandbox type from the tool
func (e *Executor) ExecuteWithSandbox(ctx context.Context, cwd, cmd string, args []string, env []string, sandboxType plugin.SandboxType, wasmBinary string) (*ExecuteResult, error) {
	switch sandboxType {
	case plugin.SandboxTypeWasm:
		if e.wasmExecutor == nil {
			return nil, fmt.Errorf("WASM executor not initialized (root directory not set)")
		}
		if wasmBinary == "" {
			return nil, fmt.Errorf("WASM binary path is required for WASM sandbox")
		}
		// For WASM, the wasmBinary is the WASM file to execute, and args are passed to it
		return e.wasmExecutor.Execute(ctx, wasmBinary, cwd, args, env, e.config.Timeout, e.config.MaxOutput)

	case plugin.SandboxTypeNone:
		// Execute without any sandbox
		return e.executeWithoutSandbox(ctx, cwd, cmd, args, env)

	case plugin.SandboxTypeBubblewrap:
		// Execute with bubblewrap sandbox
		return e.executeWithBwrap(ctx, cwd, cmd, args, env)

	default:
		// Default to using the executor's configured sandbox mode
		return e.Execute(ctx, cwd, cmd, args, env)
	}
}

// executeWithoutSandbox executes a command without any sandboxing
func (e *Executor) executeWithoutSandbox(ctx context.Context, cwd, cmd string, args []string, env []string) (*ExecuteResult, error) {
	result := &ExecuteResult{}
	startTime := time.Now()

	// Basic path validation if root directory is configured
	if e.config.RootDir != "" {
		if err := e.validatePath(cwd); err != nil {
			return nil, fmt.Errorf("cwd validation failed: %w", err)
		}
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Create command
	execCmd := exec.CommandContext(execCtx, cmd, args...)
	execCmd.Dir = cwd
	if len(env) > 0 {
		execCmd.Env = env
	}

	// Capture output with limits
	var stdout, stderr limitedBuffer
	stdout.maxSize = e.config.MaxOutput
	stderr.maxSize = e.config.MaxOutput
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	// Execute
	err := execCmd.Run()

	// Calculate duration
	result.DurationMs = time.Since(startTime).Milliseconds()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	// Handle exit code and errors
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
			result.ExitCode = -1
			result.Stderr = fmt.Sprintf("%s\n[execution timed out after %v]", result.Stderr, e.config.Timeout)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	// Add truncation notice if output was limited
	if stdout.truncated {
		result.Stdout += fmt.Sprintf("\n[output truncated, exceeded %d bytes]", e.config.MaxOutput)
	}
	if stderr.truncated {
		result.Stderr += fmt.Sprintf("\n[output truncated, exceeded %d bytes]", e.config.MaxOutput)
	}

	return result, nil
}

// executeWithBwrap executes a command with bubblewrap sandbox
func (e *Executor) executeWithBwrap(ctx context.Context, cwd, cmd string, args []string, env []string) (*ExecuteResult, error) {
	if e.sandbox == nil || !e.sandbox.IsBwrapAvailable() {
		return nil, fmt.Errorf("bubblewrap (bwrap) is required but not installed")
	}

	result := &ExecuteResult{}
	startTime := time.Now()

	// Wrap command with bwrap
	actualCmd, actualArgs, err := e.sandbox.WrapCommand(cwd, cmd, args)
	if err != nil {
		return nil, fmt.Errorf("sandbox validation failed: %w", err)
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Create command
	execCmd := exec.CommandContext(execCtx, actualCmd, actualArgs...)
	// Don't set Dir for bwrap (it sets it via --chdir)
	if len(env) > 0 {
		execCmd.Env = env
	}

	// Capture output with limits
	var stdout, stderr limitedBuffer
	stdout.maxSize = e.config.MaxOutput
	stderr.maxSize = e.config.MaxOutput
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	// Execute
	err = execCmd.Run()

	// Calculate duration
	result.DurationMs = time.Since(startTime).Milliseconds()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	// Handle exit code and errors
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
			result.ExitCode = -1
			result.Stderr = fmt.Sprintf("%s\n[execution timed out after %v]", result.Stderr, e.config.Timeout)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	// Add truncation notice if output was limited
	if stdout.truncated {
		result.Stdout += fmt.Sprintf("\n[output truncated, exceeded %d bytes]", e.config.MaxOutput)
	}
	if stderr.truncated {
		result.Stderr += fmt.Sprintf("\n[output truncated, exceeded %d bytes]", e.config.MaxOutput)
	}

	return result, nil
}
