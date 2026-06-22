# 4x Live ダッシュボード

AI 開発ループのリアルタイム監視。

## macOS Gatekeeper

4x Live アプリは Apple Developer 証明書で署名されていません。macOS が初回起動時にブロックします。

**方法 A：隔離属性を削除（推奨）**

```bash
xattr -cr /Applications/4x\ Live.app
```

**方法 B：システム設定から許可**

1. アプリをダブルクリック — macOS が「開発元を確認できないため開けません」と表示
2. **システム設定 → プライバシーとセキュリティ**を開く
3. **セキュリティ**セクションまでスクロール — ブロックされたアプリのメッセージが表示される
4. **このまま開く**をクリック、パスワードを入力するか Touch ID で確認
5. macOS は次回以降の選択を記憶します

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

プロジェクトタブバーの末尾には **Add Project**（フォルダプラスアイコン）と **Global Settings**（歯車アイコン）の2つのアクションがあります。サイドバーヘッダーにはアクティブプロジェクトの **Project Settings** 歯車と、その隣に **Clean** ボタン（ゴミ箱アイコン）があります。Clean をクリックすると確認ダイアログが表示され、クリーンされた Feature は詳細なログ、レポート、ラウンド履歴をダッシュボードで失う旨が警告されます（Feature 定義とステータスは保持されます）。確認すると、プロジェクト全体に対して [`POST /api/clean`](#post-apiclean) が呼び出され、結果がトーストで表示されます。

## Feature カード

各 Feature カードには、優先度、依存関係、停止理由（Feature が異常停止した場合）のタグが表示されます。デフォルト以外の[パイプラインプロファイル](concepts.md#pipeline-profiles)がアクティブな場合は、**プロファイルタグ**（例：`quick`、`normal`）も表示されます。高優先度の Feature（P0/P1）にはアクセントボーダーが付きます。完了した依存関係には緑のチェックマークが表示されます。`profile`、`stopReason`、`stopMessage` フィールドは `/api/tasks` JSON に含まれます。`stopReason` は短いカテゴリコード（例：`runner-error`、`guard-fail`、`no-progress`）で色分けに使用され、`stopMessage` はカテゴリラベルの下に表示される人間が読める詳細説明です。

## 新規 Feature フォーム

**New Feature** モーダルはプログレッシブフォームです。基本エリアには常に **Name**（必須）、**Description**（オプション、デフォルトは名前）、**Priority** セレクト（P0-P3 または未設定）が表示されます。**Advanced** トグルで **Custom ID**（空欄で自動生成）、**Depends**（カンマ区切りの Feature ID）、**Rules**（カンマ区切り）、動的な **Subtasks** リスト（id + name の行を追加/削除）が表示されます。送信すると [`POST /api/new`](#rest) にリクエストされます。CLI の `4x new` とダッシュボードは単一の作成パス（`feature.Create`、[コンセプト](concepts.md#feature-creation) を参照）を共有するため、同じフラグ/フィールドと ID 生成に準拠します。

## 依存関係 DAG

概要画面では、すべての Feature の依存関係グラフがインライン SVG としてレンダリングされます。外部チャートライブラリ（d3、mermaid、chart.js）は読み込みません。Feature は依存関係の深さでレイヤー配置され、エッジは各 Feature からその依存先に描画されます。ノードの色はフェーズステータスに従います：緑 = done、青 = 実行中（アクティブな実行、または coding/reviewing/testing のような進行中フェーズ）、灰色 = todo、赤 = blocked / needs-attention。ノードをクリックするとその Feature の詳細が開きます（Feature カードのクリックと同じパス）。グラフはポーリングサイクルごとにキャッシュされた `/api/tasks` データから再構築されるため、Feature の進行に合わせてリアルタイムに色が更新されます。

## バッチパネル

概要画面には[バッチ制御 API](#batch-control) をバックエンドとするバッチコントロールパネルもあります。**Start / Stop / Continue Batch** ボタン（Start は実行前に確認表示）、実行中インジケーター、Feature ごとの進捗（完了チェック、実行中マーカー、待機位置）を含むスケジュールキュー、マージコンフリクトでバッチが一時停止した場合は Feature、リポジトリ、コンフリクトファイルを表示するコンフリクトカードと Continue Batch アクションが表示されます。パネルはダッシュボードの他の部分と同じポーリングループで `GET /api/batch/status` から更新されます。

## サーバー API

ダッシュボードは REST と SSE のエンドポイントを公開します：

読み取り頻度の高いエンドポイント（`/api/tasks`、`/api/overview`、`/api/projects`、`/api/settings` など）は、プレーンな `*protocol.Workspace` ではなく `*protocol.CachedWorkspace` を通じて提供されます。サーバーは長時間実行されるため、この mtime ベースのインメモリキャッシュにより、各リクエストですべての Feature YAML と `settings.json` を再パースする必要がなくなります。[ワークスペース読み取りキャッシュ](concepts.md#workspace-read-cache-dashboard-server) を参照してください。キャッシュの無効化は自動的に行われます：書き込み（ダッシュボードまたはランナー経由）でファイルの mtime が変わるため、次の読み取りで透過的に再パースされます。

### REST

| エンドポイント | メソッド | 説明 |
|---|---|---|
| `/api/tasks` | GET | 全 Feature をリスト（Feature YAML にフォーマット問題がある場合 `warnings` 配列を含む） |
| `/api/new` | POST | 新しい Feature を作成（`name`、`description`、オプションの `customId`、`priority`、`depends`、`rules`、`subtasks` を受け付け） |
| `/api/run` | POST | Feature の実行を開始（`4x run` サブプロセスを起動） |
| `/api/stop` | POST | 実行中の Feature を停止 |
| `/api/done` | POST | Feature を完了としてマーク；worktree がある場合は自動マージ（マルチリポジトリ：オール・オア・ナッシング） |
| `/api/clean` | POST | プロジェクト内のすべてのクリーン可能な（done/abandoned）Feature のワークスペースアーティファクトを削除 |
| `/api/runs` | GET | アクティブな実行をリスト |
| `/api/batch/start` | POST | バッチ実行を開始（`4x batch run` サブプロセス）；未解決のコンフリクトがある場合は 409 |
| `/api/batch/stop` | POST | バッチをグレースフルに停止（`.4x/batch-stop` を書き込み） |
| `/api/batch/continue` | POST | コンフリクトシグナルをクリアしてバッチを再開（worktree でのコンフリクト解決後） |
| `/api/batch/status` | GET | バッチの実行状態、スケジュールキュー、現在の Feature、コンフリクトシグナル |
| `/api/events/{id}` | GET | Feature のイベントを取得 |
| `/api/overview/{id}` | GET | Feature の概要を取得（YAML フィールド + spec/plan 内容、共有 `protocol.ResolveDesignDoc` で解決。[Design Doc Resolution](concepts.md#design-doc-resolution) を参照） |
| `/api/messages/{id}` | GET | Feature のメッセージを取得 |
| `/api/evolve-report` | GET | 最新の `4x evolve` ラウンドサマリー（`.4x/evolve-report.md`）；`{content, exists}`、存在しない場合 `exists:false` |
| `/api/features/{id}/screenshots` | GET | ラウンドごとにグループ化されたスクリーンショットを取得 |
| `/api/features/{id}/screenshots/{filename}` | GET | 1枚のスクリーンショット画像を提供 |
| `/api/logs/{id}` | GET | Feature のログファイルをリスト |
| `/api/logs/{id}/{file}` | GET | 特定のログファイルを取得 |
| `/api/projects` | GET | 登録済みプロジェクトをリスト |
| `/api/projects` | POST | プロジェクトを追加（オンザフライ初期化のために `init: true` をサポート） |
| `/api/projects/{id}` | DELETE | プロジェクトを削除 |
| `/api/browse` | GET | フォルダピッカー |
| `/api/settings` | GET | プロジェクト設定を取得（`.4x/settings.json`） |
| `/api/settings` | PUT | プロジェクト設定を更新（バリデーション、バックアップ、書き込み） |
| `/api/user-config` | GET | ユーザー設定を取得（`~/.4x/settings.json`） |
| `/api/user-config` | PUT | ユーザー設定を更新（`.bak` にバックアップ後書き込み） |
| `/api/merged-config` | GET | プロジェクト + ユーザーのマージ済み有効設定の読み取り専用ビュー |
| `/api/locales` | GET | サポートされるロケール一覧を返却 |
| `/api/locales/{lang}` | GET | 対応言語の翻訳 JSON を返却 |
| `/api/supported-runners` | GET | サポートされるランナー名をリスト |

#### `POST /api/done` のレスポンス

通常は HTTP 200 を返します。`status` フィールドは状態遷移が成功した場合のみ `"done"` になります。マージコンフリクトまたはマージエラーが発生した場合、`status` は `"pending-review"` のままです。追加フィールドでマージ結果を示します：

| フィールド | 型 | 意味 |
|---|---|---|
| `merged` | bool | `true` ブランチがマージされ worktree がクリーンアップされた場合 |
| `merged` | bool | `false` worktree が存在しなかった場合（状態のみの遷移） |
| `merge_conflict` | bool | `true` マージにコンフリクトがあった場合；worktree は保持 |
| `merge_error` | string | マージエラーメッセージ；Feature は pending-review のまま |
| `conflicts` | string[] | コンフリクトファイルのリスト（`merge_conflict: true` の場合のみ） |

コンフリクト後、worktree 内のファイルを解決して `4x merge <id>` を実行してください。

マージ実行中に Feature のフェーズが変更された場合（ランナーやバックグラウンドリコンシラーがマージ中に `state.json` を更新した場合）、エンドポイントは **HTTP 409 Conflict** を `{"status":"<currentPhase>","error":"state changed during merge"}` とともに返し、done 遷移を実行しません。これは、ステイルなマージ前のスナップショットで新しい状態を上書きすることを防ぎます。

#### `POST /api/clean`

プロジェクト内のすべてのクリーン可能な Feature の `.4x/{feature-id}/` ワークスペースアーティファクト（ログ、`rounds/`、レポート、`state.json`、`events.jsonl`）を一括削除します。対象は `4x clean` と同じセット：`done`/`abandoned`、非アクティブ、ワークスペースディレクトリが存在するもの。Feature 定義（`.4x/features/*.yaml`）は保持されるため、クリーンされた Feature は最終ステータス付きでリストに表示され続けます。

`POST` 以外のリクエストは **HTTP 405** を返します。各 Feature は個別にクリーンされます。1つが失敗（例：レースでアクティブになった場合）してもスキップされ、残りは中断されません。ハンドラは常に HTTP 200 を返します：

| フィールド | 型 | 意味 |
|---|---|---|
| `cleaned` | int | アーティファクトが削除された Feature の数 |
| `freed` | int64 | 解放された合計バイト数 |
| `freed_human` | string | `freed` の人間が読める形式（例：`38M`） |
| `features` | string[] | クリーンされた Feature の ID（何もクリーンされなかった場合は `[]`） |

クリーン対象がない場合のレスポンス：`{"cleaned":0,"freed":0,"freed_human":"0B","features":[]}`。

#### バッチ制御

ダッシュボードはターミナルに戻らずにバッチ実行をエンドツーエンドで操作できます。専用の `BatchManager`（Feature ごとの `ProcessManager` とは別）が、プロジェクトの単一の `4x batch run` サブプロセスを管理します。一度に実行できるバッチは1つだけです。

- **Start**（`POST /api/batch/start`）-- UI は誤操作を防ぐため事前に確認を表示してから実行を開始します。`.4x/batch-conflict.json` がまだ存在する場合は **HTTP 409** を返すため、ステイルなコンフリクトは先に解決または Continue する必要があります。リクエストボディに `{runner, maxRounds}` を含めることができ、省略されたフィールドはマージ済みのプロジェクト/ユーザー設定にフォールバックします。
- **Stop**（`POST /api/batch/stop`）-- `.4x/batch-stop` を書き込んでグレースフルに停止します（バッチは現在の Feature を完了してから終了）。サブプロセスを kill **しません**。
- **Continue**（`POST /api/batch/continue`）-- `.4x/batch-conflict.json` をクリアしてバッチを再開します。worktree でのコンフリクト解決後に使用します。
- **Status**（`GET /api/batch/status`）-- 実行フラグ、スケジュールキュー、現在の Feature、コンフリクトシグナル（または `null`）、`lastReport`（パース済みの `.4x/batch-report.json`、存在しない場合は省略）を返します：

  ```json
  {
    "running": true,
    "queue": [
      {"featureId": "F001-auth", "name": "Auth", "status": "done", "state": "done", "position": 0},
      {"featureId": "F002-api", "name": "API", "status": "coding", "state": "running", "position": 1}
    ],
    "currentFeature": "F002-api",
    "conflict": null,
    "lastReport": null
  }
  ```

  キューは `batch.PlanBatch` から構築され、CLI と同じ依存関係・優先度順序に従います。各アイテムの `state` は `done`（Feature が done / ready-for-review）、`running`（done でないアクティブな実行）、`error`（blocked / needs-attention）、`waiting` のいずれかです。`position` は未完了アイテム（`done` と `error` を除く）に番号を付けます。

  `lastReport` は最新のバッチ実行レポート（`outcome`、カウント、ランナー、所要時間、Feature ごとの内訳。[バッチモード](batch.md#run-report) を参照）を含みます。バッチが実行されていない場合、パネルはこれを「前回のバッチレポート」サマリーカードとしてレンダリングし、Feature ごとの詳細に展開できます。`crashed` の場合は `panicMessage` も表示されます。

### スクリーンショットタブ

Feature 詳細にはスクリーンショットが存在する場合、**Screenshots** タブが含まれます。スクリーンショットはラウンドごとにグループ化され、サムネイルとして表示されます。ライトボックスで開くと、左右ナビゲーションと ESC で閉じることができます。

### SSE（Server-Sent Events）

| エンドポイント | 説明 |
|---|---|
| `/sse/events/{id}` | Feature のイベントをストリーミング（1秒ポーリング） |
| `/sse/logs/{id}` | Feature のアクティブなログファイルをストリーミング（1つまたは複数） |

イベントストリームは `events.jsonl` のバイトオフセットを追跡し、新しく追記された行のみ送信します。ファイルが**切り詰めまたはローテーション**された場合（例：`4x transition --to init` が Feature をリセットし `events.jsonl` を最初から書き直す）、ファイルサイズが追跡済みオフセットを下回ります。ストリームはこれを検出（`size < lastOffset`）してオフセットを 0 にリセットし、ファイル全体を最初から再読み取りします。サイズがオフセットと等しい場合は「新しいコンテンツなし」としてスキップされます。

ログストリーム（`/sse/logs/{id}`）も同様にバイトオフセットを追跡し、新しく追記されたコンテンツのみ送信します。ティックごとのガーベッジを回避するため、接続ごとに1回だけ割り当てられた固定 32KB 読み取りバッファを再利用します。各ティックでオフセットから EOF までループ読み取りし、32KB を超えるデルタは複数の SSE メッセージに分割され、各メッセージが同じ `{"file": "...", "content": "..."}` ペイロードを持ちます。クライアントは到着順にコンテンツを追記するため、分割は透過的です。`size <= lastOffset`（新しいコンテンツなし）の場合、ファイルを開かずにティックをスキップします。

複数のロールが同時にログを書き込んでいる場合（並列 Deep Review のサブレビュアー、または並行する reviewer + tester）、ストリームは最近更新された1つだけでなく**すべてのアクティブなログ**を tail します。`?file=` クエリパラメータなしでは、最近のウィンドウ内の mtime を持つすべてのログを追跡し（各々が独自のオフセットを持ち）、メッセージごとの `file` フィールドでクライアントが対応するペインにコンテンツをルーティングします。`?file=<name>` を指定すると、ストリームを単一のログにピン止めします。

### マルチプロジェクトルーティング

複数プロジェクトの場合、エンドポイントには `/api/project/{project-id}/...` と `/sse/project/{project-id}/...` のプレフィックスが付きます。単一プロジェクトモードでは、後方互換性のためにプレフィックスなしのパスを使用します。

#### ワークスペース解決

リーフルート（`/api/tasks`、`/api/settings`、`/api/run`、`/api/batch/*`、`/sse/events/...` など）は `NewMux`（`internal/server/server.go`）で**一度だけ**定義されます。`NewMux` は固定のワークスペースをバインドする代わりに `WorkspaceResolver` を受け取ります。これは受信リクエストから対象の `*protocol.CachedWorkspace`、その `*ProcessManager`、`*BatchManager` を返す関数です（またはエラー）。データバックドの各ハンドラは最初にリゾルバを呼び出します。不要なルート（`/api/user-config`、`/api/supported-runners`、`/api/locales`、静的アセット）はスキップします。これにより、シングルプロジェクトモードとマルチプロジェクトモードで以前それぞれ必要だった約150行の重複ハンドラ登録が不要になります。

2つのリゾルバが2つのモードをバックします：

- **`singleResolver(ws, pm)`** -- シングルプロジェクトモード（`server.Start`）。1つのワークスペースをクロージャし、常に同じ `ws`/`pm`/`bm` トリプルを返します。
- **`multiResolver(reg)`** -- マルチプロジェクトモード（`NewMultiMux`）。解決は3ステップのフローです：
  1. **プレフィックスディスパッチ（外部 mux）。** `NewMultiMux` は `/api/project/` と `/sse/project/` ハンドラを登録し、`/api/project/{id}`（または `/sse/project/{id}`）プレフィックスを除去、`getEntry(id)` でエントリを検索（不明な id → **404**）、`r.URL.Path` を残りのサブパスに書き換え、解決済みエントリをリクエスト `context` に注入してから、共有内部 `NewMux` ハンドラに転送します。
  2. **コンテキスト読み取り。** 内部ハンドラ内で、`multiResolver` はステップ 1 で注入されたエントリをリクエストコンテキストから確認し、存在する場合は直接返します。
  3. **プレフィックスなしの互換性。** エントリが注入されていない場合（プレフィックスなしのパス）、`reg.Count()` にフォールバック：`0` → **400** `no projects loaded`、`1` → その唯一のプロジェクト、`>=2` → **400** `multiple projects loaded -- use /api/project/{id}...`。

`NewMultiMux` 自体はグローバルエンドポイント（`/api/projects`、`/api/projects/`、`/api/browse`）と2つのプレフィックスディスパッチャ、および共有 `inner := NewMux(multiResolver(reg))` に転送するキャッチオールのみを登録します。プロジェクトの追加でエントリごとの mux は構築されません。`registryEntry` は `id`/`ws`/`pm`/`bm` のみを保持します。

## キーボードショートカット

| ショートカット | アクション |
|---|---|
| `Cmd+K` | 検索 |
| `Cmd+,` | Project Settings（プロジェクト内）/ Global Settings（ホーム画面） |
| `Cmd+Shift+,` | Global Settings |
| `Esc` | 現在のモーダルを閉じる |

## プロセス管理

ダッシュボードはランナーサブプロセスを管理します：

- プロジェクト設定の `max_concurrent_runs` を尊重
- stdout/stderr を run-output/run-error イベントとしてキャプチャ
- グレースフルシャットダウン：SIGTERM → 5秒 → SIGKILL

ランナーサブプロセスが終了すると、サーバーは Feature を非アクティブ化します（`Active=false`、`StopReason=process-exit`）。これはレースに対するガードがあります：ランナーは終了直前に自身の最終 `state.json`（例：`pending-review`）を書き込む場合があります。サーバーはプロセスの終了時刻を記録し、上書き前に状態を再読み取りします。`state.json` が終了時刻**以降**に更新されていた場合（`UpdatedAt >= endTime`）、ランナーの最終状態が保持され、非アクティブ化書き込みはスキップされます。これにより、サーバーが新しく書き込まれたフェーズを古いインメモリスナップショットで巻き戻すことを防ぎます。

## 共有 Web フロントエンド

ダッシュボードの UI（HTML/CSS/JS + ロケール JSON）は `dashboard/web/` に単一の真実の源として存在し、`dashboard/web/embed.go`（`web.Assets embed.FS`）を通じて `4x` バイナリに埋め込まれます。Go サーバー（`internal/server/server.go`、`internal/server/multi.go`）は `web.Assets` から静的アセットとロケールファイルを直接提供するため、同じフロントエンドがすべてのプラットフォームシェル（Go サーバー Web UI、macOS WKWebView、Tauri webview）をバックします。同期が必要なプラットフォームごとの UI コピーはありません。

## プラットフォーム

| プラットフォーム | シェル | パッケージング |
|---|---|---|
| Web UI（埋め込み） | Go サーバーが `web.Assets` を提供 | `4x live` |
| macOS ネイティブ | Swift WKWebView、バンドルされた `4x live` サーバーを自動起動 | ユニバーサル `.dmg`（`make package-macos`） |
| Windows | Tauri v2 webview、`4x` サイドカー | `.msi`（`dashboard/tauri`） |
| Linux | Tauri v2 webview、`4x` サイドカー | `.AppImage`（`dashboard/tauri`） |

すべてのデスクトップシェルは、埋め込みの `4x` サーバーがバックする `http://localhost:<port>` 上の同じ `dashboard/web/` フロントエンドを読み込みます。`.github/workflows/desktop.yml` の CI マトリックスがプラットフォームごとの `4x` バイナリをクロスコンパイルし、`.dmg` / `.msi` / `.AppImage` アーティファクトを生成します。
