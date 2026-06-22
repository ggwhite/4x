# 設定

## プロジェクト設定（`.4x/settings.json`）

`4x init` によって作成されます。プロジェクトメタデータ、ランナー定義、ロールモデルのマッピングが含まれます。

**4x Live ダッシュボード**からも視覚的に編集できます。「4x Live」タイトル横の歯車アイコン（⚙）をクリックするか、`Cmd+Shift+,` を押してください。エディタはフォームビューと生 JSON ビューの両方をサポートし、必須フィールドのバリデーションを行い、書き込み前に以前の設定を `settings.json.bak` にバックアップします。

```json
{
  "project": {
    "name": "my-project",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["go vet ./..."],
    "setup": [],
    "docs": [],
    "rules": []
  },
  "runners": {
    "claude": {
      "command": "claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}", "--output-format", "stream-json", "--verbose"],
      "model": "opus",
      "output_format": "stream-json"
    },
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    },
    "gemini": {
      "command": "gemini",
      "args": ["-y", "-p", "{prompt}"]
    },
    "agy": {
      "command": "agy",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"]
    }
  },
  "default_runner": "claude",
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

### Project セクション

| フィールド | 説明 |
|---|---|
| `name` | プロジェクト名（ディレクトリから自動検出） |
| `language` | 検出された言語 |
| `build` | ビルドコマンド |
| `test` | テストコマンド |
| `lint` | lint コマンド |
| `setup` | セットアップコマンド（例：`docker-compose up -d`） |
| `description` | プロジェクトの説明（オプション） |
| `docs` | Designer が参照するドキュメントファイルのパス |
| `rules` | ロールプロンプトに注入されるプロジェクト固有のルール |
| `includes` | ロールプロンプトにインクルードするファイル |

### Runner 設定

| フィールド | 説明 |
|---|---|
| `command` | 実行ファイル名 |
| `args` | 引数。`{prompt}` と `{promptFile}` は実行時に置換されます。`{model}` はロールのモデルに置換されます。 |
| `model` | このランナーのデフォルトモデル |
| `tiers` | ティア名をランナー固有のモデル名にマッピング（例：`{"opus": "claude-opus-4-5-20250514"}`）。検索順序：ロール model → tiers 変換 → 元の名前にフォールバック。 |
| `output_format` | `"stream-json"` にすると、runner stdout を読みやすい `.log` と raw `.stream.jsonl` に分けて記録します。 |
| `tty` | 出力キャプチャに PTY を使用します。`output_format` が `"stream-json"` の場合は PTY を使いません。 |
| `stdin` | 引数の代わりに標準入力でプロンプトを送信（Codex が使用） |
| `quiet` | ランナーのターミナル stdout 出力を抑制。出力はログファイルに記録されます。 |

`{model}` が `args` に含まれない場合、ランナーは自動的に `--model <model>` を追加します。

### Role 設定

| フィールド | 説明 |
|---|---|
| `model` | このロールのモデル名 |
| `deep_model` | 敵対的レビューパスのモデル（reviewer のみ） |
| `max_fix_rounds` | `deep-reviewing` フェーズでの最大自己修復イテレーション数（`deep-reviewer` のみ、デフォルト 2）。各イテレーションはスコープ付き mini-coder + re-verifier を実行し、上限を超えると `needs-attention` にエスカレーションされます。 |
| `instructions` | ロールプロンプトに注入される追加の指示 |
| `includes` | ロールプロンプトにインクルードするファイル |
| `screenshot_dir` | Tester のスクリーンショットディレクトリパス |
| `parallel_reviewers` | Deep Review の並列サブレビュアー数（deep-reviewer のみ、<=1 で単一エージェントモードにフォールバック） |
| `angles_per_reviewer` | サブレビュアーあたりのレビューアングル数（deep-reviewer のみ、0 で自動均等配分） |

### その他の設定フィールド

| フィールド | 説明 |
|---|---|
| `hub_repos` | 共有リポジトリ（バッチ DAG のグルーピング用） |
| `isolation` | `"worktree"` に設定すると、Feature を git worktree で実行 |
| `max_concurrent_runs` | ダッシュボードサーバー経由の最大同時実行数 |
| `commit` | コミット戦略：`"per-round"`（デフォルト）、`"on-done"`、または `"never"` |
| `profiles` | 名前付きパイプラインプロファイル（ロールサブセット）；[プロファイル](#profiles) を参照 |
| `parallel_review_test` | reviewing フェーズ中に reviewer と tester を並行実行（デフォルト `false`） |
| `auto_discover_features` | Deep Review レポートの `[NEW-FEATURE]` マーカーから Feature を自動作成（デフォルト `false`）；[自動検出 Feature](#auto-discover-features) を参照 |
| `workspace` | マルチリポジトリのワークスペース設定（リポジトリ名 → パスのマッピング） |
| `hooks` | ライフサイクルフック（フックポイントをキーとする、例：post-run） |
| `health_check` | グローバルなテスト前環境チェックコマンド（test-strategy.yaml で Feature ごとに上書き可能） |
| `test_profiles` | カスタムまたは上書きされたテストプロファイル定義（プロファイル名をキーとする） |
| `max_discovered_features` | 実行あたりの自動作成 Feature の最大数；未設定または `<= 0` の場合はデフォルト（`3`）を適用 |

### 自動検出 Feature

`auto_discover_features` が `true` の場合、実行ループは最終 Deep Review レポート（`deep-review-report.md`）が **PASS** した後にパースし、各 `[NEW-FEATURE]` マーカーを新しい Feature YAML に変換します。Deep Reviewer が発見したスコープ外の問題を埋もれさせずにキャプチャします。

- **トリガーポイント**：最終的な Deep Review が PASS した場合のみ発動（初回パスの PASS、または自己修復後の PASS）。中間ラウンド、reviewer/tester の失敗、Deep Review の FAIL/needs-attention パスでは発動しません。
- **重複排除**：各候補はトークンオーバーラップ類似度で既存の Feature の名前 + 説明、および同じバッチで既にキープされた候補と比較されます。類似した候補はスキップされます。
- **上限**：実行あたり最大 `max_discovered_features`（デフォルト `3`）件の Feature が作成されます。残りは capped として記録されます。
- **出力**：`.4x/<feature-id>/` 配下に `discovered-features.md` サマリーが書き込まれ、作成 / 重複スキップ / 制限超過の候補がリストされます。作成された Feature ごとに `feature-discovered` イベントが追記されます。

これらはすべて CLI レイヤー（プレーンテキストパース + ファイル書き込み、LLM 呼び出しなし）で行われ、`accepting` への遷移をブロックしません。エラーはベストエフォートでログに記録されます。

### プロファイル

プロファイルは Feature に対してどの phase を実行するかを選択します。シンプルな Feature は完全なパイプラインをスキップできます。リストにない phase はパススルーされ、ランナーの起動、アーティファクトのチェック、ガードの実行なしに、正規のエッジに沿って状態が進みます。`coding` は唯一の必須 phase です。これを含まないプロファイルは設定エラーです。オプションの `design-reviewing` phase はリストに含まれた場合のみ実行され、その `design-review-report.md` が PASS しないと coding が開始されません。

```json
"profiles": {
  "full": {
    "phases": [
      { "phase": "designing" },
      { "phase": "design-reviewing" },
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "deep-reviewing" },
      { "phase": "accepting" }
    ]
  },
  "normal": {
    "phases": [
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "accepting" }
    ]
  },
  "quick": {
    "phases": [
      { "phase": "coding", "model": "opus" },
      { "phase": "reviewing" }
    ]
  }
}
```

各 phase エントリはオプションの `runner` と `model` の上書きをサポートします：

| フィールド | 説明 |
|---|---|
| `phase` | Phase 名（選択可能な phase である必要があります：designing、design-reviewing、coding、reviewing、testing、deep-reviewing、accepting） |
| `runner` | この phase のオプション runner 上書き |
| `model` | この phase のオプションモデルティア上書き |

**選択優先度：**

1. `4x run --profile <name>` -- 明示的な上書き（`profiles` で検索し、次に組み込みデフォルト）。
2. `profiles` セクションが存在する場合、Feature の `priority` で自動選択：`null`/`0`/`1` → `full`、`2` → `normal`、`>=3` → `quick`。
3. `profiles` セクションが存在しない場合、すべての Feature が `full` で実行されます（優先度ベースの自動選択は無効 -- 後方互換）。

3つの組み込みプロファイル（`full`/`normal`/`quick`）は `profiles` セクションがなくても常にフォールバックとして利用可能です。アクティブなプロファイル名は Feature の状態に記録され、ダッシュボードカードに表示されます。

`parallel_review_test` が `true` で、アクティブなプロファイルが `reviewer` と `tester` の両方を有効にしている場合、reviewing フェーズ中に2つの読み取り専用ロールが同じ worktree で並行実行されます。両方が合格すれば Deep Review に進み、そうでなければコーディングに戻ります。

## ユーザー設定（`~/.4x/settings.json`）

グローバルなユーザー設定とランナーのデフォルト。`4x config` またはダッシュボードの **Global Settings** エディタ（サイドバーの ⚙G ボタン）で管理するクロスプロジェクト設定。

```json
{
  "locale": "zh-TW",
  "theme": "dark",
  "default_runner": "claude",
  "runners": {
    "claude": {
      "command": "/usr/local/bin/claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "tty": true
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" }
  }
}
```

### ユーザー設定フィールド

| フィールド | 説明 |
|---|---|
| `locale` | ロールプロンプトの指示に使用する言語 |
| `theme` | ダッシュボードのテーマ（`dark`/`light`） |
| `default_runner` | デフォルトのランナー名（プロジェクト設定で上書き可能） |
| `runners` | ランナー定義（command、args、tty など） |
| `roles` | ロールモデルのデフォルト |
| `logLevel` | 最小ログレベル（debug/info/warn/error、デフォルト "info"、FOURX_LOG_LEVEL 環境変数で上書き可能） |
| `logRetainDays` | ~/.4x/logs/ のログファイル保持日数（デフォルト 7） |

### CLI

```bash
4x config set locale zh-TW
4x config set theme dark
4x config set default_runner claude
4x config set runner.claude.command /usr/local/bin/claude
4x config set runner.claude.tty true
4x config set role.designer.model opus
4x config get runner.claude.command
4x config list
```

`args` は配列フィールドです。設定するには `~/.4x/settings.json` を直接編集してください。

### ロケール

ロールプロンプトの指示に使用する言語を設定します。サポートされる値：

| 値 | 言語 |
|---|---|
| `en` | English（デフォルト） |
| `zh-TW` | 繁體中文 |
| `zh-CN` | 简体中文 |
| `ja` | 日本語 |
| `ko` | 한국어 |
| `es` | Español |
| `fr` | Français |
| `de` | Deutsch |
| `pt` | Português |
| `ru` | Русский |
| `vi` | Tiếng Việt |

ロケールは、明示的に設定されていない場合、`LANG` 環境変数からも推論されます。

## 設定のマージ

`4x run` または `4x prompt` の実行時、ユーザーレベルとプロジェクトレベルの設定がディープマージされます：

- **優先度：** プロジェクト > ユーザー > デフォルト
- **ランナーのマージ：** フィールド単位 -- プロジェクトの非ゼロフィールドがユーザーのものを上書き。`args` は完全に置き換え（追記ではない）。`tiers` はキーレベルでマージ。
- **ロールのマージ：** フィールド単位 -- ランナーと同じ。
- **プロジェクト専用フィールド**：`default_runner`、`runners`、`roles` 以外のすべてのフィールドはプロジェクト専用で、ユーザー設定で上書きされることはありません。

ダッシュボードのプロジェクト設定エディタは**生の**プロジェクト設定を表示します（マージ結果ではありません）。マージ後の最終的な有効設定を確認するには、プロジェクト設定の **Merged** タブを使用してください。
