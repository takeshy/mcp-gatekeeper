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
	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/executor"
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
		dbPath           = flag.String("db", "", "SQLite database path for audit logging (optional)")
		enableOAuth      = flag.Bool("enable-oauth", false, "Enable OAuth 2.0 authentication (requires --db)")
		oauthIssuer      = flag.String("oauth-issuer", "", "OAuth issuer URL (optional, auto-detected if empty)")
		enableStreamable = flag.Bool("enable-streamable", false, "Enable MCP Streamable HTTP (2025-06-18)")
		sessionTTL       = flag.Duration("session-ttl", 30*time.Minute, "Session TTL for Streamable HTTP")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("mcp-gatekeeper %s\n", version.Version)
		os.Exit(0)
	}

	// Auto-detect mode based on flags
	// --upstream implies bridge mode, --addr implies http mode
	if *mode == "stdio" {
		if *upstream != "" {
			*mode = "bridge"
		} else {
			// Check if --addr was explicitly set
			addrSet := false
			flag.Visit(func(f *flag.Flag) {
				if f.Name == "addr" {
					addrSet = true
				}
			})
			if addrSet {
				*mode = "http"
			}
		}
	}

	// Get API key from env if not provided
	if *apiKey == "" {
		*apiKey = os.Getenv("MCP_GATEKEEPER_API_KEY")
	}

	// Validate OAuth requires DB
	if *enableOAuth && *dbPath == "" {
		fmt.Fprintf(os.Stderr, "Error: --enable-oauth requires --db to be specified\n")
		os.Exit(1)
	}

	// Open database if specified (optional for audit logging, required for OAuth)
	var database *db.DB
	if *dbPath != "" {
		var err error
		database, err = db.Open(*dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()
		fmt.Printf("Audit logging enabled (db: %s)\n", *dbPath)
		if *enableOAuth {
			fmt.Printf("OAuth 2.0 authentication enabled\n")
		}
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

		if err := runBridge(*addr, *upstream, upstreamEnvVars, *apiKey, *rateLimit, *maxResponseSize, *debug, database, *enableOAuth, *oauthIssuer); err != nil {
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

	// Check if any tool uses bubblewrap and prepare mount directories
	var sandboxExecutor *executor.Executor
	hasBubblewrap := false
	for _, tool := range plugins.ListTools() {
		if tool.Sandbox == plugin.SandboxTypeBubblewrap {
			hasBubblewrap = true
			break
		}
	}

	if hasBubblewrap {
		sandboxExecutor = executor.NewExecutor(&executor.ExecutorConfig{
			RootDir: rootDirAbs,
			WasmDir: wasmDirAbs,
		})
		if err := sandboxExecutor.PrepareSandbox(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to prepare sandbox directories: %v\n", err)
		} else {
			createdDirs := sandboxExecutor.GetSandboxCreatedDirs()
			if len(createdDirs) > 0 {
				fmt.Printf("Created bubblewrap mount directories: %v\n", createdDirs)
			}
		}
	}

	// Run in appropriate mode
	switch *mode {
	case "stdio":
		if err := runStdio(plugins, *apiKey, rootDirAbs, wasmDirAbs, database); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			if sandboxExecutor != nil {
				sandboxExecutor.Cleanup()
			}
			os.Exit(1)
		}
	case "http":
		if err := runHTTP(plugins, *addr, *rateLimit, rootDirAbs, wasmDirAbs, *apiKey, database, *enableOAuth, *oauthIssuer, *enableStreamable, *sessionTTL); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			if sandboxExecutor != nil {
				sandboxExecutor.Cleanup()
			}
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", *mode)
		os.Exit(1)
	}

	// Cleanup on normal exit
	if sandboxExecutor != nil {
		if err := sandboxExecutor.Cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cleanup failed: %v\n", err)
		}
	}
}

func runStdio(plugins *plugin.Config, apiKey string, rootDir string, wasmDir string, database *db.DB) error {
	// For stdio mode, we require API key to be set (either flag or env var)
	expectedAPIKey := apiKey

	server, err := mcp.NewStdioServer(plugins, apiKey, expectedAPIKey, rootDir, wasmDir, database)
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

func runHTTP(plugins *plugin.Config, addr string, rateLimit int, rootDir string, wasmDir string, apiKey string, database *db.DB, enableOAuth bool, oauthIssuer string, enableStreamable bool, sessionTTL time.Duration) error {
	config := &mcp.HTTPConfig{
		RateLimit:        rateLimit,
		RateLimitWindow:  time.Minute,
		RootDir:          rootDir,
		WasmDir:          wasmDir,
		APIKey:           apiKey,
		DB:               database,
		EnableOAuth:      enableOAuth,
		OAuthIssuer:      oauthIssuer,
		EnableStreamable: enableStreamable,
		SessionTTL:       sessionTTL,
	}
	server, err := mcp.NewHTTPServer(plugins, config)
	if err != nil {
		return fmt.Errorf("failed to create HTTP server: %w", err)
	}

	// Start streamable cleanup goroutine if enabled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if server.IsStreamableEnabled() {
		server.StartStreamableCleanup(ctx)
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
		cancel() // Stop streamable cleanup
		server.StopStreamable()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	if wasmDir != "" {
		fmt.Printf("Starting HTTP server on %s (root-dir: %s, wasm-dir: %s)\n", addr, rootDir, wasmDir)
	} else {
		fmt.Printf("Starting HTTP server on %s (root-dir: %s)\n", addr, rootDir)
	}
	if apiKey != "" {
		fmt.Printf("API key authentication enabled\n")
	}
	if enableStreamable {
		fmt.Printf("MCP Streamable HTTP enabled (session TTL: %s)\n", sessionTTL)
	}
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}

func runBridge(addr string, upstream string, upstreamEnv []string, apiKey string, rateLimit int, maxResponseSize int, debug bool, database *db.DB, enableOAuth bool, oauthIssuer string) error {
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
		DB:              database,
		EnableOAuth:     enableOAuth,
		OAuthIssuer:     oauthIssuer,
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
