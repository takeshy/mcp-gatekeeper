# MCP Gatekeeper

AIアシスタント向けに、安全なシェルコマンド実行とHTTPプロキシ機能を提供するMCP（Model Context Protocol）サーバーです。

## アーキテクチャ

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              MCP Gatekeeper                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  プロトコル層                                                       │   │
│  │  ├─ Stdioモード: MCPクライアントとの直接連携                        │   │
│  │  ├─ HTTPモード: Bearerトークン認証付きJSON-RPC 2.0                  │   │
│  │  └─ Bridgeモード: stdio MCPサーバーへのHTTPプロキシ                 │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  認証 & レート制限                                                  │   │
│  │  ├─ APIキー: シンプルなBearerトークン認証                           │   │
│  │  └─ OAuth 2.0: クライアントクレデンシャルフロー（M2M）              │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  プラグイン設定                                                     │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  許可する環境変数: ["PATH", "HOME", "LANG", "GIT_*"]                │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "git-status"                                       │   │   │
│  │  │  ├─ コマンド: git                                           │   │   │
│  │  │  ├─ 引数プレフィックス: ["status"]                          │   │   │
│  │  │  ├─ 許可する引数: ["", "--short"]                           │   │   │
│  │  │  ├─ サンドボックス: none                                    │   │   │
│  │  │  └─ UIタイプ: log                                           │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "ls"                                               │   │   │
│  │  │  ├─ コマンド: ls                                            │   │   │
│  │  │  ├─ 許可する引数: ["-la", "*"]                              │   │   │
│  │  │  ├─ サンドボックス: bubblewrap                              │   │   │
│  │  │  └─ UIタイプ: log                                           │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "ruby"                                             │   │   │
│  │  │  ├─ 許可する引数: ["-e **", "*.rb"]                         │   │   │
│  │  │  ├─ サンドボックス: wasm                                    │   │   │
│  │  │  └─ WASMバイナリ: /opt/ruby-wasm/usr/local/bin/ruby         │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  ポリシー評価（Globパターンによる引数マッチング）                   │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  サンドボックス付きコマンド実行                                     │   │
│  │  ├─ None: パス検証のみ                                              │   │
│  │  ├─ Bubblewrap: Linux名前空間分離（bwrap）                          │   │
│  │  └─ WASM: WebAssemblyサンドボックス（wazeroランタイム）             │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  監査ログ（SQLite）& MCP Apps UIレンダリング                        │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 特徴

- **3つの動作モード**: stdio, http, bridge
- **JSONプラグイン設定**: シンプルなJSONファイルでツールを定義
- **柔軟なサンドボックス**: none, bubblewrap, WASM分離
- **ポリシーベースのアクセス制御**: Globパターンによる引数検証
- **OAuth 2.0認証**: M2M通信向けクライアントクレデンシャルフロー
- **TUI管理ツール**: ターミナルUIでOAuthクライアントを管理
- **オプションの監査ログ**: 全モード対応のSQLiteログ
- **大容量レスポンス対応**: bridgeモードでの自動ファイル外部化
- **MCP Apps UI対応**: ツール出力をリッチHTMLで表示

## 動作モード

| モード | 用途 |
|--------|------|
| **stdio** | MCPクライアント（Claude Desktop等）との直接連携 |
| **http** | シェルコマンドをHTTP APIとして公開 |
| **bridge** | 既存のstdio MCPサーバーをHTTPでプロキシ |

モードは自動検出されます: `--addr`指定でhttpモード、`--upstream`指定でbridgeモード。

## インストール

```bash
go install github.com/takeshy/mcp-gatekeeper/cmd/server@latest
```

または [Releases](https://github.com/takeshy/mcp-gatekeeper/releases) からダウンロード。

## クイックスタート

### 1. プラグインディレクトリの作成

`plugin.json`とオプションのテンプレートを含むディレクトリを作成します：

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
      "description": "Gitリポジトリのステータスを表示",
      "command": "git",
      "args_prefix": ["status"],
      "allowed_arg_globs": ["", "--short", "--branch"],
      "sandbox": "none",
      "ui_type": "log"
    },
    {
      "name": "ls",
      "description": "ディレクトリ内容を表示",
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

**注意**: `args_prefix` は自動的に先頭に付加される固定引数です。`args_prefix: ["-la"]` の場合、`ls` を `args: ["/tmp"]` で呼び出すと `ls -la /tmp` が実行されます。`allowed_arg_globs` はユーザー指定の引数のみを検証します（プレフィックスは対象外）。

### 2. サーバー起動

**Stdioモード**（Claude Desktop等のMCPクライアント向け）：
```bash
./mcp-gatekeeper --mode=stdio \
  --root-dir=/home/user/projects \
  --plugin-file=my-plugin/plugin.json \
  --api-key=your-secret-key
```

**HTTPモード**：
```bash
./mcp-gatekeeper --mode=http \
  --root-dir=/home/user/projects \
  --plugins-dir=plugins/ \
  --api-key=your-secret-key \
  --addr=:8080
```

**Bridgeモード**（既存MCPサーバーのプロキシ）：
```bash
./mcp-gatekeeper --mode=bridge \
  --upstream='npx @playwright/mcp --headless' \
  --api-key=your-secret-key \
  --addr=:8080
```

### 3. テスト

```bash
# 利用可能なツール一覧
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# ツール実行
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ls","arguments":{"args":["-la"]}}}'
```

## プラグイン設定

### 単一プラグインファイル

```bash
./mcp-gatekeeper --plugin-file=my-plugin/plugin.json ...
```

### プラグインディレクトリ（複数プラグイン）

```bash
./mcp-gatekeeper --plugins-dir=plugins/ ...
```

2つの形式をサポート：
- **フラットファイル**: `plugins/*.json` ファイルを直接読み込み
- **サブディレクトリ**: `plugins/*/plugin.json` ディレクトリを読み込み

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
└── simple.json          # フラットファイルもサポート
```

ツール名は全プラグインで一意である必要があります。

### プラグインファイル形式

```json
{
  "tools": [
    {
      "name": "ツール名",
      "description": "ツールの説明",
      "command": "/path/to/executable",
      "args_prefix": ["サブコマンド"],
      "allowed_arg_globs": ["パターン1", "パターン2"],
      "sandbox": "none|bubblewrap|wasm",
      "wasm_binary": "/path/to/binary.wasm",
      "ui_type": "log|table|json",
      "ui_template": "templates/custom.html"
    }
  ],
  "allowed_env_keys": ["PATH", "HOME", "CUSTOM_*"]
}
```

| フィールド | 必須 | 説明 |
|-----------|------|------|
| `name` | Yes | 一意のツール名 |
| `description` | No | ツールの説明 |
| `command` | Yes* | 実行ファイルのパス（*wasmでは不要） |
| `args_prefix` | No | ユーザー引数の前に付加される固定引数（例: `["-la"]`） |
| `allowed_arg_globs` | No | 許可するユーザー引数のGlobパターン（args_prefix適用前に評価） |
| `sandbox` | No | `none`, `bubblewrap`, `wasm`（デフォルト: `none`） |
| `wasm_binary` | Yes* | WASMバイナリのパス（*sandbox=wasmの場合必須） |
| `ui_type` | No | 組み込みUI: `table`, `json`, `log` |
| `ui_template` | No | カスタムHTMLテンプレートのパス（plugin.jsonからの相対パス） |

**注意**: テンプレートパスはplugin.jsonファイルの場所からの相対パスです。セキュリティのため親ディレクトリ参照（`..`）は許可されません。

## CLIオプション

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--mode` | `stdio` | `stdio`, `http`, `bridge` |
| `--root-dir` | - | サンドボックスルートディレクトリ（stdio/httpで必須） |
| `--plugin-file` | - | 単一のプラグインJSONファイル |
| `--plugins-dir` | - | プラグインディレクトリ/ファイルを含むディレクトリ |
| `--api-key` | - | 認証用APIキー（または `MCP_GATEKEEPER_API_KEY` 環境変数） |
| `--db` | - | 監査ログ・OAuth用SQLiteデータベースパス（オプション） |
| `--enable-oauth` | `false` | OAuth 2.0認証を有効化（`--db`必須） |
| `--oauth-issuer` | - | OAuth発行者URL（省略時は自動検出） |
| `--addr` | `:8080` | HTTPリッスンアドレス（http/bridge） |
| `--rate-limit` | `500` | 1分あたりの最大リクエスト数（http/bridge） |
| `--upstream` | - | 上流MCPサーバーコマンド（bridgeで必須） |
| `--upstream-env` | - | 上流への環境変数（カンマ区切り） |
| `--max-response-size` | `500000` | 最大レスポンスサイズ（バイト、bridge） |
| `--debug` | `false` | デバッグログ有効化（bridge） |
| `--wasm-dir` | - | WASMバイナリ格納ディレクトリ |

## 監査ログ

`--db` を指定して監査ログを有効化：

```bash
./mcp-gatekeeper --mode=http --db=audit.db ...
```

すべての `tools/call` リクエストが `audit_logs` テーブルに記録されます：

| フィールド | 説明 |
|-----------|------|
| `mode` | サーバーモード（stdio, http, bridge） |
| `method` | MCPメソッド（例: `tools/call`） |
| `tool_name` | ツール名 |
| `params` | リクエストパラメータ（JSON） |
| `response` | レスポンス（JSON） |
| `error` | エラーメッセージ（あれば） |
| `duration_ms` | 実行時間 |
| `created_at` | タイムスタンプ |

ログの確認：
```bash
sqlite3 audit.db "SELECT mode, method, tool_name, duration_ms FROM audit_logs ORDER BY id DESC LIMIT 10"
```

## OAuth 2.0認証

MCP GatekeeperはM2M（マシン間）認証向けのOAuth 2.0クライアントクレデンシャルフローをサポートしています。シンプルなAPIキーよりも安全な認証が必要な場合に便利です。

### OAuthの有効化

```bash
./mcp-gatekeeper --mode=http \
  --db=gatekeeper.db \
  --enable-oauth \
  --addr=:8080 \
  --plugins-dir=plugins/ \
  --root-dir=/path/to/root
```

**注意**: OAuthはクライアントクレデンシャルとトークンを保存するため`--db`が必須です。

### OAuthクライアントの作成

TUI管理ツールを使用してOAuthクライアントを作成します：

```bash
./mcp-gatekeeper-admin --db=gatekeeper.db
```

「OAuth Clients」→「New Client」→クライアントIDを入力→生成されたクライアントシークレットを保存。

### OAuthフロー（クライアントクレデンシャル）

```bash
# 1. アクセストークン取得
curl -X POST http://localhost:8080/oauth/token \
  -d "grant_type=client_credentials&client_id=myclient&client_secret=SECRET"

# レスポンス:
# {
#   "access_token": "...",
#   "token_type": "Bearer",
#   "expires_in": 3600,
#   "refresh_token": "..."
# }

# 2. MCPエンドポイント呼び出し
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 3. トークンリフレッシュ（アクセストークン期限切れ時）
curl -X POST http://localhost:8080/oauth/token \
  -d "grant_type=refresh_token&refresh_token=REFRESH_TOKEN&client_id=myclient&client_secret=SECRET"
```

HTTP Basic 認証でクライアント認証することもできます:

```bash
curl -X POST http://localhost:8080/oauth/token \
  -H "Authorization: Basic BASE64(client_id:client_secret)" \
  -d "grant_type=client_credentials"
```

### OAuthエンドポイント

| エンドポイント | 説明 |
|---------------|------|
| `POST /oauth/token` | トークンエンドポイント（client_credentials, refresh_token） |
| `GET /.well-known/oauth-authorization-server` | OAuthサーバーメタデータ |
| `GET /.well-known/openid-configuration` | OpenID Connectディスカバリ |
| `GET /.well-known/oauth-protected-resource` | 保護リソースメタデータ (RFC 9728) |
| `GET /.well-known/oauth-protected-resource/{resourcePath}` | パス指定の保護リソースメタデータ |

### トークン有効期限

| トークン | 有効期限 |
|---------|---------|
| アクセストークン | 1時間 |
| リフレッシュトークン | 無期限（クライアント無効化まで） |

### 二重認証

`--api-key`と`--enable-oauth`の両方を設定した場合、どちらの認証方式でも受け付けます：
- APIキーに一致するBearerトークン
- OAuthアクセストークンのBearerトークン

## TUI管理ツール

`mcp-gatekeeper-admin`ツールはOAuthクライアントを管理するためのターミナルUIを提供します。

### インストール

```bash
# ソースからビルド
make build-admin

# または直接インストール
go install github.com/takeshy/mcp-gatekeeper/cmd/admin@latest
```

### 使い方

```bash
./mcp-gatekeeper-admin --db=gatekeeper.db
```

### 機能

- **OAuthクライアント**: OAuthクライアントの一覧、作成、無効化、削除
- **監査ログ**: 監査ログの統計表示

### キーボードショートカット

| キー | アクション |
|------|-----------|
| `j/k` または `↑/↓` | ナビゲーション |
| `Enter` | 選択 |
| `r` | クライアント無効化 |
| `d` | クライアント削除 |
| `Esc` | 戻る |
| `q` | 終了 |

## Bridgeモードの機能

### ファイル外部化

MCPレスポンス内の大きなコンテンツ（500KB超）は自動的に一時ファイルに外部化されます：

```json
{
  "type": "external_file",
  "url": "http://localhost:8080/files/abc123...",
  "mimeType": "image/png",
  "size": 1843200
}
```

ファイルは1回取得後に削除されます（ワンタイムアクセス）。

**LLM向けTip**: bridgeモード使用時にプロンプトに含めると便利：

```
MCPが {"type":"external_file","url":"...","mimeType":"...","size":...} を返した場合：
- コンテンツが大きすぎて直接含められなかったことを意味します
- HTTP経由でURLにアクセスしてファイルを取得できます（1回限り）
- ファイルは取得後に削除されます
```

## サンドボックスモード

| モード | 分離レベル | 用途 |
|--------|-----------|------|
| `none` | パス検証のみ | 信頼できるコマンド |
| `bubblewrap` | Linux名前空間分離 | ネイティブバイナリ（推奨） |
| `wasm` | WebAssemblyサンドボックス | 完全分離 |

### Bubblewrapインストール

```bash
sudo apt install bubblewrap    # Debian/Ubuntu
sudo dnf install bubblewrap    # Fedora
sudo pacman -S bubblewrap      # Arch
```

### Bubblewrapマウントディレクトリ

bubblewrapサンドボックスを使用する際、`--root-dir`内にマウントポイント用のディレクトリが作成されます：

```
root-dir/
├── bin/      # /binの読み取り専用マウント
├── dev/      # 最小限のデバイスファイル
├── etc/      # /etcの読み取り専用マウント
├── lib/      # /libの読み取り専用マウント
├── lib64/    # /lib64の読み取り専用マウント（存在する場合）
├── sbin/     # /sbinの読み取り専用マウント（存在する場合）
├── tmp/      # 一時ファイルシステム
└── usr/      # /usrの読み取り専用マウント
```

**注意**: これらのディレクトリは起動時に存在しなければ自動作成されます。終了時にmcp-gatekeeperが作成した空のディレクトリは自動的に削除されます（元から存在していたディレクトリは削除されません）。

### WASMセットアップ

WASI対応バイナリを使用。ファイルアクセスは `--root-dir` 内に制限されます。

```bash
# Ruby WASM
tar xzf ruby-*-wasm32-unknown-wasip1-full.tar.gz

# Go（独自にコンパイル）
GOOS=wasip1 GOARCH=wasm go build -o tool.wasm main.go
```

## Globパターン

| パターン | 説明 |
|---------|------|
| `""` | **空文字列 - 引数なしでの呼び出しを許可** |
| `*` | `/` 以外の任意文字列 |
| `**` | `/` を含む任意文字列 |
| `?` | 任意の1文字 |
| `[abc]` | 文字クラス |
| `{a,b}` | 選択 |

例：
- `[""]` - 引数なしのみ許可（例: `git status` を引数なしで実行）
- `["", "--short"]` - 引数なし、または `--short` を許可
- `["**"]` - 全ての引数を許可（`allowed_arg_globs` 省略と同等）
- `*.txt` - 任意の `.txt` ファイルにマッチ
- `--format=*` - 任意の `--format=` オプションにマッチ

> **重要**: 引数なしでツールを呼び出せるようにするには、`allowed_arg_globs` に `""` を含める必要があります。`""` がないと、最低1つの引数が必須になります。

## MCP Apps UI対応

ツールはプレーンテキストの代わりにインタラクティブなUIコンポーネントを返すことができます。Claude DesktopなどのMCPクライアントはリッチなHTMLインターフェースとして表示します。

### 組み込みUIタイプ

| タイプ | 説明 | 用途 |
|--------|------|------|
| `table` | ソート可能なテーブル | JSON配列、CSV、コマンド出力 |
| `json` | シンタックスハイライト付きJSON | APIレスポンス、設定ファイル |
| `log` | フィルタリング可能なログビューア | ログファイル、コマンド出力 |

### プラグイン設定

```json
{
  "name": "git-status",
  "description": "Gitステータスを表示",
  "command": "git",
  "args_prefix": ["status"],
  "allowed_arg_globs": ["", "*"],
  "sandbox": "none",
  "ui_type": "log"
}
```

| フィールド | 説明 |
|-----------|------|
| `ui_type` | `table`, `json`, `log` |
| `output_format` | `json`, `csv`, `lines`（テーブル解析用） |
| `ui_template` | カスタムHTMLテンプレートのパス（ui_typeより優先） |
| `ui_config` | 詳細なUI設定（下記参照） |

### UI設定

`ui_config` フィールドでUIの動作を細かく制御できます：

```json
{
  "name": "file-explorer",
  "description": "インタラクティブなファイルエクスプローラー",
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

| フィールド | 説明 |
|-----------|------|
| `csp.resource_domains` | CSPで許可する外部ドメイン（例: MCP App SDK用CDN） |
| `visibility` | ツールの公開範囲: `["model", "app"]`（デフォルト）または `["app"]`（アプリ専用） |

### アプリ専用ツール

`visibility: ["app"]` のツールはモデルからは見えませんが、UIからMCP Apps SDK経由で呼び出すことができます。メインUIが動的に呼び出すヘルパーツールに便利です：

```json
{
  "name": "git-staged-diff",
  "description": "ファイルのステージ済みdiffを取得（アプリ専用）",
  "command": "git",
  "args_prefix": ["diff", "--cached", "--"],
  "allowed_arg_globs": ["**"],
  "sandbox": "none",
  "ui_config": {
    "visibility": ["app"]
  }
}
```

**注意**: `/` を含むパスをマッチさせる場合は `allowed_arg_globs` で `**`（`*` ではなく）を使用してください。

### カスタムテンプレート

Goテンプレートで完全にカスタムなUIを作成できます：

```json
{
  "name": "process-list",
  "command": "ps",
  "args_prefix": ["aux"],
  "ui_template": "templates/process.html"
}
```

テンプレート変数：
- `{{.Output}}` - 生の出力文字列
- `{{.Lines}}` - 行ごとに分割された出力（配列）
- `{{.JSON}}` - パース済みJSON（有効な場合）
- `{{.JSONPretty}}` - 整形済みJSON
- `{{.IsJSON}}` - 出力が有効なJSONかどうか

テンプレート関数：
- `{{escape .Output}}` - HTMLエスケープ
- `{{json .Data}}` - JSONエンコード（安全な埋め込み用に `template.JS` を返す）
- `{{jsonPretty .Data}}` - 整形済みJSONエンコード
- `{{split .String " "}}` - 文字列を区切り文字で分割
- `{{join .Array " "}}` - 配列を区切り文字で結合
- `{{slice .Array 1}}` - 配列をインデックスからスライス
- `{{first .Array}}` - 最初の要素を取得
- `{{contains .String "text"}}` - 文字列を含むか確認
- `{{hasPrefix .String "prefix"}}` - 文字列のプレフィックス確認
- `{{trimSpace .String}}` - 空白をトリム

テンプレート例：
```html
<!DOCTYPE html>
<html>
<head><title>Process List</title></head>
<body>
  <h1>プロセス（{{len .Lines}}件）</h1>
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

### MCP Apps SDKを使ったインタラクティブテンプレート

テンプレートでMCP Apps SDKを使用すると、UIから他のツールを動的に呼び出す双方向通信が可能です：

```html
<script type="module">
// MCP Apps互換レイヤー
// window.mcpApps（obsidian-gemini-helper）と@anthropic-ai/mcp-app-sdkの両方をサポート
let mcpClient = null;

async function initMcpClient() {
  // まずインジェクトされたブリッジをチェック（obsidian-gemini-helper）
  if (window.mcpApps && typeof window.mcpApps.callTool === 'function') {
    return {
      callServerTool: (name, args) => window.mcpApps.callTool(name, args),
      type: 'bridge'
    };
  }

  // MCP App SDKにフォールバック
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

// 初期化して使用
mcpClient = await initMcpClient();
if (mcpClient) {
  // アプリ専用ツールを呼び出し
  const result = await mcpClient.callServerTool('git-staged-diff', { args: ['file.txt'] });
  console.log(result.content[0].text);
}

// テンプレートからの初期データ
const initialData = {{json .Lines}};  // 安全なJS埋め込み
</script>
```

**重要**: JavaScriptで `{{json .Lines}}` などのテンプレート関数を使用する場合、出力は自動的に安全な埋め込み形式になります（二重エスケープを防ぐため `template.JS` 型を返す）。

### 仕組み

1. `tools/list` がUI対応ツールに `_meta.ui.resourceUri` を含めて返す
2. `tools/call` が出力データを含む `_meta.ui.resourceUri` と共に結果を返す
3. クライアントが `resources/read` でURIを指定してレンダリング済みHTMLを取得

## プラグイン例

`examples/plugins/` ディレクトリを参照：

```
examples/plugins/
├── git/
│   ├── plugin.json      # インタラクティブUI付きGitコマンド
│   └── templates/
│       ├── changes.html # インタラクティブなステージ/アンステージ変更ビューア
│       ├── commits.html # インタラクティブなコミットエクスプローラー
│       ├── log.html     # git log用カスタムUI
│       └── diff.html    # git diff用カスタムUI
├── interactive/
│   ├── plugin.json      # 双方向MCP Apps対応ファイルエクスプローラー
│   └── templates/
│       └── explorer.html
└── shell/
    ├── plugin.json      # シェルコマンド（ls, cat, find, grep）
    └── templates/
        └── table.html   # カスタムテーブルUI
```

### インタラクティブGitプラグイン

gitプラグインは双方向MCP Apps通信を実演しています：

- **git-changes**: アコーディオンUIでステージ/アンステージファイルを表示。ファイルをクリックするとdiffを表示（アプリ専用ツール経由で動的に読み込み）。
- **git-commits**: コミット履歴を閲覧。コミットをクリックすると変更ファイル一覧、ファイルをクリックするとdiffを表示。

アプリ専用ヘルパーツール（`visibility: ["app"]`）：
- `git-staged-files`, `git-unstaged-files`: UI用のファイル一覧
- `git-staged-diff`, `git-unstaged-diff`: 選択ファイルのdiff取得
- `git-commit-files`, `git-file-diff`: コミット詳細の取得

インタラクティブサンプルを試すには：
```bash
cd /path/to/your/git/repo
./mcp-gatekeeper --plugins-dir=examples/plugins --root-dir=.
```

## ライセンス

MIT License
