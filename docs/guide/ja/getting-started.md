# はじめに

## インストール

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

Go 1.26+ が必要です。

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### バイナリダウンロード

macOS、Linux、Windows（amd64 / arm64）のビルド済みバイナリは [Releases](https://github.com/ggwhite/4x/releases) ページからダウンロードできます。

### macOS Gatekeeper

CLI バイナリと 4x Live ダッシュボードアプリは Apple Developer 証明書で署名されていません。macOS Gatekeeper が初回起動時にブロックします。2つの解決方法：

**方法 A：隔離属性を削除（推奨）**

```bash
# CLI バイナリ
xattr -cr /usr/local/bin/4x

# Dashboard アプリ
xattr -cr /Applications/4x\ Live.app
```

**方法 B：システム設定から許可**

1. アプリをダブルクリック — macOS が「開発元を確認できないため開けません」と表示
2. **システム設定 → プライバシーとセキュリティ**を開く
3. **セキュリティ**セクションまでスクロール — ブロックされたアプリのメッセージが表示される
4. **このまま開く**をクリック
5. パスワードを入力するか Touch ID で確認
6. アプリが起動します。macOS は次回以降の選択を記憶します

### Windows SmartScreen

バイナリはコード署名証明書で署名されていません。Chrome や Edge がダウンロードをブロックし、Windows SmartScreen が実行をブロックする場合があります。

**ブラウザによるダウンロードブロック：**

1. Chrome：ダウンロード警告をクリック → **保存** → **保存する**
2. Edge：ダウンロードバーの `...` をクリック → **保持** → **詳細表示** → **保持する**

**SmartScreen による実行ブロック：**

1. exe をダブルクリック — Windows が「WindowsによってPCが保護されました」と表示
2. **詳細情報**をクリック
3. **実行**をクリック

または PowerShell でブロックを解除：

```powershell
Unblock-File -Path .\4x.exe
```

### 確認

以下で確認してください：

```bash
4x --help
```

## プロジェクトの初期化

```bash
cd my-project
4x init
```

これにより `.4x/` ディレクトリが作成され、以下が含まれます：
- `settings.json` -- プロジェクト設定、ランナー定義、ロールモデルのマッピング
- `plugins/` -- ランナーの指示ファイル（CLAUDE.md、AGENTS.md、GEMINI.md など）
- ルートレベルのインポートファイル（CLAUDE.md、AGENTS.md、GEMINI.md など）

4x はプロジェクトの言語（Go、TypeScript、JavaScript、Java、Rust、Python）を自動検出し、ビルド/テスト/lint コマンドを事前設定します。

`.4x/` が既に存在する場合、`init` はエラーで終了します -- プラグインファイルを更新するには `4x sync` を使用してください。

## Feature の作成

```bash
4x new "User authentication with OAuth2"
# => Created feature: F001-user-authentication-w (User authentication with OAuth2)

4x new "Payment processing" --repo payment-service --repo shared-lib
# => Created: F002-payment-processing
```

Feature は `.4x/features/{id}.yaml` に保存されます。ID のフォーマットは `F{NNN}-{slug}`（slug は最大23文字）です。

`--repo` を使って、スコープ内のリポジトリを宣言します（マルチリポジトリプロジェクトの場合）。

```bash
4x new "Auth refactor" --id auth-refactor --desc "Refactor auth middleware" --priority 1
4x new "Batch mode" --subtask "extract:Extract logic" --depends F001
```

すべてのフラグ（`--id`、`--desc`、`--subtask`、`--rule`、`--depends`、`--priority`）については `4x new --help` または [CLI リファレンス](cli.md) を参照してください。

## ループの実行

```bash
# デフォルトのランナーで実行（通常は claude）
4x run F001

# ランナーを指定
4x run F001 --runner claude

# イテレーション回数を制限
4x run F001 --max-rounds 3

# タイムアウトを設定（秒、デフォルト: 3600）
4x run F001 --timeout 7200

# 特定のパイプラインプロファイルを使用
4x run F001 --profile quick

# LLM を呼び出さずにプロンプトをプレビュー
4x run F001 --dry-run
```

Feature ID はプレフィックスマッチに対応しています -- `4x run F001` と `4x run f001` のどちらでも動作します。

ループの流れ：**Design → Code → Review → Test → Deep Review → Accept → Pending Review**。Review で問題が見つかれば、Code が再実行されます。Test が失敗すれば、ループが反復されます（`--max-rounds` まで）。

## ステータスの確認

```bash
# 全 Feature
4x status

# 単一 Feature の詳細
4x status F001

# 未完了の Feature のみ表示
4x status --pending
```

## Feature の完了

ループ終了後、Feature は `pending-review` 状態になります -- 人間のサインオフを待っています。

```bash
# 出力を確認
cat .4x/F001/final-report.md
cat .4x/F001/commit-plan.md

# 完了としてマーク
4x done F001
```

## バージョン管理

`4x init` はランタイム成果物を除外する `.4x/.gitignore` を作成します。それ以外はコミットしてください：

| パス | 追跡 | 理由 |
|---|---|---|
| `.4x/settings.json` | **する** | プロジェクト設定 — チームで共有 |
| `.4x/features/*.yaml` | **する** | Feature 定義 |
| `.4x/learnings.json` | **する** | Feature 横断のレトロ知識ベース |
| `.4x/candidates.json` | **する** | 自動発見された Feature 候補プール |
| `.4x/plugins/` | **する** | Runner 指示ファイル |
| `.4x/run/` | **しない** | ランタイム成果物（状態、ログ、レポート）— `.gitignore` で自動除外 |

この機能より前に初期化された既存プロジェクトの場合は、手動で gitignore を追加してください：

```bash
printf 'run/\ngate-input.json\ngate-verdicts.json\nevolve-state.json\n' > .4x/.gitignore
```

## プラグインファイルのアップグレード

`4x` バイナリを更新した際は、埋め込みプラグインを再デプロイしてください：

```bash
4x sync            # 新しいファイルをデプロイ
4x sync --dry-run  # 変更のプレビューのみ
```

## 次のステップ

- [CLI リファレンス](cli.md) -- すべてのコマンドとフラグ
- [基本コンセプト](concepts.md) -- ロール、ステートマシン、プロトコルの理解
- [設定](configuration.md) -- モデル、ランナー、ロケールのカスタマイズ
