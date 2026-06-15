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

	web "github.com/ggwhite/4x/dashboard/web"
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
	id  string
	ws  *protocol.Workspace
	mux http.Handler
	pm  *ProcessManager
	bm  *BatchManager
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
	pm := newProcessManagerFromConfig(ws)
	bm := NewBatchManager(ws, selfBinary())
	bm.Adopt() // server 重啟後 re-attach 既有 batch run 孤兒
	r.ids[id] = true
	r.entries = append(r.entries, &registryEntry{id: id, ws: ws, mux: newMux(ws, pm, bm), pm: pm, bm: bm})
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
			if home, _ := os.UserHomeDir(); absPath == home {
				http.Error(w, "cannot use home directory as a project", http.StatusBadRequest)
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

	// 向後相容：無 prefix 路由（單一 workspace 時轉給它）
	compatError := func(w http.ResponseWriter, n int, prefixHint string) {
		if n == 0 {
			http.Error(w, "no projects loaded — add a project first", http.StatusBadRequest)
		} else {
			http.Error(w, "multiple projects loaded — use "+prefixHint, http.StatusBadRequest)
		}
	}
	compatGetWs := func(w http.ResponseWriter, hint string) *protocol.Workspace {
		n := reg.Count()
		if n != 1 {
			compatError(w, n, hint)
			return nil
		}
		entries := reg.List()
		if len(entries) != 1 {
			compatError(w, len(entries), hint)
			return nil
		}
		ws := reg.Get(entries[0].ID)
		if ws == nil {
			http.Error(w, "project unavailable", http.StatusServiceUnavailable)
		}
		return ws
	}
	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		if ws := compatGetWs(w, "/api/project/{id}/api/tasks"); ws != nil {
			handleTasks(ws, w)
		}
	})
	mux.HandleFunc("/api/messages/", func(w http.ResponseWriter, r *http.Request) {
		if ws := compatGetWs(w, "/api/project/{id}/api/messages/{featureId}"); ws != nil {
			featureID := strings.TrimPrefix(r.URL.Path, "/api/messages/")
			if !validFeatureID(featureID) {
				http.Error(w, "invalid feature id", http.StatusBadRequest)
				return
			}
			handleMessages(ws, featureID, w)
		}
	})
	mux.HandleFunc("/api/overview/", func(w http.ResponseWriter, r *http.Request) {
		if ws := compatGetWs(w, "/api/project/{id}/api/overview/{featureId}"); ws != nil {
			featureID := strings.TrimPrefix(r.URL.Path, "/api/overview/")
			if !validFeatureID(featureID) {
				http.Error(w, "invalid feature id", http.StatusBadRequest)
				return
			}
			handleOverview(ws, featureID, w)
		}
	})
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		if ws := compatGetWs(w, "/api/project/{id}/api/events/{featureId}"); ws != nil {
			featureID := strings.TrimPrefix(r.URL.Path, "/api/events/")
			if !validFeatureID(featureID) {
				http.Error(w, "invalid feature id", http.StatusBadRequest)
				return
			}
			handleEvents(ws, featureID, w)
		}
	})
	mux.HandleFunc("/sse/events/", func(w http.ResponseWriter, r *http.Request) {
		if ws := compatGetWs(w, "/sse/project/{id}/events/{featureId}"); ws != nil {
			featureID := strings.TrimPrefix(r.URL.Path, "/sse/events/")
			if !validFeatureID(featureID) {
				http.Error(w, "invalid feature id", http.StatusBadRequest)
				return
			}
			handleSSE(ws, featureID, w, r)
		}
	})
	mux.HandleFunc("/api/logs/", func(w http.ResponseWriter, r *http.Request) {
		if ws := compatGetWs(w, "/api/project/{id}/api/logs/{featureId}"); ws != nil {
			rest := strings.TrimPrefix(r.URL.Path, "/api/logs/")
			parts := strings.SplitN(rest, "/", 2)
			if !validFeatureID(parts[0]) {
				http.Error(w, "invalid feature id", http.StatusBadRequest)
				return
			}
			handleLogs(ws, rest, w)
		}
	})
	mux.HandleFunc("/api/features/", func(w http.ResponseWriter, r *http.Request) {
		if ws := compatGetWs(w, "/api/project/{id}/api/features/{featureId}/screenshots"); ws != nil {
			handleFeatureScreenshots(ws, w, r)
		}
	})
	mux.HandleFunc("/sse/logs/", func(w http.ResponseWriter, r *http.Request) {
		if ws := compatGetWs(w, "/sse/project/{id}/logs/{featureId}"); ws != nil {
			featureID := strings.TrimPrefix(r.URL.Path, "/sse/logs/")
			if !validFeatureID(featureID) {
				http.Error(w, "invalid feature id", http.StatusBadRequest)
				return
			}
			handleLogSSE(ws, featureID, w, r)
		}
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			entry := reg.getEntry(entries[0].ID)
			if entry != nil {
				entry.mux.ServeHTTP(w, r)
				return
			}
		}
		compatError(w, len(entries), "/api/project/{id}/api/settings")
	})
	mux.HandleFunc("/api/user-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetUserConfig(w)
			return
		}
		if r.Method == http.MethodPut {
			handlePutUserConfig(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/api/merged-config", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			entry := reg.getEntry(entries[0].ID)
			if entry != nil {
				entry.mux.ServeHTTP(w, r)
				return
			}
		}
		compatError(w, len(entries), "/api/project/{id}/api/merged-config")
	})
	mux.HandleFunc("/api/supported-runners", func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(protocol.SupportedRunners())
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
	for _, route := range []string{"/api/run", "/api/runs", "/api/stop", "/api/new"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			entries := reg.List()
			if len(entries) == 1 {
				entry := reg.getEntry(entries[0].ID)
				if entry != nil {
					entry.mux.ServeHTTP(w, r)
					return
				}
			}
			compatError(w, len(entries), "/api/project/{id}"+r.URL.Path)
		})
	}

	// Prefix routing: /api/project/{id}/...
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
		subPath := rest[idx:]
		r.URL.Path = subPath
		entry.mux.ServeHTTP(w, r)
	})

	// SSE prefix routing: /sse/project/{id}/events/{featureId} and /sse/project/{id}/logs/{featureId}
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
		sub := rest[idx:]
		if strings.HasPrefix(sub, "/logs/") {
			featureID := strings.TrimPrefix(sub, "/logs/")
			if !validFeatureID(featureID) {
				http.Error(w, "invalid feature id", http.StatusBadRequest)
				return
			}
			handleLogSSE(ws, featureID, w, r)
		} else {
			featureID := strings.TrimPrefix(sub, "/events/")
			if !validFeatureID(featureID) {
				http.Error(w, "invalid feature id", http.StatusBadRequest)
				return
			}
			handleSSE(ws, featureID, w, r)
		}
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
		dir = absDir
		if !strings.HasPrefix(dir, home) {
			http.Error(w, "path must be under home directory", http.StatusForbidden)
			return
		}

		entries, err := os.ReadDir(dir)
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
			full := filepath.Join(dir, e.Name())
			dirs = append(dirs, dirEntry{Name: e.Name(), Path: full, Is4x: is4xProject(full)})
		}

		currentIs4x := is4xProject(dir)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"current": dir,
			"is4x":    currentIs4x,
			"dirs":    dirs,
		})
	})

	mux.HandleFunc("/api/locales/", func(w http.ResponseWriter, r *http.Request) {
		handleGetLocale(w, r)
	})
	mux.HandleFunc("/api/locales", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/locales" {
			handleGetLocale(w, r)
			return
		}
		handleGetLocales(w)
	})

	// Static files (index.html + JS/CSS)，共用 dashboard/web 前端資產
	mux.Handle("/", http.FileServer(http.FS(web.Assets)))

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
