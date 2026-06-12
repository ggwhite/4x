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

`4x batch plan` はすべての未完了 Feature を分析し、`.4x/batch-plan.json` を生成します：

1. **依存関係 DAG** -- Feature の `depends` フィールドから有向グラフを構築
2. **循環検出** -- 循環依存がある場合はエラー
3. **Union-Find クラスタリング** -- リポジトリを共有する Feature をグループ化
4. **トポロジカルソート** -- 各クラスター内で Feature を順序付け
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

- コミット戦略は `"never"` を使用（レビュー後に手動でコミット）
- Feature 間で `.4x/batch-stop` ファイルをチェック
- 依存先が完了していない Feature はスキップ
- 各 Feature の完了後に進捗を報告

## 停止

```bash
4x batch stop
```

`.4x/batch-stop` シグナルファイルを作成します。バッチは現在の Feature を完了した後、グレースフルに終了します。

## 進捗の確認

```bash
# 次の Feature を確認
4x batch next

# 全 Feature の概要
4x status
```
