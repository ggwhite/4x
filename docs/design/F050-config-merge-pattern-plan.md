# F050: Config merge pattern deduplication — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 將 6 處重複的 ReadConfig + ReadUserConfig + MergeConfig boilerplate 收斂為 `ws.LoadMergedConfig()` 單一呼叫。

**Architecture:** 在 `Workspace` 新增 `LoadMergedConfig()` method，封裝三行 pattern。逐檔替換 6 處呼叫端。`server.go:1180` 因有 mutex 保護不在此次範圍。

**Tech Stack:** Go 標準庫

---

### Task 1: 新增 LoadMergedConfig method

**Files:**
- Modify: `internal/protocol/workspace.go:87-89`
- Test: `internal/protocol/workspace_test.go`

- [ ] **Step 1: 寫 failing test**

```go
// 加在 internal/protocol/workspace_test.go

func TestLoadMergedConfig(t *testing.T) {
	tmp := t.TempDir()
	dotDir := filepath.Join(tmp, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := Config{
		Project: ProjectConfig{Name: "test-proj"},
		Default: "claude",
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(dotDir, "settings.json"), data, 0o644)

	ws := &Workspace{Root: tmp}
	got, err := ws.LoadMergedConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Project.Name != "test-proj" {
		t.Errorf("Project.Name = %q, want test-proj", got.Project.Name)
	}
	if got.Default != "claude" {
		t.Errorf("Default = %q, want claude", got.Default)
	}
}

func TestLoadMergedConfig_NoProjectConfig_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	ws := &Workspace{Root: tmp}
	_, err := ws.LoadMergedConfig()
	if err == nil {
		t.Fatal("expected error when settings.json missing")
	}
}
```

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./internal/protocol/ -v -run TestLoadMergedConfig`
Expected: FAIL — `LoadMergedConfig` 未定義

- [ ] **Step 3: 實作 LoadMergedConfig**

在 `internal/protocol/workspace.go`，`ReadConfig` method 下方加：

```go
// LoadMergedConfig 讀取 project config 並合併 user config，封裝常見的三行 boilerplate。
// user config 讀取失敗時印 warning 但不中斷。
func (w *Workspace) LoadMergedConfig() (Config, error) {
	cfg, err := w.ReadConfig()
	if err != nil {
		return Config{}, err
	}
	if userCfg, err := ReadUserConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to read user config: %v\n", err)
	} else {
		cfg = MergeConfig(userCfg, cfg)
	}
	return cfg, nil
}
```

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./internal/protocol/ -v -run TestLoadMergedConfig`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/workspace.go internal/protocol/workspace_test.go
git commit -m "feat(F050): add Workspace.LoadMergedConfig method"
```

---

### Task 2: 替換 run.go

**Files:**
- Modify: `cmd/4x/run.go:65-77`

- [ ] **Step 1: 替換 boilerplate**

找到 `run.go` 第 65-77 行的：

```go
cfg, err := ws.ReadConfig()
if err != nil {
    if jsonOutput {
        return jsonError(err.Error())
    }
    return err
}

if userCfg, err := protocol.ReadUserConfig(); err != nil {
    fmt.Fprintf(os.Stderr, "warning: failed to read user config: %v\n", err)
} else {
    cfg = protocol.MergeConfig(userCfg, cfg)
}
```

替換為：

```go
cfg, err := ws.LoadMergedConfig()
if err != nil {
    if jsonOutput {
        return jsonError(err.Error())
    }
    return err
}
```

- [ ] **Step 2: 建置驗證**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 通過。如果 `protocol` import 變成 unused 就移除。

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/run.go
git commit -m "refactor(F050): use LoadMergedConfig in run.go"
```

---

### Task 3: 替換 batch.go（兩處）

**Files:**
- Modify: `cmd/4x/batch.go:49-52, 200-208`

- [ ] **Step 1: 替換第一處（第 49-52 行）**

找到：

```go
cfg, _ := ws.ReadConfig()
if userCfg, err := protocol.ReadUserConfig(); err == nil {
    cfg = protocol.MergeConfig(userCfg, cfg)
}
```

替換為：

```go
cfg, _ := ws.LoadMergedConfig()
```

- [ ] **Step 2: 替換第二處（第 200-208 行）**

找到：

```go
cfg, err := ws.ReadConfig()
if err != nil {
    return err
}
if userCfg, err := protocol.ReadUserConfig(); err != nil {
    fmt.Fprintf(os.Stderr, "warning: failed to read user config: %v\n", err)
} else {
    cfg = protocol.MergeConfig(userCfg, cfg)
}
```

替換為：

```go
cfg, err := ws.LoadMergedConfig()
if err != nil {
    return err
}
```

- [ ] **Step 3: 建置驗證**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 通過

- [ ] **Step 4: Commit**

```bash
git add cmd/4x/batch.go
git commit -m "refactor(F050): use LoadMergedConfig in batch.go"
```

---

### Task 4: 替換 prompt.go

**Files:**
- Modify: `cmd/4x/prompt.go:55-60`

- [ ] **Step 1: 替換 boilerplate**

找到：

```go
cfg, _ := ws.ReadConfig()
if userCfg, err := protocol.ReadUserConfig(); err != nil {
    fmt.Fprintf(os.Stderr, "warning: failed to read user config: %v\n", err)
} else {
    cfg = protocol.MergeConfig(userCfg, cfg)
}
```

替換為：

```go
cfg, _ := ws.LoadMergedConfig()
```

- [ ] **Step 2: 建置驗證**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 通過

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/prompt.go
git commit -m "refactor(F050): use LoadMergedConfig in prompt.go"
```

---

### Task 5: 替換 status.go

**Files:**
- Modify: `cmd/4x/status.go:292-298`

- [ ] **Step 1: 替換 boilerplate**

找到：

```go
cfg, err := ws.ReadConfig()
if err != nil {
    return err
}
if userCfg, err := protocol.ReadUserConfig(); err == nil {
    cfg = protocol.MergeConfig(userCfg, cfg)
}
```

替換為：

```go
cfg, err := ws.LoadMergedConfig()
if err != nil {
    return err
}
```

- [ ] **Step 2: 建置驗證**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 通過

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/status.go
git commit -m "refactor(F050): use LoadMergedConfig in status.go"
```

---

### Task 6: 替換 server.go（getMergedScreenshotDir）

**Files:**
- Modify: `internal/server/server.go:677-683`

- [ ] **Step 1: 替換 boilerplate**

找到 `getMergedScreenshotDir` 函式內：

```go
cfg, err := ws.ReadConfig()
if err != nil {
    return protocol.DefaultScreenshotDir
}
if userCfg, err := protocol.ReadUserConfig(); err == nil {
    cfg = protocol.MergeConfig(userCfg, cfg)
}
return protocol.ScreenshotDir(cfg)
```

替換為：

```go
cfg, err := ws.LoadMergedConfig()
if err != nil {
    return protocol.DefaultScreenshotDir
}
return protocol.ScreenshotDir(cfg)
```

注意：`server.go:1180` 的 `handleGetMergedConfig` 因有 `userConfigMu` mutex 保護，不替換。

- [ ] **Step 2: 建置驗證**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 通過

- [ ] **Step 3: Commit**

```bash
git add internal/server/server.go
git commit -m "refactor(F050): use LoadMergedConfig in server.go"
```

---

### Task 7: 清理 unused imports 並全量測試

**Files:**
- Modify: 各替換過的檔案（如有 unused import）

- [ ] **Step 1: 全量建置與測試**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部通過

- [ ] **Step 2: 如有 unused import 則移除**

替換後若某些檔案不再直接使用 `protocol.ReadUserConfig` 或 `protocol.MergeConfig`，`go vet` 會報 unused import。移除即可。

- [ ] **Step 3: 跑 check-docs-sync**

Run: `make check-docs-sync`
Expected: 無需更新（純 refactor，不改行為）

- [ ] **Step 4: Commit（如有 import 清理）**

```bash
git add -A
git commit -m "refactor(F050): clean up unused imports after LoadMergedConfig migration"
```
