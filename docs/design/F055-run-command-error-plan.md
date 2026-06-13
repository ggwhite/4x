# F055: Run command error handling refactor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 RunE wrapper 消除 run/transition/status 三個 command 裡共 17 處重複的 `if jsonOutput { return jsonError(...) }` 分支。

**Architecture:** 在 `json_helpers.go` 新增 `withJsonError` wrapper，包住 `RunE`，在最外層攔截 error 並統一決定輸出格式。三個 command 的 RunE 內部刪除所有 jsonOutput error 分支，直接 `return err`。

**Tech Stack:** Go 標準庫、Cobra

---

### Task 1: withJsonError wrapper

**Files:**
- Modify: `cmd/4x/json_helpers.go`
- Create: `cmd/4x/json_helpers_test.go`

- [ ] **Step 1: 寫 failing test**

```go
package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestWithJsonError_NoError_ReturnsNil(t *testing.T) {
	jsonFlag := true
	wrapped := withJsonError(&jsonFlag, func(cmd *cobra.Command, args []string) error {
		return nil
	})
	err := wrapped(nil, nil)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestWithJsonError_ErrorNoJson_ReturnsError(t *testing.T) {
	jsonFlag := false
	wrapped := withJsonError(&jsonFlag, func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("something broke")
	})
	err := wrapped(nil, nil)
	if err == nil || err.Error() != "something broke" {
		t.Errorf("expected 'something broke', got %v", err)
	}
}
```

注意：`jsonFlag=true` + error 的 case 不好測（`jsonError` 呼叫 `os.Exit(1)`），只測不觸發 jsonError 的路徑。

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./cmd/4x/ -v -run TestWithJsonError`
Expected: FAIL — `withJsonError` 未定義

- [ ] **Step 3: 實作 withJsonError**

修改 `cmd/4x/json_helpers.go`：

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// jsonError 將 error message 以 JSON 格式印到 stdout 並結束程序
func jsonError(msg string) error {
	result := struct {
		Error string `json:"error"`
	}{
		Error: msg,
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	os.Exit(1)
	return nil
}

// withJsonError 包裝 RunE，在最外層統一處理 jsonOutput error formatting。
// 當 inner function 回傳 error 且 jsonFlag 為 true 時，以 JSON 格式輸出錯誤。
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

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./cmd/4x/ -v -run TestWithJsonError`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/4x/json_helpers.go cmd/4x/json_helpers_test.go
git commit -m "feat(F055): add withJsonError RunE wrapper"
```

---

### Task 2: 重構 run.go

**Files:**
- Modify: `cmd/4x/run.go:30-135`

- [ ] **Step 1: 加 wrapper 並刪除 error 分支**

將 `RunE` 用 wrapper 包住：

```go
RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
```

最後的 `})` 要對應調整。

刪除以下 7 處 `if jsonOutput { return jsonError(...) }` 分支（保留 `return err`）：

**第 36-41 行**（`os.Getwd` error）：
```go
// 之前
cwd, err := os.Getwd()
if err != nil {
    if jsonOutput {
        return jsonError(err.Error())
    }
    return err
}
// 之後
cwd, err := os.Getwd()
if err != nil {
    return err
}
```

**第 42-47 行**（`protocol.Find` error）— 同上模式，刪 jsonOutput 分支。

**第 50-55 行**（`ResolveFeatureID` error）— 同上。

**第 57-62 行**（`LoadFeature` error）— 同上。

**第 65-70 行**（`ReadConfig` error）— 同上。

**第 83-88 行**（runner not found）：
```go
// 之前
errMsg := fmt.Sprintf("runner %q not found in config", runnerName)
if jsonOutput {
    return jsonError(errMsg)
}
return fmt.Errorf("%s", errMsg)
// 之後
if !ok {
    return fmt.Errorf("runner %q not found in config", runnerName)
}
```

**第 116-118 行**（bgCmd.Start error，在 jsonOutput 成功路徑裡）：
```go
// 之前
if err := bgCmd.Start(); err != nil {
    return jsonError(fmt.Sprintf("failed to start run: %v", err))
}
// 之後
if err := bgCmd.Start(); err != nil {
    return fmt.Errorf("failed to start run: %w", err)
}
```

注意：`if jsonOutput { ... }` 的成功路徑（第 95-135 行，background launch + JSON output）保留不動。

- [ ] **Step 2: 建置驗證**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 通過

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/run.go
git commit -m "refactor(F055): remove jsonOutput error branches from run.go"
```

---

### Task 3: 重構 transition.go

**Files:**
- Modify: `cmd/4x/transition.go:21-137`

- [ ] **Step 1: 加 wrapper 並刪除 error 分支**

將 `RunE` 用 wrapper 包住：

```go
RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
```

刪除以下分支：

**第 27-31 行**（`os.Getwd` error）：刪 jsonOutput 分支，保留 `return err`。

**第 34-38 行**（`protocol.Find` error）— 同上。

**第 42-46 行**（`ResolveFeatureID` error）— 同上。

**第 51-55 行**（`InitFeatureDir` error，在 ReadState error 分支內）— 同上。

**第 65-69 行**（`WriteState` error，同上）— 同上。

**第 82-87 行**（guard check error）：
```go
// 之前
errMsg := fmt.Sprintf("testing → accepting blocked: %s", strings.Join(result.Errors, "; "))
if jsonOutput {
    return jsonError(errMsg)
}
return fmt.Errorf("%s", errMsg)
// 之後
return fmt.Errorf("testing → accepting blocked: %s", strings.Join(result.Errors, "; "))
```

**第 91-95 行**（`state.Transition` error）— 刪 jsonOutput 分支。

**第 102-106 行**（`WriteState` error）— 刪 jsonOutput 分支。

注意：成功路徑的 JSON 輸出（第 120-133 行）保留不動。

- [ ] **Step 2: 建置驗證**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 通過

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/transition.go
git commit -m "refactor(F055): remove jsonOutput error branches from transition.go"
```

---

### Task 4: 重構 status.go

**Files:**
- Modify: `cmd/4x/status.go:20-57`

- [ ] **Step 1: 加 wrapper 並刪除 error 分支**

將 `RunE` 用 wrapper 包住：

```go
RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
```

刪除以下分支：

**第 26-30 行**（`os.Getwd` error）：刪 jsonOutput 分支，保留 `return err`。

**第 33-37 行**（`protocol.Find` error）— 同上。

**第 42-46 行**（`ResolveFeatureID` error）— 同上。

注意：成功路徑的 JSON 分支（第 48-55 行）保留不動——這是功能邏輯（呼叫不同函式），不是 error handling。

- [ ] **Step 2: 建置驗證**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 通過

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/status.go
git commit -m "refactor(F055): remove jsonOutput error branches from status.go"
```

---

### Task 5: 全量測試與文件

**Files:**
- 所有改動過的檔案

- [ ] **Step 1: 全量建置與測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部通過

- [ ] **Step 2: 跑 check-docs-sync**

Run: `make check-docs-sync`

- [ ] **Step 3: 依腳本輸出更新對應文件**

若 `NEEDS_UPDATE` 點名需要更新特定文件，更新之。否則跳過。

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs(F055): update docs for run command error handling refactor"
```
