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
- **WASM Sandboxing**: Run WebAssembly binaries in a secure wazero runtime
- **Dynamic Tool Registration**: Define custom tools per API key via TUI
- **Dual Protocol Support**: Both stdio (JSON-RPC for MCP) and HTTP API modes
- **TUI Admin Tool**: Interactive terminal interface for managing keys, tools, and viewing logs
- **Audit Logging**: Complete logging of all command requests and execution results
- **Rate Limiting**: Configurable rate limiting for the HTTP API (default: 500 req/min)

## Installation

```bash
go install github.com/takeshy/mcp-gatekeeper/cmd/server@latest
go install github.com/takeshy/mcp-gatekeeper/cmd/admin@latest
```

Or build from source:

```bash
git clone https://github.com/takeshy/mcp-gatekeeper
cd mcp-gatekeeper
go build -o mcp-gatekeeper-server ./cmd/server
go build -o mcp-gatekeeper-admin ./cmd/admin
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

**Stdio Mode (for MCP clients):**
```bash
MCP_GATEKEEPER_API_KEY=your-api-key \
./mcp-gatekeeper-server \
  --root-dir=/home/user/projects \
  --mode=stdio \
  --db=gatekeeper.db
```

### 5. Test Execution

Using curl (HTTP mode):
```bash
# List available tools
curl http://localhost:8080/v1/tools \
  -H "Authorization: Bearer your-api-key"

# Call a tool
curl -X POST http://localhost:8080/v1/tools/git \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"cwd": "/home/user/projects", "args": ["status", "--short"]}'
```

## Configuration

### Command Line Options

| Option | Default | Description |
|--------|---------|-------------|
| `--root-dir` | (required) | Root directory for command execution (sandbox) |
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
# Download from https://github.com/nickstenning/python-wasm/releases or build from source
# Use: python.wasm
```

*Node.js is not available as WASI, but you can use QuickJS:*
```bash
# Download from https://nickstenning.github.io/verless-quickjs-wasm/
# Or build from https://github.com/nickstenning/verless-quickjs-wasm
curl -LO https://nickstenning.github.io/verless-quickjs-wasm/quickjs.wasm
# Use: quickjs.wasm
```

**Configuring a WASM tool:**

In the TUI, create a tool with:
- **Name**: `my-tool`
- **Description**: `My WASM tool`
- **Command**: `my-tool` (any value, not used for WASM)
- **Sandbox**: `wasm`
- **WASM Binary**: `/path/to/my-tool.wasm`

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

### HTTP API

#### GET /v1/tools

List available tools for the authenticated API key.

**Headers:**
- `Authorization: Bearer <api-key>` (required)

**Response:**
```json
{
  "tools": [
    {
      "name": "git",
      "description": "Run git commands",
      "inputSchema": {
        "type": "object",
        "properties": {
          "cwd": {"type": "string", "description": "Working directory"},
          "args": {"type": "array", "items": {"type": "string"}, "description": "Command arguments"}
        },
        "required": ["cwd"]
      }
    }
  ]
}
```

#### POST /v1/tools/{toolName}

Execute a tool.

**Headers:**
- `Authorization: Bearer <api-key>` (required)

**Request Body:**
```json
{
  "cwd": "/path/to/directory",
  "args": ["arg1", "arg2"]
}
```

**Response:**
```json
{
  "exit_code": 0,
  "stdout": "output...",
  "stderr": "",
  "duration_ms": 45
}
```

**Error Response:**
```json
{
  "error": "arguments not in allowed patterns"
}
```

### MCP Protocol (stdio)

The server dynamically generates MCP tools from the database. Each tool registered for the API key becomes available as an MCP tool.

**Tool Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "cwd": {
      "type": "string",
      "description": "Working directory for the command"
    },
    "args": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Command arguments"
    }
  },
  "required": ["cwd"]
}
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

### Running Tests

```bash
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
