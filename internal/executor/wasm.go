package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WasmExecutor handles WASM module execution using wazero
type WasmExecutor struct {
	rootDir string
}

// NewWasmExecutor creates a new WASM executor
func NewWasmExecutor(rootDir string) *WasmExecutor {
	return &WasmExecutor{
		rootDir: rootDir,
	}
}

// Execute runs a WASM binary with the given arguments
func (w *WasmExecutor) Execute(ctx context.Context, wasmPath string, cwd string, args []string, env []string, timeout time.Duration, maxOutput int) (*ExecuteResult, error) {
	startTime := time.Now()
	result := &ExecuteResult{}

	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Read WASM binary
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM binary %s: %w", wasmPath, err)
	}

	// Create runtime
	runtime := wazero.NewRuntime(execCtx)
	defer runtime.Close(execCtx)

	// Instantiate WASI
	wasi_snapshot_preview1.MustInstantiate(execCtx, runtime)

	// Prepare output buffers
	var stdout, stderr limitedBuffer
	stdout.maxSize = maxOutput
	stderr.maxSize = maxOutput

	// Build arguments (first arg is typically the program name)
	moduleArgs := append([]string{wasmPath}, args...)

	// Configure the module
	config := wazero.NewModuleConfig().
		WithStdout(&stdout).
		WithStderr(&stderr).
		WithArgs(moduleArgs...).
		WithFSConfig(wazero.NewFSConfig().WithDirMount(w.rootDir, w.rootDir))

	// Add environment variables
	for _, e := range env {
		for i, c := range e {
			if c == '=' {
				key := e[:i]
				value := e[i+1:]
				config = config.WithEnv(key, value)
				break
			}
		}
	}

	// Compile and instantiate the module
	compiled, err := runtime.CompileModule(execCtx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WASM module: %w", err)
	}

	_, err = runtime.InstantiateModule(execCtx, compiled, config)
	if err != nil {
		// Check for timeout
		if execCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
			result.ExitCode = -1
			result.Stderr = fmt.Sprintf("[execution timed out after %v]", timeout)
			result.DurationMs = time.Since(startTime).Milliseconds()
			return result, nil
		}

		// WASM exit codes come through as errors
		// Try to extract exit code from error
		result.ExitCode = 1 // Default to 1 for errors
		result.Stderr = err.Error()
	}

	result.DurationMs = time.Since(startTime).Milliseconds()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	// Add truncation notice if output was limited
	if stdout.truncated {
		result.Stdout += fmt.Sprintf("\n[output truncated, exceeded %d bytes]", maxOutput)
	}
	if stderr.truncated {
		result.Stderr += fmt.Sprintf("\n[output truncated, exceeded %d bytes]", maxOutput)
	}

	return result, nil
}

// wasmLimitedBuffer is a bytes.Buffer with a maximum size limit for WASM output
type wasmLimitedBuffer struct {
	buf       bytes.Buffer
	maxSize   int
	truncated bool
}

func (b *wasmLimitedBuffer) Write(p []byte) (n int, err error) {
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

func (b *wasmLimitedBuffer) String() string {
	return b.buf.String()
}
