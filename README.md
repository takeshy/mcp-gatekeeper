# MCP Gatekeeper

A secure MCP (Model Context Protocol) server that controls command execution based on API key policies. Features API key-based authentication, glob-based policy rules, comprehensive audit logging, and a TUI administration tool.

## Features

- **API Key Authentication**: Secure API key management with bcrypt hashing
- **Policy-based Access Control**: Flexible glob patterns for allowing/denying commands and working directories
- **Dual Protocol Support**: Both stdio (JSON-RPC) and HTTP API modes
- **Audit Logging**: Complete logging of all command requests and execution results
- **TUI Admin Tool**: Interactive terminal interface for managing keys, policies, and viewing logs
- **Rate Limiting**: Built-in rate limiting for the HTTP API
- **Command Normalization**: Automatic path resolution and command normalization

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
3. Configure allowed/denied patterns:

Example policy:
- Allowed CWD Globs: `/home/user/**`
- Allowed Cmd Globs: `ls *`, `cat *`, `git *`
- Denied Cmd Globs: `rm -rf *`, `sudo *`

### 3. Run the Server

**HTTP Mode:**
```bash
./mcp-gatekeeper-server --mode=http --addr=:8080 --db=gatekeeper.db
```

**Stdio Mode (for MCP clients):**
```bash
MCP_GATEKEEPER_API_KEY=your-api-key ./mcp-gatekeeper-server --mode=stdio --db=gatekeeper.db
```

### 4. Test Execution

Using curl (HTTP mode):
```bash
curl -X POST http://localhost:8080/v1/execute \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"cwd": "/home/user", "cmd": "ls", "args": ["-la"]}'
```

## Configuration

### Database

The server uses SQLite for storage. Specify the database path with `--db`:

```bash
--db=/path/to/gatekeeper.db
```

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
- **Policies**: Configure allowed/denied patterns per key
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
| `Ctrl+S` | Save |

## Security Considerations

1. **API Key Storage**: API keys are hashed with bcrypt; the plaintext is only shown once at creation
2. **Policy Design**: Start with restrictive policies and add allowances as needed
3. **Audit Logs**: All requests are logged regardless of the decision
4. **Rate Limiting**: HTTP API includes per-key rate limiting
5. **Command Normalization**: Commands are normalized to prevent path traversal tricks

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
