# 使い方のヒント & ベストプラクティス

## トークン使用量に関する注意

4x は**シングルエージェントと比べて著しく多くの**トークンを消費します。各 Feature は少なくとも4つのロール（Designer → Coder → Reviewer → Tester）を経由し、それぞれが独立した LLM 呼び出しです。Review や Test の失敗で再実行が発生すると、トークンはさらに倍増します。

Feature あたりのトークン使用量の概算：

| シナリオ | LLM 呼び出し回数（概算） | 説明 |
|---|---|---|
| 一発合格（最良ケース） | 5回 | Designer + Coder + Reviewer(2パス) + Tester |
| Review で1回差し戻し | 8回 | 追加の Coder + Reviewer + Tester が1ラウンド |
| 5ラウンド全消化 | 約20回 | 毎ラウンド Coder + Reviewer + Tester |

**トークン節約のアドバイス：**
- シンプルなタスクは `--max-rounds` を下げる（`--max-rounds 2`）
- シンプルなタスクは全ロールに sonnet クラスのモデルを使用（5〜10倍安い）
- `--dry-run` でまずプロンプトの品質を確認し、無駄を避ける
- Feature の説明を明確に書き、エスカレーションや再実行を減らす
- 3ラウンド連続で進捗がない場合はループが自動停止（max-rounds まで無駄に回らない）

---

## AI エージェントを使った実際のワークフロー

これは作者が日常的に 4x を使う方法です。生の CLI コマンドではなく、AI アシストのループで、会話全体を通じて同じ会話にとどまります。

### 1. Feature を作成する

AI エージェントに Feature の作成を依頼します：

```
> 4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

### 2. ブレインストーミング — 仕様 & 計画

ループを実行する前に、エージェントに設計のブレインストーミングを依頼します：

```
> brainstorm F001
```

エージェントはブレインストーミングスキルを使って、要件、トレードオフ、エッジケースを探ります。合意が得られたら、2つの成果物が生成されます：

- `docs/design/F001-add-redis-cache-for-or-spec.md` — 設計仕様
- `docs/design/F001-add-redis-cache-for-or-plan.md` — 実装計画

これらのファイルは `CLAUDE.md` の **Docs Routing** で宣言された命名規則に従います：`docs/design/{feature-id}-spec.md` と `docs/design/{feature-id}-plan.md`。

仕様は Designer の参照入力になります。よくブレインストーミングされた仕様は、Designer がより優れたタスクブリーフを作成するため、レビューの差し戻しや再実行ラウンドが減ります。

### 3. ループを実行する

```bash
4x run F001 --runner claude
```

別のターミナルでダッシュボードを開いて進捗を確認します：

```bash
4x live -w
```

### 4. AI コードレビュー

ループが完了（`pending-review`）したら、AI エージェントに diff のレビューを依頼します：

```
> help me review the diff on branch 4x/F001-add-redis-cache-for-or
```

エージェントは `final-report.md` を読み、ブランチと main の diff を確認し、問題点を指摘します。手動またはエージェントに依頼して修正します。

### 5. マージ & クリーンアップ

満足したら、エージェントにマージとクリーンアップを依頼します：

```
> merge it and clean up the worktree
```

エージェントが実行します：
```bash
4x done F001
```

`4x done` はブランチを自動マージし、worktree を削除してブランチを削除します。マージコンフリクトがあれば手動で解決し、`4x merge F001` を実行してください。

### 6. ダッシュボードで完了をマーク

ダッシュボード（`4x live -w`）を開き、Feature カードの **Mark Done** をクリックします。これは意図的に人間のアクションです — AI ループは Feature を自動完了しません。

### なぜこれが機能するか

- **コーディング前のブレインストーミング** — 仕様はループ全体の基盤。曖昧さは実装の途中ではなく事前に解決される
- **1つの会話にとどまる** — ターミナルとツールの間のコンテキスト切り替えが不要
- **AI エージェントはブレインストーミングと Feature 実行から完全なコンテキストを持つ** — そのためレビューが情報に基づいている
- **Mark Done は手動** — あなたが最終ゲートキーパーで、AI ではない

### 4x とは何か（そして何ではないか）

4x は**ワークフローオーケストレーター**です。Designer、Coder、Reviewer、Tester ロールを順番に実行し、それらの間のステートマシンを管理します。あなたの判断を置き換えるものではありません。

実際には、ループはハッピーパスをうまく処理します：明確な仕様を持つ単純な Feature は通常1〜2ラウンドでパスします。しかし実際の開発は複雑です：

- **Coder が仕様を誤解する場合がある** — Reviewer が見つけますが、次のラウンドの修正でも要点を外す場合があります。2〜3回失敗したら、自分で介入するか AI エージェントに直接修正させた方が早い
- **テスト失敗が環境固有の場合がある** — Tester は仕様に基づいてテストを書きますが、プロジェクトに癖がある場合（カスタムテストセットアップ、不安定な CI、レガシー制約）、AI が診断できない理由でテストが失敗することがある
- **エッジケースはループ後に出現する** — 4x は仕様が説明する内容をカバーする。ビジネスロジックのエッジケース、競合状態、統合問題は手動レビューや本番使用中にしか現れないことが多い
- **複雑なリファクタリングは人間の誘導が必要な場合がある** — Feature が多数のファイルにまたがるか、暗黙の慣例の理解が必要な場合、Coder は正確だが最適でないコードを生成することがある。簡単な人間の誘導（「`pkg/util` の既存ヘルパーを使って」）で複数の再試行ラウンドを節約できる

**正しいメンタルモデル**：4x はテストカバレッジとレビューフィードバックを持つ堅固な初稿を提供します。指示に正確に従うが時に誘導が必要な、有能なジュニア開発者と考えてください。時間の節約は最初の実装を自分で書かないことからであり、プロセスから完全に自分を排除することからではありません。

### プロジェクトごとにロールをカスタマイズする

4x は状態遷移とロール切り替えのみを処理します。プロジェクトのビルド、テスト、レビュー方法は知りません。その知識はプロジェクト設定にあります。

各ロールはプロジェクトの `.4x/settings.json` を読んで何をするかを理解します。コンテキストを多く与えるほど、出力が良くなります：

```json
{
  "project": {
    "name": "my-api",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["golangci-lint run"],
    "rules": ["all exported functions must have GoDoc comments"]
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": {
      "model": "sonnet",
      "instructions": ["always use dependency injection via constructors"]
    },
    "reviewer": {
      "model": "sonnet",
      "deep_model": "opus",
      "instructions": ["check for SQL injection in all query builders"]
    },
    "tester": {
      "model": "sonnet",
      "instructions": ["use testcontainers for integration tests, not mocks"]
    }
  }
}
```

主要フィールド：

| フィールド | 効果 |
|---|---|
| `project.build/test/lint` | Coder が変更後に実行；Tester が検証に `test` を使用 |
| `project.rules` | すべてのロールにハード制約として注入 |
| `roles.*.instructions` | ロール固有のガイダンス — 何に焦点を当て、何を避けるか |
| `roles.*.includes` | 読み込む追加ファイル（例：`["docs/api-conventions.md"]`） |

これらがなければ、ロールは汎用的な動作にフォールバックします。これらがあれば、Designer があなたのアーキテクチャに合う仕様を書き、Coder があなたの規約に従い、Reviewer があなたのプロジェクト固有の落とし穴を見つけ、Tester があなたの環境で実際に実行されるテストを書きます。

詳細は [設定](configuration.md) を参照してください。

---

## CLI のみのエンドツーエンドワークフロー

上記と同じフローですが、CLI コマンドを直接使用します。AI エージェントセッションを使わない場合に便利です。

### Step 1: タスクの作成

```bash
4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

必要に応じて `.4x/features/F001-add-redis-cache-for-or.yaml` を編集し、description、priority、depends、repos などのフィールドを補足します。

### Step 2: ループの実行

```bash
# まず dry run でプロンプトを確認するのがおすすめ
4x run F001 --dry-run

# 本番実行
4x run F001 --runner claude
```

ダッシュボードを開いてリアルタイムで監視できます：

```bash
4x live -w   # 別のターミナルで
```

### Step 3: ループ完了 → pending-review

ループが完了すると、Feature は `pending-review` で停止します -- これは意図的です。AI の作業は完了しましたが、あなたのレビューが必要です。

```bash
4x status F001
# Phase: pending-review
```

### Step 4: 人によるレビュー

AI の成果物を確認します：

```bash
# 最終レポートを確認
cat .4x/F001/final-report.md

# コミット計画を確認
cat .4x/F001/commit-plan.md

# コード差分を確認
git diff                          # 非 worktree モード
git diff main...4x/F001-add-redis  # worktree モード
```

不満がある場合は：

```bash
# 手動修正後に review + test を再実行
4x transition F001 --to reviewing
4x run F001

# または最初からやり直し
4x transition F001 --to designing
4x run F001
```

### Step 5: マージ & クリーンアップ

**非 worktree モード**（変更が直接ワーキングツリーにある場合）：

```bash
# 満足したら完了をマーク
4x done F001

# commit-plan.md に従ってコミット
git add -A
git commit -m "feat: add Redis cache for order query API"
```

**Worktree モード**（変更が独立ブランチにある場合）：

```bash
# 完了をマーク
4x done F001

# メインブランチにマージ
git merge 4x/F001-add-redis-cache-for-or

# worktree とブランチをクリーンアップ
git worktree remove .worktrees/4x/F001-add-redis-cache-for-or
git branch -d 4x/F001-add-redis-cache-for-or
```

### フロー全体図

```
4x new "..."                     # タスクを作成
    ↓
4x run F001 --runner claude      # AI が自動で Design→Code→Review→Test
    ↓
pending-review                   # あなたのレビューを待つ
    ↓
review final-report / diff       # 成果物を確認
    ↓
4x done F001                     # 完了をマーク
    ↓
git merge + cleanup              # マージ、worktree/branch をクリーンアップ
```

---

## 良い Feature 説明の書き方

Feature の説明は Designer の唯一の入力です -- 明確に書くほど、より正確な仕様が生まれます。

```bash
# 悪い例：曖昧すぎて、Designer が推測してしまう
4x new "パフォーマンス改善"

# 良い例：明確な目標、境界、受け入れ基準
4x new "optimize order query API — add Redis cache, target p99 < 200ms, cache TTL 5min"
```

説明に含めるべきもの：
- **何をするか**（具体的な機能や変更）
- **なぜするか**（ビジネス上の動機や問題の説明）
- **境界**（触れないもの、既知の制約）
- **受け入れ基準**（定量化可能な成功の定義）

## Feature の粒度

1つの Feature は、独立して納品可能な1つの変更に対応します。大きすぎると Coder が迷い、Reviewer が見落とし、Test が困難になります。

| 粒度 | 適している | 適していない |
|---|---|---|
| 1つの API エンドポイント | OK | -- |
| 1つのリファクタリング（リネーム、インターフェース抽出） | OK | -- |
| 1つのバグ修正 | OK | -- |
| モジュール全体をゼロから作成 | -- | 複数の Feature + depends に分割 |
| 3つのリポジトリにまたがる大機能 | -- | 各リポジトリごとに1つの Feature にし、depends で連結 |

`depends` を活用して大きなタスクを分解します：

```bash
4x new "Add user model and migrations"           # F001
4x new "Add user registration API"               # F002, depends: [F001]
4x new "Add OAuth2 login flow"                    # F003, depends: [F002]
```

## まず Dry Run してから本番実行

新しい Feature や settings 変更後の初回は、`--dry-run` でプロンプトが妥当か確認してください：

```bash
4x run F001 --dry-run
```

これは4つのロールの完全なプロンプトを表示しますが、LLM は呼び出しません。以下を確認できます：
- Designer が十分なコンテキストを取得しているか
- プロジェクトルールが正しく注入されているか
- ロケールが正しいか

## モデル選択のアドバイス

| ロール | 推奨 | 理由 |
|---|---|---|
| Designer | opus または同等クラス | 要件の深い理解、アーキテクチャの分解が必要 |
| Coder | sonnet または同等クラス | 出力量が多いが、最強の推論は不要 |
| Reviewer（チェックリスト） | sonnet | ルールベースのチェック、速度優先 |
| Reviewer（敵対的） | opus | 隠れたバグを見つけるための深い推論が必要 |
| Tester | sonnet | テスト作成・検証実行、最強の推論は不要 |

設定方法：

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

プロジェクトがシンプルな場合（小さなバグ修正、小さなリファクタリング）、全ロールに sonnet を使用しても問題ありません。コスト削減になります。

## ラウンド数の調整

デフォルトの5ラウンドはほとんどの状況に適しています。Feature の複雑さに応じて調整してください：

| シナリオ | 推奨ラウンド数 |
|---|---|
| シンプルなバグ修正、小さな変更 | 2-3 |
| 一般的な機能開発 | 5（デフォルト） |
| 複雑なクロスモジュール機能 | 7-10 |

```bash
4x run F001 --max-rounds 3   # シンプルなタスク
4x run F001 --max-rounds 8   # 複雑なタスク
```

注意：3ラウンド連続で進捗がない場合、ループは自動停止します（max-rounds まで回す必要はありません）。

## Review 失敗への対処

Review 失敗（判定 FAIL または CRITICAL 所見）は自動的に Coder に差し戻されるため、手動介入は不要です。ただし、繰り返し失敗する場合は：

1. **review-report.md を確認** -- `.4x/run/{feature-id}/rounds/round-{N}/review-report.md`
2. **coder-report.md を確認** -- Coder が問題を理解しているか
3. **調整を検討**：
   - Feature の説明が曖昧 → 説明を書き直し、Designer から再実行
   - Reviewer が厳しすぎる → `roles.reviewer.instructions` で特定のルールを緩和
   - 本当に難しい問題 → 手動で修正し、`4x transition` で先に進める

## エスカレーションへの対処

Coder または Tester が仕様と実際の不一致を発見すると、自動的に Designer にエスカレーションします。よくあるシナリオ：

- DB スキーマが仕様と異なる（`spec-mismatch`）
- 受け入れ基準が不合理（`criteria-wrong`）
- 外部依存が不足（`blocker`）

エスカレーションは `.4x/run/{feature-id}/rounds/round-{N}/escalation.json` に記録されます。Designer がエスカレーションの内容を受け取り、仕様を再作成します。

Designer でも解決できない場合（通常はコンテキスト不足）、ループは `needs-attention` で停止します。この場合は手動介入が必要です：

```bash
# ステータスを確認
4x status F001

# 仕様やコードベースを手動修正
vim .4x/F001/task-brief.md

# coding に戻して続行
4x transition F001 --to coding
```

## 中断した Feature の再開

4x はファイルベースです -- セッションが切れても、マシンが再起動しても、状態は `.4x/` に保存されています。そのまま再実行するだけです：

```bash
4x run F001 --runner claude
```

前回のフェーズとラウンドから続行され、最初からやり直すことはありません。

## Worktree による分離

複数の Feature を同時に実行する場合や、AI の変更を分離したい場合は、worktree を有効にします：

```json
// .4x/settings.json
{
  "isolation": "worktree"
}
```

効果：
- ブランチを切る前に、各リポジトリの現在のブランチに upstream tracking branch が設定されていれば fetch して fast-forward する——ローカルが既に最新なら no-op、リモートと分岐している場合も no-op（警告のみ表示）で、未 push のローカル commit を上書きすることはない
- 各 Feature が `.worktrees/4x/{feature-id}/` で独立して作業
- ブランチ `4x/{feature-id}` が自動作成
- 完了時にマージコマンドを提示

```bash
# 完了後のマージ
git merge 4x/F001-user-auth
git worktree remove .worktrees/4x/F001-user-auth
git branch -d 4x/F001-user-auth
```

## バッチの使用タイミング

| シナリオ | `4x run` を使用 | `4x batch run` を使用 |
|---|---|---|
| 1つの Feature を実行 | OK | -- |
| 依存関係のある複数の Feature | 手動で順序を管理する必要あり | 依存関係の順序を自動処理 |
| 一晩でバックログを消化 | -- | OK、`batch stop` でいつでも停止可能 |

バッチのコミット戦略は固定で `"never"` です -- すべての変更はワーキングツリーに残り、人によるレビュー後に手動でコミットします。

## ダッシュボードの活用シナリオ

```bash
# ダッシュボードを開いて Feature を実行し、リアルタイムでログを確認
4x live -w &
4x run F001 --runner claude

# ダッシュボードから直接 Feature を起動（ターミナルを開く必要なし）
# POST /api/run と Web UI を使用

# マルチプロジェクト監視
4x live /path/to/project-a /path/to/project-b -w
```

## ロケール設定

AI にあなたの言語で回答させます：

```bash
4x config set locale zh-TW
```

設定しなくても大丈夫です -- `LANG` 環境変数から自動推論されます。

## トラブルシューティング

### Feature が needs-attention で停止している

あるフェーズに必要なアーティファクトが不足しています（例：Designer が task-brief.md を生成しなかった）。

```bash
4x status F001          # 何が不足しているか確認
4x check F001           # 完全なチェックを実行
```

手動でファイルを補うか、該当フェーズを再実行します：

```bash
4x transition F001 --to designing
4x run F001
```

### Feature が blocked で停止している

通常、ランナーの終了コード1（ソフト失敗）が原因です。ログを確認してください：

```bash
ls .4x/F001/logs/
cat .4x/F001/logs/round-1-coder.log
```

解決後に復帰させます：

```bash
4x transition F001 --to coding
4x run F001
```

### 依存関係ゲートでブロックされている

```
blocked: F001-user-model is not done (status: coding)
```

依存先の Feature を先に完了するか、手動でマークします：

```bash
4x done F001
4x run F002
```

## gstack Browse を E2E テストに統合する

[gstack](https://github.com/garrytan/gstack) は永続的なヘッドレスブラウザデーモンを提供しており、4x での Playwright ベースの E2E テストを高速化できます。テストラウンドのたびに Chromium をコールドスタートする（約3〜5秒）代わりに、デーモンがブラウザを常時起動したままにし、ラウンドをまたいでログイン状態を保持します。

これは**任意の設定**です。4x 組み込みの `web` テストプロファイルは gstack なしでも動作します。デーモンが特に役立つのは以下のケースです：

- プロジェクトにログインが必要な場合（セッション保持により、ラウンドごとの再認証が不要）
- バッチで複数の Feature を実行する場合（すべてが1つのブラウザインスタンスを共有）
- コールドスタートの遅延なしに、200ms 未満のブラウザ応答時間が欲しい場合

### セットアップ

1. gstack を Claude Code スキルとしてインストールします：

```bash
git clone --depth 1 https://github.com/garrytan/gstack.git ~/.claude/skills/gstack
cd ~/.claude/skills/gstack && ./setup
```

2. browse デーモンをバックグラウンドで起動します：

```bash
# Claude Code 内で
/browse-open http://localhost:4567
```

または手動で起動します：

```bash
cd ~/.claude/skills/gstack && bun run browse/src/server.ts
```

デーモンはランダムなポートを選択し、接続情報を `.gstack/browse.json` に書き込みます。

### 4x が gstack browse を使うよう設定する

`.4x/settings.json` で組み込みの `web` テストプロファイルを上書きします：

```json
{
  "test_profiles": {
    "web": {
      "include": "docs/test-profiles/gstack-web.md"
    }
  }
}
```

`docs/test-profiles/gstack-web.md` を作成します：

```markdown
Web UI E2E Testing with gstack Browse:

- Use gstack browse daemon instead of launching a standalone Playwright instance
- Read connection info from .gstack/browse.json (port + auth token)
- Send commands via HTTP POST to the daemon:
  - `POST /command` with `{"command": "goto", "args": ["http://localhost:4567"]}`
  - `POST /command` with `{"command": "snapshot"}` to get the accessibility tree with @e refs
  - `POST /command` with `{"command": "click", "args": ["@e5"]}` to interact with elements
  - `POST /command` with `{"command": "screenshot"}` to capture evidence
- Include Bearer token from browse.json in all requests
- Save screenshots to the configured screenshot_dir
- Each AC item must have at least one screenshot as evidence
- Do NOT launch a separate Chromium instance — use the running daemon
- If the daemon is not running, fall back to standard Playwright (npx playwright test)
```

### 例：gstack を使った test-strategy.yaml

```yaml
web: true
api: false
coder_only: false
profiles:
  - web
verify_commands:
  - "make build"
  - "make test"
```

Designer が `profiles: [web]` を指定し、`test_profiles.web` の上書きが gstack を指している場合、Tester は自動的に gstack 固有の指示をプロンプトに受け取ります。

### ログインが必要なプロジェクトの場合

認証が必要なプロジェクト（管理画面など）では、`4x run` を開始する前に gstack 経由で一度ログインしておきます：

```bash
# gstack デーモンでログインページを開く
/browse-open https://your-app.example.com/login

# 手動または gstack の fill コマンドでログイン
# セッション Cookie は以降のすべての 4x テストラウンドで保持される
```

これにより Tester はログインステップを完全にスキップできます。デーモンのブラウザはすでに有効なセッションを持っているためです。

### gstack を使わない場合

gstack を使用しない場合、組み込みの `web` プロファイルがそのまま使えます：

- Tester がテストラウンドごとに独立した Playwright インスタンスを起動
- 一時ワークスペースを作成し、ランダムなポートで `4x live` を起動
- テストを実行してスクリーンショットを撮影し、クリーンアップ
- ラウンド間で状態は保持されない（各ラウンドはクリーンな状態から開始）

プロファイルの上書きについては[テストプロファイル](concepts.md#test-profiles)を参照してください。

---

## AI エージェントに 4x を一度だけ教える

デフォルトでは、新しい AI 会話のたびに 4x ドキュメントをゼロから読み直します。**グローバル指示ファイル**を追加することで、これを解消できます。会話開始前からエージェントが 4x コマンド、ディレクトリ構造、ロール契約を把握した状態になります。

### Claude Code

`~/.claude/rules/4x.md` に 4x クイックリファレンスを作成してください（下記の例を参照）。`~/.claude/rules/` 内のファイルは全セッションに自動的に読み込まれます。

### Gemini CLI

`~/.gemini/instructions/4x.md` に同じ内容を作成してください。

### Codex

グローバルの `AGENTS.md` に 4x の指示を追加してください。

### 例：グローバルルール用 4x クイックリファレンス

[`docs/reference/4x-agent-rules.md`](../../reference/4x-agent-rules.md) をエージェントのグローバルルールディレクトリにコピーしてください。以下の内容が含まれています：

- すべての CLI コマンドとよく使うフラグ
- `.4x/` ディレクトリ構造
- ロール契約（読み取り / 書き込み / 制約）
- 状態マシンの遷移
- サポートされているランナー
