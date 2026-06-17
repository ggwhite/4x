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
| `--subtask` | `"id:name"` または `"id:name:description"` 形式のサブタスク（繰り返し指定可能） |
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

ループの流れ：init → designing → coding → reviewing → testing → deep-reviewing → accepting → pending-review。レビュー失敗時はコードが再実行されます。テスト失敗時はコーディングに戻ります。

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

## `4x batch`

複数 Feature のバッチ操作。

### `4x batch plan`

依存関係を考慮した実行計画を生成します。

```
4x batch plan [--dry-run] [--max-chain <n>]
```

| フラグ | デフォルト | 説明 |
|---|---|---|
| `--dry-run` | `false` | ファイルに書き込まずにスケジュールを表示 |
| `--max-chain` | `4` | クラスターあたりの最大チェーン長 |

`.4x/batch-plan.json` に書き込みます。

### `4x batch next`

次に実行可能な Feature を表示します（計画と現在のステータスに基づく）。

```
4x batch next [--json]
```

| フラグ | デフォルト | 説明 |
|---|---|---|
| `--json` | `false` | サブタスクフロンティア付き JSON 形式で出力 |

`--json` なしの場合、プレーンテキストで Feature ID を出力します（後方互換）。`--json` の場合、`subtaskFrontier`（依存関係がすべて完了しているサブタスク）を含む JSON オブジェクトを出力します。対象の Feature がない場合、JSON モードでは `null` を返します。

### `4x batch run`

依存関係の順序に従って Feature を逐次実行します。

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>] [--no-auto-merge]
```

| フラグ | デフォルト | 説明 |
|---|---|---|
| `--runner` | 設定のデフォルト | ランナープラグイン名 |
| `--max-rounds` | `5` | Feature あたりの最大ラウンド数 |
| `--timeout` | `3600` | フェーズごとのタイムアウト（秒） |
| `--no-auto-merge` | `false` | 完了した Feature を自動マージせず `pending-review` のままにする |

Feature 間で `.4x/batch-stop` ファイルをチェックし、グレースフルシャットダウンを行います。

実行が終了すると（正常終了、停止、中断（`SIGTERM`/`SIGINT`）、クラッシュのいずれでも）、`.4x/batch-report.json` が書き込まれます。レポートには `outcome`、完了/失敗/残りのカウント、ランナー、所要時間、各 Feature の最終ステータスが含まれます。詳細は [バッチモード → 実行レポート](batch.md#run-report) を参照してください。

デフォルトでは、Feature が完了（`pending-review` に到達）すると、バッチは worktree ブランチを自動的に main にマージし、次の Feature が更新された main からブランチを作成します。これにより、無人での連続バッチが可能になります。マージコンフリクトが発生するとバッチはグレースフルに一時停止し、Feature を `pending-review` のまま、worktree を保持した状態で `.4x/batch-conflict.json` シグナルファイル（Feature、コンフリクトリポジトリ、ファイル）を書き込みます。[ダッシュボード](dashboard.md)でコンフリクトを確認できます。コンフリクトを解決して `4x merge <id>` を実行し、`4x batch run` を再実行して続行してください。コンフリクトシグナルは各実行の開始時にクリアされます。コンフリクト以外のマージエラーは警告を出力し、バッチは次の Feature に進みます。`--no-auto-merge` を指定すると、従来の動作に戻ります（Feature は手動レビュー用に `pending-review` で停止）。

設定で `isolation: "worktree"` が指定されている場合、各 Feature は独立した worktree で実行されます。マルチリポジトリモードでは、各 Feature はコンポジット worktree（`.worktrees/4x/<feature-id>/`）を持ち、リポジトリごとのサブディレクトリがあります。コミットは完了時ではなくラウンドごとに行われます。ハブリポジトリ（`hub_repos` 設定または `workspace.repos[*].hub: true`）は共有リポジトリクラスタリングから除外され、並列実行が可能です。

### `4x batch stop`

現在の Feature 完了後に実行中のバッチを停止するよう通知します。

```
4x batch stop
```

`.4x/batch-stop` シグナルファイルを作成します。

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

## `4x mcp`

Model Context Protocol (MCP) サーバーを起動します。

```
4x mcp
```

4x MCP stdio サーバーを起動し、4x CLI コマンドを MCP ツールとして LLM クライアント（Claude Code、Cursor など）に公開します。
