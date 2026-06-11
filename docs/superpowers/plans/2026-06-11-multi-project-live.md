# Multi-Project Live Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 讓 `4x live` 支援多專案 — 啟動時選資料夾、tab 切換、跨專案搜尋，並廢棄 `4x monitor`。

**Architecture:** Server 層持有 `map[string]*protocol.Workspace`，用 project ID 做 prefix routing。前端 SPA 用 tab 模式切換專案，每個 tab 各自打 `/api/project/{id}/...`。CLI 統一為 `4x live`，支援 0/1/多個路徑引數。

**Tech Stack:** Go 1.26+, Cobra CLI, net/http, html/template (embedded), vanilla JS + Tailwind CDN, Swift AppKit/WebKit (macOS)

---

## File Structure

| 檔案 | 動作 | 職責 |
|------|------|------|
| `internal/server/projects.go` | 建立 | recent-projects.json 讀寫 + project registry（in-memory map + CRUD） |
| `internal/server/projects_test.go` | 建立 | projects.go 的測試 |
| `internal/server/multi.go` | 建立 | `NewMultiMux` — multi-workspace prefix routing + project CRUD HTTP handlers |
| `internal/server/multi_test.go` | 建立 | multi.go 的測試 |
| `internal/server/server.go` | 修改 | `NewMux` 改為 exported（已是），`Start` 改為用 `NewMultiMux` |
| `internal/server/server_test.go` | 修改 | 確保既有測試不壞 |
| `internal/server/static/index.html` | 修改 | tab bar + 專案選擇器 + 跨專案搜尋 |
| `cmd/4x/live.go` | 重寫 | 多引數 + `--web`/`--app` flag + 呼叫 `NewMultiMux` |
| `cmd/4x/monitor.go` | 刪除 | 廢棄 |
| `cmd/4x/main.go` | 修改 | 移除 `newMonitorCmd()` 註冊 |
| `dashboard/macos/Sources/main.swift` | 修改 | NSOpenPanel + poll server ready + 標題同步 |

---

### Task 1: Recent Projects 持久化（`internal/server/projects.go`）

**Files:**
- Create: `internal/server/projects.go`
- Create: `internal/server/projects_test.go`

- [ ] **Step 1: 寫 ProjectEntry 型別和 RecentProjects 結構的 failing test**

```go
// internal/server/projects_test.go
package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRecentProjects_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recent-projects.json")
	rp, err := LoadRecentProjects(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rp.Projects) != 0 {
		t.Errorf("projects = %d, want 0", len(rp.Projects))
	}
}

func TestSaveAndLoadRecentProjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recent-projects.json")

	rp := &RecentProjects{}
	rp.Touch("/tmp/project-a")
	rp.Touch("/tmp/project-b")

	if err := SaveRecentProjects(path, rp); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadRecentProjects(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(loaded.Projects))
	}
	// LRU: project-b should be first (most recent)
	if loaded.Projects[0].Path != "/tmp/project-b" {
		t.Errorf("first = %s, want /tmp/project-b", loaded.Projects[0].Path)
	}
}

func TestRecentProjects_TouchExisting(t *testing.T) {
	rp := &RecentProjects{}
	rp.Touch("/tmp/a")
	rp.Touch("/tmp/b")
	rp.Touch("/tmp/a") // touch again → should move to front

	if len(rp.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(rp.Projects))
	}
	if rp.Projects[0].Path != "/tmp/a" {
		t.Errorf("first = %s, want /tmp/a", rp.Projects[0].Path)
	}
}

func TestRecentProjects_MaxLimit(t *testing.T) {
	rp := &RecentProjects{}
	for i := 0; i < 25; i++ {
		rp.Touch(filepath.Join("/tmp", "p"+string(rune('a'+i))))
	}
	if len(rp.Projects) != 20 {
		t.Errorf("projects = %d, want 20 (max)", len(rp.Projects))
	}
}

func TestRecentProjects_Remove(t *testing.T) {
	rp := &RecentProjects{}
	rp.Touch("/tmp/a")
	rp.Touch("/tmp/b")
	rp.Remove("/tmp/a")

	if len(rp.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(rp.Projects))
	}
	if rp.Projects[0].Path != "/tmp/b" {
		t.Errorf("remaining = %s, want /tmp/b", rp.Projects[0].Path)
	}
}

func TestDefaultRecentProjectsPath(t *testing.T) {
	path, err := DefaultRecentProjectsPath()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".4x", "recent-projects.json")
	if path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
}
```

- [ ] **Step 2: 跑測試確認 fail**

Run: `cd /Users/white/github/4x && go test ./internal/server/ -run "TestLoadRecentProjects|TestSaveAndLoad|TestRecentProjects|TestDefaultRecent" -v`
Expected: FAIL — `LoadRecentProjects`, `RecentProjects`, `SaveRecentProjects`, `DefaultRecentProjectsPath` undefined

- [ ] **Step 3: 實作 projects.go**

```go
// internal/server/projects.go
package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const maxRecentProjects = 20

// ProjectEntry 記錄一個最近開過的專案
type ProjectEntry struct {
	Path       string    `json:"path"`
	LastOpened time.Time `json:"lastOpened"`
}

// RecentProjects 管理最近開過的專案列表（LRU 順序）
type RecentProjects struct {
	Projects []ProjectEntry `json:"projects"`
}

// Touch 將路徑加到最前面（若已存在則移到最前），超過上限時淘汰最舊
func (rp *RecentProjects) Touch(path string) {
	now := time.Now()
	filtered := make([]ProjectEntry, 0, len(rp.Projects))
	for _, p := range rp.Projects {
		if p.Path != path {
			filtered = append(filtered, p)
		}
	}
	rp.Projects = append([]ProjectEntry{{Path: path, LastOpened: now}}, filtered...)
	if len(rp.Projects) > maxRecentProjects {
		rp.Projects = rp.Projects[:maxRecentProjects]
	}
}

// Remove 從列表移除指定路徑
func (rp *RecentProjects) Remove(path string) {
	filtered := make([]ProjectEntry, 0, len(rp.Projects))
	for _, p := range rp.Projects {
		if p.Path != path {
			filtered = append(filtered, p)
		}
	}
	rp.Projects = filtered
}

// DefaultRecentProjectsPath 回傳 ~/.4x/recent-projects.json
func DefaultRecentProjectsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".4x", "recent-projects.json"), nil
}

// LoadRecentProjects 讀取 recent-projects.json，檔案不存在時回傳空列表
func LoadRecentProjects(path string) (*RecentProjects, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RecentProjects{}, nil
		}
		return nil, err
	}
	var rp RecentProjects
	if err := json.Unmarshal(data, &rp); err != nil {
		return nil, err
	}
	return &rp, nil
}

// SaveRecentProjects 寫入 recent-projects.json，自動建立父目錄
func SaveRecentProjects(path string, rp *RecentProjects) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
```

- [ ] **Step 4: 跑測試確認 pass**

Run: `cd /Users/white/github/4x && go test ./internal/server/ -run "TestLoadRecentProjects|TestSaveAndLoad|TestRecentProjects|TestDefaultRecent" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/projects.go internal/server/projects_test.go
git commit -m "feat(server): add recent-projects persistence (LRU, max 20)"
```

---

### Task 2: Multi-Workspace Mux（`internal/server/multi.go`）

**Files:**
- Create: `internal/server/multi.go`
- Create: `internal/server/multi_test.go`

- [ ] **Step 1: 寫 ProjectRegistry + NewMultiMux 的 failing test**

```go
// internal/server/multi_test.go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func setupMultiWorkspace(t *testing.T, name string) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: name}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	f := protocol.Feature{ID: "feat-1", Name: "Feature One", Status: "in-progress"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("feat-1"); err != nil {
		t.Fatal(err)
	}
	state := protocol.State{FeatureID: "feat-1", Phase: protocol.PhaseCoding, Round: 1, Active: true}
	if err := ws.WriteState("feat-1", state); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestNewProjectRegistry_IDFromBaseName(t *testing.T) {
	ws := setupMultiWorkspace(t, "alpha")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	if id == "" {
		t.Fatal("id should not be empty")
	}
	projects := reg.List()
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	if projects[0].Name != "alpha" {
		t.Errorf("name = %s, want alpha", projects[0].Name)
	}
}

func TestNewProjectRegistry_DuplicateBaseName(t *testing.T) {
	ws1 := setupMultiWorkspace(t, "app")
	ws2 := setupMultiWorkspace(t, "app")
	reg := NewProjectRegistry()
	id1 := reg.Add(ws1)
	id2 := reg.Add(ws2)

	if id1 == id2 {
		t.Errorf("duplicate IDs: %s", id1)
	}
}

func TestMultiMux_GetProjects(t *testing.T) {
	ws := setupMultiWorkspace(t, "my-project")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	srv := httptest.NewServer(NewMultiMux(reg, recentPath))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var projects []ProjectListItem
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	if projects[0].Name != "my-project" {
		t.Errorf("name = %s, want my-project", projects[0].Name)
	}
}

func TestMultiMux_PrefixRouting(t *testing.T) {
	ws := setupMultiWorkspace(t, "my-project")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	srv := httptest.NewServer(NewMultiMux(reg, recentPath))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/project/" + id + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var tasks []taskInfo
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if tasks[0].ID != "feat-1" {
		t.Errorf("ID = %s, want feat-1", tasks[0].ID)
	}
}

func TestMultiMux_PostProject(t *testing.T) {
	ws := setupMultiWorkspace(t, "existing")
	reg := NewProjectRegistry()
	reg.Add(ws)

	// 建立另一個有 .4x/ 的目錄
	newRoot := t.TempDir()
	newCfg := protocol.Config{Project: protocol.ProjectConfig{Name: "new-proj"}}
	if err := protocol.Init(newRoot, newCfg); err != nil {
		t.Fatal(err)
	}

	recentPath := t.TempDir() + "/recent.json"
	srv := httptest.NewServer(NewMultiMux(reg, recentPath))
	defer srv.Close()

	body := `{"path":"` + newRoot + `"}`
	resp, err := http.Post(srv.URL+"/api/projects", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	projects := reg.List()
	if len(projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(projects))
	}
}

func TestMultiMux_DeleteProject(t *testing.T) {
	ws := setupMultiWorkspace(t, "to-remove")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	srv := httptest.NewServer(NewMultiMux(reg, recentPath))
	defer srv.Close()

	req, _ := http.NewRequest("DELETE", srv.URL+"/api/projects/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if len(reg.List()) != 0 {
		t.Error("project should be removed")
	}
}

func TestMultiMux_IndexHTML(t *testing.T) {
	reg := NewProjectRegistry()
	recentPath := t.TempDir() + "/recent.json"
	srv := httptest.NewServer(NewMultiMux(reg, recentPath))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/html" {
		t.Errorf("Content-Type = %s, want text/html", ct)
	}
}
```

- [ ] **Step 2: 跑測試確認 fail**

Run: `cd /Users/white/github/4x && go test ./internal/server/ -run "TestNewProjectRegistry|TestMultiMux" -v`
Expected: FAIL — `NewProjectRegistry`, `NewMultiMux`, `ProjectListItem` undefined

- [ ] **Step 3: 實作 multi.go**

```go
// internal/server/multi.go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ggwhite/4x/internal/protocol"
)

// ProjectListItem 是 GET /api/projects 的回應項目
type ProjectListItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	TaskCount int    `json:"taskCount"`
}

type registryEntry struct {
	id string
	ws *protocol.Workspace
}

// ProjectRegistry 管理多個 workspace 的 in-memory 註冊表
type ProjectRegistry struct {
	mu      sync.RWMutex
	entries []registryEntry
	ids     map[string]bool
}

// NewProjectRegistry 建立空的 registry
func NewProjectRegistry() *ProjectRegistry {
	return &ProjectRegistry{ids: make(map[string]bool)}
}

// Add 註冊一個 workspace，回傳分配的 project ID
func (r *ProjectRegistry) Add(ws *protocol.Workspace) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	base := filepath.Base(ws.Root)
	id := base
	for i := 2; r.ids[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	r.ids[id] = true
	r.entries = append(r.entries, registryEntry{id: id, ws: ws})
	return id
}

// Remove 從 registry 移除指定 ID
func (r *ProjectRegistry) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range r.entries {
		if e.id == id {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			delete(r.ids, id)
			return true
		}
	}
	return false
}

// Get 取得指定 ID 的 workspace
func (r *ProjectRegistry) Get(id string) *protocol.Workspace {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.id == id {
			return e.ws
		}
	}
	return nil
}

// List 回傳所有已註冊專案的資訊
func (r *ProjectRegistry) List() []ProjectListItem {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]ProjectListItem, 0, len(r.entries))
	for _, e := range r.entries {
		name := filepath.Base(e.ws.Root)
		if cfg, err := e.ws.ReadConfig(); err == nil && cfg.Project.Name != "" {
			name = cfg.Project.Name
		}
		count := 0
		if features, err := e.ws.ListFeatures(); err == nil {
			count = len(features)
		}
		items = append(items, ProjectListItem{
			ID:        e.id,
			Name:      name,
			Path:      e.ws.Root,
			TaskCount: count,
		})
	}
	return items
}

// NewMultiMux 建立支援多專案的 HTTP handler
func NewMultiMux(reg *ProjectRegistry, recentPath string) http.Handler {
	mux := http.NewServeMux()

	// GET /api/projects
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(reg.List())

		case http.MethodPost:
			var body struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			ws, err := protocol.Find(body.Path)
			if err != nil {
				http.Error(w, "not a 4x project: "+err.Error(), http.StatusBadRequest)
				return
			}
			id := reg.Add(ws)

			rp, _ := LoadRecentProjects(recentPath)
			rp.Touch(ws.Root)
			SaveRecentProjects(recentPath, rp)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": id})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// DELETE /api/projects/{id}
	mux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			// 可能是 GET /api/projects/ 的 trailing slash，轉到 GET handler
			if r.Method == http.MethodGet && r.URL.Path == "/api/projects/" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(reg.List())
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/projects/")
		if id == "" {
			http.Error(w, "missing project id", http.StatusBadRequest)
			return
		}
		if !reg.Remove(id) {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Prefix routing: /api/project/{id}/...
	mux.HandleFunc("/api/project/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/project/")
		idx := strings.Index(rest, "/")
		if idx < 0 {
			http.Error(w, "missing path after project id", http.StatusBadRequest)
			return
		}
		id := rest[:idx]
		ws := reg.Get(id)
		if ws == nil {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		subPath := rest[idx:]
		r.URL.Path = subPath
		NewMux(ws).ServeHTTP(w, r)
	})

	// SSE prefix routing: /sse/project/{id}/...
	mux.HandleFunc("/sse/project/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/sse/project/")
		idx := strings.Index(rest, "/")
		if idx < 0 {
			http.Error(w, "missing path after project id", http.StatusBadRequest)
			return
		}
		id := rest[:idx]
		ws := reg.Get(id)
		if ws == nil {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		featureID := strings.TrimPrefix(rest[idx:], "/events/")
		handleSSE(ws, featureID, w, r)
	})

	// Index HTML
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, indexHTML)
	})

	return mux
}
```

- [ ] **Step 4: 跑測試確認 pass**

Run: `cd /Users/white/github/4x && go test ./internal/server/ -run "TestNewProjectRegistry|TestMultiMux" -v`
Expected: PASS

- [ ] **Step 5: 確認既有測試不壞**

Run: `cd /Users/white/github/4x && go test ./internal/server/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/multi.go internal/server/multi_test.go
git commit -m "feat(server): add multi-workspace mux with prefix routing and project CRUD"
```

---

### Task 3: 重寫 CLI `4x live`

**Files:**
- Modify: `cmd/4x/live.go`
- Modify: `cmd/4x/main.go:23-37` (移除 monitor 註冊)
- Delete: `cmd/4x/monitor.go`

- [ ] **Step 1: 重寫 live.go**

```go
// cmd/4x/live.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/server"
	"github.com/spf13/cobra"
)

func newLiveCmd() *cobra.Command {
	var (
		port    int
		webFlag bool
		appFlag bool
	)

	cmd := &cobra.Command{
		Use:   "live [path...]",
		Short: "Start the 4x Live dashboard server",
		Long: `Start the multi-project dashboard server.

Without arguments, loads recent projects and opens the project picker.
With paths, opens each as a project tab.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := server.NewProjectRegistry()

			recentPath, err := server.DefaultRecentProjectsPath()
			if err != nil {
				return err
			}

			if len(args) > 0 {
				for _, path := range args {
					ws, err := protocol.Find(path)
					if err != nil {
						fmt.Fprintf(os.Stderr, "warning: %s — %v\n", path, err)
						continue
					}
					reg.Add(ws)

					rp, _ := server.LoadRecentProjects(recentPath)
					rp.Touch(ws.Root)
					server.SaveRecentProjects(recentPath, rp)
				}
			} else {
				rp, _ := server.LoadRecentProjects(recentPath)
				for _, entry := range rp.Projects {
					ws, err := protocol.Find(entry.Path)
					if err != nil {
						continue
					}
					reg.Add(ws)
				}
			}

			url := fmt.Sprintf("http://localhost:%d", port)
			projects := reg.List()
			fmt.Printf("4x Live — %s\n", url)
			if len(projects) > 0 {
				for _, p := range projects {
					fmt.Printf("  + %s (%s)\n", p.Name, p.Path)
				}
			} else {
				fmt.Println("  No projects loaded — use the picker in the browser")
			}

			if webFlag {
				openBrowser(url)
			}
			if appFlag {
				launchNativeApp(port)
			}

			return server.StartMulti(reg, port, recentPath)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 4567, "dashboard port")
	cmd.Flags().BoolVarP(&webFlag, "web", "w", false, "open browser after start")
	cmd.Flags().BoolVarP(&appFlag, "app", "a", false, "launch native app after start")
	return cmd
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func launchNativeApp(port int) {
	if runtime.GOOS != "darwin" {
		fmt.Fprintf(os.Stderr, "native app not supported on %s yet\n", runtime.GOOS)
		return
	}
	// macOS: 嘗試啟動編譯好的 app（路徑可能需要依安裝方式調整）
	cmd := exec.Command("open", "-a", "4x Live", "--args", fmt.Sprintf("--port=%d", port))
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not launch native app: %v\n", err)
	}
}
```

- [ ] **Step 2: 在 server.go 新增 StartMulti 函式**

在 `internal/server/server.go` 的 `Start` 函式下方新增：

```go
// StartMulti 啟動多專案 dashboard server
func StartMulti(reg *ProjectRegistry, port int, recentPath string) error {
	return http.ListenAndServe(fmt.Sprintf(":%d", port), NewMultiMux(reg, recentPath))
}
```

- [ ] **Step 3: 從 main.go 移除 monitor 註冊**

修改 `cmd/4x/main.go`，把 `newMonitorCmd(),` 這行刪掉。

- [ ] **Step 4: 刪除 monitor.go**

```bash
rm cmd/4x/monitor.go
```

- [ ] **Step 5: 確認編譯和全部測試通過**

Run: `cd /Users/white/github/4x && go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/4x/live.go cmd/4x/main.go internal/server/server.go
git rm cmd/4x/monitor.go
git commit -m "feat(cli): unify live + monitor into multi-project 4x live command"
```

---

### Task 4: 前端 — Tab Bar + API Prefix 切換

**Files:**
- Modify: `internal/server/static/index.html`

這是最大的改動。分步修改現有 `index.html`，加入 tab bar、切換邏輯、API prefix。

- [ ] **Step 1: 加入 tab bar HTML 結構**

在 `<body>` 的 `<div class="flex h-screen">` 之前，插入 tab bar：

```html
<!-- Tab Bar -->
<div id="tab-bar" class="flex items-center border-b border-zinc-800/80 bg-zinc-950/80" style="min-height:36px">
  <div id="tabs" class="flex items-center overflow-x-auto flex-1 gap-0.5 px-1"></div>
  <button id="add-tab-btn" onclick="showProjectPicker()" class="px-3 py-1.5 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50 text-sm transition-colors" title="Add project">+</button>
</div>
```

- [ ] **Step 2: 加入專案選擇器 modal HTML**

在 settings-modal 之後加入：

```html
<!-- Project Picker Modal -->
<div id="picker-modal" class="modal-backdrop" onclick="if(event.target===this)closeProjectPicker()">
  <div class="modal-panel fade-in" style="width:520px">
    <div style="padding:20px 24px 16px;border-bottom:1px solid var(--border)">
      <span style="font-size:16px;font-weight:700">Open Project</span>
    </div>
    <div id="recent-list" style="overflow-y:auto;max-height:40vh"></div>
    <div style="padding:16px 24px;border-top:1px solid var(--border)">
      <div style="display:flex;gap:8px">
        <input id="path-input" type="text" placeholder="Enter project path..." autocomplete="off"
          style="flex:1;background:var(--bg-input);border:1px solid var(--border);border-radius:8px;padding:8px 12px;color:var(--text-1);font-size:13px;font-family:inherit;outline:none"
          onkeydown="if(event.key==='Enter')addProjectFromInput()">
        <button onclick="addProjectFromInput()" style="padding:8px 16px;background:var(--accent);color:#000;border:none;border-radius:8px;font-size:13px;font-weight:600;cursor:pointer">Open</button>
      </div>
      <div id="path-error" style="color:#f87171;font-size:12px;margin-top:6px;display:none"></div>
    </div>
  </div>
</div>
```

- [ ] **Step 3: 加入 JS 的多專案狀態管理**

在 `<script>` 區塊開頭（`let current = null;` 之前），插入多專案狀態：

```javascript
// ── Multi-project state ──
let projects = [];          // [{id, name, path, taskCount}]
let activeProjectId = null; // 當前 active tab 的 project ID
let openTabs = [];          // [{id, name}] — tab 順序

function apiBase() {
  if (!activeProjectId) return '';
  return '/api/project/' + activeProjectId;
}

function sseBase() {
  if (!activeProjectId) return '';
  return '/sse/project/' + activeProjectId;
}

function saveTabState() {
  localStorage.setItem('4x-tabs', JSON.stringify({ tabs: openTabs, active: activeProjectId }));
}

function loadTabState() {
  try {
    const s = JSON.parse(localStorage.getItem('4x-tabs') || '{}');
    return { tabs: s.tabs || [], active: s.active || null };
  } catch { return { tabs: [], active: null }; }
}
```

- [ ] **Step 4: 修改所有 fetch URL，改用 apiBase()**

將所有 `fetch('/api/tasks')` 改為 `fetch(apiBase() + '/api/tasks')`，同理：
- `fetch('/api/messages/' + id)` → `fetch(apiBase() + '/api/messages/' + id)`
- `fetch('/api/events/' + id)` → `fetch(apiBase() + '/api/events/' + id)`
- `new EventSource('/sse/events/' + featureId)` → `new EventSource(sseBase() + '/events/' + featureId)`

在 `load()` 函式中加入 guard：
```javascript
async function load() {
  if (!activeProjectId) { renderProjectPicker(); return; }
  const tasks = await (await fetch(apiBase() + '/api/tasks')).json();
  // ... 其餘不變
}
```

- [ ] **Step 5: 實作 tab 渲染和切換**

```javascript
function renderTabs() {
  const el = document.getElementById('tabs');
  el.innerHTML = openTabs.map(tab => {
    const isActive = tab.id === activeProjectId;
    return `<div class="flex items-center gap-1 px-3 py-1.5 text-xs cursor-pointer rounded-t transition-colors ${isActive ? 'bg-zinc-800 text-zinc-200 border-b-2' : 'text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/30'}" style="${isActive ? 'border-color:var(--accent)' : ''}" onclick="switchTab('${tab.id}')">
      <span>${esc(tab.name)}</span>
      <span class="ml-1 text-zinc-600 hover:text-zinc-400" onclick="event.stopPropagation();closeTab('${tab.id}')">&times;</span>
    </div>`;
  }).join('');
}

function switchTab(projectId) {
  activeProjectId = projectId;
  current = null;
  lastMsgCount = 0;
  disconnectSSE();
  saveTabState();
  renderTabs();
  goHome();
}

function closeTab(projectId) {
  openTabs = openTabs.filter(t => t.id !== projectId);
  if (activeProjectId === projectId) {
    activeProjectId = openTabs.length > 0 ? openTabs[0].id : null;
    current = null;
    disconnectSSE();
  }
  saveTabState();
  renderTabs();
  if (activeProjectId) load();
  else renderProjectPicker();
}

function addTab(project) {
  if (!openTabs.find(t => t.id === project.id)) {
    openTabs.push({ id: project.id, name: project.name });
  }
  activeProjectId = project.id;
  saveTabState();
  renderTabs();
  load();
}
```

- [ ] **Step 6: 實作專案選擇器**

```javascript
async function loadProjects() {
  projects = await (await fetch('/api/projects')).json() || [];
}

function showProjectPicker() {
  document.getElementById('picker-modal').classList.add('open');
  document.getElementById('path-input').value = '';
  document.getElementById('path-error').style.display = 'none';
  renderRecentList();
  document.getElementById('path-input').focus();
}

function closeProjectPicker() {
  document.getElementById('picker-modal').classList.remove('open');
}

function renderRecentList() {
  const el = document.getElementById('recent-list');
  const unopened = projects.filter(p => !openTabs.find(t => t.id === p.id));
  if (unopened.length === 0) {
    el.innerHTML = '<div style="padding:24px;text-align:center;color:var(--text-4);font-size:13px">No recent projects</div>';
    return;
  }
  el.innerHTML = unopened.map(p => `
    <div class="search-item" onclick="openExistingProject('${p.id}')">
      <span style="color:var(--text-3)">📁</span>
      <div style="flex:1;min-width:0">
        <div style="font-size:13px;font-weight:600;color:var(--text-1)">${esc(p.name)}</div>
        <div style="font-size:11px;color:var(--text-4);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(p.path)}</div>
      </div>
      <span style="font-size:11px;color:var(--text-4)">${p.taskCount} tasks</span>
    </div>`).join('');
}

function openExistingProject(id) {
  const p = projects.find(x => x.id === id);
  if (p) { closeProjectPicker(); addTab(p); }
}

async function addProjectFromInput() {
  const input = document.getElementById('path-input');
  const errorEl = document.getElementById('path-error');
  const path = input.value.trim();
  if (!path) return;

  try {
    const resp = await fetch('/api/projects', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path })
    });
    if (!resp.ok) {
      const msg = await resp.text();
      errorEl.textContent = msg;
      errorEl.style.display = 'block';
      return;
    }
    const data = await resp.json();
    await loadProjects();
    const p = projects.find(x => x.id === data.id);
    if (p) { closeProjectPicker(); addTab(p); }
  } catch (e) {
    errorEl.textContent = 'Connection error';
    errorEl.style.display = 'block';
  }
}

function renderProjectPicker() {
  document.getElementById('dashboard').innerHTML = `
    <div style="display:flex;align-items:center;justify-content:center;min-height:60vh;flex-direction:column;gap:16px">
      <div style="font-size:24px;font-weight:700;color:var(--text-1)">4x Live</div>
      <div style="font-size:14px;color:var(--text-3)">Select a project to get started</div>
      <button onclick="showProjectPicker()" style="margin-top:8px;padding:10px 24px;background:var(--accent);color:#000;border:none;border-radius:8px;font-size:14px;font-weight:600;cursor:pointer">Open Project...</button>
    </div>`;
  document.getElementById('dashboard').classList.remove('hidden');
  document.getElementById('header').classList.add('hidden');
  document.getElementById('messages').classList.add('hidden');
}
```

- [ ] **Step 7: 修改跨專案 Cmd+K 搜尋**

修改 `renderSearchResults`，搜尋所有已開啟 tab 的 features：

```javascript
let allTabTasks = {}; // projectId → tasks[]

async function loadAllTabTasks() {
  for (const tab of openTabs) {
    try {
      const tasks = await (await fetch('/api/project/' + tab.id + '/api/tasks')).json();
      allTabTasks[tab.id] = (tasks || []).map(t => ({ ...t, _projectId: tab.id, _projectName: tab.name }));
    } catch { allTabTasks[tab.id] = []; }
  }
}

function renderSearchResults(query) {
  const el = document.getElementById('search-results');
  let pool = [];

  // 跨專案搜尋
  let scopeId = null;
  let actualQuery = query;
  const scopeMatch = query.match(/^@(\S+)\s*(.*)/);
  if (scopeMatch) {
    const scopeName = scopeMatch[1];
    actualQuery = scopeMatch[2];
    const tab = openTabs.find(t => t.name.toLowerCase().includes(scopeName.toLowerCase()) || t.id.toLowerCase().includes(scopeName.toLowerCase()));
    if (tab) scopeId = tab.id;
  }

  for (const tab of openTabs) {
    if (scopeId && tab.id !== scopeId) continue;
    const tasks = allTabTasks[tab.id] || [];
    pool.push(...tasks);
  }

  searchFiltered = actualQuery ? pool.filter(t => fuzzyMatch(actualQuery, t.id + ' ' + t.name)) : pool;
  if (searchIdx >= searchFiltered.length) searchIdx = Math.max(0, searchFiltered.length - 1);

  el.innerHTML = searchFiltered.map((t, i) => {
    const isActive = t.active && t.phase && t.phase !== 'done';
    const projectLabel = openTabs.length > 1 ? `<span style="padding:1px 6px;font-size:10px;background:var(--bg-hover);border-radius:4px;color:var(--text-3)">${esc(t._projectName)}</span>` : '';
    let badgeHtml;
    if (isActive) badgeHtml = '<span style="padding:2px 8px;font-size:10px;font-weight:600;background:rgba(16,185,129,.15);color:#34d399;border:1px solid rgba(16,185,129,.3);border-radius:99px">In Progress</span>';
    else if (t.status === 'done') badgeHtml = '<span style="padding:2px 8px;font-size:10px;color:var(--text-3);border:1px solid var(--border);border-radius:99px">Done</span>';
    else if (t.status === 'blocked') badgeHtml = '<span style="padding:2px 8px;font-size:10px;color:#f87171;border:1px solid rgba(248,113,113,.3);border-radius:99px">Blocked</span>';
    else badgeHtml = '<span style="padding:2px 8px;font-size:10px;color:var(--text-4);border:1px solid var(--border);border-radius:99px">Not Started</span>';

    return `<div class="search-item ${i === searchIdx ? 'active' : ''}" onclick="selectSearch(${i})" onmouseenter="searchIdx=${i};renderSearchResults(document.getElementById('search-input').value)">
      ${projectLabel}
      <span style="font-size:13px;font-weight:600;color:${isActive ? '#34d399' : 'var(--accent)'};min-width:80px">${esc(t.id)}</span>
      <span style="flex:1;font-size:13px;color:var(--text-2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(t.name)}</span>
      ${badgeHtml}
    </div>`;
  }).join('');
}
```

修改 `selectSearch`，跨專案跳轉：

```javascript
function selectSearch(idx) {
  const t = searchFiltered[idx];
  if (!t) return;
  closeSearch();
  if (t._projectId && t._projectId !== activeProjectId) {
    switchTab(t._projectId);
  }
  current = t.id;
  load();
  loadDetail(t);
}
```

修改 `openSearch`，開啟時先載入所有 tab 的 tasks：

```javascript
async function openSearch() {
  document.getElementById('search-modal').classList.add('open');
  const inp = document.getElementById('search-input');
  inp.value = '';
  inp.focus();
  searchIdx = 0;
  await loadAllTabTasks();
  renderSearchResults('');
}
```

- [ ] **Step 8: 修改初始化流程**

修改底部的初始化邏輯（替換原本的 `load(); initSettings();`）：

```javascript
async function init() {
  initSettings();
  await loadProjects();

  const saved = loadTabState();
  if (saved.tabs.length > 0) {
    for (const tab of saved.tabs) {
      if (projects.find(p => p.id === tab.id)) {
        openTabs.push(tab);
      }
    }
    activeProjectId = openTabs.find(t => t.id === saved.active) ? saved.active : (openTabs[0]?.id || null);
  }

  if (openTabs.length === 0 && projects.length > 0) {
    // CLI 帶引數啟動，自動開啟所有已載入專案
    projects.forEach(p => openTabs.push({ id: p.id, name: p.name }));
    activeProjectId = openTabs[0]?.id || null;
  }

  renderTabs();

  if (activeProjectId) {
    load();
  } else {
    renderProjectPicker();
  }
}
init();
```

- [ ] **Step 9: 確認編譯通過**

Run: `cd /Users/white/github/4x && go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(dashboard): add tab bar, project picker, and cross-project search"
```

---

### Task 5: macOS Native App 改進

**Files:**
- Modify: `dashboard/macos/Sources/main.swift`

- [ ] **Step 1: 改寫 main.swift — 加入 NSOpenPanel、poll server、標題同步**

```swift
import AppKit
import WebKit

class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKScriptMessageHandler {
    var window: NSWindow!
    var webView: WKWebView!
    var serverPort: Int = 4567

    func applicationDidFinishLaunching(_ notification: Notification) {
        parseArgs()

        let config = WKWebViewConfiguration()
        let userContent = config.userContentController
        userContent.add(self, name: "nativeOpenFolder")
        webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = self

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1200, height: 800),
            styleMask: [.titled, .closable, .resizable, .miniaturizable],
            backing: .buffered,
            defer: false
        )
        window.title = "4x Live"
        window.contentView = webView
        window.center()
        window.setFrameAutosaveName("4xLiveWindow")
        window.makeKeyAndOrderFront(nil)

        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)

        pollServerAndLoad()
    }

    func parseArgs() {
        let args = CommandLine.arguments
        for (i, arg) in args.enumerated() {
            if arg.starts(with: "--port="), let p = Int(arg.replacingOccurrences(of: "--port=", with: "")) {
                serverPort = p
            } else if arg == "--port", i + 1 < args.count, let p = Int(args[i + 1]) {
                serverPort = p
            }
        }
    }

    func pollServerAndLoad() {
        let url = URL(string: "http://localhost:\(serverPort)/api/projects")!
        let task = URLSession.shared.dataTask(with: url) { [weak self] data, response, error in
            guard let self = self else { return }
            if let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 {
                DispatchQueue.main.async {
                    let pageURL = URL(string: "http://localhost:\(self.serverPort)")!
                    self.webView.load(URLRequest(url: pageURL))
                }
            } else {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                    self.pollServerAndLoad()
                }
            }
        }
        task.resume()
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        if message.name == "nativeOpenFolder" {
            let panel = NSOpenPanel()
            panel.canChooseDirectories = true
            panel.canChooseFiles = false
            panel.allowsMultipleSelection = false
            panel.message = "Select a 4x project folder"

            if panel.runModal() == .OK, let url = panel.url {
                let path = url.path
                let js = "addProjectFromNative('\(path.replacingOccurrences(of: "'", with: "\\'"))')"
                webView.evaluateJavaScript(js, completionHandler: nil)
            }
        }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        injectNativeBridge()
        startTitleSync()
    }

    func injectNativeBridge() {
        let js = """
        window._isNativeApp = true;
        async function addProjectFromNative(path) {
            const resp = await fetch('/api/projects', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({path: path})
            });
            if (resp.ok) {
                await loadProjects();
                const data = await resp.json();
                const p = projects.find(x => x.id === data.id);
                if (p) { closeProjectPicker(); addTab(p); }
            }
        }
        // 替換 path input 為原生 folder picker
        const origShowPicker = window.showProjectPicker;
        """
        webView.evaluateJavaScript(js, completionHandler: nil)
    }

    func startTitleSync() {
        Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            self?.webView.evaluateJavaScript("activeProjectId ? openTabs.find(t=>t.id===activeProjectId)?.name || '4x Live' : '4x Live'") { result, _ in
                if let name = result as? String {
                    DispatchQueue.main.async {
                        self?.window.title = name == "4x Live" ? name : "\(name) — 4x Live"
                    }
                }
            }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
```

- [ ] **Step 2: 確認 Swift 編譯**

Run: `cd /Users/white/github/4x/dashboard/macos && swiftc Sources/main.swift -framework AppKit -framework WebKit -o 4xLive 2>&1 | head -20`
Expected: 編譯成功（若機器沒裝 Swift toolchain 則跳過）

- [ ] **Step 3: Commit**

```bash
git add dashboard/macos/Sources/main.swift
git commit -m "feat(macos): add NSOpenPanel, server polling, and title sync"
```

---

### Task 6: 前端 — 專案選擇器中呼叫 Native Folder Picker

**Files:**
- Modify: `internal/server/static/index.html`

- [ ] **Step 1: 在專案選擇器的「Open」按鈕旁加入原生資料夾選擇支援**

在 `showProjectPicker()` 裡，偵測 `window._isNativeApp` 時替換按鈕行為：

在 path-input 的 `<div>` 區塊後，加入：

```javascript
// 在 showProjectPicker() 末尾
if (window._isNativeApp && window.webkit?.messageHandlers?.nativeOpenFolder) {
  const btn = document.querySelector('#picker-modal button[onclick="addProjectFromInput()"]');
  if (btn) {
    btn.textContent = 'Browse...';
    btn.onclick = () => window.webkit.messageHandlers.nativeOpenFolder.postMessage('open');
  }
}
```

- [ ] **Step 2: 確認編譯通過**

Run: `cd /Users/white/github/4x && go build ./cmd/4x`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/server/static/index.html
git commit -m "feat(dashboard): integrate native folder picker for macOS app"
```

---

### Task 7: 向後相容路由 + 最終整合測試

**Files:**
- Modify: `internal/server/multi.go`
- Modify: `internal/server/multi_test.go`

- [ ] **Step 1: 寫向後相容路由的 failing test**

在 `multi_test.go` 加入：

```go
func TestMultiMux_BackwardCompat_SingleProject(t *testing.T) {
	ws := setupMultiWorkspace(t, "only-one")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	srv := httptest.NewServer(NewMultiMux(reg, recentPath))
	defer srv.Close()

	// 無 prefix 的舊路由也應該能用（向後相容）
	resp, err := http.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var tasks []taskInfo
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
}
```

- [ ] **Step 2: 跑測試確認 fail**

Run: `cd /Users/white/github/4x && go test ./internal/server/ -run "TestMultiMux_BackwardCompat" -v`
Expected: FAIL — `/api/tasks` 回 404（目前 `NewMultiMux` 沒有掛無 prefix 路由）

- [ ] **Step 3: 在 NewMultiMux 加入向後相容路由**

在 `NewMultiMux` 的 prefix routing handler 之前加入：

```go
	// 向後相容：無 prefix 的 /api/tasks、/api/messages/、/api/events/、/sse/events/
	// 當只有一個 workspace 時，直接轉給它；多個時回 404 指引用 prefix
	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			handleTasks(ws, w)
			return
		}
		http.Error(w, "multiple projects loaded — use /api/project/{id}/tasks", http.StatusBadRequest)
	})
	mux.HandleFunc("/api/messages/", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			featureID := strings.TrimPrefix(r.URL.Path, "/api/messages/")
			handleMessages(ws, featureID, w)
			return
		}
		http.Error(w, "multiple projects loaded — use /api/project/{id}/messages/{featureId}", http.StatusBadRequest)
	})
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			featureID := strings.TrimPrefix(r.URL.Path, "/api/events/")
			handleEvents(ws, featureID, w)
			return
		}
		http.Error(w, "multiple projects loaded — use /api/project/{id}/events/{featureId}", http.StatusBadRequest)
	})
	mux.HandleFunc("/sse/events/", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			featureID := strings.TrimPrefix(r.URL.Path, "/sse/events/")
			handleSSE(ws, featureID, w, r)
			return
		}
		http.Error(w, "multiple projects loaded — use /sse/project/{id}/events/{featureId}", http.StatusBadRequest)
	})
```

- [ ] **Step 4: 跑測試確認 pass**

Run: `cd /Users/white/github/4x && go test ./internal/server/ -run "TestMultiMux_BackwardCompat" -v`
Expected: PASS

- [ ] **Step 5: 跑全部測試**

Run: `cd /Users/white/github/4x && go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server/multi.go internal/server/multi_test.go
git commit -m "feat(server): add backward-compatible unprefixed routes for single project"
```

---

### Task 8: 清理 + 最終驗證

**Files:**
- Modify: `internal/server/server.go` (移除 `_oldIndexHTML` 常數)
- Verify: all tests pass

- [ ] **Step 1: 移除 server.go 中的 `_oldIndexHTML` 死碼**

刪除 `server.go` 第 250-335 行的 `const _oldIndexHTML = ...` 區塊。

- [ ] **Step 2: 完整驗證**

Run: `cd /Users/white/github/4x && go build ./cmd/4x && go vet ./... && go test ./...`
Expected: ALL PASS

- [ ] **Step 3: 手動冒煙測試**

```bash
# 在任何含 .4x/ 的目錄跑
./bin/4x live --web --port 4580
# 預期：瀏覽器開啟、顯示 tab bar（可能有或沒有專案）
# 點 + 新增專案、輸入路徑、確認 tab 出現
```

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go
git commit -m "chore: remove dead _oldIndexHTML constant"
```
