# はじめに

## インストール

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

Go 1.26+ が必要です。以下で確認してください：

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
- `plugins/` -- ランナーの指示ファイル（SKILL.md、AGENTS.md など）
- ルートレベルのインポートファイル（CLAUDE.md、AGENTS.md、GEMINI.md など）

4x はプロジェクトの言語（Go、TypeScript、Java、Rust、Python）を自動検出し、ビルド/テスト/lint コマンドを事前設定します。

`.4x/` が既に存在する場合、`init` はエラーで終了します -- プラグインファイルを更新するには `4x upgrade` を使用してください。

## Feature の作成

```bash
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

4x new "Payment processing" --repo payment-service --repo shared-lib
# => Created: F002-payment-processing
```

Feature は `.4x/features/{id}.yaml` に保存されます。ID のフォーマットは `F{NNN}-{slug}`（slug は最大23文字）です。

`--repo` を使って、スコープ内のリポジトリを宣言します（マルチリポジトリプロジェクトの場合）。

## ループの実行

```bash
# デフォルトのランナーで実行（通常は claude）
4x run F001

# ランナーを指定
4x run F001 --runner claude

# イテレーション回数を制限
4x run F001 --max-rounds 3

# タイムアウトを設定（秒）
4x run F001 --timeout 7200

# LLM を呼び出さずにプロンプトをプレビュー
4x run F001 --dry-run
```

Feature ID はプレフィックスマッチに対応しています -- `4x run F001` と `4x run f001` のどちらでも動作します。

ループの流れ：**Design → Code → Review → Test → Accept → Pending Review**。Review で問題が見つかれば、Code が再実行されます。Test が失敗すれば、ループが反復されます（`--max-rounds` まで）。

## ステータスの確認

```bash
# 全 Feature
4x status

# 単一 Feature の詳細
4x status F001

# pending-review のみフィルタ
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

## プラグインファイルのアップグレード

`4x` バイナリを更新した際は、埋め込みプラグインを再デプロイしてください：

```bash
4x upgrade            # 新しいファイルをデプロイ
4x upgrade --dry-run  # 変更のプレビューのみ
```

## 次のステップ

- [CLI リファレンス](cli.md) -- すべてのコマンドとフラグ
- [基本コンセプト](concepts.md) -- ロール、ステートマシン、プロトコルの理解
- [設定](configuration.md) -- モデル、ランナー、ロケールのカスタマイズ
