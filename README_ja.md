# MCP Gatekeeper

APIキーベースのポリシーに基づいてコマンド実行を制御するセキュアなMCP（Model Context Protocol）サーバーです。APIキー認証、globベースのポリシールール、包括的な監査ログ、TUI管理ツールを備えています。

## 機能

- **APIキー認証**: bcryptハッシュによるセキュアなAPIキー管理
- **ポリシーベースのアクセス制御**: コマンドと作業ディレクトリの許可/拒否を柔軟なglobパターンで設定
- **デュアルプロトコル対応**: stdio（JSON-RPC）とHTTP APIの両モードをサポート
- **監査ログ**: すべてのコマンドリクエストと実行結果の完全なログ記録
- **TUI管理ツール**: キー、ポリシー、ログを管理するインタラクティブなターミナルインターフェース
- **レート制限**: HTTP API用の組み込みレート制限
- **コマンド正規化**: パス解決とコマンド正規化の自動処理

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
3. 許可/拒否パターンを設定：

ポリシー例：
- 許可するCWD Glob: `/home/user/**`
- 許可するCmd Glob: `ls *`, `cat *`, `git *`
- 拒否するCmd Glob: `rm -rf *`, `sudo *`

### 3. サーバーの起動

**HTTPモード：**
```bash
./mcp-gatekeeper-server --mode=http --addr=:8080 --db=gatekeeper.db
```

**stdioモード（MCPクライアント用）：**
```bash
MCP_GATEKEEPER_API_KEY=your-api-key ./mcp-gatekeeper-server --mode=stdio --db=gatekeeper.db
```

### 4. 実行テスト

curlを使用（HTTPモード）：
```bash
curl -X POST http://localhost:8080/v1/execute \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"cwd": "/home/user", "cmd": "ls", "args": ["-la"]}'
```

## 設定

### データベース

サーバーはSQLiteを使用します。`--db`でデータベースパスを指定：

```bash
--db=/path/to/gatekeeper.db
```

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
- **Policies**: キーごとの許可/拒否パターンの設定
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
| `Ctrl+S` | 保存 |

## セキュリティ上の考慮事項

1. **APIキーの保存**: APIキーはbcryptでハッシュ化され、平文は作成時に一度だけ表示
2. **ポリシー設計**: 制限的なポリシーから始めて、必要に応じて許可を追加
3. **監査ログ**: 判定結果に関わらず、すべてのリクエストがログに記録
4. **レート制限**: HTTP APIにはキーごとのレート制限を含む
5. **コマンド正規化**: パストラバーサルのトリックを防ぐためにコマンドを正規化

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
