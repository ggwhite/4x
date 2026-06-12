# 4x Live ダッシュボード

AI 開発ループのリアルタイム監視。

## ダッシュボードの起動

```bash
# 最近のプロジェクトで起動
4x live

# 特定のプロジェクトを開く
4x live /path/to/project1 /path/to/project2

# カスタムポート
4x live -p 8080

# ブラウザで自動的に開く
4x live -w

# macOS ネイティブアプリで開く
4x live -a
```

## マルチプロジェクト対応

ダッシュボードは複数のプロジェクトを同時にサポートします。パス引数なしの場合、`~/.4x/recent-projects.json`（LRU、最大20エントリ）から読み込みます。

## サーバー API

ダッシュボードは REST と SSE のエンドポイントを公開します：

### REST

| エンドポイント | メソッド | 説明 |
|---|---|---|
| `/api/tasks` | GET | 全 Feature をリスト |
| `/api/new` | POST | 新しい Feature を作成 |
| `/api/run` | POST | Feature の実行を開始（`4x run` サブプロセスを起動） |
| `/api/stop` | POST | 実行中の Feature を停止 |
| `/api/done` | POST | Feature を完了としてマーク |
| `/api/runs` | GET | アクティブな実行をリスト |
| `/api/events/{id}` | GET | Feature のイベントを取得 |
| `/api/messages/{id}` | GET | Feature のメッセージを取得 |
| `/api/logs/{id}` | GET | Feature のログファイルをリスト |
| `/api/logs/{id}/{file}` | GET | 特定のログファイルを取得 |
| `/api/projects` | GET | 登録済みプロジェクトをリスト |
| `/api/projects` | POST | プロジェクトを追加（オンザフライ初期化のために `init: true` をサポート） |
| `/api/projects` | DELETE | プロジェクトを削除 |
| `/api/browse` | GET | フォルダピッカー |

### SSE（Server-Sent Events）

| エンドポイント | 説明 |
|---|---|
| `/sse/events/{id}` | Feature のイベントをストリーミング（1秒ポーリング） |
| `/sse/logs/{id}` | Feature の最新ログファイルをストリーミング |

### マルチプロジェクトルーティング

複数プロジェクトの場合、エンドポイントには `/api/project/{project-id}/...` と `/sse/project/{project-id}/...` のプレフィックスが付きます。単一プロジェクトモードでは、後方互換性のためにプレフィックスなしのパスを使用します。

## プロセス管理

ダッシュボードはランナーサブプロセスを管理します：

- プロジェクト設定の `max_concurrent_runs` を尊重
- stdout/stderr を run-output/run-error イベントとしてキャプチャ
- グレースフルシャットダウン：SIGTERM → 5秒 → SIGKILL

## プラットフォーム

| プラットフォーム | ステータス |
|---|---|
| Web UI（埋め込み） | 利用可能 |
| macOS ネイティブ（Swift） | 計画中 |
| Electron（Windows/Linux） | 計画中 |
