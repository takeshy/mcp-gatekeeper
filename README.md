# MCP Gatekeeper

An MCP (Model Context Protocol) server that provides secure shell command execution and HTTP proxy capabilities for AI assistants.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              MCP Gatekeeper                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Protocol Layer                                                     │   │
│  │  ├─ Stdio Mode: Direct MCP client integration                       │   │
│  │  ├─ HTTP Mode: JSON-RPC 2.0 with Bearer token auth                  │   │
│  │  └─ Bridge Mode: HTTP proxy to stdio MCP servers                    │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  API Key Authentication & Rate Limiting                             │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Plugin Configuration                                               │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  Allowed Env Variables: ["PATH", "HOME", "LANG", "GIT_*"]           │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "git-log"                                            │   │   │
│  │  │  ├─ Command: git                                            │   │   │
│  │  │  ├─ Args Prefix: ["log"]                                    │   │   │
│  │  │  ├─ Allowed Args: ["", "*"]                                 │   │   │
│  │  │  ├─ Sandbox: none                                           │   │   │
│  │  │  └─ UI Template: templates/log.html                         │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "ls"                                                 │   │   │
│  │  │  ├─ Command: ls                                             │   │   │
│  │  │  ├─ Allowed Args: ["-la", "*"]                              │   │   │
│  │  │  ├─ Sandbox: bubblewrap                                     │   │   │
│  │  │  └─ UI Type: log                                            │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "ruby"                                               │   │   │
│  │  │  ├─ Allowed Args: ["-e **", "*.rb"]                         │   │   │
│  │  │  ├─ Sandbox: wasm                                           │   │   │
│  │  │  └─ WASM Binary: /opt/ruby-wasm/usr/local/bin/ruby          │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Policy Evaluation (glob pattern matching on arguments)             │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Command Execution with Sandboxing                                  │   │
│  │  ├─ None: Path validation only                                      │   │
│  │  ├─ Bubblewrap: Linux namespace isolation (bwrap)                   │   │
│  │  └─ WASM: WebAssembly sandbox (wazero runtime)                      │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Audit Logging (SQLite) & MCP Apps UI Rendering                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Features

- **Three Operating Modes**: stdio, http, bridge
- **JSON Plugin Configuration**: Define tools via simple JSON files
- **Flexible Sandboxing**: none, bubblewrap, or WASM isolation
- **Policy-Based Access Control**: Glob patterns for argument validation
- **Optional Audit Logging**: SQLite-based logging for all modes
- **Large Response Handling**: Automatic file externalization in bridge mode
- **MCP Apps UI Support**: Rich HTML interfaces for tool outputs

## Operating Modes

| Mode | Use Case |
|------|----------|
| **stdio** | Direct integration with MCP clients (Claude Desktop, etc.) |
| **http** | Expose shell commands as HTTP API |
| **bridge** | Proxy existing stdio MCP servers over HTTP |

Mode is auto-detected: `--addr` implies http mode, `--upstream` implies bridge mode.

## Installation

```bash
go install github.com/takeshy/mcp-gatekeeper/cmd/server@latest
```

Or download from [Releases](https://github.com/takeshy/mcp-gatekeeper/releases).

## Quick Start

### 1. Create a Plugin Directory

Create a directory with `plugin.json` and optional templates:

```
my-plugin/
├── plugin.json
└── templates/
    └── custom.html
```

**plugin.json**:
```json
{
  "tools": [
    {
      "name": "git-log",
      "description": "Show git commit log",
      "command": "git",
      "args_prefix": ["log"],
      "allowed_arg_globs": ["", "*"],
      "sandbox": "none",
      "ui_template": "templates/log.html"
    },
    {
      "name": "ls",
      "description": "List directory contents",
      "command": "ls",
      "allowed_arg_globs": ["*"],
      "sandbox": "bubblewrap",
      "ui_type": "log"
    }
  ],
  "allowed_env_keys": ["PATH", "HOME", "LANG"]
}
```

**Note**: `args_prefix` defines fixed arguments that are automatically prepended. With `args_prefix: ["log"]`, calling `git-log` with `args: ["--oneline"]` executes `git log --oneline`. The `allowed_arg_globs` validates user-provided args only (not the prefix).

### 2. Start the Server

**Stdio Mode** (for MCP clients like Claude Desktop):
```bash
./mcp-gatekeeper --mode=stdio \
  --root-dir=/home/user/projects \
  --plugin-file=my-plugin/plugin.json \
  --api-key=your-secret-key
```

**Claude Code Configuration** (`~/.claude/settings.json`):
```json
{
  "mcpServers": {
    "gatekeeper": {
      "command": "/path/to/mcp-gatekeeper",
      "args": [
        "--root-dir", "/home/user/projects",
        "--plugins-dir", "/path/to/plugins"
      ]
    }
  }
}
```

**HTTP Mode**:
```bash
./mcp-gatekeeper --mode=http \
  --root-dir=/home/user/projects \
  --plugins-dir=plugins/ \
  --api-key=your-secret-key \
  --addr=:8080
```

**Bridge Mode** (proxy existing MCP servers):
```bash
./mcp-gatekeeper --mode=bridge \
  --upstream='npx @playwright/mcp --headless' \
  --api-key=your-secret-key \
  --addr=:8080
```

### 3. Test It

```bash
# List available tools
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# Execute a tool
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ls","arguments":{"args":["-la"]}}}'
```

## Plugin Configuration

### Single Plugin File

```bash
./mcp-gatekeeper --plugin-file=my-plugin/plugin.json ...
```

### Plugin Directory (Multiple Plugins)

```bash
./mcp-gatekeeper --plugins-dir=plugins/ ...
```

Supports two formats:
- **Flat files**: `plugins/*.json` files are loaded directly
- **Subdirectories**: `plugins/*/plugin.json` directories are loaded

```
plugins/
├── git/
│   ├── plugin.json
│   └── templates/
│       ├── log.html
│       └── diff.html
├── shell/
│   ├── plugin.json
│   └── templates/
│       └── table.html
└── simple.json          # Flat file also supported
```

Tool names must be unique across all plugins.

### Plugin File Format

```json
{
  "tools": [
    {
      "name": "tool-name",
      "description": "Tool description",
      "command": "/path/to/executable",
      "args_prefix": ["subcommand"],
      "allowed_arg_globs": ["pattern1", "pattern2"],
      "sandbox": "none|bubblewrap|wasm",
      "wasm_binary": "/path/to/binary.wasm",
      "ui_type": "log|table|json",
      "ui_template": "templates/custom.html"
    }
  ],
  "allowed_env_keys": ["PATH", "HOME", "CUSTOM_*"]
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique tool name |
| `description` | No | Tool description |
| `command` | Yes* | Executable path (*not required for wasm) |
| `args_prefix` | No | Fixed arguments prepended to user args (e.g., `["log"]` for git-log) |
| `allowed_arg_globs` | No | Glob patterns for allowed user arguments (evaluated before args_prefix) |
| `sandbox` | No | `none`, `bubblewrap`, or `wasm` (default: `none`) |
| `wasm_binary` | Yes* | WASM binary path (*required when sandbox=wasm) |
| `ui_type` | No | Built-in UI: `table`, `json`, or `log` |
| `ui_template` | No | Path to custom HTML template (relative to plugin.json) |

**Note**: Template paths are relative to the plugin.json file location. Parent directory references (`..`) are not allowed for security.

## CLI Options

| Option | Default | Description |
|--------|---------|-------------|
| `--mode` | `stdio` | `stdio`, `http`, or `bridge` |
| `--root-dir` | - | Sandbox root directory (required for stdio/http) |
| `--plugin-file` | - | Single plugin JSON file |
| `--plugins-dir` | - | Directory containing plugin directories/files |
| `--api-key` | - | API key for authentication (or `MCP_GATEKEEPER_API_KEY` env) |
| `--db` | - | SQLite database path for audit logging (optional) |
| `--addr` | `:8080` | HTTP listen address (http/bridge) |
| `--rate-limit` | `500` | Max requests per minute (http/bridge) |
| `--upstream` | - | Upstream MCP server command (required for bridge) |
| `--upstream-env` | - | Environment variables for upstream (comma-separated) |
| `--max-response-size` | `500000` | Max response size in bytes (bridge) |
| `--debug` | `false` | Enable debug logging (bridge) |
| `--wasm-dir` | - | Directory containing WASM binaries |

## Audit Logging

Enable audit logging by specifying `--db`:

```bash
./mcp-gatekeeper --mode=http --db=audit.db ...
```

All `tools/call` requests are logged to the `audit_logs` table:

| Field | Description |
|-------|-------------|
| `mode` | Server mode (stdio, http, bridge) |
| `method` | MCP method (e.g., `tools/call`) |
| `tool_name` | Tool name |
| `params` | Request parameters (JSON) |
| `response` | Response (JSON) |
| `error` | Error message if any |
| `duration_ms` | Execution time |
| `created_at` | Timestamp |

Query logs:
```bash
sqlite3 audit.db "SELECT mode, method, tool_name, duration_ms FROM audit_logs ORDER BY id DESC LIMIT 10"
```

## Bridge Mode Features

### File Externalization

Large content (>500KB) in MCP responses is automatically externalized to temporary files:

```json
{
  "type": "external_file",
  "url": "http://localhost:8080/files/abc123...",
  "mimeType": "image/png",
  "size": 1843200
}
```

Files are deleted after one retrieval (one-time access).

**Tip for LLMs**: Include this in your prompt when using bridge mode:

```
When MCP returns {"type":"external_file","url":"...","mimeType":"...","size":...}:
- The content was too large to include directly
- Access the URL via HTTP to retrieve the file (one-time access)
- The file is deleted after retrieval
```

## Sandbox Modes

| Mode | Isolation | Use Case |
|------|-----------|----------|
| `none` | Path validation only | Trusted commands |
| `bubblewrap` | Linux namespace isolation | Native binaries (recommended) |
| `wasm` | WebAssembly sandbox | Complete isolation |

### Bubblewrap Installation

```bash
sudo apt install bubblewrap    # Debian/Ubuntu
sudo dnf install bubblewrap    # Fedora
sudo pacman -S bubblewrap      # Arch
```

### Bubblewrap Mount Directories

When using bubblewrap sandbox, mount point directories are created in `--root-dir`:

```
root-dir/
├── bin/      # Read-only mount of /bin
├── dev/      # Minimal device files
├── etc/      # Read-only mount of /etc
├── lib/      # Read-only mount of /lib
├── lib64/    # Read-only mount of /lib64 (if exists)
├── sbin/     # Read-only mount of /sbin (if exists)
├── tmp/      # Temporary filesystem
└── usr/      # Read-only mount of /usr
```

**Note**: These directories are created automatically on startup if they don't exist. On shutdown, mcp-gatekeeper automatically removes any empty directories it created (pre-existing directories are not removed).

### WASM Setup

Use WASI-compatible binaries. File access is restricted to `--root-dir`.

```bash
# Ruby WASM
tar xzf ruby-*-wasm32-unknown-wasip1-full.tar.gz

# Go (compile your own)
GOOS=wasip1 GOARCH=wasm go build -o tool.wasm main.go
```

## Glob Patterns

| Pattern | Description |
|---------|-------------|
| `*` | Any string except `/` |
| `**` | Any string including `/` |
| `?` | Single character |
| `[abc]` | Character class |
| `{a,b}` | Alternation |

Examples:
- `status **` - matches `status`, `status .`, `status --short`
- `*.txt` - matches any `.txt` file
- `--format=*` - matches any `--format=` option

## MCP Apps UI Support

Tools can return interactive UI components instead of plain text. MCP clients like Claude Desktop can display these as rich HTML interfaces.

### Built-in UI Types

| Type | Description | Best For |
|------|-------------|----------|
| `table` | Sortable table | JSON arrays, CSV, command output |
| `json` | Syntax-highlighted JSON | API responses, config files |
| `log` | Filterable log viewer | Log files, command output |

### Plugin Configuration

```json
{
  "name": "git-status",
  "description": "Show git status",
  "command": "git",
  "args_prefix": ["status"],
  "allowed_arg_globs": ["", "*"],
  "sandbox": "none",
  "ui_type": "log"
}
```

| Field | Description |
|-------|-------------|
| `ui_type` | `table`, `json`, or `log` |
| `output_format` | `json`, `csv`, or `lines` (for table parsing) |
| `ui_template` | Path to custom HTML template (overrides ui_type) |

### Custom Templates

Create fully custom UIs with Go templates:

```json
{
  "name": "git-log",
  "command": "git",
  "ui_template": "templates/log.html"
}
```

Template variables:
- `{{.Output}}` - Raw output string
- `{{.Lines}}` - Output split by lines
- `{{.JSON}}` - Parsed JSON (if valid)
- `{{.JSONPretty}}` - Pretty-printed JSON
- `{{.IsJSON}}` - Whether output is valid JSON

Template functions:
- `{{escape .Output}}` - HTML escape
- `{{json .Data}}` - JSON encode
- `{{jsonPretty .Data}}` - Pretty JSON encode
- `{{split .String " "}}` - Split string by delimiter
- `{{join .Array " "}}` - Join array with delimiter
- `{{slice .Array 1}}` - Slice array from index
- `{{first .Array}}` - Get first element
- `{{contains .String "text"}}` - Check if string contains
- `{{hasPrefix .String "prefix"}}` - Check string prefix
- `{{trimSpace .String}}` - Trim whitespace

Example template:
```html
<!DOCTYPE html>
<html>
<head><title>Git Log</title></head>
<body>
  <h1>Commits ({{len .Lines}})</h1>
  {{range .Lines}}
  {{if trimSpace .}}
  {{$parts := split . " "}}
  <div class="commit">
    <span class="hash">{{first $parts}}</span>
    <span class="message">{{escape (join (slice $parts 1) " ")}}</span>
  </div>
  {{end}}
  {{end}}
</body>
</html>
```

### How It Works

1. `tools/list` returns tools with `_meta.ui.resourceUri` for UI-enabled tools
2. `tools/call` returns results with `_meta.ui.resourceUri` containing output data
3. Client calls `resources/read` with the URI to get rendered HTML

## Example Plugins

See the `examples/plugins/` directory:

```
examples/plugins/
├── git/
│   ├── plugin.json      # Git commands (status, log, diff, branch, etc.)
│   └── templates/
│       ├── log.html     # Custom UI for git log
│       └── diff.html    # Custom UI for git diff
└── shell/
    ├── plugin.json      # Shell commands (ls, cat, find, grep)
    └── templates/
        └── table.html   # Custom table UI
```

To use example plugins:
```bash
./mcp-gatekeeper --plugins-dir=examples/plugins --root-dir=. --addr=:8080
```

## License

MIT License
