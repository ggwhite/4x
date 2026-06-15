package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ggwhite/4x/internal/logging"
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
	ws *protocol.CachedWorkspace
	pm *ProcessManager
	bm *BatchManager
}

// ProjectRegistry 管理多個 workspace 的 in-memory 註冊表
type ProjectRegistry struct {
	mu      sync.RWMutex
	entries []*registryEntry
	ids     map[string]bool
}

// NewProjectRegistry 建立空的 registry
func NewProjectRegistry() *ProjectRegistry {
	return &ProjectRegistry{ids: make(map[string]bool)}
}

// Add 註冊一個 workspace，回傳分配的 project ID（base name，重名時加數字後綴）
func (r *ProjectRegistry) Add(ws *protocol.Workspace) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	base := filepath.Base(ws.Root)
	if base == "." || base == "/" {
		if abs, err := filepath.Abs(ws.Root); err == nil {
			base = filepath.Base(abs)
		}
	}
	id := base
	for i := 2; r.ids[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	cws := protocol.NewCachedWorkspace(ws)
	pm := newProcessManagerFromConfig(cws.Workspace)
	bm := NewBatchManager(cws.Workspace, selfBinary())
	bm.Adopt()
	r.ids[id] = true
	r.entries = append(r.entries, &registryEntry{id: id, ws: cws, pm: pm, bm: bm})
	slog.Info("project added", "id", id, "path", ws.Root)
	return id
}

// Remove 從 registry 移除指定 ID
func (r *ProjectRegistry) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range r.entries {
		if e.id == id {
			if e.pm != nil {
				e.pm.Shutdown()
			}
			if e.bm != nil && e.bm.Running() {
				if err := e.bm.Stop(); err != nil {
					fmt.Fprintf(os.Stderr, "warn: failed to stop batch for project %s: %v\n", e.id, err)
				}
			}
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			delete(r.ids, id)
			slog.Info("project removed", "id", id)
			return true
		}
	}
	return false
}

// ShutdownAll 終止所有專案的 subprocess
func (r *ProjectRegistry) ShutdownAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.pm != nil {
			e.pm.Shutdown()
		}
		if e.bm != nil && e.bm.Running() {
			if err := e.bm.Shutdown(); err != nil {
				fmt.Fprintf(os.Stderr, "warn: failed to shut down batch for project %s: %v\n", e.id, err)
			}
		}
	}
}

// Get 取得指定 ID 的 workspace
func (r *ProjectRegistry) Get(id string) *protocol.CachedWorkspace {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.id == id {
			return e.ws
		}
	}
	return nil
}

// getEntry 取得指定 ID 的 registry entry（指標穩定，不受 slice 重分配影響）
func (r *ProjectRegistry) getEntry(id string) *registryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.id == id {
			return e
		}
	}
	return nil
}

// Count 回傳已註冊專案數量，不做 disk I/O
func (r *ProjectRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
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

	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(reg.List())

		case http.MethodPost:
			var body struct {
				Path string `json:"path"`
				Init bool   `json:"init"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			absPath, _ := filepath.Abs(body.Path)
			home, _ := os.UserHomeDir()
			if absPath == home {
				http.Error(w, "cannot use home directory as a project", http.StatusBadRequest)
				return
			}
			if !strings.HasPrefix(absPath, home+string(os.PathSeparator)) {
				http.Error(w, "path must be under home directory", http.StatusForbidden)
				return
			}
			ws, err := protocol.Find(body.Path)
			if err != nil || ws.Root != absPath {
				if !body.Init {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					json.NewEncoder(w).Encode(map[string]string{
						"error": "not_4x_project",
						"path":  absPath,
					})
					return
				}
				if initErr := protocol.Init(absPath, protocol.Config{
					Project: protocol.ProjectConfig{Name: filepath.Base(absPath)},
				}); initErr != nil {
					http.Error(w, "failed to init 4x project: "+initErr.Error(), http.StatusInternalServerError)
					return
				}
				ws = &protocol.Workspace{Root: absPath}
			}
			id := reg.Add(ws)

			rp, _ := LoadRecentProjects(recentPath)
			rp.Touch(ws.Root)
			_ = SaveRecentProjects(recentPath, rp)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": id})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/projects/" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(reg.List())
			return
		}
		if r.Method != http.MethodDelete {
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

	// 內層統一路由：所有 leaf 端點（/api/tasks、/sse/* 等）只在 NewMux 定義一次，
	// 由 multiResolver 依 context（prefix dispatch 注入）或 compat 邏輯解析專案。
	inner := NewMux(multiResolver(reg))

	// Prefix routing: /api/project/{id}/... — strip prefix 後注入 entry 到 context 再轉內層。
	mux.HandleFunc("/api/project/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/project/")
		idx := strings.Index(rest, "/")
		if idx < 0 {
			http.Error(w, "missing path after project id", http.StatusBadRequest)
			return
		}
		id := rest[:idx]
		entry := reg.getEntry(id)
		if entry == nil {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		r.URL.Path = rest[idx:]
		inner.ServeHTTP(w, r.WithContext(withResolvedProject(r.Context(), entry)))
	})

	// SSE prefix routing: /sse/project/{id}/events/{featureId} 與 /sse/project/{id}/logs/{featureId}
	// path 重寫為 /sse/events/{f} 或 /sse/logs/{f} 後轉內層。
	mux.HandleFunc("/sse/project/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/sse/project/")
		idx := strings.Index(rest, "/")
		if idx < 0 {
			http.Error(w, "missing path after project id", http.StatusBadRequest)
			return
		}
		id := rest[:idx]
		entry := reg.getEntry(id)
		if entry == nil {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		r.URL.Path = "/sse" + rest[idx:]
		inner.ServeHTTP(w, r.WithContext(withResolvedProject(r.Context(), entry)))
	})

	// Browse API：列出指定路徑的子目錄，供前端 folder picker 使用（限制在 home 目錄下）
	mux.HandleFunc("/api/browse", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("path")
		home, _ := os.UserHomeDir()
		if dir == "" || dir == "~" {
			if gh := filepath.Join(home, "github"); dirExists(gh) {
				dir = gh
			} else {
				dir = home
			}
		} else if strings.HasPrefix(dir, "~") {
			dir = home + dir[1:]
		}

		absDir, err := filepath.Abs(dir)
		if err != nil {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		resolved, err := filepath.EvalSymlinks(absDir)
		if err != nil {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		if !strings.HasPrefix(resolved, home) {
			http.Error(w, "path must be under home directory", http.StatusForbidden)
			return
		}

		entries, err := os.ReadDir(resolved)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		type dirEntry struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Is4x bool   `json:"is4x"`
		}
		var dirs []dirEntry
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			full := filepath.Join(resolved, e.Name())
			dirs = append(dirs, dirEntry{Name: e.Name(), Path: full, Is4x: is4xProject(full)})
		}

		currentIs4x := is4xProject(resolved)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"current": resolved,
			"is4x":    currentIs4x,
			"dirs":    dirs,
		})
	})

	// catch-all：locales、static 資產與所有 compat（無 prefix）leaf 路由皆由內層統一處理。
	mux.Handle("/", inner)

	return logging.Middleware(recoverMiddleware(mux))
}

// recoverMiddleware 攔截 handler panic，回傳 500 而非 crash 整個 server
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				slog.Error("panic recovered", "method", r.Method, "path", r.URL.Path, "error", rv)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// is4xProject 判斷目錄是否為 4x 專案（需有 .4x/features/）
func is4xProject(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".4x", "features"))
	return err == nil && info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func newProcessManagerFromConfig(ws *protocol.Workspace) *ProcessManager {
	bin := selfBinary()
	cfg, err := ws.ReadConfig()
	if err != nil {
		return NewProcessManager(ws, 1, bin)
	}
	return NewProcessManager(ws, cfg.MaxConcurrentRuns, bin)
}

func selfBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return "4x"
	}
	return exe
}
