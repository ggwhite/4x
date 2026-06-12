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

## 完全なワークフロー

タスクの作成から納品までの完全な流れ -- 4x が AI 開発を担当し、あなたが最終レビューとマージを担当します。

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

1. **review-report.md を確認** -- `.4x/{feature-id}/rounds/round-{N}/review-report.md`
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

エスカレーションは `.4x/{feature-id}/rounds/round-{N}/escalation.json` に記録されます。Designer がエスカレーションの内容を受け取り、仕様を再作成します。

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
