# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

MCP Gatekeeper is a Go-based Model Context Protocol (MCP) server that executes shell commands with flexible security controls. It enables AI assistants to run commands on a system while maintaining fine-grained access control through directory sandboxing, per-API key tool configuration, and multiple sandbox modes (none, bubblewrap, WASM).

## Build Commands

```bash
make build     # Builds mcp-gatekeeper and mcp-gatekeeper-admin binaries
make test      # Runs all Go tests (go test ./...)
make clean     # Removes dist/ directory and binary files
make release   # Cross-platform builds for linux/darwin/windows, amd64/arm64
```

Run a single test:
```bash
go test -v -run TestFunctionName ./internal/package/
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    MCP Gatekeeper                           │
├─────────────────────────────────────────────────────────────┤
│  API Key Authentication (bcrypt, per-key tools & env vars)  │
│                          ↓                                  │
│  Policy Evaluation (glob pattern matching on arguments)     │
│                          ↓                                  │
│  Command Execution with Sandboxing                          │
│  ├─ None (path validation only)                             │
│  ├─ Bubblewrap (bwrap namespace isolation)                  │
│  └─ WASM (wazero runtime with module caching)               │
│                          ↓                                  │
│  Audit Logging (SQLite) & Rate Limiting (HTTP mode)         │
└─────────────────────────────────────────────────────────────┘

Protocol Support:
  ├─ Stdio Mode: Direct MCP client integration
  ├─ HTTP Mode: JSON-RPC 2.0 with Bearer token auth
  └─ Bridge Mode: HTTP proxy to stdio MCP servers
```

## Key Source Files

**Entry Points:**
- `cmd/server/main.go` - Main MCP server with stdio/http/bridge modes
- `cmd/admin/main.go` - TUI admin tool (Bubble Tea)

**Core Components:**
- `internal/auth/auth.go` - API key generation, bcrypt hashing, authentication
- `internal/policy/evaluator.go` - Argument validation against glob patterns
- `internal/policy/matcher.go` - Glob matching with caching (gobwas/glob)
- `internal/executor/executor.go` - Command execution with timeout (30s) and output limits (1MB)
- `internal/executor/sandbox.go` - Bubblewrap namespace isolation
- `internal/executor/wasm.go` - WASM execution via wazero

**MCP Protocol:**
- `internal/mcp/types.go` - JSON-RPC 2.0 types and MCP method definitions
- `internal/mcp/stdio.go` - Stdio mode handler (line-by-line JSON-RPC)
- `internal/mcp/http.go` - HTTP mode with rate limiting and auth middleware

**HTTP Bridge:**
- `internal/bridge/client.go` - Stdio MCP client for upstream communication
- `internal/bridge/server.go` - HTTP bridge server with rate limiting

**Database:**
- `internal/db/db.go` - SQLite wrapper with embedded migrations
- `internal/db/apikey.go` - API key CRUD operations
- `internal/db/tool.go` - Tool definitions per API key
- `internal/db/audit.go` - Execution audit logging
- `internal/db/migrations/` - SQL migration files (embedded via go:embed)

**TUI Admin:**
- `internal/tui/app.go` - Main state machine for admin screens

## Execution Flow

1. Request arrives (stdio JSON-RPC or HTTP with Bearer token)
2. Auth middleware authenticates API key (HTTP checks rate limit)
3. Tool lookup by name from database for that API key
4. Policy evaluation via `Evaluator.EvaluateArgs()` - glob matching
5. If allowed: filter env vars, route to appropriate sandbox, execute
6. Audit log entry created with full request/response details
7. Return JSON-RPC result with stdout, stderr, exit code

## Security Model (5 Layers)

1. **API Key Authentication** - bcrypt-hashed keys in SQLite
2. **Tool Policy Evaluation** - Glob patterns for allowed arguments per tool
3. **Environment Variable Filtering** - Only specified env var patterns passed
4. **Directory Sandboxing** - `--root-dir` enforces chroot-like boundary
5. **Process Isolation** - Optional bwrap (namespace) or WASM (complete) sandboxing

## Key Dependencies

- `charmbracelet/bubbletea` - TUI framework
- `go-chi/chi` - HTTP router
- `gobwas/glob` - Glob pattern matching
- `modernc.org/sqlite` - Pure Go SQLite (no CGO)
- `tetratelabs/wazero` - WASM runtime

## CLI Flags

```bash
--root-dir=/path     # Required for stdio/http: sandbox root directory
--db=gatekeeper.db   # SQLite database location
--mode=stdio|http|bridge  # Protocol mode
--addr=:8080         # HTTP listen address
--api-key=...        # API key for stdio/bridge mode
--wasm-dir=/opt      # External WASM binaries directory
--rate-limit=500     # Max requests/min per key (HTTP/bridge mode)
--upstream='cmd'     # Upstream stdio MCP server command (bridge mode)
--upstream-env=...   # Comma-separated env vars for upstream (bridge mode)
```

## Per-Tool Configuration

Each API key has independent tools with:
- `name` - Unique tool name per API key
- `command` - Executable path
- `allowed_arg_globs` - JSON array of glob patterns for argument validation
- `sandbox` - "none" | "bubblewrap" | "wasm"
- `wasm_binary` - Path to WASM binary (if sandbox=wasm)
