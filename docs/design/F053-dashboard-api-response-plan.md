# F053: Dashboard API response caching — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 為 dashboard server 的 hot-path API 加入 mtime-based in-memory cache，減少重複 YAML/JSON parse。

**Architecture:** 新增 `CachedWorkspace` 內嵌 `*Workspace`，override `ListFeatures()`、`LoadFeature()`、`ReadConfig()` 三個方法加上 mtime-based cache。Server 端改用 `*CachedWorkspace`，Go embedding 讓所有 handler 的現有方法呼叫無需改動。CLI 短命程序繼續用 `*Workspace`。

**Tech Stack:** Go 標準庫（`sync`、`os`）

---

### Task 1: WorkspaceReader interface

**Files:**
- Create: `internal/protocol/reader.go`

- [ ] **Step 1: 建立 reader.go**

```go
package protocol

// WorkspaceReader 定義 Workspace 的唯讀操作，用於抽象 cache 層
type WorkspaceReader interface {
	ListFeatures() ([]Feature, error)
	LoadFeature(id string) (Feature, error)
	ReadState(featureID string) (State, error)
	ReadConfig() (Config, error)
}
```

- [ ] **Step 2: 建置確認 Workspace 滿足 interface**

在 `reader.go` 底部加編譯期檢查：

```go
var _ WorkspaceReader = (*Workspace)(nil)
```

- [ ] **Step 3: 建置驗證**

Run: `go build ./internal/protocol/ && go vet ./internal/protocol/`
Expected: 通過

- [ ] **Step 4: Commit**

```bash
git add internal/protocol/reader.go
git commit -m "feat(F053): add WorkspaceReader interface"
```

---

### Task 2: CachedWorkspace — 骨架與 ReadConfig cache

**Files:**
- Create: `internal/protocol/cached.go`
- Create: `internal/protocol/cached_test.go`

- [ ] **Step 1: 寫 failing test — ReadConfig cache hit**

```go
package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupCachedTestWorkspace(t *testing.T) (*CachedWorkspace, string) {
	t.Helper()
	tmp := t.TempDir()
	dotDir := filepath.Join(tmp, DirName)
	os.MkdirAll(filepath.Join(dotDir, FeaturesDir), 0o755)

	cfg := Config{Project: ProjectConfig{Name: "test"}, Default: "claude"}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(dotDir, ConfigFile), data, 0o644)

	ws := &Workspace{Root: tmp}
	return NewCachedWorkspace(ws), tmp
}

func TestCachedReadConfig_CacheHit(t *testing.T) {
	cws, _ := setupCachedTestWorkspace(t)

	cfg1, err := cws.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg1.Project.Name != "test" {
		t.Errorf("Name = %q, want test", cfg1.Project.Name)
	}

	cfg2, err := cws.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Default != cfg1.Default {
		t.Error("second call should return same data")
	}
}

func TestCachedReadConfig_InvalidateOnChange(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)

	_, _ = cws.ReadConfig()

	newCfg := Config{Project: ProjectConfig{Name: "updated"}, Default: "codex"}
	data, _ := json.MarshalIndent(newCfg, "", "  ")
	os.WriteFile(filepath.Join(tmp, DirName, ConfigFile), data, 0o644)

	cfg, err := cws.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project.Name != "updated" {
		t.Errorf("Name = %q, want updated (should invalidate on mtime change)", cfg.Project.Name)
	}
}
```

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./internal/protocol/ -v -run TestCachedReadConfig`
Expected: FAIL — `CachedWorkspace`、`NewCachedWorkspace` 未定義

- [ ] **Step 3: 實作 CachedWorkspace 骨架與 ReadConfig cache**

```go
package protocol

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CachedWorkspace 在 Workspace 上加 mtime-based in-memory cache，供 long-running server 使用
type CachedWorkspace struct {
	*Workspace

	mu          sync.RWMutex
	configCache *Config
	configMtime time.Time
}

var _ WorkspaceReader = (*CachedWorkspace)(nil)

// NewCachedWorkspace 建立帶 cache 的 Workspace wrapper
func NewCachedWorkspace(ws *Workspace) *CachedWorkspace {
	return &CachedWorkspace{Workspace: ws}
}

// ReadConfig 回傳 settings.json，mtime 沒變時回傳 cache
func (c *CachedWorkspace) ReadConfig() (Config, error) {
	path := filepath.Join(c.DotDir(), ConfigFile)
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}

	c.mu.RLock()
	if c.configCache != nil && info.ModTime().Equal(c.configMtime) {
		cfg := *c.configCache
		c.mu.RUnlock()
		return cfg, nil
	}
	c.mu.RUnlock()

	cfg, err := c.Workspace.ReadConfig()
	if err != nil {
		return Config{}, err
	}

	c.mu.Lock()
	c.configCache = &cfg
	c.configMtime = info.ModTime()
	c.mu.Unlock()

	return cfg, nil
}
```

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./internal/protocol/ -v -run TestCachedReadConfig`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/cached.go internal/protocol/cached_test.go
git commit -m "feat(F053): add CachedWorkspace with ReadConfig mtime cache"
```

---

### Task 3: ListFeatures cache

**Files:**
- Modify: `internal/protocol/cached.go`
- Modify: `internal/protocol/cached_test.go`

- [ ] **Step 1: 寫 failing test — ListFeatures cache hit**

```go
func TestCachedListFeatures_CacheHit(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)

	f := Feature{ID: "F001-test", Name: "Test", Status: StatusNotStarted}
	data, _ := yaml.Marshal(f)
	os.WriteFile(filepath.Join(tmp, DirName, FeaturesDir, "F001-test.yaml"), data, 0o644)

	list1, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(list1) != 1 {
		t.Fatalf("len = %d, want 1", len(list1))
	}

	list2, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 1 || list2[0].ID != list1[0].ID {
		t.Error("second call should return cached data")
	}
}

func TestCachedListFeatures_InvalidateOnNewFile(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)

	f1 := Feature{ID: "F001-a", Name: "A", Status: StatusNotStarted}
	data1, _ := yaml.Marshal(f1)
	os.WriteFile(filepath.Join(tmp, DirName, FeaturesDir, "F001-a.yaml"), data1, 0o644)

	list1, _ := cws.ListFeatures()
	if len(list1) != 1 {
		t.Fatalf("len = %d, want 1", len(list1))
	}

	f2 := Feature{ID: "F002-b", Name: "B", Status: StatusNotStarted}
	data2, _ := yaml.Marshal(f2)
	os.WriteFile(filepath.Join(tmp, DirName, FeaturesDir, "F002-b.yaml"), data2, 0o644)

	list2, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 2 {
		t.Errorf("len = %d, want 2 (should detect new file)", len(list2))
	}
}

func TestCachedListFeatures_InvalidateOnModify(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)

	f := Feature{ID: "F001-x", Name: "Original", Status: StatusNotStarted}
	data, _ := yaml.Marshal(f)
	path := filepath.Join(tmp, DirName, FeaturesDir, "F001-x.yaml")
	os.WriteFile(path, data, 0o644)

	list1, _ := cws.ListFeatures()
	if list1[0].Name != "Original" {
		t.Fatalf("Name = %q, want Original", list1[0].Name)
	}

	// 確保 mtime 不同（某些 FS 精度只到秒）
	time.Sleep(10 * time.Millisecond)
	f.Name = "Modified"
	data, _ = yaml.Marshal(f)
	os.WriteFile(path, data, 0o644)

	list2, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if list2[0].Name != "Modified" {
		t.Errorf("Name = %q, want Modified", list2[0].Name)
	}
}
```

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./internal/protocol/ -v -run TestCachedListFeatures`
Expected: FAIL — `CachedWorkspace` 沒有 override `ListFeatures`，呼叫的是 embedded `Workspace` 版本（不會失敗，但也沒 cache 邏輯可測；新增的 invalidation test 需要 cache 才有意義）

確認 test 能跑即可，cache 邏輯在 Step 3 加。

- [ ] **Step 3: 實作 ListFeatures cache**

在 `CachedWorkspace` struct 加欄位：

```go
featuresCache  []Feature
featuresMtimes map[string]time.Time // filename → mtime
```

實作 `ListFeatures()`：

```go
// ListFeatures 回傳所有 feature，目錄內容或任一 YAML mtime 改變時重新 parse
func (c *CachedWorkspace) ListFeatures() ([]Feature, error) {
	dir := filepath.Join(c.DotDir(), FeaturesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	c.mu.RLock()
	if c.featuresCache != nil && c.featuresMtimesMatch(dir, entries) {
		result := make([]Feature, len(c.featuresCache))
		copy(result, c.featuresCache)
		c.mu.RUnlock()
		return result, nil
	}
	c.mu.RUnlock()

	features, err := c.Workspace.ListFeatures()
	if err != nil {
		return nil, err
	}

	mtimes := make(map[string]time.Time)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		if info, err := e.Info(); err == nil {
			mtimes[e.Name()] = info.ModTime()
		}
	}

	c.mu.Lock()
	c.featuresCache = features
	c.featuresMtimes = mtimes
	c.mu.Unlock()

	return features, nil
}

func (c *CachedWorkspace) featuresMtimesMatch(dir string, entries []os.DirEntry) bool {
	yamlCount := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		yamlCount++
		info, err := e.Info()
		if err != nil {
			return false
		}
		cached, ok := c.featuresMtimes[e.Name()]
		if !ok || !info.ModTime().Equal(cached) {
			return false
		}
	}
	return yamlCount == len(c.featuresMtimes)
}
```

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./internal/protocol/ -v -run TestCachedListFeatures`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/cached.go internal/protocol/cached_test.go
git commit -m "feat(F053): add ListFeatures mtime cache to CachedWorkspace"
```

---

### Task 4: LoadFeature cache

**Files:**
- Modify: `internal/protocol/cached.go`
- Modify: `internal/protocol/cached_test.go`

- [ ] **Step 1: 寫 failing test**

```go
func TestCachedLoadFeature_CacheHit(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)

	f := Feature{ID: "F010-load", Name: "LoadTest", Status: StatusNotStarted}
	data, _ := yaml.Marshal(f)
	os.WriteFile(filepath.Join(tmp, DirName, FeaturesDir, "F010-load.yaml"), data, 0o644)

	f1, err := cws.LoadFeature("F010-load")
	if err != nil {
		t.Fatal(err)
	}
	if f1.Name != "LoadTest" {
		t.Errorf("Name = %q, want LoadTest", f1.Name)
	}

	f2, err := cws.LoadFeature("F010-load")
	if err != nil {
		t.Fatal(err)
	}
	if f2.Name != f1.Name {
		t.Error("second call should return cached data")
	}
}

func TestCachedLoadFeature_InvalidateOnModify(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)

	f := Feature{ID: "F010-load2", Name: "Before", Status: StatusNotStarted}
	data, _ := yaml.Marshal(f)
	path := filepath.Join(tmp, DirName, FeaturesDir, "F010-load2.yaml")
	os.WriteFile(path, data, 0o644)

	_, _ = cws.LoadFeature("F010-load2")

	time.Sleep(10 * time.Millisecond)
	f.Name = "After"
	data, _ = yaml.Marshal(f)
	os.WriteFile(path, data, 0o644)

	got, err := cws.LoadFeature("F010-load2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "After" {
		t.Errorf("Name = %q, want After", got.Name)
	}
}
```

- [ ] **Step 2: 跑 test 確認失敗**

Run: `go test ./internal/protocol/ -v -run TestCachedLoadFeature`
Expected: FAIL（呼叫 embedded 版本，沒有 cache 行為；invalidation test 仍會通過但沒有 cache 層）

- [ ] **Step 3: 實作 LoadFeature cache**

在 `CachedWorkspace` struct 加欄位：

```go
featureCache  map[string]Feature   // id → Feature
featureMtime  map[string]time.Time // id → mtime
```

實作：

```go
// LoadFeature 回傳單一 feature，mtime 沒變時回傳 cache
func (c *CachedWorkspace) LoadFeature(id string) (Feature, error) {
	path := filepath.Join(c.DotDir(), FeaturesDir, id+".yaml")
	info, err := os.Stat(path)
	if err != nil {
		return Feature{}, fmt.Errorf("read feature %s: %w", id, err)
	}

	c.mu.RLock()
	if c.featureCache != nil {
		if cached, ok := c.featureCache[id]; ok {
			if mt, ok := c.featureMtime[id]; ok && info.ModTime().Equal(mt) {
				c.mu.RUnlock()
				return cached, nil
			}
		}
	}
	c.mu.RUnlock()

	f, err := c.Workspace.LoadFeature(id)
	if err != nil {
		return Feature{}, err
	}

	c.mu.Lock()
	if c.featureCache == nil {
		c.featureCache = make(map[string]Feature)
		c.featureMtime = make(map[string]time.Time)
	}
	c.featureCache[id] = f
	c.featureMtime[id] = info.ModTime()
	c.mu.Unlock()

	return f, nil
}
```

- [ ] **Step 4: 跑 test 確認通過**

Run: `go test ./internal/protocol/ -v -run TestCachedLoadFeature`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/cached.go internal/protocol/cached_test.go
git commit -m "feat(F053): add LoadFeature mtime cache to CachedWorkspace"
```

---

### Task 5: Server 改用 CachedWorkspace

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/multi.go`

- [ ] **Step 1: server.go — 全域替換 `*protocol.Workspace` → `*protocol.CachedWorkspace`**

將所有 handler 函式簽名中的 `ws *protocol.Workspace` 改為 `ws *protocol.CachedWorkspace`：

```
NewMux(ws *protocol.CachedWorkspace, pm *ProcessManager)
Start(ws *protocol.CachedWorkspace, pm *ProcessManager, port int)
handlePostRun(ws *protocol.CachedWorkspace, ...)
handlePostNew(ws *protocol.CachedWorkspace, ...)
handleTasks(ws *protocol.CachedWorkspace, ...)
handleOverview(ws *protocol.CachedWorkspace, ...)
handleMessages(ws *protocol.CachedWorkspace, ...)
handleEvents(ws *protocol.CachedWorkspace, ...)
handleSSE(ws *protocol.CachedWorkspace, ...)
handleLogs(ws *protocol.CachedWorkspace, ...)
handleFeatureScreenshots(ws *protocol.CachedWorkspace, ...)
getMergedScreenshotDir(ws *protocol.CachedWorkspace)
handleGetScreenshots(ws *protocol.CachedWorkspace, ...)
handleServeScreenshot(ws *protocol.CachedWorkspace, ...)
handleLogSSE(ws *protocol.CachedWorkspace, ...)
handlePostDone(ws *protocol.CachedWorkspace, ...)
transitionDone(ws *protocol.CachedWorkspace, ...)
handleGetSettings(ws *protocol.CachedWorkspace, ...)
handlePutSettings(ws *protocol.CachedWorkspace, ...)
reloadProcessManager(ws *protocol.CachedWorkspace, ...)
handleGetMergedConfig(ws *protocol.CachedWorkspace, ...)
```

Go embedding 確保所有現有方法呼叫（`ws.Root`、`ws.SaveFeature()`、`ws.WriteState()` 等）繼續 work。而 `ws.ListFeatures()`、`ws.LoadFeature()`、`ws.ReadConfig()` 自動走 cached 版本。

- [ ] **Step 2: multi.go — registryEntry 和 Add 改用 CachedWorkspace**

`registryEntry` struct：

```go
type registryEntry struct {
	id  string
	ws  *protocol.CachedWorkspace
	mux http.Handler
	pm  *ProcessManager
}
```

`Add()` 內部 wrap：

```go
func (r *ProjectRegistry) Add(ws *protocol.Workspace) string {
	// ... existing id logic ...
	cws := protocol.NewCachedWorkspace(ws)
	pm := newProcessManagerFromConfig(cws.Workspace)
	r.ids[id] = true
	r.entries = append(r.entries, &registryEntry{id: id, ws: cws, mux: NewMux(cws, pm), pm: pm})
	return id
}
```

`Get()` 回傳型別改為 `*protocol.CachedWorkspace`：

```go
func (r *ProjectRegistry) Get(id string) *protocol.CachedWorkspace {
```

`List()` 裡的 `e.ws.ReadConfig()` 和 `e.ws.ListFeatures()` 自動走 cache。

- [ ] **Step 3: 建置驗證**

Run: `go build ./... && go vet ./...`
Expected: 通過

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go internal/server/multi.go
git commit -m "refactor(F053): server uses CachedWorkspace for mtime-based caching"
```

---

### Task 6: live.go 和其他呼叫端適配

**Files:**
- Modify: `cmd/4x/live.go`（如有直接用 `server.Start` 的地方）
- Possibly modify: 其他呼叫 `server.NewMux` 或 `reg.Get()` 的檔案

- [ ] **Step 1: 檢查所有 `server.Start`、`server.NewMux`、`reg.Get` 呼叫端**

Run: `grep -rn "server\.Start\|server\.NewMux\|reg\.Get\|\.Get(" cmd/4x/ internal/server/ --include="*.go" | grep -v _test.go`

依結果調整呼叫端。`live.go` 裡 `reg.Add(ws)` 不需要改（`Add` 內部已做 wrap）。

若有直接呼叫 `server.Start(ws, ...)` 的地方，改為：

```go
cws := protocol.NewCachedWorkspace(ws)
server.Start(cws, pm, port)
```

- [ ] **Step 2: 建置驗證**

Run: `go build ./... && go vet ./...`
Expected: 通過

- [ ] **Step 3: Commit**

```bash
git add cmd/4x/ internal/server/
git commit -m "refactor(F053): adapt call sites for CachedWorkspace"
```

---

### Task 7: 全量測試與文件

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
git commit -m "docs(F053): update docs for dashboard API caching"
```
