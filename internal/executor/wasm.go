package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WasmExecutor handles WASM module execution using wazero
type WasmExecutor struct {
	rootDir string
	wasmDir string // Optional: external WASM binary directory (mounted as /.wasm)

	// Cache for compiled modules
	cacheMu sync.RWMutex
	cache   map[string]*wasmCache
}

type wasmCache struct {
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
}

// NewWasmExecutor creates a new WASM executor
func NewWasmExecutor(rootDir string, wasmDir string) *WasmExecutor {
	return &WasmExecutor{
		rootDir: rootDir,
		wasmDir: wasmDir,
		cache:   make(map[string]*wasmCache),
	}
}

// getOrCompile retrieves a cached compiled module or compiles it
func (w *WasmExecutor) getOrCompile(ctx context.Context, wasmPath string) (wazero.Runtime, wazero.CompiledModule, error) {
	w.cacheMu.RLock()
	cached, ok := w.cache[wasmPath]
	w.cacheMu.RUnlock()

	if ok {
		return cached.runtime, cached.compiled, nil
	}

	// Need to compile
	w.cacheMu.Lock()
	defer w.cacheMu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := w.cache[wasmPath]; ok {
		return cached.runtime, cached.compiled, nil
	}

	// Read WASM binary
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read WASM binary %s: %w", wasmPath, err)
	}

	// Create runtime with compilation cache
	runtime := wazero.NewRuntime(ctx)

	// Instantiate WASI
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	// Compile the module
	compiled, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		runtime.Close(ctx)
		return nil, nil, fmt.Errorf("failed to compile WASM module: %w", err)
	}

	// Cache it
	w.cache[wasmPath] = &wasmCache{
		runtime:  runtime,
		compiled: compiled,
	}

	return runtime, compiled, nil
}

// Execute runs a WASM binary with the given arguments
func (w *WasmExecutor) Execute(ctx context.Context, wasmPath string, cwd string, args []string, env []string, timeout time.Duration, maxOutput int) (*ExecuteResult, error) {
	startTime := time.Now()
	result := &ExecuteResult{}

	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Get or compile the module
	runtime, compiled, err := w.getOrCompile(execCtx, wasmPath)
	if err != nil {
		return nil, err
	}

	// Prepare output buffers
	var stdout, stderr limitedBuffer
	stdout.maxSize = maxOutput
	stderr.maxSize = maxOutput

	// Configure filesystem - mount rootDir as / for sandboxing
	// This makes rootDir appear as the root filesystem to the WASM module
	fsConfig := wazero.NewFSConfig().WithDirMount(w.rootDir, "/")

	// If wasmDir is set, mount it as /.wasm for external WASM binaries
	if w.wasmDir != "" {
		fsConfig = fsConfig.WithDirMount(w.wasmDir, "/.wasm")
	}

	// Convert host paths to guest paths
	toGuestPath := func(hostPath string) string {
		// First check if path is under wasmDir (external WASM binaries)
		if w.wasmDir != "" && strings.HasPrefix(hostPath, w.wasmDir) {
			return "/.wasm" + strings.TrimPrefix(hostPath, w.wasmDir)
		}
		// Then check if path is under rootDir
		if strings.HasPrefix(hostPath, w.rootDir) {
			return strings.TrimPrefix(hostPath, w.rootDir)
		}
		return hostPath
	}

	// Convert paths for guest
	guestWasmPath := toGuestPath(wasmPath)
	guestCwd := toGuestPath(cwd)
	guestArgs := append([]string{guestWasmPath}, args...)

	// Find WASM installation root (directory containing usr/)
	wasmRoot := filepath.Dir(wasmPath)
	for i := 0; i < 10; i++ {
		// Check if this directory contains "usr"
		if _, err := os.Stat(filepath.Join(wasmRoot, "usr")); err == nil {
			break
		}
		parent := filepath.Dir(wasmRoot)
		if parent == wasmRoot {
			break
		}
		wasmRoot = parent
	}

	// Configure the module
	config := wazero.NewModuleConfig().
		WithStdout(&stdout).
		WithStderr(&stderr).
		WithArgs(guestArgs...).
		WithFSConfig(fsConfig)

	// Set working directory if supported
	if guestCwd != "" {
		config = config.WithEnv("PWD", guestCwd)
	}

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

	// Auto-detect Ruby and set RUBYLIB for standard library access
	if filepath.Base(wasmPath) == "ruby" {
		// Set RUBYLIB to include the standard library path (as guest path)
		libDir := filepath.Join(wasmRoot, "usr", "local", "lib", "ruby")
		if entries, err := os.ReadDir(libDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && entry.Name()[0] >= '0' && entry.Name()[0] <= '9' {
					rubyLib := toGuestPath(filepath.Join(libDir, entry.Name()))
					config = config.WithEnv("RUBYLIB", rubyLib)
					break
				}
			}
		}
		// Disable gems to avoid path resolution issues
		guestArgs = append([]string{guestWasmPath, "--disable-gems"}, args...)
		config = config.WithArgs(guestArgs...)
	}

	// Instantiate the module with a unique name to allow multiple instances
	moduleName := fmt.Sprintf("module-%d", time.Now().UnixNano())
	config = config.WithName(moduleName)

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

// Close closes the executor and releases resources
func (w *WasmExecutor) Close() {
	w.cacheMu.Lock()
	defer w.cacheMu.Unlock()

	ctx := context.Background()
	for _, c := range w.cache {
		c.runtime.Close(ctx)
	}
	w.cache = make(map[string]*wasmCache)
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
