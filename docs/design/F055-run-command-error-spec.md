# F055: Run command error handling refactor

## 現狀

`if jsonOutput { return jsonError(...) }` pattern 在三個 command 大量重複：
- `run.go` — 7 次
- `transition.go` — 5 次
- `status.go` — 5 次

`jsonOutput` 是 package-level var，多個 command 共用，不乾淨。`jsonError()` 內部呼叫 `os.Exit(1)` 繞過 Cobra 的 error handling。

## 設計

### RunE wrapper

新增 `withJsonError` wrapper，在最外層攔截 error 統一輸出：

```go
func withJsonError(jsonFlag *bool, fn func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
    return func(cmd *cobra.Command, args []string) error {
        err := fn(cmd, args)
        if err != nil && *jsonFlag {
            return jsonError(err.Error())
        }
        return err
    }
}
```

每個 command 的 `RunE` 改用 wrapper：

```go
cmd := &cobra.Command{
    RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
        // 業務邏輯，直接 return err
    }),
}
```

### 清理

- 刪除所有 `if jsonOutput { return jsonError(...) }` 分支
- `jsonOutput` 從 package-level var 改為每個 command 的 local var
- `jsonError()` 保留但移除 `os.Exit(1)`，改成正常 return error（由 Cobra 處理 exit code）

## 影響範圍

| 檔案 | 改動 |
|---|---|
| `cmd/4x/json_helpers.go` | 新增 `withJsonError`，修改 `jsonError` 移除 `os.Exit(1)` |
| `cmd/4x/run.go` | 刪除 7 處 jsonOutput 分支，`jsonOutput` 改 local var，RunE 加 wrapper |
| `cmd/4x/transition.go` | 刪除 5 處 jsonOutput 分支，`jsonOutput` 改 local var，RunE 加 wrapper |
| `cmd/4x/status.go` | 刪除 5 處 jsonOutput 分支，`jsonOutput` 改 local var，RunE 加 wrapper |

## 約束

- 不改 CLI 的 error output format（純文字 vs JSON 結構不變）
- 不影響 exit code 行為
