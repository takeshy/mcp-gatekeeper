package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
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
	config *ExecutorConfig
}

// NewExecutor creates a new Executor
func NewExecutor(config *ExecutorConfig) *Executor {
	if config == nil {
		config = DefaultConfig()
	}
	return &Executor{config: config}
}

// Execute executes a command with the given parameters
func (e *Executor) Execute(ctx context.Context, cwd, cmd string, args []string, env []string) (*ExecuteResult, error) {
	result := &ExecuteResult{}
	startTime := time.Now()

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
