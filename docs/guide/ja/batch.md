# バッチモード

依存関係を考慮した順序で複数の Feature を実行します。

## ワークフロー

```bash
# 1. 実行計画を生成
4x batch plan

# 2. 次に何があるか確認
4x batch next

# 3. 対象の Feature をすべて実行
4x batch run --runner claude

# 4. グレースフルに停止（現在の Feature を完了してから終了）
4x batch stop
```

## 計画作成

`4x batch plan` はすべての未完了 Feature（`abandoned` や `ready-for-review` を含む）を分析し、`.4x/batch-plan.json` を生成します：

1. **依存関係 DAG** -- Feature の `depends` フィールドから有向グラフを構築
2. **循環検出** -- 循環依存がある場合はエラー
3. **Union-Find クラスタリング** -- 非ハブリポジトリを共有する Feature をグループ化（`hub_repos` 設定または `workspace.repos[*].hub: true` で定義されたハブリポジトリはクラスタリングから除外）
4. **トポロジカルソート** -- 各クラスター内で Feature を順序付け。複数の Feature が同時に対象になった場合（未解決の依存関係がない）、`priority` で順序付けされます（数値が小さい = 優先度が高い。優先度のない Feature は最後にソート）。優先度が同じ場合は Feature ID で安定的・決定的な順序になります。
5. **チェーンスケジューリング** -- 長い依存チェーンを分割（最大長は `--max-chain` で設定可能）

```bash
# 計画をプレビュー
4x batch plan --dry-run

# チェーン長を制限
4x batch plan --max-chain 3
```

出力例：

```
  cluster-1: F001-auth → F003-oauth | F002-api
  cluster-2: F004-payment

Schedule (4 features):
  [slot 1] F001-auth —
  [slot 2] F002-api —
  [slot 2] F004-payment —
  [slot 3] F003-oauth after [F001-auth]
```

## 実行

`4x batch run` は依存関係の順序に従って Feature を逐次実行します：

```bash
4x batch run --runner claude --max-rounds 3 --timeout 7200
```

- `--runner` はオプションです。省略時はワークスペース設定のデフォルトランナーにフォールバック
- コミット戦略は `"never"`（分離なし）または `"per-round"`（worktree 分離 -- Feature worktree 内でラウンドごとに自動コミット）を使用
- Feature 間で `.4x/batch-stop` ファイルをチェック
- 依存先が完了していない Feature はスキップ（依存関係は `done`、`abandoned`、または `ready-for-review` で完了とみなされます。`feature.BatchCompleted` を参照）
- 各 Feature の実行前にランタイムで依存関係が再チェックされます。未解決の場合、Feature は `blocked` としてマークされスキップ
- 2回失敗した Feature（`needs-attention`、`blocked`、または `in-progress` のまま）は、バッチ実行の残りでスキップされます
- 各 Feature の完了後に進捗を報告

Feature が `ready-for-review` に到達すると、自動的に main にマージされ `done` としてマークされます。次の Feature の worktree は更新された main から開始されます。`--no-auto-merge` を指定すると無効になり、Feature は `ready-for-review` で停止します。マージコンフリクト時はバッチが一時停止します（[マージコンフリクト](#merge-conflicts) を参照）。コンフリクト以外のエラーは警告を出力し、バッチは次の Feature に進みます。

```bash
# 自動マージなしで実行
4x batch run --runner claude --no-auto-merge
```

> **注意：** `batch run` は内部で常にスクラッチからプランを再生成します（既存の `batch-plan.json` は無視）。また、`batch plan` より厳密なフィルタを使用します -- `done`、`abandoned`、`ready-for-review` の Feature は実行から除外されます。`batch plan` はスケジュールのプレビューや `batch next` への入力に便利ですが、`batch run` の前提条件ではありません。

## 停止

```bash
4x batch stop
```

`.4x/batch-stop` シグナルファイルを作成します。バッチは現在の Feature を完了した後、グレースフルに終了します。

## マージコンフリクト

自動マージでコンフリクトが発生すると、バッチは一時停止し、`.4x/batch-conflict.json` に Feature、コンフリクトリポジトリ（マルチリポジトリモード）、影響を受けるファイルを記録します。worktree は保持されるため、コンフリクトを解決できます。シグナルファイルにより[ダッシュボード](dashboard.md)がコンフリクトを表示し、**Continue Batch** アクションを提供します。内部的にはシグナルファイルをクリアして `4x batch run` を再開します。CLI からは、ファイルを解決し、`4x merge <id>` を実行してから `4x batch run` を再実行して続行します。コンフリクトファイルは各バッチ実行の開始時に自動クリアされます。

## 実行レポート

すべてのバッチ実行は終了時に `.4x/batch-report.json` を書き込みます。正常終了、停止、中断、クラッシュのいずれでも書き込まれます。レポートには全体統計（total / completed / failed / remaining）、ランナー、合計所要時間、各 Feature の名前、最終ステータス、所要時間、ラウンド数、停止理由が記録されます。

`outcome` フィールドは実行の終了方法を示します：

- `completed` -- すべての Feature が完了
- `stopped` -- Stop（`.4x/batch-stop`）を押したか、自動マージコンフリクトで一時停止
- `interrupted` -- バッチプロセスが `SIGTERM`/`SIGINT` を受信；実行中だった Feature がレポートに記録される
- `crashed` -- バッチプロセスがパニック；レポートはベストエフォートで `panicMessage` を含む

[ダッシュボード](dashboard.md)はバッチが実行されていない時にこのファイルを読み、「前回のバッチレポート」サマリーカードを表示します。Feature ごとの詳細に展開できます。レポートは実行停止後にのみ書き込まれ、Feature ごとの実行ループ内では書き込まれないため、バッチスループットへのオーバーヘッドはありません。

## 進捗の確認

```bash
# 次の Feature を確認（Feature ID を出力）
4x batch next

# サブタスクフロンティア付き JSON 出力
4x batch next --json

# 全 Feature の概要
4x status
```

`--json` を指定すると、サブタスク依存関係フロンティア（すべての依存関係が完了し、作業可能なサブタスクのセット）が出力に含まれます：

```json
{
  "featureId": "F044-subtask-frontier",
  "slot": 0,
  "subtaskFrontier": ["parse-depends", "build-dag"]
}
```

対象の Feature がない場合は `null` を返します。
