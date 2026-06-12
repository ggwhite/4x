# 基本コンセプト

## 4つのロール

| ロール | 責務 | 入力 | 出力 | 禁止事項 |
|---|---|---|---|---|
| **Designer** | 要件を分析し、仕様を作成し、受け入れ基準とテスト戦略を定義 | Feature の説明、コードベース | `task-brief.md`, `acceptance-criteria.md`, `test-strategy.yaml` | ソースコードの変更 |
| **Coder** | 仕様の通りに実装 | `task-brief.md`、以前のテスト/レビューレポート | ソースコード, `coder-report.md` | 受け入れ基準やテストスクリプトの変更 |
| **Reviewer** | バグ、セキュリティ問題、仕様違反を検出 | Diff、仕様、Coder レポート、プロジェクトルール | `review-report.md` | ソースコードの変更 |
| **Tester** | エビデンスに基づいて受け入れ基準を検証 | 受け入れ基準、Coder レポート、テスト戦略 | テストスクリプト, `test-report.md`, `verify.json`, `final-report.md`, `commit-plan.md` | ソースコードの変更 |

各ロールは**分離**されています -- Coder は実装中に以前のレビューフィードバックを見ることはありません。Tester は Coder ではなく Designer が書いた基準に対して検証します。

### Review：2つのフェーズ

1. **チェックリストレビュー**（標準モデル） -- プロジェクトのハードルール（セキュリティ、並行性、エラーハンドリング、スタイル）に対してチェック
2. **敵対的レビュー**（ディープモデル） -- 「この差分に隠れている最悪のバグは何か？」所見は重大度で評価。

### エスカレーション

Coder または Tester は以下の場合に Designer にエスカレーションできます：

| 理由 | 意味 |
|---|---|
| `spec-mismatch` | DB/API が仕様と一致しない |
| `criteria-wrong` | 受け入れ基準が不正確 |
| `blocker` | 依存関係やインフラの問題が不足 |
| `scope-change` | スコープ外のリポジトリを変更する必要がある |

エスカレーションは `escalation.json` に記録されます。ループは自動的に Designer に差し戻します。

---

## ステートマシン

```
init → designing → coding → reviewing → testing → accepting → pending-review → done
                     ↑          ↓           ↓
                     ├── amending ←──────────┘
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
| `testing` | `accepting`, `amending`, `designing` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`, `coding`, `testing` |
| `needs-attention` | `designing`, `coding`, `testing` |
| any | `blocked`, `needs-attention` |

### ラウンドカウンター

- `coding` に入る際にラウンドが0であれば1に設定
- `amending` に入るとラウンドがインクリメント
- `ShouldStop` はラウンドが maxRounds 以上、または3回以上連続で進捗がない場合にトリガー

### ループ内のフェーズ判定

| フェーズ | 条件 | アクション |
|---|---|---|
| `designing` | `task-brief.md` が存在しない | → `needs-attention` |
| `coding` / `amending` | `escalation.json` に `spec-mismatch` または `criteria-wrong` がある | → `designing` |
| `reviewing` | 判定行が FAIL で始まるか `[CRITICAL]` を含む | → `amending` |
| `testing` | `verify.json` が不合格またはアーティファクトが不足 | → `amending` |

---

## ファイルプロトコル

ロールは共有コンテキストウィンドウではなく、`.4x/` ディレクトリを通じて通信します。

```
.4x/
├── settings.json                    # プロジェクト設定
├── plugins/                         # ランナー指示ファイル
├── batch-plan.json                  # バッチ実行計画
├── batch-stop                       # グレースフル停止シグナル
├── features/
│   └── {id}.yaml                    # Feature 定義（正規ソース）
└── {feature-id}/
    ├── state.json                   # フェーズ、ロール、ラウンド、アクティブ、ランナー、runners、停止理由
    ├── events.jsonl                 # 監査証跡
    ├── baseline.json                # コーディング前のスナップショット（HEAD、ブランチ、ダーティファイル）
    ├── task-brief.md                # Designer → Coder: 仕様 + アーキテクチャ
    ├── acceptance-criteria.md       # Designer → Tester: テスト可能な基準
    ├── test-strategy.yaml           # Designer → Tester: テストアプローチ
    ├── final-report.md              # ループ終了時のサマリー
    ├── commit-plan.md               # 変更をコミットに分割する方法
    ├── logs/
    │   └── round-{N}-{role}.log     # ラウンドごと・ロールごとの実行ログ
    └── rounds/round-{N}/
        ├── coder-report.md          # Coder の作業内容
        ├── review-report.md         # Reviewer の所見 + 判定
        ├── test-report.md           # Tester の結果
        ├── verify.json              # {passed, round, role, commands[]}
        └── escalation.json          # {needed, reason, detail}
```

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: medium
repos: []
subtasks: []
rules: []
depends: []
```

`status` はクイックリスティングのために `state.json` のフェーズをミラーリングします。`depends` はこの Feature を実行する前に完了している必要がある Feature ID をリストします。

---

## ガードレール

CLI によって強制される決定的なチェック -- AI の判断には依存しません。

| ガードレール | 機能 |
|---|---|
| **必須ファイル** | フェーズに適切なアーティファクトの存在を検証（例：designing 後の `task-brief.md`） |
| **ベースライン** | コーディング前の状態をキャプチャ（HEAD、ブランチ、ダーティファイル）；ダーティファイルが存在する場合は警告 |
| **スコープ** | `git diff --name-only HEAD` のトップレベルディレクトリを Feature の宣言済みリポジトリと比較 |
| **依存関係** | 依存先の Feature が完了していない場合、`4x run` をブロック |
| **バックログドリフト** | `.4x/features/*.yaml` と外部ミラーが同期していない場合に警告 |
| **Testing → Accepting ゲート** | `verify.json`（passed=true）、`test-report.md`、`final-report.md`、`commit-plan.md` が必要 |

`4x check <feature-id>` で手動実行できます。

---

## Pending Review ゲート

ループは直接 `done` にはなりません。accepting の後、Feature は `pending-review` に入ります -- 人間が AI の成果物をレビューするのを待ちます。

```
... → accepting → pending-review → (人間がレビュー) → 4x done F001
```

これにより、Feature が完了と見なされる前に必ず人間がサインオフすることが保証されます。
