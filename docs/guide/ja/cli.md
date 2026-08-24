# CLI リファレンス

すべての feature-id 引数は大文字小文字を区別しないプレフィックスマッチに対応しています。`4x run f001`、`4x run F001-user`、`4x run F001` はすべて `F001-user-authentication-w` に解決されます。曖昧なプレフィックスは一致候補を列挙してエラーになります。

---

## `4x init`

カレントディレクトリに `.4x/` ワークスペースを初期化します。

```
4x init
```

- プロジェクトの言語とビルド/テスト/lint コマンドを自動検出
- 6つのデフォルトランナー（claude、codex、gemini、agy、copilot、cursor）を含む `.4x/settings.json` を作成
- 埋め込みプラグインファイルを `.4x/plugins/` にデプロイ
- ルートレベルのファイル（CLAUDE.md、AGENTS.md、GEMINI.md、AGY.md、.cursorrules）に `@import` 行を追加
- `.4x/` が既に存在する場合はエラー

### `4x init --dump-templates`

組み込みのロールプロンプトテンプレートを `.4x/templates/` に出力し、プロジェクトでカスタマイズできるようにします。

```
4x init --dump-templates          # 組み込みテンプレートを .4x/templates/ に書き込む
4x init --dump-templates --force  # 既存のテンプレートファイルを上書き
```

- `.4x/` が既に存在する必要があります（先に `4x init` を実行してください）
- 埋め込まれているすべての `*.md.tmpl`（`locale.tmpl` を含む）を `.4x/templates/` に書き込みます
- 既存ファイルは `--force` なしでは警告付きでスキップされます
- プロンプト生成時に `.4x/templates/{file}` が埋め込みテンプレートより優先されます（ファイル全体の上書き）。`locale.tmpl` と各ロールテンプレートはそれぞれ独立してフォールバックします

---

## `4x new <title>`

オプションのメタデータを指定して新しい Feature を作成します。

```
4x new "Feature title" [flags]
```

| フラグ | 説明 |
|---|---|
| `--id` | Feature ID のカスタム slug（自動截断をスキップ） |
| `--desc` | Feature の説明（デフォルトはタイトル） |
| `--subtask` | `"id:name"` 形式のサブタスク（繰り返し指定可能）。最初のコロンより前が id、残り全体が name（name にはコロンを含められる。例：`10:00`、`group:artifact`、URL）。description は作成後に YAML を編集して設定 |
| `--rule` | ルール参照（繰り返し指定可能） |
| `--depends` | 依存する Feature ID（繰り返し指定可能） |
| `--priority` | 優先度（0=critical、1=high、2=medium、3=low） |
| `--repo` | スコープ内のリポジトリ（繰り返し指定可能） |
| `--json` | JSON 形式で出力 |

ステータス `not-started` で `.4x/features/F{NNN}-{slug}.yaml` を作成します。
自動生成される slug は語句境界で截断されます。`--id` で上書き可能です。
作成は共有パス `feature.Create` を通じて実行されます（[コンセプト](concepts.md#feature-creation) を参照）。ダッシュボードの `POST /api/new` も同じロジックを使用するため、ここのフラグはダッシュボードの新規 Feature フォームと一対一で対応します。

使用例：
```bash
4x new "Dashboard SPA file split"
4x new "Global settings" --id global-settings --desc "Add ~/.4x/settings.json"
4x new "Auth refactor" --subtask "extract-mw:Extract middleware" --subtask "add-tests:Add tests"
```

---

## `4x run <feature-id>`

Feature に対して Design-Code-Review-Test ループを実行します。

```
4x run <feature-id> [flags]
```

| フラグ | デフォルト | 説明 |
|---|---|---|
| `--runner` | 設定のデフォルト | ランナープラグイン名 |
| `--max-rounds` | `5` | ループの最大イテレーション数 |
| `--timeout` | `3600` | フェーズごとのタイムアウト（秒） |
| `--dry-run` | `false` | LLM を呼び出さずにロールプロンプトを表示 |
| `--json` | `false` | 実行を開始し JSON 形式で即座に返す |
| `--profile` | auto | パイプラインプロファイル（`full`/`normal`/`quick` またはカスタム）；優先度ベースの自動選択を上書き |

`--profile` は実行するロールを選択します。組み込みプロファイル：`full`（全6ロール）、`normal`（coder/reviewer/tester/acceptor）、`quick`（coder/reviewer）。プロファイルに含まれないロールはパススルーされます（ランナーを起動せずに正規の状態遷移エッジに沿って進みます）。省略時は、`settings.json` に `profiles` セクションがあれば Feature の優先度に基づいて自動選択されます（ない場合は `full`）。詳細は [設定 → プロファイル](configuration.md#profiles) を参照してください。

ループの流れ：init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → accepting → pending-review。レビュー失敗時はコードが再実行されます。テスト失敗時はコーディングに戻ります。

Designer 以外の各ランナー完了後、ガードレールチェック（スコープ、ベースライン、必須ファイル）が自動的に実施されます。違反が検出されると Feature は `needs-attention` に遷移し、ループが停止します。Designer は免除されます（ソースコードを変更しないため）。

レビューの判定は `PASS` で始まる必要があります。`## Verdict` 見出しと判定テキストの間の空行は無視されます。曖昧な出力（`TODO`、`ERROR`、判読不能なテキスト、`## Verdict` ブロックの欠落）は失敗として扱われます。

`settings.json` または Feature YAML で宣言されたフェーズフックは、ループ内の各フェーズ遷移の前後に自動実行されます。設定の詳細は [フェーズフック](concepts.md#phase-hooks) を参照してください。

`testing` フェーズに入る際（`pre_testing` フックの後、Tester ランナーの起動前）、`health_check` が設定されている場合は環境のヘルスチェックが実行されます。チェックコマンドは順番に実行され、失敗時はリカバリコマンドが一度実行されてからチェックが再試行されます。それでも環境が失敗する場合、Feature は `needs-attention` に遷移しループが停止します。設定の詳細は [ヘルスチェック](concepts.md#health-check) を参照してください。

`settings.json` で `auto_discover_features` が有効な場合、最終的な Deep Review が **PASS** すると、`deep-review-report.md` 内の `[NEW-FEATURE]` マーカーが解析され、Deep Reviewer が指摘したスコープ外の問題に対して自動的に Feature YAML が作成されます（重複排除・上限あり）。詳細は [設定 → 自動検出 Feature](configuration.md#auto-discover-features) および [コンセプト → 自動検出 Feature](concepts.md#auto-discovered-features) を参照してください。

フィーチャーが `blocked` または `needs-attention` フェーズにある場合、現在のロールに基づいて適切な再開フェーズに自動復旧します。

依存関係ゲートを自動チェックします -- 依存先の Feature が完了していない場合はブロックされます。

設定で `isolation: "worktree"` が指定されている場合、`.worktrees/4x/<feature-id>/` 下の git worktree で実行されます。マルチリポジトリモード（workspace.repos が設定されている場合）では、各リポジトリが `.worktrees/4x/<feature-id>/<repo-name>/` 下に独自の worktree を持ち、ワークスペースレベルのファイル（go.work、Makefile など）が隣にコピーされます。Coder プロンプトには `== Workspace Repos ==` セクションが含まれ、worktree モードでは各エントリがリポジトリ名を相対パスとして表示します（例：`core → core/`）。

---

## `4x status [feature-id]`

Feature のステータスを表示します。

```
4x status              # 全 Feature を状態別にグループ表示
4x status <feature-id> # 単一 Feature の詳細とサブタスク
4x status --pending    # done/abandoned の Feature を非表示
4x status --json       # JSON 形式で出力
```

| フラグ | 説明 |
|---|---|
| `--pending` | done/abandoned の Feature を非表示 |
| `--json` | JSON 形式で出力 |

グループ：Running、Review、Pending、Todo、Done（done は最大5件表示）。バックログドリフト警告を含みます。

単一 Feature の詳細表示（`4x status <feature-id>`）で、スクリーンショットが存在する場合は以下も表示されます：

`Screenshots: <total> (round 1: <n>, round 2: <n>, ...)`

---

## `4x cost`

各 runner が書き出す stream log から feature 全体の run コストを集計します。読み取り専用で、run データを変更しません。

```
4x cost                       # 全 feature の role 別コスト表
4x cost --feature <id>        # 単一 feature の round 別・role 別明細
4x cost --by-round            # round 別合計 + retry（round>=2）比率
4x cost --feature <id> --by-round  # 単一 feature の round 別明細
4x cost --json                # 構造化出力（上記いずれかのビュー）
```

| Flag | 説明 |
|---|---|
| `--feature <id>` | 単一 feature に絞り込み、round 別・role 別の明細を表示 |
| `--by-round` | round 単位で集計し、retry（round>=2）の比率を表示 |
| `--json` | JSON で出力 |

データソースは `logs/*.stream.jsonl` を主とし（role invocation ごとに 1 ファイル、`total_cost_usd` を含む）、ファイル名に round と role がエンコードされています。stream log が一切ない feature（古い run）については、`events.jsonl` の `run-end` イベントを補助として使用します。`total_cost_usd` フィールドを欠く stream log はスキップされ、失敗とはせず `Skipped N stream log(s)` のカウントとして報告されます。

デフォルトの表は総コスト順にソートされた `ROLE / CALLS / TOTAL($) / AVG($) / PCT(%)` と `TOTAL` 行を表示します。`--by-round` は `TYPE` 列（round 0–1 は `initial`、round≥2 は `retry`）を追加し、retry 比率を USD とパーセントで報告します。

---

## `4x subtask <feature-id> <subtask-id>`

Feature 内のサブタスクのステータスを更新します。

```
4x subtask <feature-id> <subtask-id> --status <status>
```

| フラグ | 説明 |
|---|---|
| `--status` | 新しいステータス：`done`、`in-progress`、`blocked`、`not-started`、`ready-for-review`（必須） |

使用例：
```
4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
```

---

## `4x approve <feature-id>`

enriched auto-discover が生成した `draft` Feature を承認し、`draft → not-started` に遷移させます。これにより meta-loop が Feature を処理対象とします。Draft は `enrich_discovered_features` が有効で `enrich_auto_approve` が `false` の場合にのみ作成されます。Feature が `draft` ステータスでない場合はエラーになります。

```
4x approve F042-some-discovered-feature
```

---

## `4x reject <feature-id>`

enriched auto-discover が生成した `draft` Feature を却下し、`draft → abandoned` に遷移させます。これにより meta-loop の対象から外れます。Feature が `draft` ステータスでない場合はエラーになります。

```
4x reject F042-some-discovered-feature
```

---

## `4x retry <feature-id>`

`needs-attention` または `blocked` でスタックした Feature を適切な作業フェーズに戻し、直ちに `4x run` を起動します。`4x transition --to <phase> <id> && 4x run <id>` と同等です。

`--to` を省略した場合、ターゲットフェーズは `state.json` に記録された `role` から**自動検出**されます——feature が `needs-attention`/`blocked` に入る前にスタックしていたロールを、対応する作業フェーズに逆算します（例：`role: designer` → `designing`；`role: coder` → ラウンドに応じて `coding` または `amending`）。自動検出に成功すると、起動前に `auto-detected target phase from role "<role>": <phase>` と出力されます。ロールをマッピングできない場合（空または不明）は `accepting` にフォールバックします。明示的に `--to <phase>` を渡すと自動検出より優先されます。

```
4x retry F042-some-feature              # state.json の role からターゲットフェーズを自動検出
4x retry F042-some-feature --to amending
```

| フラグ | 説明 |
|------|-------------|
| `--to <phase>` | 復帰先のターゲットフェーズ（デフォルト：`state.json` の role から自動検出、マッピングできない場合は `accepting`） |
| `--phase-override <phase>:<runner>:<model>` | 再起動される `4x run` に転送されます（繰り返し指定可）—— `4x run` の `--phase-override` と同じ形式・意味 |

手動の `transition` / `retry --to <phase>` で設定されたフェーズは、その後の `4x run` の復旧処理でも尊重されます：`manualPhase` フラグが付与され、`SmartResumePhase` がディスク上の成果物から導出した以前のフェーズへ上書きすることを防ぎます。これにより `retry --to deep-reviewing` は実際に `deep-reviewing` から再開され、`coding` に引き戻されることはありません。

状態を変更するコマンド（`transition`、`retry`、`force-done`、`done`）は `state.json` に対して単一のロック付き read-modify-write として phase 変更を行うため、実行中の `4x run` が書き込んでいる feature に対して実行しても、互いの更新を上書きすることはありません。フィーチャー単位のロックがタイムアウト内に取得できない場合、コマンドはハングせず明確なエラーで失敗します。

Feature が `needs-attention` または `blocked` でない場合はエラーになります。

---

## `4x gate`

マイニングされた候補 feature に F097 evolve **value gate** の拒否レイヤーを適用します。純粋な CLI 確定的拒否であり、LLM を呼び出しません。`gate` LLM ロールは2つのフェーズの間で実行され（evolve driver がオーケストレーション）、`gate-verdicts.json` を出力します。

`--pre` または `--post` のいずれかを指定する必要があります：

- `--pre` — PRE-拒否：`.4x/candidates.json` を読み込み、既存の feature やバッチ内の重複と Jaccard 類似の候補を除外し、生存者を `.4x/gate-input.json` に書き込みます。
- `--post` — POST-拒否：`.4x/gate-input.json` + `.4x/gate-verdicts.json` を読み込み、オーバーライド不可のハード拒否（non-accept / `why_not_hack` 欠落 / `value_floor` 未満 / 既存と重複 / `max_accept_per_run` 超過 / `max_backlog_undone` 超過）を適用し、通過した候補（`value_score`/`why_not_hack` 付き）を `.4x/accepted-candidates.json` に書き込みます。

閾値は `settings.json` の `evolution` セクション（`value_floor`、`max_accept_per_run`、`max_backlog_undone`、`dedup_threshold`）から取得されます。

```
4x gate --pre
4x gate --post
```

---

## `4x evolve`

継続的自己改善パイプラインを1ラウンド実行し、既存の進化パーツを繰り返し実行可能なクローズドループに接続します：

**mine → gate (pre → gate LLM ロール → post) → enrich → enqueue → (オプション) auto-run メタループ → learnings が次のラウンドにフィードバック。**

CLI 層は直接 LLM を呼び出しません — gate ロールと enrichment はどちらも `runner` サブプロセスとして実行されます。各呼び出しはちょうど**1ラウンド**を実行します。複数ラウンドは外部駆動（cron または `4x evolve` の繰り返し呼び出し）で行います。各ラウンドの結果は `.4x/evolve-report.md` に書き込まれます。

パイプラインステップ：

1. **mine** — `.4x/` をスキャンして失敗シグナル（エスカレーション / スタックした feature / 繰り返しの FAIL パターン）を検出し、重複排除して `.4x/candidates.json` にマージします。
2. **gate pre** — Jaccard 重複排除で生存者を `.4x/gate-input.json` に書き込みます。
3. **gate role** — `gate` LLM ロールを起動して `.4x/gate-verdicts.json` を書き込みます。
4. **gate post** — オーバーライド不可の拒否 + 収束キャップを適用し、`.4x/accepted-candidates.json` に書き込みます。
5. **enrich + enqueue** — 通過した各候補を `not-started` feature YAML に具現化します（enrichment 失敗時は候補テキストから作成したベア feature にフォールバックし、`enriched=false` とマークします）。
6. **auto-run**（オプション）— エンキューされた各 feature のメタループを実行します（F098 self-mod scope guard で保護）。

アンチスピン：あるラウンドで何も受け入れられなかった場合、`.4x/evolve-state.json` の `consecutiveNoAccept` がインクリメントされます。`evolution.max_idle_rounds`（デフォルト 3、`<= 0` で無効化）に達すると、次の呼び出しは早期に中止し、レポートを `Halted` とマークして exit 0 で終了します。`--force` でオーバーライドできます。

```
4x evolve                        # 1ラウンド実行、feature は not-started のまま
4x evolve --dry-run              # 読み取り専用：mine/dedupe サマリーを表示、ファイル書き込みなし
4x evolve --auto-run             # エンキューされた feature のメタループも実行
4x evolve --force                # アンチスピン停止をバイパス
```

| フラグ | 説明 |
|---|---|
| `--auto-run` | エンキューされた各 feature のメタループを実行（F098 self-mod guard は常に強制） |
| `--dry-run` | 読み取り専用分析：mined/deduped 数を表示、ファイル書き込み・runner 起動・feature 作成なし |
| `--min-occurrences` | 失敗パターンが候補になるための distinct-feature 閾値（デフォルト 3） |
| `--force` | アンチスピン停止をオーバーライドし、連続アイドルラウンド後も実行 |
| `--runner` | gate / enrich / auto-run に使用する runner プラグイン（デフォルト `evolution.gate_runner` またはプロジェクトデフォルト） |
| `--timeout` | LLM サブプロセスのタイムアウト秒数（デフォルト 3600） |
| `--max-rounds` | `--auto-run` 時の feature あたりの最大ラウンド数（デフォルト 5） |

Dashboard は `GET /api/evolve-report` で最新レポートを表示します。

---

## `4x check <feature-id>`

状態遷移なしでガードレールチェックを実行します。

```
4x check <feature-id> [--json]
```

| フラグ | 説明 |
|---|---|
| `--json` | 結果を JSON で出力 |

チェック内容：必須ファイル、ベースライン、スコープ、依存関係、バックログドリフト。合格時は終了コード0、失敗時は1。

---

## `4x doctor`

マージされた設定（`.4x/settings.json` + `~/.4x/settings.json`）とワークスペースの整合性に対して、実行前にワンショットの読み取り専用ヘルスチェックを行います。LLM は一切呼び出さず、ランナーのインストールも不要です。

```
4x doctor [--json]
```

| フラグ | 説明 |
|---|---|
| `--json` | レポート全体を JSON で出力（CI 向け） |

チェックはセクションごとにグループ化されます：

- **settings** -- `settings.json` が読み込み可能、`project.name` が非空、ランナーが1つ以上定義済み、`default_runner` がランナーマップに存在。
- **runners** -- 各ランナーの `command` が `PATH` 上で解決可能か確認（見つからない場合は WARN、リモートマシンにある場合があるため FAIL ではない）。
- **roles** -- デフォルトランナー経由で各ロール（designer/coder/reviewer/tester/acceptor）が使用する実際のモデルを解決。reviewer の `deep_model` も確認。
- **workspace** -- 孤立した worktree（Feature が done/abandoned だが `.worktrees/4x/<id>` が残っている）、ダングリング worktree（ディレクトリはあるが対応する Feature がない）、ステイル状態（`active=true` だがプロセスが消えている）、不正な Feature YAML。

各行は `✅`（PASS）、`⚠️`（WARN）、または `❌`（FAIL）のプレフィックスが付き、最後にサマリーカウントが表示されます。

終了コード：FAIL がない場合は `0`（WARN は終了コードに影響しない）、いずれかのチェックが失敗した場合は `1`。`doctor` は厳密に読み取り専用で、`state.json` の書き換え、worktree のクリーンアップ、設定の変更は行いません。

```bash
# CI ゲート：FAIL チェックがあればビルドを失敗させる
4x doctor --json | jq -e '[.checks[] | select(.severity == "FAIL")] | length == 0'
```

---

## `4x verify <feature-id>`

Feature の `test-strategy.yaml` に記載された verify コマンドを実行し、結果を `rounds/round-{N}/verify.json` に書き込みます。

コマンドは `verify_groups` でグループに整理できます：グループは並列実行され、グループ内のコマンドは逐次実行されます。グループ内のコマンドが失敗すると、そのグループの残りのコマンドはスキップされますが、他のグループは実行を継続します。`verify_commands` のみ定義されている場合は、単一の逐次 `default` グループにフォールバックします。両方を宣言するとエラーになります。

並列実行は CLI が完全に処理します（LLM は関与しません）。Tester ロールは verify コマンドを自分で実行する代わりにこのコマンドを呼び出します。人間もデバッグ用にスタンドアロンで実行できます。

```
4x verify <feature-id> [--round N] [--timeout 5m] [--json]
```

| フラグ | 説明 |
|---|---|
| `--round` | ラウンド番号（デフォルト：state.json の現在のラウンド） |
| `--timeout` | 全グループの全体タイムアウト（デフォルト：5m） |
| `--json` | verify.json 全体を JSON で出力 |

スキップされていないすべてのコマンドが合格した場合は終了コード 0、いずれかが失敗した場合は 1。

---

## `4x transition <feature-id>`

状態遷移を強制します。

```
4x transition <feature-id> --to <phase> [--role <role>] [--json]
```

| フラグ | 説明 |
|---|---|
| `--to` | 遷移先のフェーズ（必須） |
| `--role` | 遷移を実行するロール |
| `--json` | JSON 形式で出力 |

ステートマシンに従って遷移が合法かを検証します。状態が存在しない場合は自動初期化します。`testing → accepting` の遷移では追加ゲートが実行されます（verify.json、test-report.md、final-report.md が存在し、verify が合格している必要があります）。

`settings.json` または Feature YAML が `hooks` を宣言している場合、遷移前に `pre_{phase}` フックが、遷移後に `post_{phase}` フックが実行されます。`block` の pre フックが失敗すると遷移が中断されます。`block` の post フックが失敗すると Feature が `needs-attention` に移行します。設定形式の詳細は [フェーズフック](concepts.md#phase-hooks) を参照してください。

---

## `4x event <feature-id>`

`events.jsonl` にイベントを追記します。

```
4x event <feature-id> --type <type> [--role <role>] [--round <n>] [--action <action>] [--detail <text>]
```

| フラグ | 説明 |
|---|---|
| `--type` | イベントタイプ（必須） |
| `--role` | イベントをトリガーしたロール |
| `--round` | ラウンド番号 |
| `--action` | アクション名 |
| `--detail` | 追加の詳細テキスト |

---

## `4x prompt <feature-id>`

Feature のロールプロンプトを表示します。

```
4x prompt <feature-id> [--role <role>] [--round <n>]
```

| フラグ | 説明 |
|---|---|
| `--role` | 対象ロール（省略時は現在の状態から推論） |
| `--round` | ラウンド番号 |

ロケール注入（ユーザー設定または `LANG` 環境変数から）、プランニングドキュメントの自動インクルード、プロジェクト/ロールインクルードに対応しています。spec/plan ドキュメントは共有リゾルバ（`protocol.ResolveDesignDoc`）で検索されます。Feature YAML の `spec`/`plan` フィールドが優先され、次に `docs/design/{id}-{type}.md`、最後に `FNNN-` プレフィックスを除いた `docs/design/{slug}-{type}.md` へフォールバックします。これにより、プロンプトとダッシュボード概要で常に同じドキュメントが表示されます。詳細は [Design Doc Resolution](concepts.md#design-doc-resolution) を参照してください。

`tester` ロールの場合、Feature の `test-strategy.yaml` に記載された `profiles` が解決され（`loadProfiles` 経由）、`== Test Profile: {name} ==` ブロックとしてプロンプトに注入されます。各プロファイルの内容は `settings.json` の `test_profiles[name]`（`content` または `include`）から取得され、存在しない場合は組み込みの `templates/profiles/{name}.md` が使用されます。詳細は [テストプロファイル](concepts.md#test-profiles) を参照してください。

---

## `4x done <feature-id>`

pending-review 状態の Feature を完了としてマークします。Feature に worktree（`.worktrees/4x/<id>`）がある場合、ブランチを main に自動マージし、worktree とブランチを削除します。

```
4x done <feature-id>
```

Feature が `pending-review` フェーズにある場合のみ動作します。その他のフェーズではエラーになります。

マージコンフリクトまたはマージエラーが発生した場合、Feature は `pending-review` のままとなり、worktree は保持され、ガイダンスが表示されます。マルチリポジトリモードでは、コンフリクトのあるリポジトリ名が `repo: <name>` として表示されます。コンフリクトを解決した後、`4x merge <id>` で完了してください。

マージの前に、4x はメインワークスペース内で自身が書き込んだ pipeline 状態（`.4x/features/*.yaml`、`.4x/learnings.json`、`.4x/learnings-context.md`）を `chore(<feature-id>): 4x pipeline state` としてコミットします。このコミットは対象パスを限定しているため、メインワークスペースにある他の未コミットの tracked 変更はそのまま残り、従来どおりマージを中止させます。`4x merge` も完了前に同じ処理を行います。

---

## `4x force-done <feature-id>`

<!-- alias: 4x forcedone -->

通常のパイプラインをスキップしてどのフェーズからでも Feature を強制完了します。なぜ通常のパイプラインをスキップするかを記録するために `--reason` が必須です。

```
4x force-done <feature-id> --reason "コードレビュー済み、テストは合格。E2E テストはマージ後に実施予定"
```

`pending-review` に遷移し、reason を含む `force-done` イベントを記録した後、`4x done` と同じマージフローを実行します。`needs-attention`、`blocked`、またはアクティブなフェーズから動作します。

Dashboard はこれを `POST /api/force-done`（`{id, reason}`）として公開します。

| フラグ | 説明 |
|---|---|
| `--reason` | Feature を強制完了する理由（必須） |
| `--json` | 結果を JSON で出力 |

---

## `4x merge <feature-id>`

`4x done` でのコンフリクト解決後にマージを完了します。

```
4x merge <feature-id>
```

Feature が `pending-review` または `done` フェーズにあり、`.worktrees/4x/<id>` に worktree が存在する場合のみ動作します。worktree 内のコンフリクト解決をコミットし、main にマージした後、worktree とブランチを削除します。Feature がまだ `pending-review` の場合、マージ成功後に `done` としてマークされます。

マルチリポジトリモードでは、コンフリクトの解決はリポジトリごとにコミットされ（`.worktrees/4x/<id>/<repo-name>/` 下の各リポジトリが個別にステージ・コミット）、その後すべてのリポジトリがオール・オア・ナッシングでマージされます。コンフリクトが再発した場合、`repo: <name>` としてリポジトリ名が表示されます。

---

## `4x clean [feature-id]`

完了した Feature のワークスペースアーティファクト（`logs/`、`rounds/`、レポート、`state.json`、`events.jsonl`）を削除し、ディスクスペースを解放します。Feature 定義（`.4x/features/*.yaml`）と Feature ステータスは常に保持されます。

```
4x clean              # クリーン可能な Feature とサイズを一覧、確認後にクリーン
4x clean --dry-run    # 一覧のみ、削除なし
4x clean --force      # 確認プロンプトをスキップ
4x clean <feature-id> # 単一 Feature をクリーン（done/abandoned であること）
```

`done` または `abandoned` ステータスで、ワークスペースディレクトリが存在する Feature のみが対象です。アクティブ（実行中）の Feature はクリーンされず、`blocked`/`needs-attention` の Feature はデバッグアーティファクトを残すために保持されます。クリーンはステートマシンの遷移ではなく、Feature のライフサイクルを変更しません。

---

## `4x learn`

レトロラーニングを管理します。ラーニングは `.4x/learnings.json` に蓄積される、Feature をまたいだ開発上の学びです。

各 Feature の Acceptor が `retro-learnings.json` を書き込み、CLI がそれを `.4x/learnings.json` に収集します。各ロールのプロンプトを生成する際、CLI はそのロールのカテゴリで `.4x/learnings.json` を直接フィルタリング（active/candidate の配分枠付き）して注入します——Designer が先に選択する中間ステップはありません。Learnings は完全に CLI が管理します。Runner が `learnings.json` を直接書き込むことはなく、learnings の失敗は警告のみで状態遷移をブロックしません。

```
4x learn add --category <cat> --content <text>  # learning を手動追加（standalone session 用）
4x learn add --category ops --content "..." --json  # JSON 出力：{"id":"L0xx","added":true}
4x learn list                     # active + candidate の learning を一覧（デフォルト）
4x learn list --category=testing  # カテゴリでフィルタ
4x learn list --status=active     # ステータスでフィルタ（active, candidate, stale, promoted）
4x learn list --ineffective       # 非効果的なエントリのみ表示（used≥3 + 30日 + 2 つ以上の異なる Feature から類似内容）
4x learn list --ineffective-reset # v2 マイグレーションで ineffective フラグがリセットされたエントリのみ表示
4x learn prune                    # 非アクティブな active を candidate に降格、放置された candidate を stale へ老化、stale を削除
4x learn prune --dry-run          # 降格される active と削除される stale をプレビュー（書き込みなし）
4x learn promote <id>             # learning を promoted としてマーク（保持するが注入しなくなる）
4x learn remove <id>              # learning エントリを削除
4x learn context                  # .4x/learnings-context.md のスナップショットを生成
```

`learn add` は既存エントリとの類似チェック（完全一致・正規化・Jaccard 類似度）を行います。ファジー重複が見つかった場合、既存 ID を報告して書き込みません。

- カテゴリ：`design`、`code-quality`、`testing`、`review`、`tooling`、`process`、`ops`
- ステータス：`active`（注入可能）、`candidate`（新規 harvest、クロスフィーチャー検証待ち）、`stale`（老化済み、削除待ち）、`promoted`（テンプレート/指示として昇格済み）
- 各 learning は `confidence` スコア（0〜1）を持ち、エントリがロールのプロンプトに注入されるたびに強化されます。プロンプト注入と `.4x/learnings-context.md` は confidence を最優先、次に新しさ、次に ID の順でランク付けし、トークン予算に達すると最も低いスコアのエントリから切り捨てます。`confidence` 値を持たない旧エントリは `used_count` から算出される決定的なスコアにフォールバックします（読み取り時に書き戻されることはありません）
- `4x learn prune` はまず、非アクティブな active エントリを `candidate` に降格します：`active` な learning の最終ヒット時刻（`last_used`、なければ `activated_at`、それもなければ `created_at`）が `evolution.active_demote_days`（デフォルト 90 日、0 で降格を無効化）より古い場合、削除されるのではなく再び `candidate` に戻され、candidate の老化フローに委ねられます。`promoted` エントリは決して降格されません
- 続いて `4x learn prune` は一度も使われていない candidate を老化させます：`used_count=0` の `candidate` が作成されてから `evolution.candidate_max_idle_days`（デフォルト 30 日、0 で老化を無効化）より経過している場合、`stale` としてマークされ、サンプルプールが実際に収束するようにします。老化は `prune` 実行時のみ発生し、active/promoted エントリには影響しません。`--dry-run` は降格される active と老化/stale になる candidate を別々にプレビューし、実際には削除しません（同じ実行で降格されたばかりの active が削除されることはありません）
- candidate エントリは ID に `*` 接尾辞が付きます。別のフィーチャーで独立に生成されるか、Designer に選択されると自動的に active に昇格します
- 非効果的なエントリは `active!` ステータスで表示されます：3回以上注入、30日以上経過、かつ類似内容（Jaccard ≥ 0.3）が 2 つ以上の異なる Feature から引き続き発生している——この 3 条件をすべて満たす場合に該当し、その learning が繰り返しの問題を減らせていないことを示します。フラグは harvest のたびに再評価され、いずれかの条件が成立しなくなった時点で自動的に解除されます。v2 形式より前に書かれた store は初回ロード時に `ineffective` フラグが一度だけリセットされます。影響を受けたエントリは `4x learn list --ineffective-reset` で確認できます（リセットがディスクに反映されるのは次回の store 書き込み時です）
- アクティブエントリが 100 件を超えると `4x learn prune` を促す警告が表示されます（エントリは自動削除されません）

---

## `4x mine`

`.4x/` 全履歴を失敗シグナルでスキャンし、候補プールを `.4x/candidates.json` に集約します。auto-discovery（単一実行の Deep Review PASS 時にのみ発動し `[NEW-FEATURE]` マーカーを解析）とは異なり、miner は**すべての** Feature を対象に最も濃密な失敗データ（エスカレーション、スタックした Feature、繰り返しのレビュー失敗）をスイープします。

miner は純粋な CLI/プロトコル層スキャンです。LLM を呼び出さず、Feature を作成しません。候補の生成のみを行い、候補を実際の Feature に昇格させるかどうかは F097 ゲートが決定します。

```
4x mine                          # スキャンして .4x/candidates.json を書き込む
4x mine --dry-run                # 書き込まずにサマリーを表示
4x mine --min-occurrences 5      # 失敗パターンの閾値を上げる（デフォルト 3）
4x mine --output path.json       # カスタムパスに書き込む
```

| フラグ | デフォルト | 説明 |
|---|---|---|
| `--min-occurrences` | `3` | 繰り返しレビュー問題が候補になるために必要な distinct-feature 数 |
| `--output` | `.4x/candidates.json` | 候補プールの出力パス |
| `--dry-run` | `false` | サマリーのみ表示、書き込みなし |

3つのスキャナーがプールに候補を供給し、それぞれがトレーサビリティのために `source` をタグ付けします：

- **escalation** — 各ラウンドの `escalation.json`（`spec-mismatch` / `criteria-wrong` / `blocker` / `scope-change`）を読み込む
- **stuck** — `needs-attention` / `abandoned` / `blocked` でスタックした Feature。ブロック理由は `state.json` または最新エスカレーションから抽出
- **fail-pattern** — `>= --min-occurrences` 個の distinct Feature にまたがって繰り返すレビュー / Deep Review FAIL 問題（Jaccard 類似度でクラスタリング）

スキャンはベストエフォートです。1つの壊れた Feature は警告のみでスキャン全体を中断しません。候補は既存 Feature、前回の `candidates.json`、およびバッチ内で重複排除（Jaccard）されます。

---

## `4x config`

ユーザーレベルの設定（`~/.4x/settings.json`）を管理します。

```
4x config list          # 全ユーザー設定を表示
4x config get <key>     # 値を取得
4x config set <key> <value>  # 値を設定
```

キーはドット区切りのパスです。サポートされる形式：

| キー | 例 | 説明 |
|---|---|---|
| `locale` | `4x config set locale zh-TW` | UI / プロンプトのロケール |
| `theme` | `4x config set theme dark` | ダッシュボードのテーマ |
| `default_runner` | `4x config set default_runner claude` | デフォルトのランナープラグイン |
| `runner.<name>.<field>` | `4x config set runner.claude.model opus` | ランナーごとの `command`/`model`/`tty`/`stdin`/`quiet` |
| `role.<name>.<field>` | `4x config get role.deep-reviewer.model` | ロールごとの `model`/`deep_model`/`parallel_reviewers`/`angles_per_reviewer` |

`role.deep-reviewer.parallel_reviewers` は Deep Review がファンアウトする並列サブレビュアーの数を制御します（`1` = 単一エージェントフォールバック）。`role.deep-reviewer.angles_per_reviewer` は各グループのアングル数を固定します（未設定の場合は自動 `ceil(11/N)` バランシング）。詳細は [コンセプト → 並列 Deep Review](concepts.md) を参照してください。

---

## `4x sync`

既存プロジェクトに埋め込みプラグインファイルを再デプロイします。

```
4x sync [--dry-run]
```

| フラグ | 説明 |
|---|---|
| `--dry-run` | ファイルを書き込まずに差分を報告 |

各ファイルを created、updated、current として報告します。

---

## `4x skills`

このリポジトリの `skills/` ディレクトリに同梱された skill を管理します。インストールは **symlink のみ** — 4x は `skills/<name>/` を `~/.claude/skills/<name>` にリンクするため、後で `git pull` すると再インストールせずに skill が自動更新されます。これらのコマンドは 4x リポジトリ内で実行してください（`skills/` ディレクトリはカレントディレクトリから上に辿って検出されます）。

```
4x skills list [--json]     # 利用可能な skill を一覧表示（名前 + 説明）
4x skills install <name>    # skills/<name>/ を ~/.claude/skills/<name> にリンク
4x skills remove <name>     # ~/.claude/skills/<name> の symlink を削除
```

- `list` はインストール済みの skill を `✓` で示し、owner-only の skill（例: `4x-autopilot`）を WARNING で表示します。
- `install` は冪等です — すでにリンク済みの skill を再インストールしても何も起きません。実ディレクトリや別の場所を指す symlink の上書きは拒否します。
- `remove` は symlink のみを削除します。リポジトリ内のファイルは決して削除せず、実体（symlink でない）エントリの削除は拒否します。

`4x-autopilot` をインストールすると WARNING が表示されます: これは owner-only（完全自動マージ）です。

---

## `4x live [path...]`

4x Live ダッシュボードサーバーを起動します。

```
4x live [path...] [flags]
```

| フラグ | 短縮形 | デフォルト | 説明 |
|---|---|---|---|
| `--port` | `-p` | `4567` | サーバーポート |
| `--web` | `-w` | `false` | ブラウザで開く |
| `--app` | `-a` | `false` | macOS ネイティブアプリで開く |

パスなしの場合、`~/.4x/recent-projects.json`（LRU、最大20件）から最近のプロジェクトを読み込みます。パス指定時は、それぞれをプロジェクトタブとして開きます。

---

## `4x guard-tool`

<!-- alias: 4x guardtool -->
内部の PreToolUse フック（隠しコマンド、機械用）。`claude` runner が `reviewer`/`deep-reviewer` ロール向けに注入します。ラウンドの review-package.md が存在する場合、レビュアー自身の `git diff`/`git log`/`git show` 呼び出しは review-package.md を指すメッセージとともにソフトに拒否されます。Claude Code の hook JSON を stdin から、`FOURX_ROLE` / `FOURX_REVIEW_PACKAGE` 環境変数を読み取ります。parse 失敗や非該当コマンドは許可されます（exit 0）。build/test/lint や他のロールをブロックせず、実行を失敗させることもありません。

```
echo '{"tool_name":"Bash","tool_input":{"command":"git diff HEAD"}}' | FOURX_ROLE=reviewer FOURX_REVIEW_PACKAGE=/path/to/review-package.md 4x guard-tool
```

---

## `4x mcp`

Model Context Protocol (MCP) サーバーを起動します。

```
4x mcp
```

4x MCP stdio サーバーを起動し、4x CLI コマンドを MCP ツールとして LLM クライアント（Claude Code、Cursor など）に公開します。
