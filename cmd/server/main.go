package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/mcp"
)

func main() {
	var (
		mode      = flag.String("mode", "stdio", "Server mode: stdio or http")
		dbPath    = flag.String("db", "gatekeeper.db", "SQLite database path")
		addr      = flag.String("addr", ":8080", "HTTP server address (for http mode)")
		apiKey    = flag.String("api-key", "", "API key for stdio mode (or MCP_GATEKEEPER_API_KEY env var)")
		rateLimit = flag.Int("rate-limit", 500, "Rate limit per API key per minute (for http mode)")
	)
	flag.Parse()

	// Get API key from env if not provided
	if *apiKey == "" {
		*apiKey = os.Getenv("MCP_GATEKEEPER_API_KEY")
	}

	// Open database
	database, err := db.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Run in appropriate mode
	switch *mode {
	case "stdio":
		if err := runStdio(database, *apiKey); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "http":
		if err := runHTTP(database, *addr, *rateLimit); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}

func runStdio(database *db.DB, apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("API key required for stdio mode (use --api-key or MCP_GATEKEEPER_API_KEY env var)")
	}

	server, err := mcp.NewStdioServer(database, apiKey)
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

func runHTTP(database *db.DB, addr string, rateLimit int) error {
	config := &mcp.HTTPConfig{
		RateLimit:       rateLimit,
		RateLimitWindow: time.Minute,
	}
	server := mcp.NewHTTPServer(database, config)

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

	fmt.Printf("Starting HTTP server on %s\n", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}
