package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/bridge"
	"github.com/takeshy/mcp-gatekeeper/internal/mcp"
	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
	"github.com/takeshy/mcp-gatekeeper/internal/version"
)

func main() {
	var (
		showVersion     = flag.Bool("version", false, "Show version and exit")
		mode            = flag.String("mode", "stdio", "Server mode: stdio, http, or bridge")
		addr            = flag.String("addr", ":8080", "HTTP server address (for http/bridge mode)")
		apiKey          = flag.String("api-key", "", "API key for authentication (or MCP_GATEKEEPER_API_KEY env var)")
		rateLimit       = flag.Int("rate-limit", 500, "Rate limit per minute (for http/bridge mode)")
		rootDir         = flag.String("root-dir", "", "Root directory for command execution (required for stdio/http, acts as chroot)")
		wasmDir         = flag.String("wasm-dir", "", "Directory containing WASM binaries (mounted as /.wasm in WASM sandbox)")
		pluginsDir      = flag.String("plugins-dir", "", "Directory containing plugin JSON files (required for stdio/http)")
		pluginFile      = flag.String("plugin-file", "", "Single plugin JSON file (alternative to plugins-dir)")
		upstream        = flag.String("upstream", "", "Upstream stdio MCP server command (for bridge mode, e.g., 'node /path/to/server.js')")
		upstreamEnv     = flag.String("upstream-env", "", "Comma-separated environment variables for upstream server (e.g., 'KEY1=val1,KEY2=val2')")
		maxResponseSize = flag.Int("max-response-size", 500000, "Max response size in bytes for bridge mode (default 500000)")
		debug           = flag.Bool("debug", false, "Enable debug logging (logs request/response for bridge mode)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("mcp-gatekeeper %s\n", version.Version)
		os.Exit(0)
	}

	// Get API key from env if not provided
	if *apiKey == "" {
		*apiKey = os.Getenv("MCP_GATEKEEPER_API_KEY")
	}

	// Bridge mode - no plugins needed, just proxy to upstream
	if *mode == "bridge" {
		if *upstream == "" {
			fmt.Fprintf(os.Stderr, "Error: --upstream is required for bridge mode\n")
			fmt.Fprintf(os.Stderr, "Usage: %s --mode=bridge --upstream='node /path/to/mcp-server.js' [options]\n", os.Args[0])
			os.Exit(1)
		}

		// Parse upstream environment variables
		var upstreamEnvVars []string
		if *upstreamEnv != "" {
			upstreamEnvVars = strings.Split(*upstreamEnv, ",")
		}

		if err := runBridge(*addr, *upstream, upstreamEnvVars, *apiKey, *rateLimit, *maxResponseSize, *debug); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Validate required root-dir for stdio/http modes
	var rootDirAbs string
	if *rootDir == "" {
		fmt.Fprintf(os.Stderr, "Error: --root-dir is required\n")
		fmt.Fprintf(os.Stderr, "Usage: %s --root-dir=/path/to/allowed/directory [options]\n", os.Args[0])
		os.Exit(1)
	}

	// Validate root-dir exists and is a directory
	var err error
	rootDirAbs, err = filepath.Abs(*rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid root-dir path: %v\n", err)
		os.Exit(1)
	}

	info, err := os.Stat(rootDirAbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: root-dir does not exist: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: root-dir is not a directory: %s\n", rootDirAbs)
		os.Exit(1)
	}

	// Validate wasm-dir if provided
	var wasmDirAbs string
	if *wasmDir != "" {
		wasmDirAbs, err = filepath.Abs(*wasmDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid wasm-dir path: %v\n", err)
			os.Exit(1)
		}
		info, err := os.Stat(wasmDirAbs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: wasm-dir does not exist: %v\n", err)
			os.Exit(1)
		}
		if !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: wasm-dir is not a directory: %s\n", wasmDirAbs)
			os.Exit(1)
		}
	}

	// Load plugins
	var plugins *plugin.Config
	if *pluginFile != "" {
		plugins, err = plugin.LoadFromFile(*pluginFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to load plugin file: %v\n", err)
			os.Exit(1)
		}
	} else if *pluginsDir != "" {
		plugins, err = plugin.LoadFromDir(*pluginsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to load plugins: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Error: --plugins-dir or --plugin-file is required for stdio/http mode\n")
		fmt.Fprintf(os.Stderr, "Usage: %s --root-dir=/path --plugins-dir=/path/to/plugins [options]\n", os.Args[0])
		os.Exit(1)
	}

	// Print loaded tools
	printLoadedTools(plugins)

	// Run in appropriate mode
	switch *mode {
	case "stdio":
		if err := runStdio(plugins, *apiKey, rootDirAbs, wasmDirAbs); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "http":
		if err := runHTTP(plugins, *addr, *rateLimit, rootDirAbs, wasmDirAbs, *apiKey); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}

func runStdio(plugins *plugin.Config, apiKey string, rootDir string, wasmDir string) error {
	// For stdio mode, we require API key to be set (either flag or env var)
	expectedAPIKey := apiKey

	server, err := mcp.NewStdioServer(plugins, apiKey, expectedAPIKey, rootDir, wasmDir)
	if err != nil {
		return fmt.Errorf("failed to create stdio server: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	return server.Run(ctx)
}

func runHTTP(plugins *plugin.Config, addr string, rateLimit int, rootDir string, wasmDir string, apiKey string) error {
	config := &mcp.HTTPConfig{
		RateLimit:       rateLimit,
		RateLimitWindow: time.Minute,
		RootDir:         rootDir,
		WasmDir:         wasmDir,
		APIKey:          apiKey,
	}
	server, err := mcp.NewHTTPServer(plugins, config)
	if err != nil {
		return fmt.Errorf("failed to create HTTP server: %w", err)
	}

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      server.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	}()

	if wasmDir != "" {
		fmt.Printf("Starting HTTP server on %s (root-dir: %s, wasm-dir: %s)\n", addr, rootDir, wasmDir)
	} else {
		fmt.Printf("Starting HTTP server on %s (root-dir: %s)\n", addr, rootDir)
	}
	if apiKey != "" {
		fmt.Printf("API key authentication enabled\n")
	}
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}

func runBridge(addr string, upstream string, upstreamEnv []string, apiKey string, rateLimit int, maxResponseSize int, debug bool) error {
	// Parse upstream command with shell-like syntax support
	parts, err := bridge.ParseCommand(upstream)
	if err != nil {
		return fmt.Errorf("invalid upstream command: %w", err)
	}
	if len(parts) == 0 {
		return fmt.Errorf("empty upstream command")
	}

	config := &bridge.ServerConfig{
		Command:         parts[0],
		Args:            parts[1:],
		Env:             upstreamEnv,
		APIKey:          apiKey,
		Timeout:         30 * time.Second,
		RateLimit:       rateLimit,
		RateLimitWindow: time.Minute,
		MaxResponseSize: maxResponseSize,
		Debug:           debug,
	}

	server, err := bridge.NewServer(config)
	if err != nil {
		return fmt.Errorf("failed to create bridge server: %w", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start upstream connection
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start bridge: %w", err)
	}

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      server.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Starting HTTP bridge on %s (upstream: %s, max-response-size: %d)\n", addr, upstream, maxResponseSize)
	if debug {
		fmt.Printf("Debug logging enabled\n")
	}
	if apiKey != "" {
		fmt.Printf("API key authentication enabled\n")
	}
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}

func printLoadedTools(plugins *plugin.Config) {
	tools := plugins.ListTools()
	fmt.Println("=== Loaded Tools ===")
	if len(tools) == 0 {
		fmt.Println("(no tools loaded)")
		return
	}

	for _, tool := range tools {
		sandboxInfo := string(tool.Sandbox)
		if tool.Sandbox == plugin.SandboxTypeWasm && tool.WasmBinary != "" {
			sandboxInfo = fmt.Sprintf("wasm: %s", tool.WasmBinary)
		} else if tool.Sandbox == plugin.SandboxTypeBubblewrap {
			sandboxInfo = fmt.Sprintf("bubblewrap: %s", tool.Command)
		} else if tool.Sandbox == plugin.SandboxTypeNone {
			sandboxInfo = fmt.Sprintf("none: %s", tool.Command)
		}
		fmt.Printf("  - %s: %s [%s]\n", tool.Name, tool.Description, sandboxInfo)
	}
	fmt.Println()
}
