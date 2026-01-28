# MCP Gatekeeper

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-2025--11-green)](https://modelcontextprotocol.io/)

A security-focused MCP (Model Context Protocol) gateway that enables AI assistants to safely execute shell commands with fine-grained access control.

**Why MCP Gatekeeper?**

- **Security First**: Multi-layer protection with policy-based argument validation, environment variable filtering, and sandboxing (bubblewrap/WASM)
- **Flexible Deployment**: Run as stdio server for Claude Desktop, HTTP API for web services, or bridge proxy for existing MCP servers
- **Bridge Mode**: Expose any stdio-based MCP server (Playwright, filesystem, etc.) over HTTP with authentication, rate limiting, and large response handling
- **OAuth 2.0 Ready**: Machine-to-machine authentication with client credentials flow ([MCP SEP-1046](https://github.com/modelcontextprotocol/ext-auth))
- **Plugin Architecture**: Define tools via simple JSON files with glob-based argument patterns
- **Rich UI Support**: Generate interactive HTML interfaces via MCP Apps for command outputs

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
│  │  Authentication & Rate Limiting                                     │   │
│  │  ├─ API Key: Simple Bearer token authentication                     │   │
│  │  └─ OAuth 2.0: Client credentials flow (M2M)                        │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Plugin Configuration                                               │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  Allowed Env Variables: ["PATH", "HOME", "LANG", "GIT_*"]           │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  Tool: "git-status"                                         │   │   │
│  │  │  ├─ Command: git                                            │   │   │
│  │  │  ├─ Args Prefix: ["status"]                                 │   │   │
│  │  │  ├─ Allowed Args: ["", "--short"]                           │   │   │
│  │  │  ├─ Sandbox: none                                           │   │   │
│  │  │  └─ UI Type: log                                            │   │   │
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
- **OAuth 2.0 Authentication**: Client credentials flow for M2M communication
- **TUI Admin Tool**: Manage OAuth clients via terminal UI
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
      "name": "git-status",
      "description": "Show git repository status",
      "command": "git",
      "args_prefix": ["status"],
      "allowed_arg_globs": ["", "--short", "--branch"],
      "sandbox": "none",
      "ui_type": "log"
    },
    {
      "name": "ls",
      "description": "List directory contents",
      "command": "ls",
      "args_prefix": ["-la"],
      "allowed_arg_globs": ["", "**"],
      "sandbox": "bubblewrap",
      "ui_type": "log"
    }
  ],
  "allowed_env_keys": ["PATH", "HOME", "LANG"]
}
```

**Note**: `args_prefix` defines fixed arguments that are automatically prepended. With `args_prefix: ["-la"]`, calling `ls` with `args: ["/tmp"]` executes `ls -la /tmp`. The `allowed_arg_globs` validates user-provided args only (not the prefix).

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
| `args_prefix` | No | Fixed arguments prepended to user args (e.g., `["-la"]` for ls) |
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
| `--db` | - | SQLite database path for audit logging and OAuth (optional) |
| `--enable-oauth` | `false` | Enable OAuth 2.0 authentication (requires `--db`) |
| `--oauth-issuer` | - | OAuth issuer URL (optional, auto-detected if empty) |
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

## OAuth 2.0 Authentication

MCP Gatekeeper supports OAuth 2.0 client credentials flow for machine-to-machine (M2M) authentication. This is useful when you need more secure authentication than simple API keys.

### Enable OAuth

```bash
./mcp-gatekeeper --mode=http \
  --db=gatekeeper.db \
  --enable-oauth \
  --addr=:8080 \
  --plugins-dir=plugins/ \
  --root-dir=/path/to/root
```

**Note**: OAuth requires `--db` to store client credentials and tokens.

### Create OAuth Clients

Use the TUI admin tool to create OAuth clients:

```bash
./mcp-gatekeeper-admin --db=gatekeeper.db
```

Navigate to "OAuth Clients" → "New Client" → Enter client ID → Save the generated client secret.

### OAuth Flow (Client Credentials)

```bash
# 1. Get access token
curl -X POST http://localhost:8080/oauth/token \
  -d "grant_type=client_credentials&client_id=myclient&client_secret=SECRET"

# Response:
# {
#   "access_token": "...",
#   "token_type": "Bearer",
#   "expires_in": 3600,
#   "refresh_token": "..."
# }

# 2. Call MCP endpoint
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 3. Refresh token (when access token expires)
curl -X POST http://localhost:8080/oauth/token \
  -d "grant_type=refresh_token&refresh_token=REFRESH_TOKEN&client_id=myclient&client_secret=SECRET"
```

You can also use HTTP Basic auth for client credentials:

```bash
curl -X POST http://localhost:8080/oauth/token \
  -H "Authorization: Basic BASE64(client_id:client_secret)" \
  -d "grant_type=client_credentials"
```

### OAuth Endpoints

| Endpoint | Description |
|----------|-------------|
| `POST /oauth/token` | Token endpoint (client_credentials, refresh_token) |
| `GET /.well-known/oauth-authorization-server` | OAuth server metadata |
| `GET /.well-known/openid-configuration` | OpenID Connect discovery |
| `GET /.well-known/oauth-protected-resource` | Protected resource metadata (RFC 9728) |
| `GET /.well-known/oauth-protected-resource/{resourcePath}` | Protected resource metadata for a specific path |

### Token Expiration

| Token | Lifetime |
|-------|----------|
| Access Token | 1 hour |
| Refresh Token | Unlimited (until client revoked) |

### Dual Authentication

When both `--api-key` and `--enable-oauth` are set, either authentication method is accepted:
- Bearer token matching API key
- Bearer token from OAuth access token

## TUI Admin Tool

The `mcp-gatekeeper-admin` tool provides a terminal UI for managing OAuth clients.

### Installation

```bash
# Build from source
make build-admin

# Or install directly
go install github.com/takeshy/mcp-gatekeeper/cmd/admin@latest
```

### Usage

```bash
./mcp-gatekeeper-admin --db=gatekeeper.db
```

### Features

- **OAuth Clients**: List, create, revoke, and delete OAuth clients
- **Audit Logs**: View audit log statistics

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Navigate |
| `Enter` | Select |
| `r` | Revoke client |
| `d` | Delete client |
| `Esc` | Go back |
| `q` | Quit |

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
| `""` | **Empty string - allows calling with no arguments** |
| `*` | Any string except `/` |
| `**` | Any string including `/` |
| `?` | Single character |
| `[abc]` | Character class |
| `{a,b}` | Alternation |

Examples:
- `[""]` - allows only no arguments (e.g., `git status` with no args)
- `["", "--short"]` - allows no arguments OR `--short`
- `["**"]` - allows any arguments (equivalent to omitting `allowed_arg_globs`)
- `*.txt` - matches any `.txt` file
- `--format=*` - matches any `--format=` option

> **Important**: If you want to allow calling a tool with no arguments, you must include `""` in `allowed_arg_globs`. Without it, the tool requires at least one argument.

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
| `ui_config` | Advanced UI configuration (see below) |

### UI Configuration

The `ui_config` field provides fine-grained control over UI behavior:

```json
{
  "name": "file-explorer",
  "description": "Interactive file explorer",
  "command": "ls",
  "args_prefix": ["-la"],
  "allowed_arg_globs": ["", "**"],
  "sandbox": "none",
  "ui_template": "templates/explorer.html",
  "ui_config": {
    "csp": {
      "resource_domains": ["esm.sh"]
    },
    "visibility": ["model", "app"]
  }
}
```

| Field | Description |
|-------|-------------|
| `csp.resource_domains` | Allowed external domains for CSP (e.g., CDN for MCP App SDK) |
| `visibility` | Tool visibility: `["model", "app"]` (default) or `["app"]` (app-only) |

### App-Only Tools

Tools with `visibility: ["app"]` are hidden from the model but can be called from UI via MCP Apps SDK. This is useful for helper tools that the main UI calls dynamically:

```json
{
  "name": "git-staged-diff",
  "description": "Get staged diff for a file (app-only)",
  "command": "git",
  "args_prefix": ["diff", "--cached", "--"],
  "allowed_arg_globs": ["**"],
  "sandbox": "none",
  "ui_config": {
    "visibility": ["app"]
  }
}
```

**Note**: Use `**` (not `*`) in `allowed_arg_globs` when paths containing `/` need to be matched.

### Custom Templates

Create fully custom UIs with Go templates:

```json
{
  "name": "process-list",
  "command": "ps",
  "args_prefix": ["aux"],
  "ui_template": "templates/process.html"
}
```

Template variables:
- `{{.Output}}` - Raw output string
- `{{.Lines}}` - Output split by lines (array)
- `{{.JSON}}` - Parsed JSON (if valid)
- `{{.JSONPretty}}` - Pretty-printed JSON
- `{{.IsJSON}}` - Whether output is valid JSON

Template functions:
- `{{escape .Output}}` - HTML escape
- `{{json .Data}}` - JSON encode (returns `template.JS` for safe embedding)
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
<head><title>Process List</title></head>
<body>
  <h1>Processes ({{len .Lines}})</h1>
  <table>
  {{range .Lines}}
  {{if trimSpace .}}
  <tr><td>{{escape .}}</td></tr>
  {{end}}
  {{end}}
  </table>
</body>
</html>
```

### Interactive Templates with MCP Apps SDK

Templates can use the MCP Apps SDK for bidirectional communication, allowing the UI to call other tools dynamically:

```html
<script type="module">
// MCP Apps compatibility layer
// Supports both window.mcpApps (obsidian-gemini-helper) and @anthropic-ai/mcp-app-sdk
let mcpClient = null;

async function initMcpClient() {
  // Check for injected bridge first (obsidian-gemini-helper)
  if (window.mcpApps && typeof window.mcpApps.callTool === 'function') {
    return {
      callServerTool: (name, args) => window.mcpApps.callTool(name, args),
      type: 'bridge'
    };
  }

  // Fall back to MCP App SDK
  try {
    const { App } = await import('https://esm.sh/@anthropic-ai/mcp-app-sdk@0.1');
    const app = new App({ name: 'My App', version: '1.0.0' });
    await app.connect();
    return {
      callServerTool: (name, args) => app.callServerTool(name, args),
      type: 'sdk'
    };
  } catch (e) {
    console.log('MCP App SDK not available:', e.message);
    return null;
  }
}

// Initialize and use
mcpClient = await initMcpClient();
if (mcpClient) {
  // Call an app-only tool
  const result = await mcpClient.callServerTool('git-staged-diff', { args: ['file.txt'] });
  console.log(result.content[0].text);
}

// Initialize data from template
const initialData = {{json .Lines}};  // Safe JS embedding
</script>
```

**Important**: When using `{{json .Lines}}` or similar template functions in JavaScript, the output is automatically safe for embedding (returns `template.JS` type to prevent double-escaping).

### How It Works

1. `tools/list` returns tools with `_meta.ui.resourceUri` for UI-enabled tools
2. `tools/call` returns results with `_meta.ui.resourceUri` containing output data
3. Client calls `resources/read` with the URI to get rendered HTML

## Example Plugins

See the `examples/plugins/` directory:

```
examples/plugins/
├── git/
│   ├── plugin.json      # Git commands with interactive UI
│   └── templates/
│       ├── changes.html # Interactive staged/unstaged changes viewer
│       ├── commits.html # Interactive commit explorer
│       ├── log.html     # Custom UI for git log
│       └── diff.html    # Custom UI for git diff
├── interactive/
│   ├── plugin.json      # File explorer with bidirectional MCP Apps
│   └── templates/
│       └── explorer.html
└── shell/
    ├── plugin.json      # Shell commands (ls, cat, find, grep)
    └── templates/
        └── table.html   # Custom table UI
```

### Interactive Git Plugin

The git plugin demonstrates bidirectional MCP Apps communication:

- **git-changes**: Shows staged/unstaged files in an accordion UI. Click a file to view its diff (loaded dynamically via app-only tools).
- **git-commits**: Browse commit history. Click a commit to see changed files, click a file to see its diff.

App-only helper tools (`visibility: ["app"]`):
- `git-staged-files`, `git-unstaged-files`: List files for the UI
- `git-staged-diff`, `git-unstaged-diff`: Get diffs for selected files
- `git-commit-files`, `git-file-diff`: Get commit details

To try the interactive examples:
```bash
cd /path/to/your/git/repo
./mcp-gatekeeper --plugins-dir=examples/plugins --root-dir=.
```

## License

MIT License
