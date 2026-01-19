# MCP Gatekeeper

**シェルコマンドを実行し、その結果を返す**MCP（Model Context Protocol）サーバーです。ClaudeなどのAIアシスタントがシステム上でコマンドを実行し、stdout、stderr、終了コードを受け取ることができます。

シェルアクセスを提供しながらも、システムを安全に保つための柔軟なセキュリティ制御を備えています：

- **ディレクトリサンドボックス** - すべてのコマンドは指定されたルートディレクトリに制限（chroot風）
- **APIキーベースのアクセス制御** - 各クライアントに独自のAPIキーとカスタマイズ可能なツールを付与
- **ツールベースアーキテクチャ** - APIキーごとに個別のサンドボックス設定を持つツールを定義
- **Globベースの引数制限** - ツールごとに許可する引数を細かく制御
- **複数のサンドボックスモード** - ツールごとにbubblewrap、WASM、またはサンドボックスなしを選択
- **監査ログ** - すべてのコマンド実行履歴を確認可能

## 機能

- **シェルコマンド実行**: シェルコマンドを実行し、stdout、stderr、終了コードを取得
- **ディレクトリサンドボックス**: 必須の`--root-dir`オプションにより、すべての操作を指定ディレクトリに制限
- **ツールごとのサンドボックス選択**: 各ツールは`none`、`bubblewrap`、`wasm`のサンドボックスを使用可能
- **Bubblewrapサンドボックス**: `bwrap`統合による真のプロセス分離
- **WASMサンドボックス**: 安全なwazeroランタイムでWebAssemblyバイナリを実行（モジュールキャッシュ付き）
- **動的ツール登録**: TUIを通じてAPIキーごとにカスタムツールを定義
- **デュアルプロトコル対応**: stdioとHTTPの両モードでMCP JSON-RPCプロトコルを使用
- **TUI管理ツール**: キー、ツール、ログを管理するインタラクティブなターミナルインターフェース
- **監査ログ**: すべてのコマンドリクエストと実行結果の完全なログ記録
- **レート制限**: HTTP API用の設定可能なレート制限（デフォルト: 500リクエスト/分）

## アーキテクチャ

### 概念図

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
│  │  │  ツール: "ruby"                                             │   │   │
│  │  │  ├─ コマンド: ruby                                          │   │   │
│  │  │  ├─ 許可された引数: ["-e **", "*.rb"]                       │   │   │
│  │  │  ├─ サンドボックス: wasm                                    │   │   │
│  │  │  └─ WASMバイナリ: /opt/ruby-wasm/usr/local/bin/ruby         │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "cat"                                              │   │   │
│  │  │  ├─ コマンド: /usr/bin/cat                                  │   │   │
│  │  │  ├─ 許可された引数: ["*.txt", "*.md", "*.json"]             │   │   │
│  │  │  └─ サンドボックス: bubblewrap                              │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "ls"                                               │   │   │
│  │  │  ├─ コマンド: /usr/bin/ls                                   │   │   │
│  │  │  ├─ 許可された引数: [] (すべて許可)                         │   │   │
│  │  │  └─ サンドボックス: bubblewrap                              │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         API Key: "readonly-user"                     │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                     │   │
│  │  許可された環境変数: ["PATH"]                                       │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  ツール: "cat"                                              │   │   │
│  │  │  ├─ コマンド: /usr/bin/cat                                  │   │   │
│  │  │  ├─ 許可された引数: ["README*"]                             │   │   │
│  │  │  └─ サンドボックス: bubblewrap                              │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### データベース構造

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

### セキュリティフロー

```
┌─────────────────────────────────────────────────────────┐
│                      リクエスト                          │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  1. APIキー認証                                         │
│     - キーの検証 (bcrypt hash)                          │
│     - アクティブ状態の確認                               │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  2. ツール存在確認                                       │
│     - APIキーに紐づくツールか確認                        │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  3. 引数検証 (ポリシー)                                  │
│     - allowed_arg_globs とマッチするか確認              │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  4. 環境変数フィルタ                                     │
│     - allowed_env_keys でフィルタリング                 │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  5. サンドボックス実行                                   │
│     ┌─────────┬─────────────┬───────────────┐          │
│     │  none   │ bubblewrap  │     wasm      │          │
│     ├─────────┼─────────────┼───────────────┤          │
│     │パス検証 │ namespace   │ wazero        │          │
│     │のみ     │ 分離        │ 完全分離      │          │
│     │         │ rootDir=/   │ rootDir=/     │          │
│     └─────────┴─────────────┴───────────────┘          │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  6. 監査ログ記録                                        │
└─────────────────────────────────────────────────────────┘
```

## インストール

```bash
go install github.com/takeshy/mcp-gatekeeper/cmd/server@latest
go install github.com/takeshy/mcp-gatekeeper/cmd/admin@latest
```

またはソースからビルド：

```bash
git clone https://github.com/takeshy/mcp-gatekeeper
cd mcp-gatekeeper
go build -o mcp-gatekeeper-server ./cmd/server
go build -o mcp-gatekeeper-admin ./cmd/admin
```

## クイックスタート

### 1. APIキーの作成

```bash
./mcp-gatekeeper-admin --db gatekeeper.db
```

TUIで：
1. 「API Keys」を選択
2. `n`キーで新しいキーを作成
3. キーの名前を入力
4. **生成されたAPIキーを保存**（再表示されません）

### 2. ツールの設定

TUIのAPI Keys画面で：
1. APIキーを選択してEnterで詳細を表示
2. `t`キーでツール管理画面へ
3. `n`キーで新しいツールを作成

ツール設定例：
- **Name**: `git`（これがMCPツール名になります）
- **Description**: `Run git commands`
- **Command**: `/usr/bin/git`
- **Allowed Arg Globs**: `status **`, `log **`, `diff **`（1行に1パターン）
- **Sandbox**: `bubblewrap`

### 3. 許可する環境変数の設定（オプション）

APIキー詳細画面で：
1. `v`キーで環境変数の編集へ
2. `PATH`、`HOME`、`GO*`などのパターンを追加（1行に1パターン）
3. Ctrl+Sで保存

### 4. サーバーの起動

**HTTPモード：**
```bash
./mcp-gatekeeper-server \
  --root-dir=/home/user/projects \
  --mode=http \
  --addr=:8080 \
  --db=gatekeeper.db
```

**WASMディレクトリを指定（外部WASMバイナリ用）：**
```bash
./mcp-gatekeeper-server \
  --root-dir=/home/user/projects \
  --wasm-dir=/opt \
  --mode=http \
  --addr=:8080 \
  --db=gatekeeper.db
```

**stdioモード（MCPクライアント用）：**
```bash
MCP_GATEKEEPER_API_KEY=your-api-key \
./mcp-gatekeeper-server \
  --root-dir=/home/user/projects \
  --mode=stdio \
  --db=gatekeeper.db
```

### 5. 実行テスト

MCP JSON-RPCプロトコルでcurlを使用（HTTPモード）：
```bash
# 初期化
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "initialize"}'

# 利用可能なツールを一覧表示
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}'

# ツールを呼び出し
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "git", "arguments": {"cwd": "/home/user/projects", "args": ["status", "--short"]}}}'
```

## 設定

### コマンドラインオプション

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--root-dir` | (必須) | コマンド実行のルートディレクトリ（サンドボックス） |
| `--wasm-dir` | - | WASMバイナリを格納するディレクトリ（WASMサンドボックス内で`/.wasm`としてマウント） |
| `--mode` | `stdio` | サーバーモード: `stdio` または `http` |
| `--db` | `gatekeeper.db` | SQLiteデータベースパス |
| `--addr` | `:8080` | HTTPサーバーアドレス（httpモード用） |
| `--rate-limit` | `500` | APIキーごとの分間レート制限（httpモード用） |
| `--api-key` | - | stdioモード用のAPIキー（または`MCP_GATEKEEPER_API_KEY`環境変数） |

### ディレクトリサンドボックス (--root-dir)

`--root-dir`オプションは**必須**で、chroot風のサンドボックスを作成します：

```bash
# すべてのコマンドを/home/user/projects内に制限
./mcp-gatekeeper-server --root-dir=/home/user/projects --mode=http
```

- コマンドはルートディレクトリ外のパスにアクセスできません
- シンボリックリンクは脱出を防ぐために解決されます
- このオプションなしでサーバーは起動しません

### WASMディレクトリ (--wasm-dir)

`--wasm-dir`オプションにより、WASMバイナリをルートディレクトリ外に配置できます：

```bash
# WASMバイナリは/opt、作業ディレクトリは/home/user/projects
./mcp-gatekeeper-server --root-dir=/home/user/projects --wasm-dir=/opt --mode=http
```

- WASMバイナリはWASMサンドボックス内で`/.wasm`としてマウントされます
- 作業ディレクトリ（`--root-dir`）はWASMサンドボックス内で`/`としてマウントされます
- これにより、WASMランタイムとユーザーデータを分離できます

### ツール設定

各ツールには以下の設定があります：

| フィールド | 説明 |
|-----------|------|
| `name` | MCPツール名（APIキーごとにユニーク） |
| `description` | MCPクライアントに表示されるツールの説明 |
| `command` | 実行ファイルの絶対パス（例: `/usr/bin/git`） |
| `allowed_arg_globs` | 許可する引数のGlobパターン |
| `sandbox` | サンドボックスモード: `none`、`bubblewrap`、`wasm` |
| `wasm_binary` | WASMバイナリのパス（`wasm`サンドボックス時に必須） |

### サンドボックスモード

| モード | 説明 |
|--------|------|
| `none` | プロセス分離なし、パス検証のみ |
| `bubblewrap` | `bwrap`を使用した完全な名前空間分離 |
| `wasm` | wazeroランタイムでWebAssemblyバイナリを実行 |

**なぜbubblewrapが必要か？**

パス検証だけでは作業ディレクトリ（`cwd`）のみをチェックします。プロセス分離がないと、`ruby -e "File.read('/etc/passwd')"`のようなスクリプトはサンドボックス外のファイルにアクセスできてしまいます。

bubblewrap（`bwrap`）を使用すると、コマンドは分離された名前空間で実行され：
- ルートディレクトリのみ書き込み可能
- システムディレクトリ（`/usr`、`/bin`、`/lib`）は読み取り専用
- ネットワークアクセスはブロック
- シンボリックリンクや絶対パスでの脱出は不可能

**bubblewrapのインストール：**

```bash
# Debian/Ubuntu
sudo apt install bubblewrap

# Fedora/RHEL
sudo dnf install bubblewrap

# Arch Linux
sudo pacman -S bubblewrap
```

**WASMサンドボックス：**

最大限の分離のために、WebAssemblyバイナリを実行できます：
- WASIサポートでコンパイル
- wazeroランタイムで実行（純粋なGo、CGO不要）
- ファイルシステムアクセスはルートディレクトリに制限
- ネットワークアクセスなし
- **コンパイル済みモジュールがキャッシュ**され、2回目以降の実行が高速化

**WASMバイナリの作成：**

様々な言語でWASMにコンパイルできます：

*Rustを使用：*
```bash
# WASIターゲットをインストール
rustup target add wasm32-wasip1

# 新しいプロジェクトを作成
cargo new --bin my-tool
cd my-tool

# WASI用にビルド
cargo build --release --target wasm32-wasip1

# バイナリは target/wasm32-wasip1/release/my-tool.wasm に生成される
```

*Goを使用：*
```bash
# WASI用にビルド
GOOS=wasip1 GOARCH=wasm go build -o my-tool.wasm main.go
```

*C/C++を使用（WASI SDK）：*
```bash
# WASI SDKをインストール https://github.com/WebAssembly/wasi-sdk
export WASI_SDK_PATH=/opt/wasi-sdk

# コンパイル
$WASI_SDK_PATH/bin/clang -o my-tool.wasm my-tool.c
```

**スクリプト言語のWASMランタイム：**

WASMにコンパイルされたインタプリタでスクリプトを実行できます：

*Ruby (ruby.wasm)：*
```bash
# https://github.com/ruby/ruby.wasm/releases からダウンロード
# 最新の ruby-*-wasm32-unknown-wasip1-full.tar.gz を選択
tar xzf ruby-*-wasm32-unknown-wasip1-full.tar.gz
# 使用: ruby-*-wasm32-unknown-wasip1-full/usr/local/bin/ruby
```

*Python (python.wasm)：*
```bash
# VMware Labs WebAssembly Language Runtimes からダウンロード
# https://github.com/vmware-labs/webassembly-language-runtimes/releases
# python-*.wasm リリースを探す
curl -LO "https://github.com/vmware-labs/webassembly-language-runtimes/releases/download/python/3.12.0%2B20231211-040d5a6/python-3.12.0.wasm"
# 使用: python-3.12.0.wasm（標準ライブラリ内蔵、約26MB）
```

*JavaScript (QuickJS)：*
```bash
# QuickJS-NG リリースからダウンロード
# https://github.com/quickjs-ng/quickjs/releases
curl -LO "https://github.com/quickjs-ng/quickjs/releases/latest/download/qjs-wasi.wasm"
# 使用: qjs-wasi.wasm（約1.4MB、JSONは組み込み）
```

**WASMランタイム比較：**

| ランタイム | サイズ | コンパイル時間 | JSONサポート |
|-----------|--------|--------------|--------------|
| Ruby | 約50MB（stdlib含む） | 約9秒 | `require 'json'`（自動設定） |
| Python | 約26MB（内蔵） | 約3.6秒 | `import json`（組み込み） |
| QuickJS | 約1.4MB | 約0.5秒 | `JSON.stringify()`（組み込み） |

注: コンパイル時間は初回実行時のみです。コンパイル済みモジュールはキャッシュされ、2回目以降は高速に実行されます。

**WASMツールの設定：**

TUIでツールを作成する際：
- **Name**: `ruby`
- **Description**: `Execute Ruby scripts in WASM sandbox`
- **Command**: `ruby`（任意の値、WASMでは使用されない）
- **Sandbox**: `wasm`
- **WASM Binary**: `/opt/ruby-wasm/usr/local/bin/ruby`

WASMバイナリはWASIの`args_get`経由で引数を受け取り、ルートディレクトリ内のファイルにアクセスできます。

### Globパターン

以下のglob構文がサポートされています：

| パターン | 説明 |
|---------|------|
| `*` | `/`以外の任意の文字列にマッチ |
| `**` | `/`を含む任意の文字列にマッチ |
| `?` | 任意の1文字にマッチ |
| `[abc]` | セット内の任意の文字にマッチ |
| `{a,b}` | `a`または`b`にマッチ |

`allowed_arg_globs`の例：
- `status **` - `status`と任意の引数を許可
- `log --oneline **` - `log --oneline`と任意のパスを許可
- `diff **` - `diff`と任意の引数を許可
- 空（パターンなし）- すべての引数を許可

## APIリファレンス

### MCP JSON-RPCプロトコル

stdioとHTTPの両モードでMCP JSON-RPC 2.0プロトコルを使用します。HTTPモードは`POST /mcp`でリクエストを受け付けます。

#### initialize

MCPセッションを初期化します。

**リクエスト：**
```json
{"jsonrpc": "2.0", "id": 1, "method": "initialize"}
```

**レスポンス：**
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

認証されたAPIキーで利用可能なツールを一覧表示します。

**リクエスト：**
```json
{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}
```

**レスポンス：**
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
            "cwd": {"type": "string", "description": "コマンドの作業ディレクトリ（デフォルトはルートディレクトリ）"},
            "args": {"type": "array", "items": {"type": "string"}, "description": "コマンド引数"}
          },
          "required": []
        }
      }
    ]
  }
}
```

#### tools/call

ツールを実行します。

**リクエスト：**
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

**レスポンス：**
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

**エラーレスポンス：**
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

### HTTP認証

HTTPモードではBearerトークン認証が必要です：

```
Authorization: Bearer your-api-key
```

## TUI管理ツール

管理ツールの機能：

- **API Keys**: APIキーの作成、表示、無効化
- **Tools**: APIキーごとのツール設定（コマンド、引数、サンドボックス）
- **Environment Variables**: キーごとの許可する環境変数の設定
- **Audit Logs**: 実行履歴の閲覧と検査
- **Test Execute**: 実際のコマンドでツール実行をテスト

### キーボードショートカット

| キー | 操作 |
|------|------|
| `↑/↓` または `j/k` | ナビゲーション |
| `Enter` | 選択/確認 |
| `Esc` | 戻る |
| `n` | 新規作成 |
| `e` | 編集 |
| `d` | 削除/無効化 |
| `t` | ツール管理（APIキー詳細画面） |
| `v` | 環境変数編集（APIキー詳細画面） |
| `q` | 終了 |
| `Tab` | 次のフィールド |
| `Ctrl+S` | 保存 |

## セキュリティ上の考慮事項

1. **ディレクトリサンドボックス**: すべてのコマンドは`--root-dir`に制限され、外部のパスは拒否されます
2. **ツールごとのサンドボックス**: 各ツールは分離レベルを指定可能（none、bubblewrap、wasm）
3. **引数制限**: `allowed_arg_globs`で渡せる引数を制限
4. **APIキーの保存**: APIキーはbcryptでハッシュ化され、平文は作成時に一度だけ表示
5. **監査ログ**: 判定結果に関わらず、すべてのリクエストがログに記録（平文で保存）
6. **レート制限**: HTTP APIにはキーごとの設定可能なレート制限を含む
7. **シンボリックリンク解決**: シンボリックリンクはサンドボックス脱出を防ぐために解決

**セキュリティレベル：**

| サンドボックスモード | 保護レベル | 備考 |
|---------------------|-----------|------|
| `wasm` | 最高 | WASIサンドボックス、システムコールなし |
| `bubblewrap` | 高 | 完全な名前空間分離、ネイティブバイナリ推奨 |
| `none` | 基本 | パス検証のみ、信頼できるコマンド用 |

## 開発

### テストの実行

```bash
go test ./...
```

### プロジェクト構造

```
mcp-gatekeeper/
├── cmd/
│   ├── server/          # MCPサーバー（stdio/HTTP）
│   └── admin/           # TUI管理ツール
├── internal/
│   ├── auth/            # APIキー認証
│   ├── policy/          # 引数評価エンジン
│   ├── executor/        # コマンド実行エンジン
│   ├── db/              # データベースアクセス層
│   ├── mcp/             # MCPプロトコル実装
│   └── tui/             # TUI画面
└── README.md
```

## ライセンス

MIT License
