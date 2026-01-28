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
│  │  APIキー認証 & レート制限                                           │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                     ↓                                       │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  プラグイン設定                                                     │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  許可する環境変数: ["PATH", "HOME", "LANG", "GIT_*"]                │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "git-log"                                          │   │   │
│  │  │  ├─ コマンド: git                                           │   │   │
│  │  │  ├─ 許可する引数: ["log --oneline *", "log -n *"]           │   │   │
│  │  │  ├─ サンドボックス: none                                    │   │   │
│  │  │  └─ UIテンプレート: templates/log.html                      │   │   │
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
      "name": "git-log",
      "description": "Gitコミットログを表示",
      "command": "git",
      "allowed_arg_globs": ["log --oneline *", "log --oneline"],
      "sandbox": "none",
      "ui_template": "templates/log.html"
    },
    {
      "name": "ls",
      "description": "ディレクトリ内容を表示",
      "command": "ls",
      "allowed_arg_globs": ["*"],
      "sandbox": "bubblewrap",
      "ui_type": "log"
    }
  ],
  "allowed_env_keys": ["PATH", "HOME", "LANG"]
}
```

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
| `allowed_arg_globs` | No | 許可する引数のGlobパターン |
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
| `--db` | - | 監査ログ用SQLiteデータベースパス（オプション） |
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
| `*` | `/` 以外の任意文字列 |
| `**` | `/` を含む任意文字列 |
| `?` | 任意の1文字 |
| `[abc]` | 文字クラス |
| `{a,b}` | 選択 |

例：
- `status **` - `status`, `status .`, `status --short` にマッチ
- `*.txt` - 任意の `.txt` ファイルにマッチ
- `--format=*` - 任意の `--format=` オプションにマッチ

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
  "allowed_arg_globs": ["status", "status *"],
  "sandbox": "none",
  "ui_type": "log"
}
```

| フィールド | 説明 |
|-----------|------|
| `ui_type` | `table`, `json`, `log` |
| `output_format` | `json`, `csv`, `lines`（テーブル解析用） |
| `ui_template` | カスタムHTMLテンプレートのパス（ui_typeより優先） |

### カスタムテンプレート

Goテンプレートで完全にカスタムなUIを作成できます：

```json
{
  "name": "git-log",
  "command": "git",
  "ui_template": "templates/log.html"
}
```

テンプレート変数：
- `{{.Output}}` - 生の出力文字列
- `{{.Lines}}` - 行ごとに分割された出力
- `{{.JSON}}` - パース済みJSON（有効な場合）
- `{{.JSONPretty}}` - 整形済みJSON
- `{{.IsJSON}}` - 出力が有効なJSONかどうか

テンプレート関数：
- `{{escape .Output}}` - HTMLエスケープ
- `{{json .Data}}` - JSONエンコード
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
<head><title>Git Log</title></head>
<body>
  <h1>コミット（{{len .Lines}}件）</h1>
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

### 仕組み

1. `tools/list` がUI対応ツールに `_meta.ui.resourceUri` を含めて返す
2. `tools/call` が出力データを含む `_meta.ui.resourceUri` と共に結果を返す
3. クライアントが `resources/read` でURIを指定してレンダリング済みHTMLを取得

## プラグイン例

`examples/plugins/` ディレクトリを参照：

```
examples/plugins/
├── git/
│   ├── plugin.json      # Gitコマンド（status, log, diff, branch等）
│   └── templates/
│       ├── log.html     # git log用カスタムUI
│       └── diff.html    # git diff用カスタムUI
└── shell/
    ├── plugin.json      # シェルコマンド（ls, cat, find, grep）
    └── templates/
        └── table.html   # カスタムテーブルUI
```

使用例：
```bash
./mcp-gatekeeper --plugins-dir=examples/plugins --root-dir=. --addr=:8080
```

## ライセンス

MIT License
