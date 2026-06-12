# 設定

## プロジェクト設定（`.4x/settings.json`）

`4x init` によって作成されます。プロジェクトメタデータ、ランナー定義、ロールモデルのマッピングが含まれます。

```json
{
  "project": {
    "name": "my-project",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["go vet ./..."],
    "setup": [],
    "docs": [],
    "rules": []
  },
  "runners": {
    "claude": {
      "command": "claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "model": "opus",
      "tty": true
    },
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    },
    "gemini": {
      "command": "gemini",
      "args": ["-y", "-p", "{prompt}"]
    },
    "agy": {
      "command": "agy",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"]
    }
  },
  "default": "claude",
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

### Project セクション

| フィールド | 説明 |
|---|---|
| `name` | プロジェクト名（ディレクトリから自動検出） |
| `language` | 検出された言語 |
| `build` | ビルドコマンド |
| `test` | テストコマンド |
| `lint` | lint コマンド |
| `setup` | セットアップコマンド（例：`docker-compose up -d`） |
| `docs` | Designer が参照するドキュメントファイルのパス |
| `rules` | ロールプロンプトに注入されるプロジェクト固有のルール |

### Runner 設定

| フィールド | 説明 |
|---|---|
| `command` | 実行ファイル名 |
| `args` | 引数。`{prompt}` と `{promptFile}` は実行時に置換されます。`{model}` はロールのモデルに置換されます。 |
| `model` | このランナーのデフォルトモデル |
| `model_map` | ロールモデル名をランナー固有の名前にマッピング（例：`{"opus": "claude-opus-4-5-20250514"}`）。検索順序：ロール model → model_map 変換 → 元の名前にフォールバック。 |
| `tty` | 出力キャプチャに PTY を使用（Claude Code のような ANSI 出力を持つ CLI ツールに必要） |
| `stdin` | 引数の代わりに標準入力でプロンプトを送信（Codex が使用） |
| `quiet` | ランナーのターミナル stdout 出力を抑制。出力はログファイルに記録されます。 |

`{model}` が `args` に含まれない場合、ランナーは自動的に `--model <model>` を追加します。

### Role 設定

| フィールド | 説明 |
|---|---|
| `model` | このロールのモデル名 |
| `deep_model` | 敵対的レビューパスのモデル（reviewer のみ） |
| `instructions` | ロールプロンプトに注入される追加の指示 |
| `includes` | ロールプロンプトにインクルードするファイル |

### その他の設定フィールド

| フィールド | 説明 |
|---|---|
| `hub_repos` | 共有リポジトリ（バッチ DAG のグルーピング用） |
| `isolation` | `"worktree"` に設定すると、Feature を git worktree で実行 |
| `max_concurrent_runs` | ダッシュボードサーバー経由の最大同時実行数 |
| `commit` | コミット戦略：`"per-round"`（デフォルト）、`"on-done"`、または `"never"` |

## ユーザー設定（`~/.4x/settings.json`）

グローバルなユーザー設定。`4x config` で管理します。

```bash
4x config set locale zh-TW
4x config get locale
4x config list
```

### ロケール

ロールプロンプトの指示に使用する言語を設定します。サポートされる値：

| 値 | 言語 |
|---|---|
| `en` | English（デフォルト） |
| `zh-TW` | 繁體中文 |
| `zh-CN` | 简体中文 |
| `ja` | 日本語 |
| `ko` | 한국어 |
| `es` | Español |
| `fr` | Français |
| `de` | Deutsch |
| `pt` | Português |
| `ru` | Русский |
| `vi` | Tiếng Việt |

ロケールは、明示的に設定されていない場合、`LANG` 環境変数からも推論されます。
