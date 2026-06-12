# CLI リファレンス

すべての feature-id 引数は大文字小文字を区別しないプレフィックスマッチに対応しています。`4x run f001`、`4x run F001-user`、`4x run F001` はすべて `F001-user-authentication-w` に解決されます。曖昧なプレフィックスは一致候補を列挙してエラーになります。

---

## `4x init`

カレントディレクトリに `.4x/` ワークスペースを初期化します。

```
4x init
```

- プロジェクトの言語とビルド/テスト/lint コマンドを自動検出
- 4つのデフォルトランナー（claude、codex、gemini、agy）を含む `.4x/settings.json` を作成
- 埋め込みプラグインファイルを `.4x/plugins/` にデプロイ
- ルートレベルのファイル（CLAUDE.md、AGENTS.md、GEMINI.md、AGY.md）に `@import` 行を追加
- `.4x/` が既に存在する場合はエラー

---

## `4x new <title>`

新しい Feature を作成します。

```
4x new "Feature title" [--repo <repo>...] [--json]
```

| フラグ | 説明 |
|---|---|
| `--repo` | スコープ内のリポジトリ（マルチリポジトリの場合に繰り返し指定可能） |
| `--json` | JSON 形式で出力 |

ステータス `not-started` で `.4x/features/F{NNN}-{slug}.yaml` を作成します。

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

ループの流れ：init → designing → coding → reviewing → testing → accepting → pending-review。レビュー失敗時はコードが再実行されます。テスト失敗時はコーディングに戻ります。

フィーチャーが `blocked` または `needs-attention` フェーズにある場合、現在のロールに基づいて適切な再開フェーズに自動復旧します。

依存関係ゲートを自動チェックします -- 依存先の Feature が完了していない場合はブロックされます。

設定で `isolation: "worktree"` が指定されている場合、`.worktrees/4x/<feature-id>/` 下の git worktree で実行されます。

---

## `4x status [feature-id]`

Feature のステータスを表示します。

```
4x status              # 全 Feature を状態別にグループ表示
4x status <feature-id> # 単一 Feature の詳細とサブタスク
4x status --pending    # pending-review の Feature をフィルタ
4x status --json       # JSON 形式で出力
```

| フラグ | 説明 |
|---|---|
| `--pending` | pending-review の Feature をフィルタ |
| `--json` | JSON 形式で出力 |

グループ：Running、Review、Pending、Todo、Done（done は最大5件表示）。バックログドリフト警告を含みます。

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

ステートマシンに従って遷移が合法かを検証します。状態が存在しない場合は自動初期化します。`testing → accepting` の遷移では追加ゲートが実行されます（verify.json、test-report.md、final-report.md、commit-plan.md が存在し、verify が合格している必要があります）。

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

ロケール注入（ユーザー設定または `LANG` 環境変数から）、計画ドキュメントの自動インクルード（`docs/design/{id}-spec.md` および `{id}-plan.md`）、プロジェクト/ロールインクルードに対応しています。

---

## `4x done <feature-id>`

pending-review 状態の Feature を完了としてマークします。

```
4x done <feature-id>
```

Feature が `pending-review` フェーズにある場合のみ動作します。その他のフェーズではエラーになります。

---

## `4x config`

ユーザーレベルの設定（`~/.4x/settings.json`）を管理します。

```
4x config list          # 全ユーザー設定を表示
4x config get <key>     # 値を取得
4x config set <key> <value>  # 値を設定
```

現在サポートされているキー：`locale`。

---

## `4x upgrade`

既存プロジェクトに埋め込みプラグインファイルを再デプロイします。

```
4x upgrade [--dry-run]
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
4x batch next
```

### `4x batch run`

依存関係の順序に従って Feature を逐次実行します。

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>]
```

| フラグ | デフォルト | 説明 |
|---|---|---|
| `--runner` | 設定のデフォルト | ランナープラグイン名 |
| `--max-rounds` | `5` | Feature あたりの最大ラウンド数 |
| `--timeout` | `3600` | フェーズごとのタイムアウト（秒） |

Feature 間で `.4x/batch-stop` ファイルをチェックし、グレースフルシャットダウンを行います。

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
