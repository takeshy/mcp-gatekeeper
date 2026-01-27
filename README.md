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
| `--db` | `gatekeeper.db` | SQLite database path |
| `--addr` | `:8080` | HTTP address (http/bridge) |
| `--api-key` | - | API key (stdio/bridge) |
| `--rate-limit` | `500` | Rate limit per minute (http/bridge) |
| `--upstream` | - | Upstream command (required for bridge) |
| `--upstream-env` | - | Environment variables for upstream (comma-separated) |
| `--max-response-size` | `500000` | Max response size in bytes (bridge only) |
| `--debug` | `false` | Enable debug logging (bridge only) |
| `--wasm-dir` | - | WASM binary directory |

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
