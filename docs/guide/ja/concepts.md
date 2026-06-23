# 基本コンセプト

## 4つのロール

| ロール | 責務 | 入力 | 出力 | 禁止事項 |
|---|---|---|---|---|
| **Designer** | 要件を分析し、仕様を作成し、受け入れ基準とテスト戦略を定義 | Feature の説明、コードベース | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | ソースコードの変更 |
| **Coder** | 仕様の通りに実装 | `task-brief.md`、以前のテスト/レビューレポート | ソースコード, `coder-report.md` | 受け入れ基準やテストスクリプトの変更 |
| **Reviewer** | バグ、セキュリティ問題、仕様違反を検出 | Diff、仕様、Coder レポート、プロジェクトルール | `review-report.md` | ソースコードの変更 |
| **Tester** | エビデンスに基づいて受け入れ基準を検証 | 受け入れ基準、Coder レポート、テスト戦略 | テストスクリプト, `test-report.md`, `verify.json`, `final-report.md` | ソースコードの変更 |

各ロールは**分離**されています -- Coder は実装中に以前のレビューフィードバックを見ることはありません。Tester は Coder ではなく Designer が書いた基準に対して検証します。

### 追加のループロール

ループの後半で動作する2つの追加ロール：

| ロール | フェーズ | 責務 |
|---|---|---|
| **Deep Reviewer** | `deep-reviewing` | 敵対的レビュー -- diff 全体から最悪のバグを見つける |
| **Acceptor** | `accepting` | 未解決の課題を集約し、人間のレビュー用に `final-report.md` を作成 |

Acceptor は独自のモデル設定（`roles.acceptor.model`）を使用します（Designer とは別）。すべてのラウンドのレポートを丸ごと読み直すのではなく、最終ラウンドの review/test/deep-review レポートとエスカレーションを読み、未解決の課題を抽出します。

### パイプラインプロファイル

**パイプラインプロファイル**は、特定の Feature に対してどのロールを実行するかを選択します。シンプルな作業では常に全6ロールパイプラインを実行する代わりにロールをスキップできます。組み込みプロファイル：

| プロファイル | ロール |
|---|---|
| `full` | designer、coder、reviewer、tester、deep-reviewer、acceptor |
| `normal` | coder、reviewer、tester、acceptor |
| `quick` | coder、reviewer |

`coder` は常に必須です。`profiles` が設定されている場合、Feature の優先度に基づいてプロファイルが自動選択されます（最高優先度 → `full`、次に `normal`、次に `quick`）。`--profile` で選択を上書きできます。アクティブなプロファイルに含まれないロールはスキップされ、ランナーを起動せずに同じ有効な状態遷移エッジに沿って進みます。設定の詳細は [設定](configuration.md) の `profiles`、`parallel_review_test`、`coder_model` を参照してください。

### Review：2つのフェーズ

1. **チェックリストレビュー**（標準モデル） -- プロジェクトのハードルール（セキュリティ、並行性、エラーハンドリング、スタイル）に対してチェック
2. **敵対的レビュー**（ディープモデル） -- 「この差分に隠れている最悪のバグは何か？」所見は重大度で評価。

### Deep Review の自己修復

Deep Reviewer がブロッキングの問題を検出すると、`deep-reviewing` フェーズ内で**その場で修復**します。`amending → reviewing → testing` の全工程に差し戻す代わりの手段です。Reviewer と Tester は Deep Review の前に既にパスしているため、高コストなチェーン全体（特にディープモデル）を再実行するのは無駄です。

同一フェーズ内でループは2つのスコープ付きサブロールを生成し、レポートが合格するか上限に達するまで繰り返します：

| サブロール | モデル | 読み取り | 書き込み | スコープ |
|---|---|---|---|---|
| **mini-coder** | coder モデル | `deep-review-report.md` の `## Issues` のみ（`task-brief.md` は読まない） | ソースコード、`coder-report.md` | Deep Reviewer が指摘した問題のみ |
| **re-verifier** | reviewer モデル | 以前の問題 + 今回の mini-coder の diff | `deep-reverify-{n}.md`、`deep-review-report.md` の `## Verdict` を更新 | 旧問題の修正確認と新 diff にバグがないか検証 |

フェーズは `deep-reviewing` のまま維持されます（サブロールはステートマシンのフェーズではありません）。re-verifier がクリーンな PASS を確認すると、ループは `accepting` に進みます。ループは最大 `roles.deep-reviewer.max_fix_rounds` 回（デフォルト 2）実行されます。mini-coder が Feature スコープ外のファイルを編集した場合、または上限到達時に依然として失敗している場合、Feature は FAIL レポートを保持したまま `needs-attention` にエスカレーションされます。

### 並列 Deep Review

Deep Review は11の異なるアングル（正確性、品質、規約、履歴、フィードバック等）をカバーします。`roles.deep-reviewer.parallel_reviewers` が1より大きい場合、1つのエージェントに全11アングルを担当させる代わりに、複数のフォーカスされたサブレビュアーにアングルをファンアウトします。これは `/code-review` が次元ごとにレビューを分割するのと同じ手法で、各エージェントのコンテキスト負荷とアテンションドリフトを軽減します。

ファンアウトは完全に 4x CLI が駆動します（LLM 自体のサブエージェントやツール機能には依存しません）。`deep-reviewing` フェーズは単一フェーズのまま：

| サブロール | モデル | 読み取り | 書き込み |
|---|---|---|---|
| **sub-reviewer** (xN) | ディープモデル | diff + 割り当てられたアングルサブセット | `deep-review-partial-{i}.md` |
| **synthesizer** | ディープモデル | すべての部分レポートの全内容 | `deep-review-report.md` |

アングルは均等かつ重複なく分割されます：デフォルトの `parallel_reviewers: 3` では `[1-4]`、`[5-8]`、`[9-11]`（正確性 / 品質+規約 / 履歴+フィードバック）となります。`roles.deep-reviewer.angles_per_reviewer` でグループサイズを明示的に固定できます。未設定の場合は自動 `ceil(11/N)` バランシングです。N個のサブレビュアーが並列実行された後、単一の synthesizer が重複排除、コンフリクト裁定、信頼度スコアリングの統一を行い、自己修復ループと `parseReviewVerdict` が既に使用しているのと同じ `deep-review-report.md` 形式にまとめます。下流の処理は一切変更されません。

`parallel_reviewers` が未設定または `<= 1` の場合、元の単一エージェントフローにフォールバックします：1つの Deep Reviewer が全11アングルをレンダリングし、部分レポートや synthesizer なしで `deep-review-report.md` を直接書き込みます。

### 自動検出 Feature

Deep Reviewer は現在の Feature のスコープ外だが実在する問題（潜在的バグ、技術的負債、不足機能）を発見することがよくあります。着地先がなければ、それらの指摘はレポートに埋もれてしまいます。`auto_discover_features` が有効な場合、実行ループが自動的にキャプチャします。

Deep Reviewer は各スコープ外候補を `deep-review-report.md` の `## Discovered Issues` セクション内に `[NEW-FEATURE] <title>` ブロック（短い説明付き）として記述します。**最終的な Deep Review が PASS** した後（`accepting` に到達する唯一の2つのパス：初回パスの PASS と自己修復の re-verifier が PASS に反転した場合）、ループはそれらのブロックを解析し、CLI レイヤーで完全に（LLM 呼び出しなし）：

- 各候補を既存の Feature および既にキープされた候補と Jaccard トークンオーバーラップ類似度で**重複排除**します。
- カウントを `max_discovered_features`（デフォルト `3`）で**制限**します。残りは capped として記録されます。
- キープされた候補を新しい Feature YAML として**作成**します（ステータス `not-started`、`4x new` と同じ番号付け）。作成ごとに `feature-discovered` イベントを追記します。
- 結果（作成 / 重複スキップ / 制限超過）を `.4x/run/{feature-id}/discovered-features.md` に**サマリー**として出力します。

このステップはベストエフォートです。エラーが発生しても `accepting` への遷移をブロックしません。最終的な Deep Review PASS でのみ実行されます（中間ラウンドや FAIL/`needs-attention` パスでは実行されません）。設定については [設定 → 自動検出 Feature](configuration.md#auto-discover-features) を参照してください。

### Evolve Driver

`4x evolve` は mine、F097 value gate、enrichment を1つの繰り返し実行可能なクローズドループに接続します：**mine → gate (pre → gate LLM ロール → post) → enrich → enqueue → (オプション) auto-run → learnings が次のラウンドにフィードバック**。CLI 層は LLM に触れません — gate ロールと enrichment はどちらも `runner.Runner` サブプロセスとして実行され、インライン API 呼び出しは一切ありません。

パイプラインの順序は **mine → gate → enrich → enqueue**（mine → enrich → gate ではありません）：gate は未加工の `Candidate` を消費するため、enrichment — 候補を完全な `feature.Feature` に具現化する処理 — は gate の生存者に対してのみ実行され、拒否された候補に LLM コストを浪費しません。通過した候補は `not-started` feature YAML としてエンキューされます（value gate の通過**がそのまま**承認です。draft→not-started の第2ステップはありません）。enrichment が失敗または破棄された場合でも、候補はその説明テキストから作成されたベア feature としてエンキューされ、`enriched=false` とマークされます — gate がすでにその価値を保証しています。

各呼び出しはちょうど**1ラウンド**を実行します。繰り返しラウンドは外部駆動（cron または繰り返し呼び出し）です。各ラウンドは `.4x/evolve-report.md`（Mined / Accepted / Rejected / Enqueued / Auto-Run / Halted）に書き込まれ、Dashboard が `GET /api/evolve-report` で表示します。

**アンチスピン停止**は、成果なしにループが永遠に回り続けるのを防ぎます。`.4x/evolve-state.json` は呼び出しをまたいで `consecutiveNoAccept` を永続化します。何も受け入れなかったラウンドはインクリメントし、何かを受け入れたラウンドはゼロにリセットします。`evolution.max_idle_rounds` に達すると、次の呼び出しはマイニング前に中止し、レポートを `Halted` とマークして exit 0 で終了します。この設定は**未設定**（`nil` → デフォルト `3`）と明示的な `<= 0`（停止を無効化 — 常に実行）を区別します。`--force` は1回の停止をオーバーライドします。

`--auto-run` を使用すると、エンキューされた各 feature のメタループが即座に実行され、常に F098 self-mod scope guard の下で動作します：`self_mod_guard.protected_paths` に触れる承認されていない feature は自動完了されず、レポートで `SelfModBlocked` とマークされます（`4x done --approve-self-mod` で解除）。`--dry-run` は読み取り専用 — mine/dedupe サマリーを表示し、何も書き込まず、runner を起動せず、feature を作成しません（`--auto-run` がある場合は警告付きで無視します）。

### エスカレーション

Coder または Tester は以下の場合にエスカレーションできます：

| 理由 | 意味 | ルーティング先 |
|---|---|---|
| `spec-mismatch` | DB/API が仕様と一致しない | Designer |
| `criteria-wrong` | 受け入れ基準が不正確 | Designer |
| `blocker` | 依存関係やインフラの問題が不足 | `needs-attention`（人間の介入） |
| `scope-change` | スコープ外のリポジトリを変更する必要がある | Designer |

エスカレーションは `escalation.json` に記録されます。ループは `spec-mismatch`、`criteria-wrong`、`scope-change` を自動的に Designer に差し戻します。`blocker` エスカレーションは人間の介入のために `needs-attention` に移行します。

---

## ステートマシン

```
init → designing → coding → reviewing → testing → deep-reviewing → accepting → pending-review → done
                     ↑          ↓           ↓            ↓
                     ├── amending ←──────────┴────────────┘
                     ↑      ↓
                     └──────┘
```

### すべての有効な遷移

| 遷移元 | 遷移先 |
|---|---|
| `init` | `designing` |
| `designing` | `coding` |
| `coding` | `reviewing`, `designing` |
| `reviewing` | `testing`, `amending` |
| `amending` | `reviewing`, `designing` |
| `testing` | `deep-reviewing`, `amending`, `designing` |
| `deep-reviewing` | `accepting`, `amending` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`, `coding`, `testing` |
| `needs-attention` | `designing`, `coding`, `testing` |
| any | `blocked`, `needs-attention`, `done`, `abandoned` |

### ラウンドカウンター

- `coding` に入る際にラウンドが0であれば1に設定
- `amending` に入るとラウンドがインクリメント
- `ShouldStop` はラウンドが maxRounds 以上、または3回以上連続で進捗がない場合にトリガー

### ループ内のフェーズ判定

| フェーズ | 条件 | アクション |
|---|---|---|
| `designing` | `task-brief.md` または `acceptance-criteria.md` が不足 | → `needs-attention` |
| `coding` / `amending` | `escalation.json` に `spec-mismatch`、`criteria-wrong`、または `scope-change` がある | → `designing` |
| `reviewing` | レビュー不合格（明示的な `PASS` または `CONDITIONAL PASS` 判定 AND レポート内に `[CRITICAL]`/`[WARNING]` 問題がゼロであることが必要） | → `amending` |
| `testing` | `verify.json` が不合格またはアーティファクトが不足 | → `amending` |
| `deep-reviewing` | Deep Review が FAIL | その場で自己修復（mini-coder + re-verifier）、最大 `max_fix_rounds` 回；PASS → `accepting`、それ以外 → `needs-attention` |
| any（Designer 以外） | ガードチェックでスコープ違反、ベースラインドリフト、必須ファイルの欠落を検出 | → `needs-attention` |

---

## ファイルプロトコル

ロールは共有コンテキストウィンドウではなく、`.4x/` ディレクトリを通じて通信します。

```
.4x/
├── settings.json                    # プロジェクト設定
├── plugins/                         # ランナー指示ファイル
├── batch-plan.json                  # バッチ実行計画
├── batch-stop                       # グレースフル停止シグナル
├── batch-pid                        # 実行中のバッチサブプロセスの PID（サーバー孤立プロセス回収用）
├── batch-conflict.json              # バッチ自動マージコンフリクトシグナル（一時停止中）
├── batch-report.json                # 前回のバッチ実行レポート（統計 + Feature ごとの結果）
├── features/
│   └── {id}.yaml                    # Feature 定義（正規ソース）
└── run/                            # ランタイム成果物（feature ごとの作業ディレクトリ）
    └── {feature-id}/
        ├── state.json                   # フェーズ、ロール、ラウンド、アクティブ、ランナー、runners、停止理由、プロファイル
        ├── events.jsonl                 # 監査証跡
        ├── baseline.json                # コーディング前のスナップショット（HEAD、ブランチ、ダーティファイル）
        ├── task-brief.md                # Designer → Coder: 仕様 + アーキテクチャ
        ├── acceptance-criteria.md       # Designer → Tester: テスト可能な基準
        ├── test-strategy.yaml           # Designer → Tester: テストアプローチ
        ├── final-report.md              # ループ終了時のサマリー
        ├── logs/
        │   ├── round-{N}-{role}.log              # ラウンドごと・ロールごとの実行ログ
        │   ├── round-{N}-deep-reviewer-{i}.log   # 並列サブレビュアーごと（ファンアウト時）
        │   └── round-{N}-synthesizer.log         # 部分レポートをマージする synthesizer
        └── rounds/round-{N}/
            ├── coder-report.md            # Coder の作業内容
            ├── review-report.md           # Reviewer の所見 + 判定
            ├── test-report.md             # Tester の結果
            ├── deep-review-partial-{i}.md # 並列サブレビュアーの所見（ファンアウト時）
            ├── deep-review-report.md      # マージ済み Deep Review（synthesizer 出力、または単一エージェント）
            ├── verify.json                # {passed, round, role, commands[]}
            └── escalation.json            # {needed, reason, detail}
```

### バッチシグナルファイル

2つのトップレベルシグナルファイルが、実行中のバッチと外部オブザーバー（CLI およびダッシュボード）を連携させます：

- **`batch-stop`** -- 空のマーカーファイル。`4x batch run` は Feature 間でこのファイルをポーリングし、存在すればグレースフルに停止します（[バッチモード](batch.md) を参照）。
- **`batch-conflict.json`** -- バッチの自動マージでマージコンフリクトが発生し一時停止した際に書き込まれます。ダッシュボードが git を再実行せずにコンフリクトを表示するのに十分な情報を含みます：

  ```json
  {
    "featureId": "F003-oauth",
    "featureName": "OAuth login",
    "conflictRepo": "core",
    "files": ["internal/auth/token.go"],
    "detectedAt": "2026-06-15T00:00:00Z"
  }
  ```

  モノリポモードでは `conflictRepo` は空です。このファイルは各バッチ実行の開始時、および一時停止したバッチの継続時にクリアされます。

- **`batch-report.json`** -- バッチ実行の終了時（正常終了、停止、中断、クラッシュ）に書き込まれます。上記2つのシグナルファイルと異なり、バッチが非アクティブ時に「前回のバッチレポート」としてダッシュボードが表示するため、実行間で保持されます。`outcome`、全体のカウント（`total`/`completed`/`failed`/`remaining`）、ランナー、合計所要時間、Feature ごとの内訳（最終ステータス、ラウンド数、停止理由）を記録します。`crashed` の場合は `panicMessage` も含まれます。アトミックに書き込まれます（一時ファイル + リネーム）ので、ダッシュボードが不完全なレポートを読むことはありません。

### アトミック状態書き込み

`state.json` は複数のアクターにより同時に読み書きされます（実行ループ、ダッシュボードサーバー、バックグラウンドリコンシラー）。リーダーが不完全なファイルを読むのを防ぐため、`WriteState` はインプレースに書き込みません。状態をマーシャルし、一時ファイル（`.state-*.json`）を**同じディレクトリ**に書き込み（同じファイルシステムであることを保証し、リネームがアトミックになる）、`os.Rename` で `state.json` を上書きします。これにより、リーダーは常に完全な旧ファイルか完全な新ファイルのいずれかを見ます。失敗時には一時ファイルが削除されるため `.state-*.json` の残骸は蓄積されません。ファイルロックは使用しません。正確性はアトミックリネームと `UpdatedAt` の比較により保証されます。

### ワークスペース読み取りキャッシュ（ダッシュボードサーバー）

CLI は短命なプロセスです：各コマンドは必要な `.4x/` ファイルを一度読んで終了するため、常にプレーンな `*protocol.Workspace` を使用します。ダッシュボードサーバー（`4x live`）は逆に長時間実行され、すべての API リクエストが同じファイルを再読み取りします。マルチプロジェクト x マルチ Feature のワークスペース（例：5プロジェクト x 50 Feature）では、単一のリクエストで数百の YAML/JSON パースが発生し得ます。

これを回避するため、サーバーは各ワークスペースを `*protocol.CachedWorkspace`（`internal/protocol/cached.go`）でラップします。これは `WorkspaceReader` インターフェース（`internal/protocol/reader.go`）で宣言された読み取り専用操作に対する、mtime ベースのインメモリキャッシュです：

- **`ReadConfig`** -- `settings.json` をキャッシュ。`os.Stat` でファイルの mtime を比較し、変更時のみ再パースします。
- **`ListFeatures`** -- Feature リスト全体をキャッシュ。`os.ReadDir` で `.yaml` ファイルセットと各ファイルの mtime を比較し、追加・削除・変更時のみ再パースします。コピーを返すためコーラーは自由にミューテーション可能です。緩やかなバリデーションを使用：フォーマットに問題のある Feature（例：subtask status が不正）もスキップせず `Warnings` 付きでリストに含めます。
- **`LoadFeature`** -- 各 Feature を ID でキャッシュ。YAML の mtime をキーとします。厳格なバリデーションを使用——フォーマットに問題がある場合は error を返します。
- **`ReadState`** -- 意図的に**キャッシュしません**（頻繁に変更、小さなファイル、高速パース）。埋め込みの `*Workspace` にフォールスルーします。

無効化は暗黙的です：書き込みメソッド（`SaveFeature`、`WriteState` 等）がキャッシュに通知する必要はありません。次の読み取りが新しい mtime を検出するためです。キャッシュはオプトインです -- サーバーのみが `CachedWorkspace` を構築し、CLI は同一の動作で `*Workspace` を使い続けます。

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: 1  # 数値: 0-1 = full プロファイル, 2 = normal, 3+ = quick (省略時は nil/未設定)
repos: []
subtasks: []
rules: []
depends: []
spec: ""     # オプション：設計仕様の明示パス（docs/design/ ルックアップを上書き）
plan: ""     # オプション：実装計画の明示パス
hooks: {}    # オプション：フェーズフック（settings.json と同形式）
```

`status` はクイックリスティングのために `state.json` のフェーズをミラーリングします。有効な値：`not-started`、`in-progress`、`ready-for-review`、`needs-attention`、`blocked`、`done`、`abandoned`。`abandoned` の Feature は完了として扱われ（依存関係をブロックしない）、ダッシュボードでは取り消し線で表示されます。`depends` はこの Feature を実行する前に完了（または abandoned）している必要がある Feature ID をリストします。`repos` はこの Feature が触れるリポジトリ名（`workspace.repos` から）をリストします。空の場合はスコープ内のすべてのリポジトリが対象です。

#### Design Doc Resolution

ダッシュボードの概要と `4x prompt` のプランニングドキュメント注入は、1つの共有リゾルバ（`protocol.ResolveDesignDoc`）で Feature の spec/plan を検索するため、両方とも常に同じドキュメントを参照します。ドキュメントタイプ（`spec`/`plan`）ごとの解決順序：

1. Feature YAML の `spec`/`plan` フィールド（非空時はパスとして読み取り、相対パスはワークスペースルートを基準に解決）。
2. `docs/design/{feature.ID}-{type}.md`。
3. `docs/design/{slug}-{type}.md`（`slug` は ID から `FNNN-` プレフィックスを除いたもの。ID と異なる場合のみ試行）。

最初に見つかったファイルが使用されます。いずれも一致しない場合、そのドキュメントは不在として扱われます。

### Feature の作成

`Feature`/`Subtask`/`Status` 型と作成ロジックは独立した `internal/feature` パッケージにあります（ID 生成、バックログドリフト、スクリーンショットヘルパーもここに移動済み）。`protocol.Workspace` と `protocol.CachedWorkspace` は `feature.Store` インターフェースを満たし、`feature` は `protocol` をインポートしません（一方向依存、`Store` による疎結合）。CLI（`4x new`）とダッシュボード（`POST /api/new`）の両方が単一のエントリポイント `feature.Create(store, opts)` を通じて Feature を作成するため、番号付け、ID 截断、デフォルトフィールドがエントリポイントに関わらず同一に動作します。

### ワークスペース設定（マルチリポジトリ）

デフォルトでは、4x はモノリポモードで動作します。複数のリポジトリをまたいで作業するには、`.4x/settings.json` で宣言します：

```json
{
  "workspace": {
    "repos": {
      "backend": { "path": "backend/", "hub": false },
      "frontend": { "path": "frontend/", "hub": false },
      "infra": { "path": "infra/", "hub": true }
    }
  }
}
```

各エントリはリポジトリ名をパス（ワークスペースルートからの相対パス）とオプションの `hub` フラグにマッピングします。ハブリポジトリは複数の Feature が触れる共有インフラで、`4x batch plan` のスコープクラスタリングから除外されます。

モノリポモード（`workspace.repos` なし）では、すべてのスコープチェックと git 操作が単一のリポジトリルートを使用します。

---

## ガードレール

CLI によって強制される決定的なチェック -- AI の判断には依存しません。

| ガードレール | 機能 |
|---|---|
| **必須ファイル** | フェーズに適切なアーティファクトの存在を検証（例：designing 後の `task-brief.md`） |
| **ベースライン** | コーディング前の状態をキャプチャ（HEAD、ブランチ、ダーティファイル）；ダーティファイルが存在する場合は警告 |
| **スコープ** | モノリポモード：`git diff --name-only HEAD` のトップレベルディレクトリを Feature の宣言済みリポジトリと比較。マルチリポモード：すべてのワークスペースリポジトリに対して `gitops.Ops.DetectChangedRepos()` を使用 |
| **依存関係** | 依存先の Feature が完了していない場合、`4x run` をブロック |
| **バックログドリフト** | `.4x/features/*.yaml` と外部ミラーが同期していない場合に警告 |
| **Testing → Accepting ゲート** | `verify.json`（passed=true）、`test-report.md`、`final-report.md` が必要 |

`4x check <feature-id>` で手動実行できます。

---

## フェーズフック

フェーズフックを使うと、フェーズ遷移の前後にシェルコマンドを自動実行できます。Docker コンテナの起動、テストデータベースのシード、テスト後のクリーンアップなどに便利です。フックは CLI が実行し、AI ロールは関与しません。

### 設定

フックは `settings.json` の `hooks` キー配下に宣言します。キー形式は `pre_{phase}` または `post_{phase}` です：

```json
{
  "hooks": {
    "pre_coding": [
      { "run": "docker compose up -d", "on_fail": "block" }
    ],
    "post_testing": [
      { "run": "docker compose down", "on_fail": "warn" }
    ]
  }
}
```

各エントリは2つのフィールドを持つ `HookEntry` です：

| フィールド | 型 | 説明 |
|---|---|---|
| `run` | string | `sh -c` で実行されるシェルコマンド |
| `on_fail` | string | `"block"`（デフォルト）または `"warn"`（大文字小文字不問） |

Feature YAML ファイルも同じ形式で `hooks` フィールドを宣言できます。Feature がグローバル設定と同じキーのフックを定義した場合、Feature の定義がグローバルを**完全に置き換え**ます（キー内でのマージはありません）。

### 実行順序

```
pre_{target_phase} フック（配列順）
  ↓ on_fail=block のフックが失敗 → needs-attention に遷移、中断
state.Transition()
  ↓
遷移イベントを記録
  ↓
post_{target_phase} フック（配列順）
  ↓ on_fail=block のフックが失敗 → needs-attention に遷移（ロールバックなし）
```

### 失敗時の動作

| `on_fail` | フック失敗 | 効果 |
|---|---|---|
| `block`（デフォルト） | pre フック | Feature を `needs-attention` に移行；フェーズ遷移を中断 |
| `block`（デフォルト） | post フック | フェーズは既に変更済み；Feature を `needs-attention` に移行 |
| `warn` | いずれか | 結果をログに記録；実行を継続 |

### ログ

各フック実行は `events.jsonl` に `type: "hook"` イベントを追記します：

```json
{
  "ts": "2026-06-14T10:00:00+08:00",
  "type": "hook",
  "phase": "coding",
  "action": "pre_coding",
  "cmd": "docker compose up -d",
  "status": "pass",
  "detail": "exit 0, 1.2s"
}
```

stdout/stderr の全出力は `.4x/run/{feature-id}/hook-logs/{timestamp}-hook-{n}.log` に書き込まれます。

### フックのマージ（`MergeHooks`）

グローバルフックと Feature フックは `MergeHooks` でマージされます：すべてのグローバルキーがコピーされ、同名のキーは Feature のもので完全に上書きされます。グローバルのみに存在するキーは保持されます。両方が nil の場合は nil を返します。

---

## ヘルスチェック

Tester ロールの開始前に、CLI がビルドの合格、サービスの稼働、エンドポイントの応答など環境の健全性を自動検証できます。ここで壊れた環境を検出すれば、無駄なテストサイクルを丸ごと節約できます。ヘルスチェックは CLI が実行し、AI ロールは関与しません。`testing` フェーズへの進入時、`pre_testing` フックの後、Tester ランナーの起動前にのみ実行されます。

### 設定

ヘルスチェックには3つのフィールドがあります（`internal/protocol/types.go` の `HealthCheck`）：

| フィールド | 型 | 説明 |
|---|---|---|
| `commands` | `[]string` | 順番に実行されるチェックコマンド；いずれかが失敗すると停止 |
| `recovery` | `[]string` | オプション。チェック失敗時に環境を修復するため順番に実行 |
| `timeout` | `int` | コマンドごとのタイムアウト（秒）；`<= 0` の場合はデフォルト `30` を適用 |

`settings.json` でグローバルに宣言できます：

```json
{
  "health_check": {
    "commands": ["make build"],
    "recovery": ["docker compose up -d"],
    "timeout": 30
  }
}
```

または `test-strategy.yaml` で Feature ごとに宣言できます（`Workspace.ReadTestStrategy` で読み取り）：

```yaml
health_check:
  commands: ["make build", "curl -s http://localhost:8080/health"]
  recovery: ["make dev-up"]
  timeout: 60
```

**マージ：** `ResolveHealthCheck` はフィールドレベルのマージではなく、グループ全体の上書きを行います。`test-strategy.yaml` が `health_check` を定義している場合、グローバル設定を完全に置き換えます。いずれも設定されていない場合、ヘルスチェックはスキップされ、Tester が即座に開始されます。

### 実行フロー

```
testing フェーズに入る（pre_testing フックは実行済み）
  ↓
commands を順番に実行（各コマンドに個別のタイムアウト）
  ├─ すべて合格 → Tester を開始
  └─ いずれか失敗 →
      ├─ recovery なし → needs-attention にエスカレーション
      └─ recovery あり → recovery コマンドを順番に実行
          ├─ recovery 失敗 → needs-attention にエスカレーション
          └─ recovery 合格 → すべてのコマンドを一度だけ再実行
              ├─ 合格 → Tester を開始
              └─ まだ失敗 → needs-attention にエスカレーション
```

リカバリは最大1回のみトリガーされます。複数回のリトライやバックオフループはありません。

### 失敗時の動作

最終失敗時に `type: "health-check-failed"` イベント（ロール `tester`、失敗したコマンドとエラーを `detail` に含む）を記録し、Feature を `needs-attention` に遷移させ、`StopReason` を `health-check-failed` に設定してループを停止します。各コマンドはコマンドごとのタイムアウト付きで `sh -c` で実行されます。タイムアウトは失敗としてカウントされ、出力はデバッグ用に stderr に書き込まれます。

---

## テストプロファイル

**テストプロファイル**は再利用可能なテスト方法論のブロックで、Designer が Feature にタグ付けすることで、Tester のプロンプトにマッチするガイダンスが自動注入されます。`settings.json` の `roles.tester.instructions` で巨大な1つのリストをすべての Feature で共有する代わりに使えます。

> **[パイプラインプロファイル](#pipeline-profiles)**（`Config.Profiles`、*どのロールを実行するか*を選択）と混同しないでください。テストプロファイル（`Config.TestProfiles`）は Tester プロンプトにのみ*テスト方法論の内容*を注入します。

### プロファイルの宣言

Designer が `test-strategy.yaml` にプロファイルをリストします（`internal/protocol/types.go` の `TestStrategy.Profiles`）：

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

`profiles` は `omitempty` です。これのない `test-strategy.yaml` は従来どおり動作します（注入なし）。

### 組み込みプロファイル

4つのプロファイルがバイナリに埋め込まれています（`templates/profiles/*.md`、`templates.ProfilesFS` 経由で公開）：

| プロファイル | 方法論 |
|---|---|
| `unit` | Go `go test`、`t.TempDir()` 分離、テーブル駆動、エラーケース、AC ごとの verify.json |
| `web` | `4x live` ダッシュボードに対して Playwright テスト；ヘッドレス、分離ワークスペース + ランダムポート、証拠としてスクリーンショット、ユーザーの実行中サーバーに干渉しない |
| `api` | HTTP エンドポイントテスト -- ステータスコード、レスポンスボディ、エッジケース、認証 |
| `e2e` | エンドツーエンドのマルチサービスフロー、DB 状態とクロスサービスの整合性 |

### settings.json での上書き

プロジェクトは `Config.TestProfiles`（`test_profiles`）でプロファイルを置き換えまたは拡張できます。キーはプロファイル名（`TestProfileOverride`）：

```json
{
  "test_profiles": {
    "web": { "content": "Cypress でテスト..." },
    "lua": { "include": "docs/test-profiles/lua.md" }
  }
}
```

- `content` -- インライン置換テキスト
- `include` -- ファイルパス（ワークスペースルートからの相対パス）

**解決順序**（プロファイル名ごと）：`test_profiles[name].content` → `test_profiles[name].include` → 組み込み `profiles/{name}.md`。上書きはフィールドレベルのマージではなく完全置換です。不明な名前（上書きなし、組み込みなし）は stderr に警告を出力してスキップされます。

Tester プロンプトは各解決済みプロファイルを `== Test Profile: {name} ==` ブロックとしてレンダリングします。読み込みは `loadProfiles` / `resolveProfileContent`（`cmd/4x/prompt.go`）で実装されています。

---

## Pending Review ゲート

ループは直接 `done` にはなりません。accepting の後、Feature は `pending-review` に入ります -- 人間が AI の成果物をレビューするのを待ちます。

```
... → accepting → pending-review → (人間がレビュー) → 4x done F001
```

これにより、Feature が完了と見なされる前に必ず人間がサインオフすることが保証されます。
