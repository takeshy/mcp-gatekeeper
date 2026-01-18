# MCP Gatekeeper

**シェルコマンドを実行し、その結果を返す**MCP（Model Context Protocol）サーバーです。ClaudeなどのAIアシスタントがシステム上でコマンドを実行し、stdout、stderr、終了コードを受け取ることができます。

フルシェルアクセスを提供しながらも、システムを安全に保つための柔軟なセキュリティ制御を備えています：

- **ディレクトリサンドボックス** - すべてのコマンドは指定されたルートディレクトリに制限（chroot風）
- **APIキーベースのアクセス制御** - 各クライアントに独自のAPIキーとカスタマイズ可能な権限を付与
- **Globベースのポリシールール** - 許可するコマンドとディレクトリを細かく制御
- **監査ログ** - すべてのコマンド実行履歴を確認可能

## 機能

- **シェルコマンド実行**: 任意のシェルコマンドを実行し、stdout、stderr、終了コードを取得
- **ディレクトリサンドボックス**: 必須の`--root-dir`オプションにより、すべての操作を指定ディレクトリに制限
- **Bubblewrapサンドボックス**: オプションの`bwrap`統合による真のプロセス分離（自動検出）
- **柔軟なセキュリティ**: APIキーごとにglobパターンで許可/拒否するコマンドとディレクトリを設定
- **デュアルプロトコル対応**: stdio（MCP用JSON-RPC）とHTTP APIの両モードをサポート
- **TUI管理ツール**: キー、ポリシー、ログを管理するインタラクティブなターミナルインターフェース
- **監査ログ**: すべてのコマンドリクエストと実行結果の完全なログ記録（平文で保存）
- **レート制限**: HTTP API用の設定可能なレート制限（デフォルト: 500リクエスト/分）

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

### 2. ポリシーの設定

TUIのAPI Keys画面で：
1. APIキーを選択
2. `e`キーでポリシーを編集
3. 許可/拒否パターンを設定（CWDフィールドでCtrl+Spaceでパス補完可能）

ポリシー例：
- 許可するCWD Glob: `/home/user/projects/**`
- 許可するCmd Glob: `ls *`, `cat *`, `git *`
- 拒否するCmd Glob: `rm -rf *`, `sudo *`

### 3. サーバーの起動

**HTTPモード：**
```bash
./mcp-gatekeeper-server \
  --root-dir=/home/user/projects \
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

### 4. 実行テスト

curlを使用（HTTPモード）：
```bash
curl -X POST http://localhost:8080/v1/execute \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"cwd": "/home/user/projects", "cmd": "ls", "args": ["-la"]}'
```

## 設定

### コマンドラインオプション

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--root-dir` | (必須) | コマンド実行のルートディレクトリ（サンドボックス） |
| `--mode` | `stdio` | サーバーモード: `stdio` または `http` |
| `--db` | `gatekeeper.db` | SQLiteデータベースパス |
| `--addr` | `:8080` | HTTPサーバーアドレス（httpモード用） |
| `--rate-limit` | `500` | APIキーごとの分間レート制限（httpモード用） |
| `--sandbox` | `auto` | サンドボックスモード: `auto`、`bwrap`、`none` |
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

### サンドボックスモード (--sandbox)

`--sandbox`オプションはプロセス分離を制御します：

| モード | 説明 |
|--------|------|
| `auto` | `bwrap`が利用可能なら使用、なければパス検証にフォールバック |
| `bwrap` | bubblewrapサンドボックスを要求（未インストール時は警告を出してフォールバック） |
| `none` | パス検証のみ使用（プロセス分離なし） |

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

`--sandbox=auto`（デフォルト）の場合、サーバーは自動的に`bwrap`の可用性を検出して使用します。

### ポリシーの優先順位

2つのモードが利用可能：

- `deny_overrides`（デフォルト）: 拒否ルールが先にチェックされ、コマンドが拒否された場合は許可ルールにマッチしてもブロック
- `allow_overrides`: 許可ルールが優先され、コマンドが許可ルールにマッチすれば拒否ルールにマッチしても実行許可

### Globパターン

以下のglob構文がサポートされています：

| パターン | 説明 |
|---------|------|
| `*` | `/`以外の任意の文字列にマッチ |
| `**` | `/`を含む任意の文字列にマッチ |
| `?` | 任意の1文字にマッチ |
| `[abc]` | セット内の任意の文字にマッチ |
| `{a,b}` | `a`または`b`にマッチ |

例：
- `/home/**` - /home配下のすべてのパス
- `/usr/bin/*` - /usr/bin内の任意のコマンド
- `git *` - 任意のgitコマンド
- `rm -rf *` - 再帰的強制削除をブロック

## APIリファレンス

### HTTP API

#### POST /v1/execute

コマンドを実行します。

**ヘッダー：**
- `Authorization: Bearer <api-key>`（必須）

**リクエストボディ：**
```json
{
  "cwd": "/path/to/directory",
  "cmd": "command",
  "args": ["arg1", "arg2"]
}
```

**レスポンス：**
```json
{
  "exit_code": 0,
  "stdout": "output...",
  "stderr": "",
  "duration_ms": 45
}
```

**エラーレスポンス：**
```json
{
  "error": "command denied by policy: ..."
}
```

### MCPプロトコル（stdio）

サーバーは以下のツールを持つMCPプロトコルを実装：

#### execute

シェルコマンドを実行します。

**入力スキーマ：**
```json
{
  "type": "object",
  "properties": {
    "cwd": {
      "type": "string",
      "description": "コマンドの作業ディレクトリ"
    },
    "cmd": {
      "type": "string",
      "description": "実行するコマンド"
    },
    "args": {
      "type": "array",
      "items": { "type": "string" },
      "description": "コマンド引数"
    }
  },
  "required": ["cwd", "cmd"]
}
```

## TUI管理ツール

管理ツールの機能：

- **API Keys**: APIキーの作成、表示、無効化
- **Policies**: キーごとの許可/拒否パターンの設定（パス補完機能付き）
- **Audit Logs**: 実行履歴の閲覧と検査
- **Test Execute**: ポリシーに対するコマンドのテスト

### キーボードショートカット

| キー | 操作 |
|------|------|
| `↑/↓` または `j/k` | ナビゲーション |
| `Enter` | 選択/確認 |
| `Esc` | 戻る |
| `n` | 新規作成 |
| `e` | 編集 |
| `d` | 削除/無効化 |
| `q` | 終了 |
| `Tab` | 次のフィールド |
| `Ctrl+Space` | パス補完（CWDフィールド） |
| `Ctrl+S` | 保存 |

## セキュリティ上の考慮事項

1. **ディレクトリサンドボックス**: すべてのコマンドは`--root-dir`に制限され、外部のパスは拒否されます
2. **Bubblewrap分離**: 利用可能な場合、コマンドは分離された名前空間で実行されファイルシステムの脱出を防止
3. **APIキーの保存**: APIキーはbcryptでハッシュ化され、平文は作成時に一度だけ表示
4. **ポリシー設計**: 制限的なポリシーから始めて、必要に応じて許可を追加
5. **監査ログ**: 判定結果に関わらず、すべてのリクエストがログに記録（平文で保存）
6. **レート制限**: HTTP APIにはキーごとの設定可能なレート制限を含む
7. **シンボリックリンク解決**: シンボリックリンクはサンドボックス脱出を防ぐために解決

**セキュリティレベル：**

| サンドボックスモード | 保護レベル | 備考 |
|---------------------|-----------|------|
| `bwrap` | 高 | 完全な名前空間分離、本番環境推奨 |
| `none` | 基本 | パス検証のみ、スクリプトは絶対パスでバイパス可能 |

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
│   ├── policy/          # ポリシー評価エンジン
│   ├── executor/        # コマンド実行エンジン
│   ├── db/              # データベースアクセス層
│   ├── mcp/             # MCPプロトコル実装
│   └── tui/             # TUI画面
└── README.md
```

## ライセンス

MIT License
