# F051: Multi-Mux Route Deduplication — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 消除 `NewMultiMux` 中複製的 ~150 行路由，改為統一的 `WorkspaceResolver` 模式，路由只在 `NewMux` 定義一次。

**Architecture:** 定義 `WorkspaceResolver` function type，`NewMux` 改為接受 resolver。單一模式用 `singleResolver`（直接回傳固定 ws/pm），multi 模式用 `multiResolver`（從 URL prefix 或 compat 邏輯查找）。`NewMultiMux` 簡化為只註冊全域端點（`/api/projects`、`/api/browse`）+ 把其他請求轉發到 `NewMux(multiResolver)`。

**Tech Stack:** Go 1.26+, net/http, 標準 testing package

---

### File Structure

| 檔案 | 職責 | 改動類型 |
|---|---|---|
| `internal/server/resolver.go` | `WorkspaceResolver` 型別、`singleResolver`、`multiResolver` | 新增 |
| `internal/server/server.go` | `NewMux` 改接受 resolver，handler 改用 resolver 取 ws/pm | 修改 |
| `internal/server/multi.go` | 刪除重複路由（行 231-498），簡化為全域端點 + 轉發 | 修改 |
| `internal/server/server_test.go` | `NewMux(ws, nil)` → `NewMux(singleResolver(ws, nil))` | 修改 |
| `internal/server/multi_test.go` | 不動（`NewMultiMux` 簽名不變） | 不動 |
| `internal/server/resolver_test.go` | resolver 單元測試 | 新增 |

---

### Task 1: 定義 WorkspaceResolver 型別與 singleResolver

**Files:**
- Create: `internal/server/resolver.go`
- Create: `internal/server/resolver_test.go`

- [ ] **Step 1: 寫 singleResolver 的 failing test**

在 `internal/server/resolver_test.go`：

```go
package server

import (
	"net/http"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestSingleResolver(t *testing.T) {
	ws := &protocol.Workspace{Root: "/tmp/test"}
	resolver := singleResolver(ws, nil)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	got, pm, err := resolver(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != ws {
		t.Error("workspace mismatch")
	}
	if pm != nil {
		t.Error("pm should be nil")
	}
}
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/server/ -run TestSingleResolver -v`
Expected: FAIL — `singleResolver` 不存在

- [ ] **Step 3: 實作 resolver.go**

在 `internal/server/resolver.go`：

```go
package server

import (
	"net/http"

	"github.com/ggwhite/4x/internal/protocol"
)

// WorkspaceResolver 從 HTTP request 解析出對應的 workspace 和 process manager
type WorkspaceResolver func(r *http.Request) (*protocol.Workspace, *ProcessManager, error)

// singleResolver 回傳固定 workspace 的 resolver，用於單一專案模式
func singleResolver(ws *protocol.Workspace, pm *ProcessManager) WorkspaceResolver {
	return func(r *http.Request) (*protocol.Workspace, *ProcessManager, error) {
		return ws, pm, nil
	}
}
```

- [ ] **Step 4: 執行測試確認通過**

Run: `go test ./internal/server/ -run TestSingleResolver -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/resolver.go internal/server/resolver_test.go
git commit -m "feat(F051): add WorkspaceResolver type and singleResolver"
```

---

### Task 2: 實作 multiResolver

**Files:**
- Modify: `internal/server/resolver.go`
- Modify: `internal/server/resolver_test.go`

- [ ] **Step 1: 寫 multiResolver prefix routing 的 failing test**

在 `internal/server/resolver_test.go` 加入：

```go
func TestMultiResolver_PrefixRoute(t *testing.T) {
	ws := setupMultiWorkspace(t, "test-proj")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/project/"+id+"/api/tasks", nil)
	got, _, err := resolver(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != ws {
		t.Error("workspace mismatch")
	}
	if r.URL.Path != "/api/tasks" {
		t.Errorf("path = %s, want /api/tasks", r.URL.Path)
	}
}

func TestMultiResolver_SSEPrefixRoute(t *testing.T) {
	ws := setupMultiWorkspace(t, "sse-proj")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/sse/project/"+id+"/events/feat-1", nil)
	got, _, err := resolver(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != ws {
		t.Error("workspace mismatch")
	}
	if r.URL.Path != "/sse/events/feat-1" {
		t.Errorf("path = %s, want /sse/events/feat-1", r.URL.Path)
	}
}

func TestMultiResolver_CompatSingleProject(t *testing.T) {
	ws := setupMultiWorkspace(t, "only-one")
	reg := NewProjectRegistry()
	reg.Add(ws)

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	got, _, err := resolver(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != ws {
		t.Error("workspace mismatch")
	}
}

func TestMultiResolver_CompatMultiProject(t *testing.T) {
	ws1 := setupMultiWorkspace(t, "proj-a")
	ws2 := setupMultiWorkspace(t, "proj-b")
	reg := NewProjectRegistry()
	reg.Add(ws1)
	reg.Add(ws2)

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	_, _, err := resolver(r)
	if err == nil {
		t.Error("expected error for multiple projects")
	}
}

func TestMultiResolver_CompatZeroProject(t *testing.T) {
	reg := NewProjectRegistry()

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	_, _, err := resolver(r)
	if err == nil {
		t.Error("expected error for zero projects")
	}
}

func TestMultiResolver_PrefixNotFound(t *testing.T) {
	reg := NewProjectRegistry()

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/project/nonexist/api/tasks", nil)
	_, _, err := resolver(r)
	if err == nil {
		t.Error("expected error for unknown project")
	}
}
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/server/ -run TestMultiResolver -v`
Expected: FAIL — `multiResolver` 不存在

- [ ] **Step 3: 實作 multiResolver**

在 `internal/server/resolver.go` 加入：

```go
// multiResolver 從 URL prefix 或 compat 邏輯查找 workspace，用於多專案模式
func multiResolver(reg *ProjectRegistry) WorkspaceResolver {
	return func(r *http.Request) (*protocol.Workspace, *ProcessManager, error) {
		path := r.URL.Path

		// prefix routing: /api/project/{id}/... 或 /sse/project/{id}/...
		if strings.HasPrefix(path, "/api/project/") {
			rest := strings.TrimPrefix(path, "/api/project/")
			idx := strings.Index(rest, "/")
			if idx < 0 {
				return nil, nil, fmt.Errorf("missing path after project id")
			}
			id := rest[:idx]
			entry := reg.getEntry(id)
			if entry == nil {
				return nil, nil, fmt.Errorf("project %q not found", id)
			}
			r.URL.Path = rest[idx:]
			return entry.ws, entry.pm, nil
		}
		if strings.HasPrefix(path, "/sse/project/") {
			rest := strings.TrimPrefix(path, "/sse/project/")
			idx := strings.Index(rest, "/")
			if idx < 0 {
				return nil, nil, fmt.Errorf("missing path after project id")
			}
			id := rest[:idx]
			entry := reg.getEntry(id)
			if entry == nil {
				return nil, nil, fmt.Errorf("project %q not found", id)
			}
			sub := rest[idx:]
			r.URL.Path = "/sse" + sub
			return entry.ws, entry.pm, nil
		}

		// compat: 無 prefix，registry 剛好 1 個就用它
		n := reg.Count()
		if n == 0 {
			return nil, nil, fmt.Errorf("no projects loaded — add a project first")
		}
		if n != 1 {
			return nil, nil, fmt.Errorf("multiple projects loaded — use /api/project/{id}%s", path)
		}
		entries := reg.List()
		entry := reg.getEntry(entries[0].ID)
		if entry == nil {
			return nil, nil, fmt.Errorf("project unavailable")
		}
		return entry.ws, entry.pm, nil
	}
}
```

在 import 加入 `"fmt"` 和 `"strings"`。

- [ ] **Step 4: 執行測試確認通過**

Run: `go test ./internal/server/ -run TestMultiResolver -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/resolver.go internal/server/resolver_test.go
git commit -m "feat(F051): add multiResolver with prefix and compat routing"
```

---

### Task 3: NewMux 改為接受 WorkspaceResolver

**Files:**
- Modify: `internal/server/server.go:36-184`
- Modify: `internal/server/server_test.go`（所有 `NewMux(ws, pm)` 呼叫）

- [ ] **Step 1: 修改 NewMux 簽名**

在 `internal/server/server.go`，把 `NewMux` 改為：

```go
// NewMux 建立 dashboard 的 HTTP handler。
func NewMux(resolver WorkspaceResolver) http.Handler {
```

- [ ] **Step 2: 修改每個 handler 改用 resolver**

每個用到 `ws` 的 handler 改為在函式開頭呼叫 resolver。以 `/api/tasks` 為例：

```go
mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
    ws, _, err := resolver(r)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    handleTasks(ws, w)
})
```

需要 pm 的 handler（run/runs/stop/new）改為永遠註冊，handler 內部檢查 pm：

```go
mux.HandleFunc("/api/run", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    ws, pm, err := resolver(r)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    if pm == nil {
        http.Error(w, "run not available", http.StatusServiceUnavailable)
        return
    }
    handlePostRun(ws, pm, w, r)
})
```

不依賴 ws 的路由（`/api/user-config`、`/api/supported-runners`、`/api/locales`、`/api/locales/`、`/`）維持原樣不呼叫 resolver。

`/api/settings` 需要 ws 做讀寫，也要改用 resolver：

```go
mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
        ws, _, err := resolver(r)
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }
        handleGetSettings(ws, w)
        return
    }
    if r.Method == http.MethodPut {
        ws, pm, err := resolver(r)
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }
        handlePutSettings(ws, w, r)
        reloadProcessManager(ws, pm)
        return
    }
    http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
})
```

`/api/merged-config`、`/api/done` 同理。

完整需改的路由清單（13 條）：
1. `/api/tasks` — 加 resolver
2. `/api/run` — 加 resolver + pm check
3. `/api/runs` — 加 resolver + pm check
4. `/api/stop` — 加 resolver + pm check
5. `/api/new` — 加 resolver + pm check
6. `/api/settings` — 加 resolver
7. `/api/merged-config` — 加 resolver
8. `/api/done` — 加 resolver
9. `/api/messages/` — 加 resolver
10. `/api/overview/` — 加 resolver
11. `/api/events/` — 加 resolver
12. `/api/logs/` — 加 resolver
13. `/api/features/` — 加 resolver
14. `/sse/events/` — 加 resolver
15. `/sse/logs/` — 加 resolver

- [ ] **Step 3: 修改 Start 函式**

```go
func Start(ws *protocol.Workspace, pm *ProcessManager, port int) error {
	return http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), NewMux(singleResolver(ws, pm)))
}
```

- [ ] **Step 4: 修改 server_test.go 所有呼叫處**

全域替換 `NewMux(ws, nil)` → `NewMux(singleResolver(ws, nil))`，`NewMux(ws, pm)` → `NewMux(singleResolver(ws, pm))`。

- [ ] **Step 5: 編譯確認無錯誤**

Run: `go build ./... && go vet ./...`
Expected: 無錯誤

- [ ] **Step 6: 跑 server_test.go 確認全部通過**

Run: `go test ./internal/server/ -run 'Test[^M]' -v`
Expected: 全部 PASS（排除 Multi 開頭的測試先跑非 multi 的）

- [ ] **Step 7: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "refactor(F051): NewMux accepts WorkspaceResolver instead of direct ws/pm"
```

---

### Task 4: 簡化 NewMultiMux — 刪除重複路由

**Files:**
- Modify: `internal/server/multi.go:60,151-498`

- [ ] **Step 1: 修改 registry.Add 不再建 per-entry mux**

在 `internal/server/multi.go` 的 `Add` 方法中，移除 `NewMux(ws, pm)` 呼叫。`registryEntry` 的 `mux` 欄位刪除：

```go
type registryEntry struct {
	id string
	ws *protocol.Workspace
	pm *ProcessManager
}
```

`Add` 方法改為：

```go
func (r *ProjectRegistry) Add(ws *protocol.Workspace) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := filepath.Base(ws.Root)
	if r.ids[id] {
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s-%d", id, i)
			if !r.ids[candidate] {
				id = candidate
				break
			}
		}
	}

	pm := NewProcessManager(ws)
	r.entries = append(r.entries, &registryEntry{id: id, ws: ws, pm: pm})
	r.ids[id] = true
	return id
}
```

- [ ] **Step 2: 簡化 NewMultiMux**

刪除行 231-498（所有 compat 路由、prefix routing、locales、static files），改為：

```go
func NewMultiMux(reg *ProjectRegistry, recentPath string) http.Handler {
	outerMux := http.NewServeMux()

	// 全域管理端點：/api/projects
	outerMux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// ... 保持現有的 GET /api/projects 邏輯不變
		case http.MethodPost:
			// ... 保持現有的 POST /api/projects 邏輯不變
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	outerMux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		// ... 保持現有的 DELETE /api/projects/{id} 邏輯不變
	})

	// 全域端點：/api/browse
	outerMux.HandleFunc("/api/browse", func(w http.ResponseWriter, r *http.Request) {
		// ... 保持現有的 browse 邏輯不變（行 431-483）
	})

	// 統一路由：所有其他請求走 NewMux + multiResolver
	inner := NewMux(multiResolver(reg))
	outerMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r)
	})

	return outerMux
}
```

關鍵改動：
- `/api/projects`、`/api/projects/`、`/api/browse` 保持為全域端點直接處理
- 所有其他路由（`/api/*`、`/sse/*`、`/`）由 `NewMux(multiResolver(reg))` 統一處理
- `multiResolver` 負責 prefix strip 和 compat 邏輯
- locales 和 static files 由 `NewMux` 內部處理（已經有了）
- 刪除所有 `compatGetWs`、`compatError` 以及重複的 handler 註冊

- [ ] **Step 3: 刪除 registryEntry 的 mux 欄位相關程式碼**

確認 `entry.mux` 在 multi.go 中不再被引用。刪除 `getEntry` 方法中的 mux 相關邏輯（如果有的話），或把 `getEntry` 保留但只回傳 ws/pm。

- [ ] **Step 4: 編譯確認無錯誤**

Run: `go build ./... && go vet ./...`
Expected: 無錯誤

- [ ] **Step 5: 跑全部 server 測試**

Run: `go test ./internal/server/ -v`
Expected: 全部 PASS（包括 multi_test.go 的所有 compat、prefix、settings 測試）

- [ ] **Step 6: Commit**

```bash
git add internal/server/multi.go
git commit -m "refactor(F051): remove duplicated routes from NewMultiMux, delegate to NewMux"
```

---

### Task 5: 清理與收尾

**Files:**
- Modify: `internal/server/multi.go`（清理未使用的 import/variable）
- Modify: `.4x/features/F051-multi-mux-route.yaml`

- [ ] **Step 1: 跑 go vet 和 lint 清理**

Run: `go vet ./... && go build ./...`

如果有 unused import 或 variable 警告，清掉。

- [ ] **Step 2: 跑 doc sync 檢查**

Run: `make check-docs-sync`

如果輸出 `NEEDS_UPDATE`，更新被點名的檔案。

- [ ] **Step 3: 最終驗證**

Run: `go build ./cmd/4x && go vet ./... && go test ./...`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore(F051): cleanup unused imports and variables after route dedup"
```
