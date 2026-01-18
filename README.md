# MCP Gatekeeper

An MCP (Model Context Protocol) server that **executes shell commands and returns their results**. It enables AI assistants like Claude to run commands on your system and receive stdout, stderr, and exit codes.

While providing shell access, MCP Gatekeeper includes flexible security controls to keep your system safe:

- **Directory sandboxing** - All commands are restricted to a specified root directory (chroot-like)
- **API key-based access control** - Each client gets its own API key with customizable permissions
- **Glob-based policy rules** - Fine-grained control over which commands and directories are allowed
- **Audit logging** - Complete history of all command executions for review

## Features

- **Shell Command Execution**: Run any shell command and receive stdout, stderr, and exit code
- **Directory Sandbox**: Mandatory `--root-dir` restricts all operations to a specified directory
- **Bubblewrap Sandboxing**: Optional `bwrap` integration for true process isolation (auto-detected)
- **Flexible Security**: Configure allowed/denied commands and directories per API key using glob patterns
- **Dual Protocol Support**: Both stdio (JSON-RPC for MCP) and HTTP API modes
- **TUI Admin Tool**: Interactive terminal interface for managing keys, policies, and viewing logs
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

### 2. Configure Policy

In the TUI API Keys screen:
1. Select your API key
2. Press `e` to edit its policy
3. Configure allowed/denied patterns (Ctrl+Space for path completion in CWD field)

Example policy:
- Allowed CWD Globs: `/home/user/projects/**`
- Allowed Cmd Globs: `ls *`, `cat *`, `git *`
- Denied Cmd Globs: `rm -rf *`, `sudo *`

### 3. Run the Server

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

### 4. Test Execution

Using curl (HTTP mode):
```bash
curl -X POST http://localhost:8080/v1/execute \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"cwd": "/home/user/projects", "cmd": "ls", "args": ["-la"]}'
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
| `--sandbox` | `auto` | Sandbox mode: `auto`, `bwrap`, or `none` |
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

### Sandbox Mode (--sandbox)

The `--sandbox` option controls process isolation:

| Mode | Description |
|------|-------------|
| `auto` | Automatically use `bwrap` if available, fall back to path validation |
| `bwrap` | Require bubblewrap sandboxing (warns and falls back if not installed) |
| `none` | Use only path validation (no process isolation) |

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

The server automatically detects if `bwrap` is available and uses it when `--sandbox=auto` (default).

### Policy Precedence

Two modes are available:

- `deny_overrides` (default): Deny rules are checked first; if a command is denied, it's blocked even if it matches an allow rule
- `allow_overrides`: Allow rules take precedence; if a command matches an allow rule, it's permitted even if it matches a deny rule

### Glob Patterns

The following glob syntax is supported:

| Pattern | Description |
|---------|-------------|
| `*` | Matches any sequence of characters except `/` |
| `**` | Matches any sequence including `/` |
| `?` | Matches any single character |
| `[abc]` | Matches any character in the set |
| `{a,b}` | Matches either `a` or `b` |

Examples:
- `/home/**` - All paths under /home
- `/usr/bin/*` - Any command in /usr/bin
- `git *` - Any git command
- `rm -rf *` - Block recursive force remove

## API Reference

### HTTP API

#### POST /v1/execute

Execute a command.

**Headers:**
- `Authorization: Bearer <api-key>` (required)

**Request Body:**
```json
{
  "cwd": "/path/to/directory",
  "cmd": "command",
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
  "error": "command denied by policy: ..."
}
```

### MCP Protocol (stdio)

The server implements the MCP protocol with the following tool:

#### execute

Execute a shell command.

**Input Schema:**
```json
{
  "type": "object",
  "properties": {
    "cwd": {
      "type": "string",
      "description": "Working directory for the command"
    },
    "cmd": {
      "type": "string",
      "description": "Command to execute"
    },
    "args": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Command arguments"
    }
  },
  "required": ["cwd", "cmd"]
}
```

## TUI Admin Tool

The admin tool provides:

- **API Keys**: Create, view, revoke API keys
- **Policies**: Configure allowed/denied patterns per key (with path completion)
- **Audit Logs**: Browse and inspect execution history
- **Test Execute**: Test commands against policies

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Navigate |
| `Enter` | Select/Confirm |
| `Esc` | Go back |
| `n` | New item |
| `e` | Edit |
| `d` | Delete/Revoke |
| `q` | Quit |
| `Tab` | Next field |
| `Ctrl+Space` | Path completion (in CWD field) |
| `Ctrl+S` | Save |

## Security Considerations

1. **Directory Sandbox**: All commands are restricted to `--root-dir`; paths outside are rejected
2. **Bubblewrap Isolation**: When available, commands run in isolated namespaces preventing filesystem escape
3. **API Key Storage**: API keys are hashed with bcrypt; the plaintext is only shown once at creation
4. **Policy Design**: Start with restrictive policies and add allowances as needed
5. **Audit Logs**: All requests are logged regardless of the decision (stored in plain text)
6. **Rate Limiting**: HTTP API includes configurable per-key rate limiting
7. **Symlink Resolution**: Symlinks are resolved to prevent sandbox escape

**Security Levels:**

| Sandbox Mode | Protection Level | Notes |
|--------------|------------------|-------|
| `bwrap` | High | Full namespace isolation, recommended for production |
| `none` | Basic | Path validation only, scripts can bypass via absolute paths |

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
│   ├── policy/          # Policy evaluation engine
│   ├── executor/        # Command execution engine
│   ├── db/              # Database access layer
│   ├── mcp/             # MCP protocol implementation
│   └── tui/             # TUI screens
└── README.md
```

## License

MIT License
