# MCP Gatekeeper

An MCP (Model Context Protocol) server that **executes shell commands and returns their results**. It enables AI assistants like Claude to run commands on your system and receive stdout, stderr, and exit codes.

While providing shell access, MCP Gatekeeper includes flexible security controls to keep your system safe:

- **Directory sandboxing** - All commands are restricted to a specified root directory (chroot-like)
- **API key-based access control** - Each client gets its own API key with customizable tools
- **Tool-based architecture** - Define specific tools per API key with individual sandbox settings
- **Glob-based argument restrictions** - Fine-grained control over which arguments are allowed per tool
- **Multiple sandbox modes** - Choose between bubblewrap, WASM, or no sandboxing per tool
- **Audit logging** - Complete history of all command executions for review

## Features

- **Shell Command Execution**: Run shell commands and receive stdout, stderr, and exit code
- **Directory Sandbox**: Mandatory `--root-dir` restricts all operations to a specified directory
- **Per-Tool Sandbox Selection**: Each tool can use `none`, `bubblewrap`, or `wasm` sandbox
- **Bubblewrap Sandboxing**: `bwrap` integration for true process isolation
- **WASM Sandboxing**: Run WebAssembly binaries in a secure wazero runtime with module caching
- **Dynamic Tool Registration**: Define custom tools per API key via TUI
- **Dual Protocol Support**: Both stdio and HTTP modes using MCP JSON-RPC protocol
- **TUI Admin Tool**: Interactive terminal interface for managing keys, tools, and viewing logs
- **Audit Logging**: Complete logging of all command requests and execution results
- **Rate Limiting**: Configurable rate limiting for the HTTP API (default: 500 req/min)

## Architecture

### Conceptual Overview

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
│  │  │  Tool: "ruby"                                               │   │   │
│  │  │  ├─ Command: ruby                                           │   │   │
│  │  │  ├─ Allowed Args: ["-e **", "*.rb"]                         │   │   │
│  │  │  ├─ Sandbox: wasm                                           │   │   │
│  │  │  └─ WASM Binary: /opt/ruby-wasm/usr/local/bin/ruby          │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "cat"                                                │   │   │
│  │  │  ├─ Command: /usr/bin/cat                                   │   │   │
│  │  │  ├─ Allowed Args: ["*.txt", "*.md", "*.json"]               │   │   │
│  │  │  └─ Sandbox: bubblewrap                                     │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "ls"                                                 │   │   │
│  │  │  ├─ Command: /usr/bin/ls                                    │   │   │
│  │  │  ├─ Allowed Args: [] (all allowed)                          │   │   │
│  │  │  └─ Sandbox: bubblewrap                                     │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         API Key: "readonly-user"                     │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  Allowed Env Variables: ["PATH"]                                    │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "cat"                                                │   │   │
│  │  │  ├─ Command: /usr/bin/cat                                   │   │   │
│  │  │  ├─ Allowed Args: ["README*"]                               │   │   │
│  │  │  └─ Sandbox: bubblewrap                                     │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Database Structure

```
┌──────────────┐       ┌──────────────────────────────────────┐
│   api_keys   │       │                tools                 │
├──────────────┤       ├──────────────────────────────────────┤
│ id           │───┐   │ id                                   │
│ name         │   │   │ api_key_id (FK) ──────────────────┐  │
│ key_hash     │   │   │ name                               │  │
│ status       │   └──►│ description                        │  │
│ allowed_env_ │       │ command                            │  │
│   keys []    │       │ allowed_arg_globs []               │  │
│ created_at   │       │ sandbox (none/bubblewrap/wasm)     │  │
└──────────────┘       │ wasm_binary                        │  │
                       │ created_at                         │  │
       1 : N           └──────────────────────────────────────┘
```

### Security Flow

```
┌─────────────────────────────────────────────────────────┐
│                      Request                            │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  1. API Key Authentication                              │
│     - Verify key (bcrypt hash)                          │
│     - Check active status                               │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  2. Tool Verification                                   │
│     - Check if tool exists for this API key             │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  3. Argument Validation (Policy)                        │
│     - Match against allowed_arg_globs                   │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  4. Environment Variable Filter                         │
│     - Filter by allowed_env_keys                        │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  5. Sandbox Execution                                   │
│     ┌─────────┬─────────────┬───────────────┐          │
│     │  none   │ bubblewrap  │     wasm      │          │
│     ├─────────┼─────────────┼───────────────┤          │
│     │ Path    │ Namespace   │ wazero        │          │
│     │ valid-  │ isolation   │ complete      │          │
│     │ ation   │ rootDir=/   │ isolation     │          │
│     │ only    │             │ rootDir=/     │          │
│     └─────────┴─────────────┴───────────────┘          │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  6. Audit Logging                                       │
└─────────────────────────────────────────────────────────┘
```

## Installation

```bash
go install github.com/takeshy/mcp-gatekeeper/cmd/server@latest
go install github.com/takeshy/mcp-gatekeeper/cmd/admin@latest
```

Or download from [Releases](https://github.com/takeshy/mcp-gatekeeper/releases).

Or build from source:

```bash
git clone https://github.com/takeshy/mcp-gatekeeper
cd mcp-gatekeeper
make build
```

Check version:

```bash
./mcp-gatekeeper --version
./mcp-gatekeeper-admin --version
```

## Quick Start

### 1. Create an API Key

```bash
./mcp-gatekeeper-admin --db gatekeeper.db
```

In the TUI:
1. Select "API Keys"
2. Press `n` to create a new key
3. Enter a name for the key
4. **Save the generated API key** (it won't be shown again)

### 2. Configure Tools

In the TUI API Keys screen:
1. Select your API key and press Enter to view details
2. Press `t` to manage tools
3. Press `n` to create a new tool

Example tool configuration:
- **Name**: `git` (this becomes the MCP tool name)
- **Description**: `Run git commands`
- **Command**: `/usr/bin/git`
- **Allowed Arg Globs**: `status **`, `log **`, `diff **` (one per line)
- **Sandbox**: `bubblewrap`

### 3. Configure Allowed Environment Variables (Optional)

In the API Key detail screen:
1. Press `v` to edit allowed environment variables
2. Add patterns like `PATH`, `HOME`, `GO*` (one per line)
3. Press Ctrl+S to save

### 4. Run the Server

**HTTP Mode:**
```bash
./mcp-gatekeeper-server \
  --root-dir=/home/user/projects \
  --mode=http \
  --addr=:8080 \
  --db=gatekeeper.db
```

**With WASM directory (for external WASM binaries):**
```bash
./mcp-gatekeeper-server \
  --root-dir=/home/user/projects \
  --wasm-dir=/opt \
  --mode=http \
  --addr=:8080 \
  --db=gatekeeper.db
```

**Stdio Mode (for MCP clients):**
```bash
MCP_GATEKEEPER_API_KEY=your-api-key \
./mcp-gatekeeper-server \
  --root-dir=/home/user/projects \
  --mode=stdio \
  --db=gatekeeper.db
```

### 5. Test Execution

Using curl with MCP JSON-RPC protocol (HTTP mode):
```bash
# Initialize
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "initialize"}'

# List available tools
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}'

# Call a tool
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "git", "arguments": {"cwd": "/home/user/projects", "args": ["status", "--short"]}}}'
```

## Configuration

### Command Line Options

| Option | Default | Description |
|--------|---------|-------------|
| `--root-dir` | (required) | Root directory for command execution (sandbox) |
| `--wasm-dir` | - | Directory containing WASM binaries (mounted as `/.wasm` in WASM sandbox) |
| `--mode` | `stdio` | Server mode: `stdio` or `http` |
| `--db` | `gatekeeper.db` | SQLite database path |
| `--addr` | `:8080` | HTTP server address (for http mode) |
| `--rate-limit` | `500` | Rate limit per API key per minute (for http mode) |
| `--api-key` | - | API key for stdio mode (or use `MCP_GATEKEEPER_API_KEY` env var) |

### Directory Sandbox (--root-dir)

The `--root-dir` option is **required** and creates a chroot-like sandbox:

```bash
# All commands restricted to /home/user/projects
./mcp-gatekeeper-server --root-dir=/home/user/projects --mode=http
```

- Commands cannot access paths outside the root directory
- Symlinks are resolved to prevent escape attempts
- The server refuses to start without this option

### WASM Directory (--wasm-dir)

The `--wasm-dir` option allows WASM binaries to be located outside the root directory:

```bash
# WASM binaries in /opt, working directory in /home/user/projects
./mcp-gatekeeper-server --root-dir=/home/user/projects --wasm-dir=/opt --mode=http
```

- WASM binaries are mounted as `/.wasm` inside the WASM sandbox
- Working directory (`--root-dir`) is mounted as `/` inside the WASM sandbox
- This allows separation of WASM runtimes from user data

### Tool Configuration

Each tool has the following settings:

| Field | Description |
|-------|-------------|
| `name` | MCP tool name (unique per API key) |
| `description` | Tool description shown to MCP clients |
| `command` | Absolute path to executable (e.g., `/usr/bin/git`) |
| `allowed_arg_globs` | Glob patterns for allowed arguments |
| `sandbox` | Sandbox mode: `none`, `bubblewrap`, or `wasm` |
| `wasm_binary` | Path to WASM binary (required for `wasm` sandbox) |

### Sandbox Modes

| Mode | Description |
|------|-------------|
| `none` | No process isolation, only path validation |
| `bubblewrap` | Full namespace isolation using `bwrap` |
| `wasm` | Run WebAssembly binaries in wazero runtime |

**Why bubblewrap?**

Path validation alone only checks the working directory (`cwd`). Without process isolation, a script like `ruby -e "File.read('/etc/passwd')"` could still access files outside the sandbox.

With bubblewrap (`bwrap`), commands run in an isolated namespace where:
- Only the root directory is writable
- System directories (`/usr`, `/bin`, `/lib`) are read-only
- Network access is blocked
- The process cannot escape via symlinks or absolute paths

**Installing bubblewrap:**

```bash
# Debian/Ubuntu
sudo apt install bubblewrap

# Fedora/RHEL
sudo dnf install bubblewrap

# Arch Linux
sudo pacman -S bubblewrap
```

**WASM Sandbox:**

For maximum isolation, you can run WebAssembly binaries:
- Compiled with WASI support
- Runs in wazero runtime (pure Go, no CGO)
- Filesystem access restricted to root directory
- No network access
- **Compiled modules are cached** for fast subsequent executions

**Creating WASM binaries:**

You can compile programs to WASM using various languages. Here are some examples:

*Using Rust:*
```bash
# Install the WASI target
rustup target add wasm32-wasip1

# Create a new project
cargo new --bin my-tool
cd my-tool

# Build for WASI
cargo build --release --target wasm32-wasip1

# The binary will be at target/wasm32-wasip1/release/my-tool.wasm
```

*Using Go:*
```bash
# Build for WASI
GOOS=wasip1 GOARCH=wasm go build -o my-tool.wasm main.go
```

*Using C/C++ (with WASI SDK):*
```bash
# Install WASI SDK from https://github.com/WebAssembly/wasi-sdk
export WASI_SDK_PATH=/opt/wasi-sdk

# Compile
$WASI_SDK_PATH/bin/clang -o my-tool.wasm my-tool.c
```

**Using scripting language runtimes in WASM:**

You can run scripts in sandboxed interpreters compiled to WASM:

*Ruby (ruby.wasm):*
```bash
# Download from https://github.com/ruby/ruby.wasm/releases
# Choose the latest ruby-*-wasm32-unknown-wasip1-full.tar.gz
tar xzf ruby-*-wasm32-unknown-wasip1-full.tar.gz
# Use: ruby-*-wasm32-unknown-wasip1-full/usr/local/bin/ruby
```

*Python (python.wasm):*
```bash
# Download from VMware Labs WebAssembly Language Runtimes
# https://github.com/vmware-labs/webassembly-language-runtimes/releases
# Look for python-*.wasm releases
curl -LO "https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python/3.12.0%2B20231211-040d5a6/python-3.12.0.wasm"
# Use: python-3.12.0.wasm (standard library is bundled, ~26MB)
```

*JavaScript (QuickJS):*
```bash
# Download from QuickJS-NG releases
# https://github.com/quickjs-ng/quickjs/releases
curl -LO "https://github.com/quickjs-ng/quickjs/releases/latest/download/qjs-wasi.wasm"
# Use: qjs-wasi.wasm (~1.4MB, JSON is built-in)
```

**WASM Runtime Comparison:**

| Runtime | Size | Compile Time | JSON Support |
|---------|------|--------------|--------------|
| Ruby | ~50MB (with stdlib) | ~9s | `require 'json'` (auto-configured) |
| Python | ~26MB (bundled) | ~3.6s | `import json` (built-in) |
| QuickJS | ~1.4MB | ~0.5s | `JSON.stringify()` (built-in) |

Note: Compile time is for first execution only. Compiled modules are cached for fast subsequent runs.

**Configuring a WASM tool:**

In the TUI, create a tool with:
- **Name**: `ruby`
- **Description**: `Execute Ruby scripts in WASM sandbox`
- **Command**: `ruby` (any value, not used for WASM)
- **Sandbox**: `wasm`
- **WASM Binary**: `/opt/ruby-wasm/usr/local/bin/ruby`

The WASM binary receives arguments via WASI's `args_get` and can access files within the root directory.

### Glob Patterns

The following glob syntax is supported:

| Pattern | Description |
|---------|-------------|
| `*` | Matches any sequence of characters except `/` |
| `**` | Matches any sequence including `/` |
| `?` | Matches any single character |
| `[abc]` | Matches any character in the set |
| `{a,b}` | Matches either `a` or `b` |

Examples for `allowed_arg_globs`:
- `status **` - Allow `status` with any arguments
- `log --oneline **` - Allow `log --oneline` with any path
- `diff **` - Allow `diff` with any arguments
- Empty (no patterns) - Allow all arguments

## API Reference

### MCP JSON-RPC Protocol

Both stdio and HTTP modes use MCP JSON-RPC 2.0 protocol. HTTP mode accepts requests at `POST /mcp`.

#### initialize

Initialize the MCP session.

**Request:**
```json
{"jsonrpc": "2.0", "id": 1, "method": "initialize"}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2024-11-05",
    "capabilities": {"tools": {}},
    "serverInfo": {"name": "mcp-gatekeeper", "version": "1.0.0"}
  }
}
```

#### tools/list

List available tools for the authenticated API key.

**Request:**
```json
{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "git",
        "description": "Run git commands",
        "inputSchema": {
          "type": "object",
          "properties": {
            "cwd": {"type": "string", "description": "Working directory for the command (defaults to root directory)"},
            "args": {"type": "array", "items": {"type": "string"}, "description": "Command arguments"}
          },
          "required": []
        }
      }
    ]
  }
}
```

#### tools/call

Execute a tool.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "git",
    "arguments": {
      "cwd": "/path/to/directory",
      "args": ["status", "--short"]
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [{"type": "text", "text": "M  README.md\n"}],
    "isError": false,
    "metadata": {"exitCode": 0, "stderr": ""}
  }
}
```

**Error Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "error": {
    "code": -32001,
    "message": "Arguments denied by policy",
    "data": "Args not in allowed patterns"
  }
}
```

### HTTP Authentication

HTTP mode requires Bearer token authentication:

```
Authorization: Bearer your-api-key
```

## TUI Admin Tool

The admin tool provides:

- **API Keys**: Create, view, revoke API keys
- **Tools**: Configure tools per API key (command, args, sandbox)
- **Environment Variables**: Configure allowed environment variables per key
- **Audit Logs**: Browse and inspect execution history
- **Test Execute**: Test tool execution with real commands

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Navigate |
| `Enter` | Select/Confirm |
| `Esc` | Go back |
| `n` | New item |
| `e` | Edit |
| `d` | Delete/Revoke |
| `t` | Manage tools (in API Key detail) |
| `v` | Edit env vars (in API Key detail) |
| `q` | Quit |
| `Tab` | Next field |
| `Ctrl+S` | Save |

## Security Considerations

1. **Directory Sandbox**: All commands are restricted to `--root-dir`; paths outside are rejected
2. **Per-Tool Sandbox**: Each tool can specify its isolation level (none, bubblewrap, wasm)
3. **Argument Restrictions**: Use `allowed_arg_globs` to limit what arguments can be passed
4. **API Key Storage**: API keys are hashed with bcrypt; the plaintext is only shown once at creation
5. **Audit Logs**: All requests are logged regardless of the decision (stored in plain text)
6. **Rate Limiting**: HTTP API includes configurable per-key rate limiting
7. **Symlink Resolution**: Symlinks are resolved to prevent sandbox escape

**Security Levels:**

| Sandbox Mode | Protection Level | Notes |
|--------------|------------------|-------|
| `wasm` | Highest | WASI sandbox, no system calls |
| `bubblewrap` | High | Full namespace isolation, recommended for native binaries |
| `none` | Basic | Path validation only, use for trusted commands |

## Development

### Build Commands

```bash
make build    # Build mcp-gatekeeper and mcp-gatekeeper-admin
make test     # Run tests
make clean    # Remove build artifacts
make release  # Build for all platforms (linux/darwin/windows, amd64/arm64)
```

### Database

The database is automatically created and migrated on startup:

- If `gatekeeper.db` doesn't exist, it will be created
- Migrations in `internal/db/migrations/*.sql` are applied automatically
- Schema version is tracked in `schema_migrations` table
- When upgrading to a new release, just run the new binary - schema changes are applied automatically

### Running Tests

```bash
make test
# or
go test ./...
```

### Project Structure

```
mcp-gatekeeper/
├── cmd/
│   ├── server/          # MCP server (stdio/HTTP)
│   └── admin/           # TUI admin tool
├── internal/
│   ├── auth/            # API key authentication
│   ├── policy/          # Argument evaluation engine
│   ├── executor/        # Command execution engine
│   ├── db/              # Database access layer
│   ├── mcp/             # MCP protocol implementation
│   └── tui/             # TUI screens
└── README.md
```

## License

MIT License
