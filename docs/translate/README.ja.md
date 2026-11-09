<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/banner-dark.svg">
  <img src="../assets/banner-light.svg" alt="4X：設計、実装、レビュー、テスト。" width="100%">
</picture>

**Design・Code・Review・Test を互いに隔離された AI ロールに分割し、決定的なガードレールで制御するエージェント型開発ループ。**

<p>
  <a href="https://github.com/ggwhite/4x/actions/workflows/ci.yml"><img src="https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/ggwhite/4x/releases"><img src="https://img.shields.io/github/v/release/ggwhite/4x" alt="最新リリース"></a>
  <a href="https://pkg.go.dev/github.com/ggwhite/4x"><img src="https://pkg.go.dev/badge/github.com/ggwhite/4x.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/ggwhite/4x"><img src="https://goreportcard.com/badge/github.com/ggwhite/4x" alt="Go Report Card"></a>
  <a href="../../LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="ライセンス：MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey" alt="プラットフォーム：macOS、Linux、Windows">
</p>

[インストール](#インストール) · [クイックスタート](#クイックスタート) · [機能](#機能) · [ループの仕組み](#ループの仕組み) · [ガードレール](#決定的なガードレール) · [アーキテクチャ](#アーキテクチャ) · [ドキュメント](#ドキュメント) · [FAQ](#faq)

[English](../../README.md) | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md) | **日本語** | [한국어](README.ko.md) | [Español](README.es.md)

</div>

---

<div align="center">
  <a href="../assets/tour.mp4"><img src="../assets/tour.webp" alt="1 回の実際の実行を早送りした 4x ツアー：(1) 4x init が既存の Go プロジェクト greet をセットアップし、go build と go test のコマンドを検出する。(2) 4x new が feature F001 Greet by name を作成する。(3) 4x run F001 が Claude Code を Designer、Coder、Reviewer、Tester の順に動かす。Tester のチェックが verify 許可リストでブロックされたため feature は Designer にエスカレーションされ、Designer がチェックを書き直し、2 ラウンド目は Accept と pending-review まで通過して合計コストが表示される。(4) 4x status が pending-review の feature を一覧表示し、4x done F001 で完了にすると、go run . -name Ada が hello, Ada を出力する" width="900"></a>
  <br>
  <sub>▶ <a href="../assets/tour.mp4">ツアー全編を見る</a> · Claude Code による 1 回の実際の実行（約 4 分、$2）、エージェントの待ち時間は短縮</sub>
</div>

## なぜ 4x か

シングルエージェントのコーディングは速いが脆い。1 つの AI に設計・実装・レビュー・テストを、同じ流れのまま同じバイアスで任せることになる。小さなタスクならそれで回るが、本格的な機能開発では破綻する。

4x はこのループを分割する。各ロールは担当が明確で、範囲が限定され、他のロールの推論にはアクセスできない。Designer はコードを書かず、Coder は自分の成果物を評価せず、Reviewer は設計上あえて敵対的で、Tester は実装前に書かれた基準に対して検証する。次に何が起きてよいかを決めるのは LLM ではなく Go CLI だ。4X ストラテジーゲーム（eXplore、eXpand、eXploit、eXterminate）のように、それぞれ異なる強みを持つロールが 1 つのゴールに向かって収束する。本番環境で通用する機能を作るというゴールだ。

## 機能

<table>
<tr>
<td width="42%" valign="middle">

### 隔離された 4 つのロール

**Designer → Coder → Reviewer → Tester → Accept**。各ロールは独立した runner プロセスで、`.4x/` 内のファイルを介してのみやり取りする。Reviewer の **FAIL** や検証失敗でコードは **amending** に送られ、仕様の誤りは Designer へ**エスカレーション**される。パイプラインの **profile**（`full`、`normal`、`lite`、`quick` または独自定義）で実行するフェーズを選べる。

</td>
<td><img src="../assets/feature-loop.webp" alt="デモ実行中の 4x Live の Messages タブ：task-brief.md と acceptance-criteria.md に関する Designer のメッセージが表示され、続いて Coder、Reviewer、Tester のレポートが順に現れる。それぞれにモデルとコストが付く。Tester がエスカレーションし、Designer が brief を書き直し、2 回目の Coder、Reviewer、Tester を経て Acceptor が feature を Review でクローズする"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### 決定的なガードレール

ガードレールは**プロンプトではなく Go のコード**だ。`4x check` は **scope**、**baseline** スナップショット、必須ファイル、**verify のエビデンス**を検証する。**ステートマシン**は不正なフェーズ遷移を拒否し、`4x verify` はプロジェクトの**許可リスト**にあるコマンドしか実行しない。runner が言葉巧みにすり抜けることはできない。

</td>
<td><img src="../assets/feature-guardrails.webp" alt="ターミナル：4x check F001 が完了したデモ feature のガードレールチェックがすべて通過したと報告する。4x transition F001 --to coding --json は invalid transition: pending-review to coding で拒否される。4x verify F001 が test-strategy.yaml の go build、go vet、go test チェックを受け入れ基準ごとに実行し PASSED を出力する"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### 4x Live ダッシュボード

`4x live` は REST + **SSE** で**マルチプロジェクトダッシュボード**を提供する。フェーズの変化に合わせて feature カードが Todo から Running、Review へと移動し、feature ごとのラウンド数、runner、コストが表示される。同じ Web UI がバイナリに埋め込まれており、**macOS（Swift）** と **Windows / Linux（Tauri）** のアプリにもラップされている。

</td>
<td><img src="../assets/feature-live.webp" alt="デモ実行中の 4x Live プロジェクト概要：F001 Greet by name のカードが Todo から Running に移り、フェーズラベルが designing、coding、reviewing、testing と切り替わる。Running カウンター、ステータスのドーナツグラフ、Currently running パネルもそれに追従する。最後にカードは Mark Done ボタン付きで Review に留まり、Pending review パネルにも表示される"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### ロール別・ラウンド別のコスト

すべての runner 呼び出しが、モデル、所要時間、**USD コストまたはトークン数**とともに記録される。`4x cost` は**ロールごと**に集計し、`--by-round` は**リトライ**にどれだけ費やしたかを示し、`--feature` は 1 つの feature をラウンドごとに分解する。レビュー失敗が実際にいくらかかったのかが分かる。

</td>
<td><img src="../assets/feature-cost.webp" alt="ターミナル：4x cost がデモプロジェクトのロール別（designer、coder、reviewer、tester、acceptor）支出を、呼び出し回数、合計、平均、割合とともに表示する。続いて 4x cost --feature F001-greet-by-name が同じ呼び出しをラウンドとロールごとに一覧表示する"></td>
</tr>
</table>

### その他の機能

- **7 つの runner**：Claude Code、Codex、Gemini CLI、Antigravity、Copilot、Cursor、OpenCode が同じ `.4x/` ファイルプロトコルを共有する。ロールごと、フェーズごとに組み合わせられる（`--phase-override`）。
- **適応型パイプライン**：profile アドバイザーが feature の複雑さから `full` / `normal` / `quick` を提案する。ロールごとのモデルは `strong` / `fast` の 2 段階で指定する。
- **クラッシュリカバリー**：runner がクラッシュしても最後に保存した状態から再開する。一時的な API エラー（ネットワーク、レート制限）はバックオフ付きでリトライする。
- **人による承認**：すべての feature は `pending-review` で止まる。`4x done` で完了にしてブランチをマージする（`issue_tracker` を使う場合は PR/MR を作成する）。
- **Issue ファーストの MR フロー**：オプションの `issue_tracker` モード。`4x new` が issue を作成またはリンクし、`4x done` はローカルでマージする代わりにブランチを push して PR/MR を作成する。GitHub か GitLab（セルフホスト含む）かは各リポジトリの remote から判定する。
- **依存関係**：`4x new --depends F001` で、依存先が完了するまで feature の開始を止める。ダッシュボードには依存グラフが描画される。
- **自己進化**：`4x mine` / `4x gate` / `4x evolve` が過去の実行から失敗シグナルを掘り起こし、候補を価値ゲートでふるい分け、改善用の feature をキューに積む。振り返りの学び（`4x learn`）は以降のロールプロンプトに注入される。
- **MCP サーバー**と **Claude Code スキル**（`4x-audit`、`4x-autopilot`）、詳細は後述。
- **30 以上の CLI コマンド**：`run`、`status`、`check`、`verify`、`retry`、`cost`、`live`、`doctor`、`clean`、`mcp`、`skills`、フェーズフック、構造化ログなど。[CLI リファレンス](../guide/ja/cli.md)を参照。

## インストール

### Homebrew（macOS / Linux）

```bash
brew install ggwhite/tap/fourx
```

### Go install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### シェルスクリプト

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### バイナリをダウンロード

macOS、Linux、Windows（amd64 / arm64）向けのビルド済みバイナリは [Releases](https://github.com/ggwhite/4x/releases) ページにある。

<details>
<summary><b>チェックサムの検証</b>（手動ダウンロードの場合）</summary>

シェルスクリプトはダウンロードしたファイルのチェックサムを自動で検証し、失敗すれば中断する。[Releases](https://github.com/ggwhite/4x/releases) ページからアーカイブやバイナリを手動でダウンロードした場合は、展開する前に自分で検証すること。そのリリースの `checksums.txt` を同じディレクトリにダウンロードし、次のいずれかを実行する：

```bash
# Linux (and macOS with coreutils)
sha256sum --check --ignore-missing checksums.txt

# macOS (default)
shasum -a 256 --ignore-missing --check checksums.txt
```

チェックサムが `OK` と表示されてから、バイナリを展開して実行すること。

</details>

## クイックスタート

```bash
# Initialize in your project
cd my-project
4x init

# Create a feature
4x new "User authentication with OAuth2"
# => Created feature: F001-user-authentication (User authentication with OAuth2)

# Run the full loop
4x run F001 --runner claude

# Check status
4x status

# Review and complete
4x done F001

# Or watch it live
4x live -w
```

`4x run` は Design-Code-Review-Test ループを自動で回す。Review で問題が見つかれば Code がもう一度走り、Test が失敗すればループが繰り返される。`--max-rounds`、`--profile`、`--timeout` で制御はあなたの手に残る。

## 4 つのロール

| ロール | 仕事 | 出力 |
|---|---|---|
| **Designer** | 要件を分析し、仕様と受け入れ基準を作成する | `task-brief.md`、`acceptance-criteria.md`、`test-strategy.yaml` |
| **Coder** | 仕様どおりに正確に実装する | ソースコード、`coder-report.md` |
| **Reviewer** | バグと仕様違反を検出する（チェックリスト + 敵対的レビュー） | 判定付きの `review-report.md` |
| **Tester** | 受け入れ基準に対してエビデンス付きで検証する | `test-report.md`、`verify.json` |

各ロールは**隔離**されている。Coder は Reviewer の過去のフィードバックを見ない。Tester は Coder ではなく Designer が書いた基準に対して検証する。この分離が、シングルエージェントのワークフローにつきまとう盲点を防ぐ。`full` profile では、4 つのコアロールに加えて Design Reviewer、多角的な Deep Reviewer、Fixer、Acceptor が入る。

## ループの仕組み

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/loop-dark.svg">
  <img src="../assets/loop-light.svg" alt="Designer、Coder、Reviewer、Tester、Accept、Pending Review、Done が横一列に並ぶ。Reviewer の FAIL や検証失敗でコードは amending に送られ、Reviewer に戻る。エスカレーションは Designer に戻る。Done の前に人が 4x done で承認する。" width="100%">
</picture>

- **Review 失敗**（判定 FAIL または CRITICAL の指摘）でコードは amending に差し戻される
- **Test 失敗**（verify が不合格）でコードは amending に差し戻される
- **エスカレーション**（仕様の不一致、基準の誤り）は Designer に戻される
- **Pending review** ゲートにより、完了にする前に必ず人がレビューする
- **ラウンド上限**（デフォルト 5）が無限ループを防ぐ

正当な遷移は [`internal/state/machine.go`](../../internal/state/machine.go) に定義されている。ステートマシンの全体は[コアコンセプト](../guide/ja/concepts.md)を参照。

## 決定的なガードレール

AI の判断ではなく CLI が強制する：

| ガードレール | 役割 |
|---|---|
| **スコープチェック** | 変更ファイルは宣言されたリポジトリ内になければならない |
| **ベースラインスナップショット** | 安全にロールバックできるよう、コーディング前の状態を記録する |
| **ステートマシン** | フェーズは正当な順序でしか進めない |
| **エビデンス要件** | Tester はコマンド出力を含む `verify.json` を提出しなければならない |
| **テストゲート** | `verify.json` + テストレポート + 最終レポートが必須 |
| **verify 許可リスト** | `4x verify` は `verify_command_allowlist` にあるコマンドしか実行しない |
| **依存関係ゲート** | 依存先が未完了の feature は開始できない |

## アーキテクチャ

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/architecture-dark.svg">
  <img src="../assets/architecture-light.svg" alt="あなたが 4x CLI（Go）を操作する。CLI は決定的なガードレールで LLM を呼ばず、スコープチェック、プロトコル、ステートマシン、依存関係ゲートを担う。作業は .4x/ ディレクトリのファイルプロトコルを通じて runner（Claude Code、Codex、Gemini、Antigravity、Copilot、Cursor、OpenCode）に渡される。4x Live は SSE イベントを受け取り、マルチプロジェクトのリアルタイム監視を表示する。" width="100%">
</picture>

**レイヤー 1：CLI** は決定的な処理をすべて担う。スコープ検証、状態遷移、ベースラインスナップショット、エビデンス収集。LLM を一切呼ばないので、ガードレールは AI の判断に依存しない。

**レイヤー 2：Runner** は CLI のプロトコルと好みの AI ツールを橋渡しする。どの runner も同じ `.4x/` ファイルプロトコルを話すが、それぞれのプラットフォーム固有の機能を使う。

**レイヤー 3：Live** はマルチプロジェクトダッシュボードだ。エージェントの作業をリアルタイムで見て、フェーズ遷移を確認し、ログをストリーミングできる。REST + SSE API。

## Autopilot

複数の feature をキューに積み（順序は `--depends` で指定）、あとは Claude Code スキル `4x-autopilot` に任せる。次に実行可能な feature を選び、実行し、承認し、マージして次へ進む。すべて無人で行う。これは以前の `4x batch` コマンドを置き換えるものだ。

## MCP サーバー

`4x mcp` は stdio 上で Model Context Protocol サーバーを起動する。CLI を `4x_status`、`4x_new`、`4x_run`、`4x_stop`、`4x_check`、`4x_verify`、`4x_done`、`4x_doctor`、`4x_learn_*` などのツールとして MCP クライアントに公開する。

```bash
4x mcp
```

## Claude Code スキル

4x には、Claude Code のセッションからパイプラインを操作するためのオプションの [Claude Code Skills](https://docs.anthropic.com/en/docs/claude-code) が 2 つ付属する。[`npx skills`](https://github.com/vercel-labs/skills) でインストールでき、このリポジトリを clone する必要はない：

```bash
npx skills add ggwhite/4x --skill 4x-audit       # scan past run artifacts, report + optionally file gap features
npx skills add ggwhite/4x --skill 4x-autopilot   # drive the full pick → run → merge → next loop unattended
npx skills add ggwhite/4x --all                  # install both
```

- **`4x-audit`**：discovered-feature-gaps、エスカレーション／レビューのレポート、繰り返し現れる learnings のパターンをスキャンし、カテゴリ別の HTML レポートを生成する。feature YAML を一括作成することもできる。
- **`4x-autopilot`**：4x の状態をポーリングし、feature をライフサイクル全体（design → code → review → test → merge）にわたって確認なしで進める。マージも含む。**オーナー専用**：共有リポジトリで使う前に、スキル内の警告を必ず読むこと。

このリポジトリを clone している場合は、代わりに `4x skills install <name>` でスキルを `~/.claude/skills/` にシンボリックリンクできる。

## 権限モデル

> [!WARNING]
> **4x は AI エージェントを非対話モードで実行する。** AI エージェントの自律実行を許容できるプロジェクトでのみ 4x を使うこと。

<details>
<summary>これが意味すること</summary>

`4x init` の際、ループが自律的に回るよう、runner には権限確認をスキップするフラグ（`--dangerously-skip-permissions`、`-y`、`--yolo`）が設定される。

安全境界は CLI の決定的なガードレール（スコープロック、ベースラインスナップショット、ステートマシン、verify 許可リスト）が担う。runner を起動する前に、4x は機密性の高い環境変数（`*_TOKEN`、`*_KEY`、`*_SECRET`、`AWS_*` など）を環境から除外する。詳しくは [Runner とプラグイン](../guide/ja/runners.md)を参照。

</details>

## トレードオフ

4x を選ぶことは、速度とコストを構造と正確性に引き換えることだ。自分のプロジェクトにそのトレードオフが必要かどうか、正直に見極めてほしい。

<details>
<summary><b>強み、弱み、そして 4x を使うべき（でない）とき</b></summary>

### 強み

- **ロールの隔離がセルフレビューのバイアスをなくす。** Coder は自分の成果物を評価しない。Reviewer は設計上あえて敵対的だ。シングルエージェントのワークフローでは同じモデルがコードを書いて承認するが、4x はそうしない。
- **決定的なガードレールは AI の判断に依存しない。** スコープロック、ステートマシン、エビデンス要件。これらは LLM に「スコープ内に留まってください」とプロンプトでお願いするのではなく、Go で書かれた CLI が強制する。
- **ファイルベースのプロトコルで LLM に依存しない。** Claude、Gemini、Codex を切り替えたり、ロールごとに組み合わせたりできる。ベンダーロックインも SDK への依存もない。
- **クラッシュに強い状態管理。** すべては `.4x/` のファイルにある。セッションが落ちても、マシンが再起動しても、`4x run` は止まった場所から正確に再開する。
- **人がループの中に残る。** `pending-review` ゲートにより、AI の成果物は完了にする前に必ず人がレビューする。AI が提案し、あなたが決める。
- **大規模リファクタリングを手なずける。** 1 回の AI セッションには大きすぎる変更（ゴッドオブジェクトの分割、パッケージの抽出、API の移行）を、適切な profile を持つ依存関係付きの feature に分解できる。1 つのコンテキストウィンドウでは抱えきれない、複数フェーズにわたる順序付け、レビュー、検証を 4x が引き受ける。
- **バックログ規模まで拡張できる。** 依存関係と autopilot スキルを組み合わせれば、何十もの feature を夜のうちにキューに積み、朝にまとめてレビューできる。

### 弱み

- **トークンコストが大幅に高い。** 1 つの feature は最低でも 4 回以上の独立した LLM 呼び出しを経る。レビューが失敗すればそれが倍になる。同じタスクでシングルエージェント方式の 3〜10 倍のトークンコストを見込んでおくこと。コストの見積もりは[使い方のヒント](../guide/ja/usage-tips.md)を参照。
- **単純なタスクでは遅い。** 1 行のバグ修正に Designer、Reviewer、Tester は要らない。ささいな変更ではループ全体のオーバーヘッドが無駄になる。簡単な修正にはシングルエージェントのツールを使うこと。
- **セットアップのコスト。** `4x init`、feature YAML、設定ファイル。始める前に踏むべき手順がある。使い捨てのスクリプトには見合わない。
- **ループ構造が固定。** Design → Code → Review → Test の順序は固定されている。ワークフローが 4 つのロールに収まらなければ、フレームワークを使うどころか戦うことになる。
- **品質はプロンプトの質で決まる。** あいまいな feature の説明はあいまいな仕様を生み、それが間違ったコードを生む。4x は構造を加えるが、ゴミを入れればゴミが出ることに変わりはない。手順が増えるだけだ。

### 4x を使うべきとき

- 正しさが求められる機能（決済、認証、データパイプライン）
- 敵対的レビューが効く作業（セキュリティに敏感なコード）
- feature のバックログを片付けるとき
- AI が生成したコードの監査証跡がほしいチーム

### 4x を使うべきでないとき

- その場限りの簡単な修正や探索的なプロトタイピング
- 正確さよりスピードが重要なタスク
- トークン予算が厳しいプロジェクト
- どのみち自分でコードをレビューする、一人でのハッキング

</details>

## ドキュメント

| ドキュメント | 説明 |
|---|---|
| **[ユーザーガイド](../guide/ja/)** | 使い方の完全なドキュメント |
| [はじめに](../guide/ja/getting-started.md) | インストールと初回実行 |
| [CLI リファレンス](../guide/ja/cli.md) | すべてのコマンドとフラグ |
| [コアコンセプト](../guide/ja/concepts.md) | ロール、ステートマシン、プロトコル、ガードレール |
| [設定](../guide/ja/configuration.md) | 設定、モデル、ロケール、runner |
| [Runner とプラグイン](../guide/ja/runners.md) | 対応 runner とプラグイン契約 |
| [ダッシュボード](../guide/ja/dashboard.md) | 4x Live マルチプロジェクトダッシュボード |
| [使い方のヒント](../guide/ja/usage-tips.md) | モデル選択、コスト、トラブルシューティング |

<details>
<summary><b>プロジェクト構成</b></summary>

```
4x/
  cmd/4x/              CLI entry point (Cobra), one file per subcommand
  internal/
    protocol/           .4x/ file format, workspace, types
    state/              State machine (phase transitions)
    guard/              Guardrail checks (scope, baseline, evidence)
    orchestrator/       Run loop orchestration
    runner/             Subprocess runner interface
    server/             SSE + REST server for Live dashboard
    mcp/                MCP server (stdio)
  plugins/
    claude-code/        Claude Code runner instructions
    codex/              Codex runner instructions
    gemini/             Gemini runner instructions
    agy/                Antigravity runner instructions
    copilot/            Copilot runner instructions
    cursor/             Cursor rules
    opencode/           OpenCode runner instructions
    embed.go            go:embed plugin files into binary
  dashboard/
    web/                Dashboard UI, embedded into the binary
    macos/              Swift native app
    tauri/              Windows / Linux app
  skills/               4x-audit, 4x-autopilot Claude Code skills
  templates/            Role prompt templates
  docs/
    guide/              User documentation
    architecture/       System-level design docs
    design/             Mechanism design docs
    reference/          Plugin contract
```

</details>

## FAQ

<details>
<summary><b>4x は LLM の API を直接呼び出す？</b></summary>

いいえ。CLI は純粋な Go で、LLM への依存はゼロだ。AI とのやり取りはすべて、runner がそれぞれのプラットフォーム固有の機能を使って行う。

</details>

<details>
<summary><b>ロールごとに別の LLM を使える？</b></summary>

使える。`.4x/settings.json` でロールごとのモデルを設定する。Design には Claude、Code には Gemini、といった具合だ。どれも同じ `.4x/` ファイルを読む。

</details>

<details>
<summary><b>Devin / SWE-agent / OpenHands とは何が違う？</b></summary>

それらはすべてを一度にこなす自律型エージェントだ。4x は、決定的なガードレールでマルチロールの協調を構造化する*フレームワーク*である。単一の自律型エージェントというより、AI のための CI パイプラインに近い。

</details>

## 開発

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
make build      # bin/4x
make test       # go test -race ./...
make lint       # go vet + gofmt + golangci-lint
```

ワークフロー全体は [CONTRIBUTING.md](../../CONTRIBUTING.md) を参照。

<details>
<summary><b>README のクリップと図を録り直す</b></summary>

クリップはすべて、ソースからビルドした実際のバイナリを使い、`/tmp/4xdemo` 配下の使い捨てプロジェクトと `HOME` で録画している。録画はヘッドレスで行う。ターミナルは [`vhs`](https://github.com/charmbracelet/vhs)、ダッシュボードは Python Playwright（ヘッドレス Chromium）。`vhs`、`ffmpeg`、`img2webp`（libwebp）、Python の `playwright` が必要で、ツアーにはログイン済みの `claude` CLI も必要だ。

```bash
docs/assets/record/render.sh tour      # fresh /tmp/4xdemo, ONE real 4x run (about $2 of tokens), all clips
docs/assets/record/render.sh recut     # re-encode the clips from the last run's frames, no agent run
python3 docs/assets/diagrams/gen.py    # architecture / loop SVGs (light + dark)
python3 docs/assets/diagrams/snapshot.py /tmp/svg docs/assets/*-light.svg   # check frames + reduced motion
```

`render.sh tour` は [`setup-demo.sh`](../assets/record/setup-demo.sh)（ソースから 4x をビルドし、小さな Go プロジェクト「greet」を作る）を実行し、[`capture-live.py`](../assets/record/capture-live.py) が `4x live` のスクリーンショットを撮る間に [`tour.tape`](../assets/record/tour.tape) を録画し、各機能のクリップを録り、最後に [`tour-end.tape`](../assets/record/tour-end.tape) で feature を承認する。[`retime.py`](../assets/record/retime.py) が長いエージェントの待ち時間をカットし、`img2webp` で WebP にエンコードする。Claude Code runner は、ユーザー設定、フック、プラグイン、MCP サーバーをスキップするラッパー経由で呼び出される。

新しいクリップをコミットする前に、すべてのフレームを OCR し（`swiftc docs/assets/record/ocr.swift -o /tmp/ocr` のあと `magick clip.webp -coalesce f-%03d.png` と `/tmp/ocr f-*.png`）、テキストにユーザー名、ホスト名、メールアドレス、トークンが含まれていないか grep すること。

</details>

## 誕生の経緯

4x は、大規模なプラットフォーム刷新で 60 以上の機能を出荷した DCT（Designer-Coder-Tester）という本番システムの中から生まれた。生き残ったパターン（ロールの隔離、ファイルベースのプロトコル、決定的なスコープチェック、エビデンスに基づくテスト）が 4x になった。生き残らなかった部分（特定の LLM 向けのハック、コンテキスト共有の前提、信頼頼みのガードレール）は意図的に外した。

## ライセンス

[MIT](../../LICENSE)

---

<p align="center">
  <strong>AI が正しいコードを書くことを祈るのはやめよう。検証を始めよう。</strong>
</p>
