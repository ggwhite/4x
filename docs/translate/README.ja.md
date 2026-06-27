[English](../../README.md) | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md) | **日本語** | [한국어](README.ko.md) | [Español](README.es.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/ggwhite/4x.svg)](https://pkg.go.dev/github.com/ggwhite/4x)
[![Go Report Card](https://goreportcard.com/badge/github.com/ggwhite/4x)](https://goreportcard.com/report/github.com/ggwhite/4x)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg)](https://github.com/ggwhite/4x/actions/workflows/ci.yml)

<p align="center">
  <img src="../assets/4x-banner.svg" alt="4X — Design. Code. Review. Test." width="480">
</p>

<p align="center">
  <img src="../assets/demo.gif" alt="4x demo" width="720">
</p>

**4x は、ソフトウェア開発ループを4つの専門フェーズに分割するマルチロール AI 開発フレームワークです** -- Design（設計）、Code（実装）、Review（レビュー）、Test（テスト） -- それぞれ専用の AI エージェントが担当します。4X ストラテジーゲーム（eXplore, eXpand, eXploit, eXterminate）のように、異なる強みを持つ異なる役割が連携して複雑さを克服するシステムを表しています。

---

## 主な機能

| カテゴリ | ハイライト |
|---|---|
| **マルチロールループ** | Design → Code → Review → Test → Deep Review → Accept、ロール分離。Adaptive pipeline が機能の複雑さに応じて profile を選択（full / mini / quick）。 |
| **6種類の AI ランナー** | Claude Code · Codex · Gemini CLI · Antigravity · Copilot · Cursor — 統一された `.4x/` ファイルプロトコル、ロールごとに混在可能。 |
| **ダッシュボード（4x Live）** | macOS ネイティブ（Swift）+ Windows / Linux（Tauri）。リアルタイム SSE モニタリング、依存関係グラフ、ランナーログストリーミング、スクリーンショットギャラリー、設定 UI、バッチモニタリング。6言語 i18n、システム通知、メニューバー統合。 |
| **決定的ガードレール** | ステートマシン、スコープロック、ベースラインスナップショット、エビデンスベースのテストゲート、依存関係ゲート — Go CLI で強制、LLM プロンプトに依存しない。 |
| **クラッシュリカバリー** | ランナー中断 → 最後に保存された状態から自動復旧。一時的な API エラー（ネットワーク、レート制限）→ 自動バックオフリトライ。 |
| **バッチモード** | 依存関係を考慮した DAG スケジューリング、完了時の自動マージ、バッチレポート、グレースフルストップ。数十の Feature を夜間にキューイングし、翌朝レビュー。 |
| **MCP サーバー** | Model Context Protocol サーバー、MCP 互換クライアントとの統合用。 |
| **20以上の CLI コマンド** | `run`、`batch`、`live`、`doctor`、`clean`、`verify`、`mcp`、フェーズフック、ヘルスチェック、構造化ログなど。 |
| **自己進化** | 過去のランからの改善シグナルのマイニング、自動発見 Feature の充実化、evolution value gate + アンチハック、自己修正スコープガード、継続的改善ドライバー（`4x evolve`）。4x は自らの失敗から学び、自己改善を繰り返す。 |

## なぜ 4x なのか？

シングルエージェントによるコーディングは速いが脆い。一つの AI に設計・実装・レビュー・テストのすべてを同じ呼吸で、同じバイアスのまま依頼する。小さなタスクには有効だが、本格的な機能開発では破綻する。

4x はループを分割する。各ロールには専念すべき仕事、限定されたスコープがあり、他のロールの推論にアクセスできない。Designer はコードを書かない。Coder は自分の成果物を評価しない。Reviewer は設計上、敵対的である。Tester は実装前に書かれた基準に対して検証する。

その結果：本番環境に耐える機能が生まれる。

## トレードオフ

4x を選ぶということは、スピードとコストを構造と正確性と引き換えにすることを意味する。あなたのプロジェクトにそのトレードが必要かどうか、正直に判断してほしい。

### 強み

- **ロール分離がセルフレビューのバイアスを排除する。** Coder は自分の成果物を評価しない。Reviewer は設計上、敵対的である。シングルエージェントのワークフローでは同じモデルがコードを書いて承認するが、4x ではそうならない。
- **決定的なガードレール（guardrail）は AI の判断に依存しない。** スコープロック、ステートマシン、エビデンス要件 -- これらは Go で書かれた CLI によって強制され、LLM に「スコープ内に留まってください」とプロンプトするのではない。
- **ファイルベースのプロトコルにより LLM に依存しない。** Claude、Gemini、Codex を切り替えたり、ロールごとに混在させたりできる。ベンダーロックインなし、SDK 依存なし。
- **クラッシュに強い状態管理。** すべてが `.4x/` ファイルに保存される。セッションが終了しても、マシンが再起動しても -- `4x run` は中断した場所から正確に再開する。
- **人間がループ内に留まる。** `pending-review` ゲートにより、AI の成果物が完了と見なされる前に必ず人間がレビューする。AI が提案し、あなたが決定する。
- **大規模リファクタリングを制御できる。** 単一の AI セッションでは処理しきれない大きな変更 — God Object の分割、パッケージの抽出、API の移行 — を依存関係のある複数の feature に分割し、それぞれに適切な profile を指定できる。4x がフェーズ間の順序制御、レビュー、検証を担うため、単一のコンテキストウィンドウの限界を超えない。
- **バッチ（batch）モードがスケールする。** 依存関係を考慮したスケジューリングにより、数十の機能を一晩中キューに入れ、翌朝レビューできる。

### 弱み

- **トークンコストが大幅に増加する。** 各機能は最低でも4回以上の個別 LLM 呼び出しを経る。レビュー失敗でさらに倍増する。同じタスクに対してシングルエージェントの3〜10倍のトークンコストを見込むこと。[使い方のヒント](../guide/ja/usage-tips.md)でコスト見積もりを参照。
- **シンプルなタスクには遅い。** 1行のバグ修正に Designer、Reviewer、Tester は不要。フルループのオーバーヘッドは些細な変更には無駄になる。クイックフィックスにはシングルエージェントツールを使うこと。
- **セットアップコストがかかる。** `4x init`、feature YAML、settings 設定 -- 開始前に準備が必要。使い捨てのスクリプトには割に合わない。
- **ループ構造が固定的。** Design → Code → Review → Test のシーケンスは固定。ワークフローが4つのロールに合わない場合、フレームワークと戦うことになる。
- **品質はプロンプトの品質に依存する。** 曖昧な機能説明は曖昧な仕様を生み、それが誤ったコードを生む。4x は構造を追加するが、ゴミを入れればゴミが出る -- ステップが増えるだけ。

### 4x を使うべき場面

- 正確性が求められる機能（決済、認証、データパイプライン）
- 敵対的レビューが効果的な場面（セキュリティに敏感なコード）
- 機能バックログのバッチ処理
- AI 生成コードの監査証跡が必要なチーム

### 4x を使うべきでない場面

- 一回限りのクイックフィックスや探索的プロトタイピング
- 正確性よりスピードが重要なタスク
- トークン予算が厳しいプロジェクト
- 自分でコードをレビューするソロハッキングセッション

## アーキテクチャ

```
 You
  |
  v
+--------------------------------------------------+
|  4x CLI (Go)                                     |
|  Deterministic guardrails. No LLM calls.         |
|  Scope checks, protocol, state machine, batch    |
+--------+-----------------------------------------+
         |  .4x/ directory (file-based protocol)
         v
+--------------------------------------------------+
|  Runners                                         |
|  Claude Code | Codex | Gemini | Antigravity      |
|  Copilot | Cursor                                |
|  Each uses native platform capabilities          |
+--------+-----------------------------------------+
         |  SSE events
         v
+--------------------------------------------------+
|  4x Live (Dashboard)                             |
|  Multi-project real-time monitoring              |
+--------------------------------------------------+
```

**レイヤー 1 -- CLI** は決定的な処理をすべて担当する：スコープ検証、状態遷移、ベースラインスナップショット、エビデンス収集。LLM を呼び出すことはない。ガードレールは AI の判断に依存しない。

**レイヤー 2 -- ランナー（Runner）** は CLI プロトコルとあなたが選んだ AI ツールを橋渡しする。Claude Code、Codex、Gemini、Antigravity、Copilot、Cursor -- いずれも同じ `.4x/` ファイルプロトコルを使いつつ、各プラットフォーム固有の機能を活用する。

**レイヤー 3 -- Live** はマルチプロジェクトダッシュボード。AI エージェントの作業をリアルタイムで監視し、フェーズ遷移を確認し、ログをストリーミングする。REST + SSE API。

## インストール

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### バイナリダウンロード

macOS、Linux、Windows（amd64 / arm64）のビルド済みバイナリは [Releases](https://github.com/ggwhite/4x/releases) ページからダウンロードできます。

## クイックスタート

```bash
# プロジェクトで初期化
cd my-project
4x init

# Feature を作成
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

# フルループを実行
4x run F001 --runner claude

# ステータスを確認
4x status

# レビューして完了
4x done F001

# またはリアルタイムで監視
4x live -w
```

`4x run` は Design-Code-Review-Test ループを自動的に駆動する。Review で問題が見つかれば、Code が再実行される。Test が失敗すれば、ループが反復される。`--max-rounds` と `--timeout` フラグで制御を維持できる。

## 4つのロール

| ロール | 仕事 | 出力 |
|---|---|---|
| **Designer** | 要件を分析し、仕様 + 受け入れ基準を作成 | `task-brief.md`, `acceptance-criteria.md` |
| **Coder** | 仕様に従って正確に実装 | ソースコード, `coder-report.md` |
| **Reviewer** | バグと仕様違反を検出（チェックリスト + 敵対的） | `review-report.md`（判定付き） |
| **Tester** | 受け入れ基準に対してエビデンスで検証 | `test-report.md`, `verify.json` |

各ロールは**分離**されている。Coder は Reviewer の過去のフィードバックを見ない。Tester は Coder ではなく Designer が書いた基準に対して検証する。この分離が、シングルエージェントのワークフローを悩ませる盲点を防ぐ。

## ループの動作

```
Designer → Coder → Reviewer → Tester → Accept → Pending Review → Done
                      ↓           ↓                                 ↑
                   amending ←─────┘                          human sign-off
```

- **Review 失敗**（判定 FAIL または CRITICAL 所見）はコードを修正に差し戻す
- **Test 失敗**（verify が不合格）はコードを修正に差し戻す
- **エスカレーション**（仕様不一致、基準不正）は Designer に差し戻す
- **Pending review** ゲートにより、完了マーク前に必ず人間がレビューする
- **ラウンド予算**（デフォルト5）が無限ループを防止する

## 決定的なガードレール

CLI によって強制され、AI の判断には依存しない：

| ガードレール | 機能 |
|---|---|
| **スコープチェック** | 変更されたファイルが宣言済みリポジトリ内にあることを確認 |
| **ベースラインスナップショット** | コーディング前の状態を安全なロールバックのために記録 |
| **ステートマシン** | フェーズが正当な順序で進行することを保証 |
| **エビデンス要件** | Tester はコマンド出力を含む verify.json を提供する必要がある |
| **テスティングゲート** | verify.json + test-report + final-report が必要 |
| **依存関係ゲート** | 未完了の依存関係がある Feature は開始できない |

## バッチモード

```bash
4x batch plan            # 依存関係を考慮した実行計画を生成
4x batch run --runner claude  # 対象の Feature をすべて順番に実行
4x batch stop            # 現在の Feature 完了後にグレースフルシャットダウン
```

## パーミッションモデル

**4x は AI エージェントを非対話モードで実行する。** `4x init` の際に、ランナーはパーミッションプロンプトをスキップするフラグ（`--dangerously-skip-permissions`、`-y`、`approval: full-auto`）付きで設定され、ループが自律的に動作する。

CLI の決定的なガードレール（スコープロック、ベースラインスナップショット、ステートマシン）が安全境界を提供する。

**自律的な AI エージェント実行に問題がないプロジェクトでのみ 4x を使用すること。**

## ドキュメント

| ドキュメント | 説明 |
|---|---|
| **[ユーザーガイド](../guide/ja/)** | 完全な使用方法ドキュメント |
| [はじめに](../guide/ja/getting-started.md) | インストールと初回実行 |
| [CLI リファレンス](../guide/ja/cli.md) | すべてのコマンドとフラグ |
| [基本コンセプト](../guide/ja/concepts.md) | ロール、ステートマシン、プロトコル、ガードレール |
| [設定](../guide/ja/configuration.md) | Settings、モデル、ロケール、ランナー |
| [ランナー & プラグイン](../guide/ja/runners.md) | サポートされるランナーとプラグイン規約 |
| [ダッシュボード](../guide/ja/dashboard.md) | 4x Live マルチプロジェクトダッシュボード |
| [バッチモード](../guide/ja/batch.md) | 依存関係を考慮したバッチ実行 |

## プロジェクト構造

```
4x/
  cmd/4x/              CLI entry point (Cobra)
  internal/
    protocol/           .4x/ file format, workspace, types
    state/              State machine (phase transitions)
    guard/              Guardrail checks (scope, baseline, evidence)
    batch/              Dependency DAG, batch scheduler
    runner/             Subprocess runner interface
    server/             SSE + REST server for Live dashboard
  plugins/
    claude-code/        Claude Code skill + workflow
    codex/              Codex runner instructions
    gemini/             Gemini runner instructions
    agy/                Antigravity runner instructions
    copilot/            Copilot runner instructions + workflow
    cursor/             Cursor rules
    embed.go            go:embed plugin files into binary
  dashboard/
    macos/              Swift native app (planned)
  docs/
    guide/              User documentation
    architecture/       System-level design docs
    design/             Mechanism design docs
    reference/          Plugin contract
```

## FAQ

**Q: 4x は LLM API を直接呼び出しますか？**
いいえ。CLI は LLM 依存のない純粋な Go で書かれています。ランナーが各プラットフォーム固有の機能を使ってすべての AI 対話を処理します。

**Q: ロールごとに異なる LLM を使えますか？**
はい。`.4x/settings.json` でロールごとのモデルを設定できます。設計に Claude、実装に Gemini を使用する -- いずれも同じ `.4x/` ファイルを読み書きします。

**Q: Devin / SWE-agent / OpenHands とはどう違いますか？**
それらはすべてを一回で実行する自律型エージェントです。4x は決定的なガードレールを備えたマルチロール協調を構造化する *フレームワーク* です。単一の自律型エージェントというよりも、AI のための CI パイプラインに近いです。

## 開発の経緯

4x は、大規模プラットフォーム刷新プロジェクトで 60 以上の機能を出荷した DCT（Designer-Coder-Tester）という本番システムの中から生まれました。生き残ったパターン -- ロール分離、ファイルベースプロトコル、決定的なスコープチェック、エビデンスベースのテスト -- が 4x になりました。生き残らなかった部分 -- LLM 固有のハック、共有コンテキストの前提、信頼ベースのガードレール -- は意図的に除外されました。

## コントリビュート

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
go build ./cmd/4x
go test ./...
```

## ライセンス

[MIT](LICENSE)

---

<p align="center">
  <strong>AI が正しいコードを書くことを祈るのはやめよう。検証しよう。</strong>
</p>
