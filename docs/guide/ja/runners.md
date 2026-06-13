# ランナー & プラグイン

## ランナーとは？

ランナー（Runner）は、4x CLI と AI ツールを橋渡しするものです。CLI がロールプロンプトを生成し、状態を管理します。ランナーはプロンプトを AI に送り、出力をキャプチャします。

ランナーは `.4x/settings.json` の `runners` キーで設定されます。CLI はランナーをサブプロセスとして起動します。

## 組み込みランナー

| ランナー | AI ツール | モード | ステータス |
|---|---|---|---|
| `claude` | Claude Code CLI | Stream JSON | 利用可能 |
| `codex` | OpenAI Codex CLI | Stdin | 利用可能 |
| `gemini` | Google Gemini CLI | Argument | 利用可能 |
| `agy` | Antigravity CLI | Argument | 利用可能 |
| `copilot` | GitHub Copilot CLI | Argument | 利用可能（手動設定が必要） |
| `cursor` | Cursor IDE | Rules file | 利用可能（手動設定が必要） |

`4x init` はデフォルトで claude、codex、gemini、agy を設定します。copilot と cursor は手動で `settings.json` に追加する必要があります。

## プラグインファイル

各ランナーには `4x` バイナリに埋め込まれた指示ファイルがあります。`4x init` がそれらを `.4x/plugins/` にデプロイし、ルートレベルのファイルにインポート行を追加します：

| ランナー | プラグインファイル | ルートインポート |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| copilot | `AGENTS.md` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

また、共有指示ファイルがすべてのランナー向けに `.4x/plugins/shared/` にデプロイされます：

| ファイル | 用途 |
|---|---|
| `shared/CREATOR.md` | Feature Creator フロー — AI が `4x new` でフィーチャーを作成するのをガイド |

バイナリの更新後は `4x upgrade` でプラグインファイルを再デプロイしてください。

## ランナー実行モデル

```
4x run F001 --runner claude
    │
    ├── 現在のロールのプロンプトを生成
    ├── プロンプト付きでランナーサブプロセスを起動
    │     claude --dangerously-skip-permissions -p "..." --output-format stream-json --verbose
    ├── 出力を .4x/F001/logs/round-N-role.log にキャプチャ
    ├── 出力アーティファクトをチェック
    └── 状態を遷移して繰り返し
```

### 終了コード

| コード | 意味 | アクション |
|---|---|---|
| 0 | 成功 | 次のフェーズに進む |
| 1 | ソフト失敗 | Feature が `blocked` に移行 |
| 2 | ハードエラー | ループが停止、対応が必要 |
| timeout | 制限時間内に応答なし | ソフト失敗として扱う |

### Stream JSON モード

`output_format: "stream-json"` のランナーは、dashboard が tail する読みやすい `.log` と、デバッグ用の raw `.stream.jsonl` の 2 種類を書き込みます。Claude Code はデフォルトでこのモードを使います。

### PTY モード

`tty: true` のランナーは、ANSI エスケープシーケンスを含む完全な出力をキャプチャするために疑似端末を使用します。ステートフルな ANSI ストリッパーがログファイルをクリーンにします。`output_format` が `"stream-json"` の場合、この経路は使いません。

### Stdin モード

`stdin: true` のランナー（Codex）は、コマンドライン引数の代わりに標準入力でプロンプトを受け取ります。

## ロールごとに異なるモデルを使用する

`.4x/settings.json` で設定します：

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

ランナーを混在させることもできます -- 設計に Claude、実装に Gemini を使用するなど -- 各フェーズを異なる `--runner` フラグで手動実行し、フェーズ間で `4x transition` を使用します。

## プラグインの作成

プラグインはシンプルな規約に従います -- `.4x/` ファイルを読み、AI の作業を行い、結果を書き戻します：

1. `.4x/features/{id}.yaml` を読んで Feature を把握
2. `state.json` を読んで現在のフェーズを把握
3. フェーズ固有の入力を読む（task-brief.md、スコープなど）
4. 作業を行う（LLM を呼び出す、ツールを実行するなど）
5. フェーズ固有の出力を書く（coder-report.md、review-report.md など）
6. 適切な終了コードで終了（0 = 成功、1 = ソフト失敗、2 = ハードエラー）

SDK は不要。ランタイム依存もなし。ファイルだけ。
