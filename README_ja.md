# MCP Gatekeeper

MCP (Model Context Protocol) サーバー。AIアシスタントに**シェルコマンド実行**と**MCPサーバーのHTTPプロキシ**機能を提供します。

## 3つの動作モード

| モード | 用途 |
|--------|------|
| **stdio** | MCPクライアント（Claude Desktop等）との直接連携でシェルコマンドを実行 |
| **http** | HTTP API としてシェルコマンド実行機能を公開 |
| **bridge** | 既存の stdio MCP サーバーを HTTP でプロキシ |

## アーキテクチャ

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              MCP Gatekeeper                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         API Key: "dev-team"                          │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  許可された環境変数: ["PATH", "HOME", "LANG", "GO*"]                 │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "git"                                              │   │   │
│  │  │  ├─ コマンド: /usr/bin/git                                  │   │   │
│  │  │  ├─ 許可された引数: ["status **", "log **", "diff **"]      │   │   │
│  │  │  └─ サンドボックス: bubblewrap                              │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "ruby"                                             │   │   │
│  │  │  ├─ コマンド: ruby                                          │   │   │
│  │  │  ├─ 許可された引数: ["-e **", "*.rb"]                       │   │   │
│  │  │  ├─ サンドボックス: wasm                                    │   │   │
│  │  │  └─ WASMバイナリ: /opt/ruby-wasm/usr/local/bin/ruby         │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         API Key: "readonly"                         │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "cat"                                              │   │   │
│  │  │  ├─ コマンド: /usr/bin/cat                                  │   │   │
│  │  │  ├─ 許可された引数: ["*.md", "*.txt"]                       │   │   │
│  │  │  └─ サンドボックス: bubblewrap                              │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## インストール

```bash
go install github.com/takeshy/mcp-gatekeeper/cmd/server@latest
go install github.com/takeshy/mcp-gatekeeper/cmd/admin@latest
```

または [Releases](https://github.com/takeshy/mcp-gatekeeper/releases) からダウンロード。

## クイックスタート

### 1. APIキーとツールの作成

```bash
./mcp-gatekeeper-admin --db gatekeeper.db
```

TUIで:
1. "API Keys" → `n` で新規作成 → **APIキーを控える**（再表示不可）
2. APIキー選択 → `t` でツール管理 → `n` で新規作成

ツール設定例:
- Name: `git`
- Command: `/usr/bin/git`
- Allowed Args: `status **`, `log **`, `diff **`（1行1パターン）
- Sandbox: `bubblewrap`

### 2. サーバー起動

**HTTP モード:**
```bash
# リクエストごとの認証（クライアントが Authorization ヘッダーを送信）
./mcp-gatekeeper --mode=http --root-dir=/home/user/projects --db=gatekeeper.db

# 固定APIキー（クライアントからの認証ヘッダー不要）
./mcp-gatekeeper --mode=http --root-dir=/home/user/projects --db=gatekeeper.db --api-key=your-key

# 環境変数を使用
MCP_GATEKEEPER_API_KEY=your-key ./mcp-gatekeeper --mode=http --root-dir=/home/user/projects --db=gatekeeper.db
```

**Stdio モード:**
```bash
MCP_GATEKEEPER_API_KEY=your-key ./mcp-gatekeeper --mode=stdio --root-dir=/home/user/projects --db=gatekeeper.db
```

**Bridge モード（stdio MCPサーバーをHTTPでプロキシ）:**
```bash
./mcp-gatekeeper --mode=bridge --upstream='npx @anthropic-ai/mcp-server' --addr=:8080 --api-key=your-secret

# Playwright MCP の例（ヘッドレスブラウザ自動化）
./mcp-gatekeeper --mode=bridge --addr=:8090 --upstream='npx @playwright/mcp --executable-path /path/to/chrome --headless --no-sandbox --isolated'

# デバッグログ付き
./mcp-gatekeeper --debug --mode=bridge --addr=:8090 --upstream='npx @playwright/mcp --headless'

# 監査ログ付き（オプション）
./mcp-gatekeeper --mode=bridge --upstream='npx @playwright/mcp --headless' --db=gatekeeper.db

# APIキー認証付き
./mcp-gatekeeper --mode=bridge --upstream='npx @playwright/mcp --headless' --api-key=your-secret-key
```

### 3. 実行テスト

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"git","arguments":{"args":["status"]}}}'
```

## CLIオプション

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--mode` | `stdio` | `stdio`, `http`, `bridge` |
| `--root-dir` | (必須) | サンドボックスルート（stdio/http で必須） |
| `--db` | `gatekeeper.db` | SQLite データベースパス（bridge ではオプション） |
| `--addr` | `:8080` | HTTP アドレス（http/bridge） |
| `--api-key` | - | 固定APIキー（全モード対応、または `MCP_GATEKEEPER_API_KEY` 環境変数） |
| `--rate-limit` | `500` | レート制限/分（http/bridge） |
| `--upstream` | - | 上流コマンド（bridge で必須） |
| `--upstream-env` | - | 上流への環境変数（カンマ区切り） |
| `--max-response-size` | `500000` | 最大レスポンスサイズ（バイト、bridge のみ） |
| `--debug` | `false` | デバッグログ有効化（bridge のみ） |
| `--wasm-dir` | - | WASMバイナリ格納ディレクトリ |

## Bridge モードの機能

### ファイル外部化

MCPレスポンス内の大きなコンテンツ（500KB超）は自動的に一時ファイルに外部化されます。クライアントはHTTP経由でファイルを取得するURLを受け取ります。

**レスポンス形式:**
```json
{
  "type": "external_file",
  "url": "http://localhost:8090/files/abc123...",
  "mimeType": "image/png",
  "size": 1843200
}
```

**ファイル取得:**
```bash
curl http://localhost:8090/files/abc123...
```

ファイルは1回取得後に削除されます（ワンタイムアクセス）。`--api-key` が設定されている場合、`/files/{key}` エンドポイントにも認証が必要です。

### 監査ログ

`--db` を指定すると、すべてのMCPリクエストとレスポンスが `bridge_audit_logs` テーブルに記録されます。

```bash
# 監査ログを有効化
./mcp-gatekeeper --mode=bridge --upstream='...' --db=gatekeeper.db
```

**記録されるフィールド:**
- `method` - MCPメソッド（例: `tools/call`, `initialize`）
- `params` - リクエストパラメータ（JSON）
- `response` - 外部化前の元のレスポンス（JSON）
- `error` - エラーメッセージ（あれば）
- `request_size` / `response_size` - サイズ（バイト）
- `duration_ms` - 処理時間（ミリ秒）
- `created_at` - タイムスタンプ

**ログの確認:**
```bash
sqlite3 gatekeeper.db "SELECT id, method, response_size, duration_ms FROM bridge_audit_logs ORDER BY id DESC LIMIT 10"
```

## サンドボックス

| モード | 分離レベル | 用途 |
|--------|-----------|------|
| `none` | パス検証のみ | 信頼できるコマンド |
| `bubblewrap` | 名前空間分離 | ネイティブバイナリ（推奨） |
| `wasm` | 完全分離 | WASI対応バイナリ |

### Bubblewrap

```bash
# インストール
sudo apt install bubblewrap  # Debian/Ubuntu
sudo dnf install bubblewrap  # Fedora
sudo pacman -S bubblewrap    # Arch
```

### WASM

WASI対応バイナリを使用。ファイルアクセスは `--root-dir` 内に制限。

```bash
# Ruby
tar xzf ruby-*-wasm32-unknown-wasip1-full.tar.gz

# Python
curl -LO https://github.com/vmware-labs/webassembly-language-runtimes/releases/.../python-3.12.0.wasm

# Go
GOOS=wasip1 GOARCH=wasm go build -o tool.wasm main.go
```

## Globパターン

| パターン | 説明 |
|---------|------|
| `*` | `/` 以外の任意文字列 |
| `**` | `/` を含む任意文字列 |
| `?` | 任意の1文字 |
| `[abc]` | 文字クラス |
| `{a,b}` | 選択 |

例: `status **`, `log --oneline **`, `diff **`

## ライセンス

MIT License
