# F056: syncFeatureStatus 統一 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把分散在 `cmd/4x/transition.go` 和 `internal/server/server.go` 的 feature status 同步邏輯統一為 `Workspace.SyncFeatureStatus` 方法。

**Architecture:** 在 `internal/protocol/workspace.go` 新增方法，然後逐一替換所有呼叫端。純重構，行為不變。

**Tech Stack:** Go 1.26+

---

### Task 1: 新增 Workspace.SyncFeatureStatus 方法

**Files:**
- Modify: `internal/protocol/workspace.go`
- Test: `internal/protocol/workspace_test.go`

- [ ] **Step 1: 寫 failing test**

在 `internal/protocol/workspace_test.go` 加入（如果檔案不存在就新建）：

```go
func TestSyncFeatureStatus(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Project: ProjectConfig{Name: "test"}}
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: root}

	f := Feature{ID: "feat-1", Name: "Test", Status: StatusNotStarted}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	if err := ws.SyncFeatureStatus("feat-1", PhaseCoding); err != nil {
		t.Fatal(err)
	}

	got, err := ws.LoadFeature("feat-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusInProgress {
		t.Errorf("status = %s, want %s", got.Status, StatusInProgress)
	}
}

func TestSyncFeatureStatus_Done(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Project: ProjectConfig{Name: "test"}}
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: root}

	f := Feature{ID: "feat-2", Name: "Test", Status: StatusInProgress}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	if err := ws.SyncFeatureStatus("feat-2", PhaseDone); err != nil {
		t.Fatal(err)
	}

	got, err := ws.LoadFeature("feat-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDone {
		t.Errorf("status = %s, want %s", got.Status, StatusDone)
	}
}

func TestSyncFeatureStatus_NotFound(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Project: ProjectConfig{Name: "test"}}
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: root}

	err := ws.SyncFeatureStatus("nonexist", PhaseCoding)
	if err == nil {
		t.Error("expected error for missing feature")
	}
}
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/protocol/ -run TestSyncFeatureStatus -v`
Expected: FAIL — `SyncFeatureStatus` 不存在

- [ ] **Step 3: 實作 SyncFeatureStatus**

在 `internal/protocol/workspace.go` 加入：

```go
// SyncFeatureStatus 將 feature YAML 的 Status 欄位同步為對應 phase 的狀態
func (w *Workspace) SyncFeatureStatus(featureID string, phase Phase) error {
	f, err := w.LoadFeature(featureID)
	if err != nil {
		return fmt.Errorf("sync feature status: load: %w", err)
	}
	f.Status = PhaseToStatus(phase)
	if err := w.SaveFeature(f); err != nil {
		return fmt.Errorf("sync feature status: save: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: 執行測試確認通過**

Run: `go test ./internal/protocol/ -run TestSyncFeatureStatus -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/workspace.go internal/protocol/workspace_test.go
git commit -m "feat(F056): add Workspace.SyncFeatureStatus method"
```

---

### Task 2: 替換 cmd/4x 的呼叫端

**Files:**
- Modify: `cmd/4x/transition.go:109,147-158`
- Modify: `cmd/4x/run.go`（約 8 處）
- Modify: `cmd/4x/done.go:93`

- [ ] **Step 1: 刪除 transition.go 的 syncFeatureStatus helper**

刪除 `cmd/4x/transition.go` 行 147-158 的 `syncFeatureStatus` 函式。

- [ ] **Step 2: 替換 transition.go 的呼叫**

行 109 的 `syncFeatureStatus(ws, featureID, phase)` 改為 `ws.SyncFeatureStatus(featureID, phase)`。（回傳值和錯誤處理格式相同，直接替換即可。）

- [ ] **Step 3: 替換 run.go 的所有呼叫**

全域替換 `syncFeatureStatus(ws, ` → `ws.SyncFeatureStatus(`。約 8 處，每處的參數順序相同（featureID, phase），不需調整。

- [ ] **Step 4: 替換 done.go 的呼叫**

行 93 的 `syncFeatureStatus(ws, featureID, phase)` 改為 `ws.SyncFeatureStatus(featureID, phase)`。

- [ ] **Step 5: 編譯確認無錯誤**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 6: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/4x/transition.go cmd/4x/run.go cmd/4x/done.go
git commit -m "refactor(F056): replace syncFeatureStatus helper with Workspace method"
```

---

### Task 3: 替換 server.go 的 transitionDone

**Files:**
- Modify: `internal/server/server.go:950-976`

- [ ] **Step 1: 修改 transitionDone**

在 `internal/server/server.go` 的 `transitionDone` 函式中，找到這兩行（約行 966-969）：

```go
f.Status = protocol.PhaseToStatus(protocol.PhaseDone)
if err := ws.SaveFeature(f); err != nil {
    return protocol.State{}, fmt.Errorf("failed to save feature: %w", err)
}
```

替換為：

```go
if err := ws.SyncFeatureStatus(featureID, protocol.PhaseDone); err != nil {
    return protocol.State{}, fmt.Errorf("failed to sync feature status: %w", err)
}
```

`transitionDone` 的參數 `f protocol.Feature` 如果不再被其他地方使用，可以移除。檢查函式內其他行有沒有用到 `f`——如果沒有，把參數簽名也清掉，並更新呼叫端 `handlePostDone`。

- [ ] **Step 2: 編譯確認無錯誤**

Run: `go build ./... && go vet ./...`
Expected: 無錯誤

- [ ] **Step 3: 跑 server 測試**

Run: `go test ./internal/server/ -v`
Expected: 全部 PASS

- [ ] **Step 4: 最終全域驗證**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go
git commit -m "refactor(F056): use Workspace.SyncFeatureStatus in transitionDone"
```
