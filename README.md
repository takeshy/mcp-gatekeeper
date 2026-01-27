# MCP Gatekeeper

An MCP (Model Context Protocol) server that provides shell command execution capabilities to AI assistants.

## Three Operating Modes

| Mode | Use Case |
|------|----------|
| **stdio** | Direct integration with MCP clients (Claude Desktop, etc.) |
| **http** | Expose as HTTP API |
| **bridge** | Expose existing stdio MCP servers over HTTP |

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              MCP Gatekeeper                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         API Key: "dev-team"                          │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  Allowed Env Variables: ["PATH", "HOME", "LANG", "GO*"]             │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "git"                                                │   │   │
│  │  │  ├─ Command: /usr/bin/git                                   │   │   │
│  │  │  ├─ Allowed Args: ["status **", "log **", "diff **"]        │   │   │
│  │  │  └─ Sandbox: bubblewrap                                     │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "ruby"                                               │   │   │
│  │  │  ├─ Command: ruby                                           │   │   │
│  │  │  ├─ Allowed Args: ["-e **", "*.rb"]                         │   │   │
│  │  │  ├─ Sandbox: wasm                                           │   │   │
│  │  │  └─ WASM Binary: /opt/ruby-wasm/usr/local/bin/ruby          │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         API Key: "readonly"                         │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "cat"                                                │   │   │
│  │  │  ├─ Command: /usr/bin/cat                                   │   │   │
│  │  │  ├─ Allowed Args: ["*.md", "*.txt"]                         │   │   │
│  │  │  └─ Sandbox: bubblewrap                                     │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Installation

```bash
go install github.com/takeshy/mcp-gatekeeper/cmd/server@latest
go install github.com/takeshy/mcp-gatekeeper/cmd/admin@latest
```

Or download from [Releases](https://github.com/takeshy/mcp-gatekeeper/releases).

## Quick Start

### 1. Create API Key and Tools

```bash
./mcp-gatekeeper-admin --db gatekeeper.db
```

In the TUI:
1. "API Keys" → `n` to create → **Save the API key** (shown only once)
2. Select API key → `t` for tools → `n` to create

Tool configuration example:
- Name: `git`
- Command: `/usr/bin/git`
- Allowed Args: `status **`, `log **`, `diff **` (one pattern per line)
- Sandbox: `bubblewrap`

### 2. Start Server

**HTTP Mode:**
```bash
# With per-request auth (client sends Authorization header)
./mcp-gatekeeper --mode=http --root-dir=/home/user/projects --db=gatekeeper.db

# With fixed API key (no auth header required from client)
./mcp-gatekeeper --mode=http --root-dir=/home/user/projects --db=gatekeeper.db --api-key=your-key

# Using environment variable
MCP_GATEKEEPER_API_KEY=your-key ./mcp-gatekeeper --mode=http --root-dir=/home/user/projects --db=gatekeeper.db
```

**Stdio Mode:**
```bash
MCP_GATEKEEPER_API_KEY=your-key ./mcp-gatekeeper --mode=stdio --root-dir=/home/user/projects --db=gatekeeper.db
```

**Bridge Mode:**
```bash
./mcp-gatekeeper --mode=bridge --upstream='npx @anthropic-ai/mcp-server' --addr=:8080

# Playwright MCP example (headless browser automation)
./mcp-gatekeeper --mode=bridge --addr=:8090 --upstream='npx @playwright/mcp --executable-path /path/to/chrome --headless --no-sandbox --isolated'

# With debug logging
./mcp-gatekeeper --debug --mode=bridge --addr=:8090 --upstream='npx @playwright/mcp --headless'

# With audit logging (optional)
./mcp-gatekeeper --mode=bridge --upstream='npx @playwright/mcp --headless' --db=gatekeeper.db

# With API key authentication
./mcp-gatekeeper --mode=bridge --upstream='npx @playwright/mcp --headless' --api-key=your-secret-key
```

### 3. Test Execution

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"git","arguments":{"args":["status"]}}}'
```

## CLI Options

| Option | Default | Description |
|--------|---------|-------------|
| `--mode` | `stdio` | `stdio`, `http`, `bridge` |
| `--root-dir` | (required) | Sandbox root (required for stdio/http) |
| `--db` | `gatekeeper.db` | SQLite database path (optional for bridge) |
| `--addr` | `:8080` | HTTP address (http/bridge) |
| `--api-key` | - | Fixed API key for all modes (or `MCP_GATEKEEPER_API_KEY` env var) |
| `--rate-limit` | `500` | Rate limit per minute (http/bridge) |
| `--upstream` | - | Upstream command (required for bridge) |
| `--upstream-env` | - | Environment variables for upstream (comma-separated) |
| `--max-response-size` | `500000` | Max response size in bytes (bridge only) |
| `--debug` | `false` | Enable debug logging (bridge only) |
| `--wasm-dir` | - | WASM binary directory |

## Bridge Mode Features

### File Externalization

Large content (>500KB) in MCP responses is automatically externalized to temporary files. Clients receive a URL to retrieve the file via HTTP.

**Response format:**
```json
{
  "type": "external_file",
  "url": "http://localhost:8090/files/abc123...",
  "mimeType": "image/png",
  "size": 1843200
}
```

**File retrieval:**
```bash
curl http://localhost:8090/files/abc123...
```

Files are deleted after one retrieval (one-time access). If `--api-key` is set, the `/files/{key}` endpoint also requires authentication.

### Audit Logging

When `--db` is specified, all MCP requests and responses are logged to the `bridge_audit_logs` table.

```bash
# Enable audit logging
./mcp-gatekeeper --mode=bridge --upstream='...' --db=gatekeeper.db
```

**Logged fields:**
- `method` - MCP method (e.g., `tools/call`, `initialize`)
- `params` - Request parameters (JSON)
- `response` - Original response before externalization (JSON)
- `error` - Error message if any
- `request_size` / `response_size` - Sizes in bytes
- `duration_ms` - Processing time in milliseconds
- `created_at` - Timestamp

**Query logs:**
```bash
sqlite3 gatekeeper.db "SELECT id, method, response_size, duration_ms FROM bridge_audit_logs ORDER BY id DESC LIMIT 10"
```

## Sandbox

| Mode | Isolation Level | Use Case |
|------|-----------------|----------|
| `none` | Path validation only | Trusted commands |
| `bubblewrap` | Namespace isolation | Native binaries (recommended) |
| `wasm` | Full isolation | WASI-compatible binaries |

### Bubblewrap

```bash
# Installation
sudo apt install bubblewrap  # Debian/Ubuntu
sudo dnf install bubblewrap  # Fedora
sudo pacman -S bubblewrap    # Arch
```

### WASM

Uses WASI-compatible binaries. File access restricted to `--root-dir`.

```bash
# Ruby
tar xzf ruby-*-wasm32-unknown-wasip1-full.tar.gz

# Python
curl -LO https://github.com/aspect-build/aspect-cli/releases/.../python-3.12.0.wasm

# Go
GOOS=wasip1 GOARCH=wasm go build -o tool.wasm main.go
```

## Glob Patterns

| Pattern | Description |
|---------|-------------|
| `*` | Any string except `/` |
| `**` | Any string including `/` |
| `?` | Any single character |
| `[abc]` | Character class |
| `{a,b}` | Alternation |

Examples: `status **`, `log --oneline **`, `diff **`

## License

MIT License
