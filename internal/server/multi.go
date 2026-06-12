package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	id  string
	ws  *protocol.Workspace
	mux http.Handler
	pm  *ProcessManager
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

// Add 註冊一個 workspace，回傳分配的 project ID（base name，重名時加數字後綴）
func (r *ProjectRegistry) Add(ws *protocol.Workspace) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	base := filepath.Base(ws.Root)
	id := base
	for i := 2; r.ids[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	pm := newProcessManagerFromConfig(ws)
	r.ids[id] = true
	r.entries = append(r.entries, registryEntry{id: id, ws: ws, mux: NewMux(ws, pm), pm: pm})
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

// getEntry 取得指定 ID 的 registry entry（含快取的 mux）
func (r *ProjectRegistry) getEntry(id string) *registryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i := range r.entries {
		if r.entries[i].id == id {
			return &r.entries[i]
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
	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			handleTasks(ws, w)
			return
		}
		compatError(w, len(entries), "/api/project/{id}/tasks")
	})
	mux.HandleFunc("/api/messages/", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			featureID := strings.TrimPrefix(r.URL.Path, "/api/messages/")
			handleMessages(ws, featureID, w)
			return
		}
		compatError(w, len(entries), "/api/project/{id}/messages/{featureId}")
	})
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			featureID := strings.TrimPrefix(r.URL.Path, "/api/events/")
			handleEvents(ws, featureID, w)
			return
		}
		compatError(w, len(entries), "/api/project/{id}/events/{featureId}")
	})
	mux.HandleFunc("/sse/events/", func(w http.ResponseWriter, r *http.Request) {
		entries := reg.List()
		if len(entries) == 1 {
			ws := reg.Get(entries[0].ID)
			featureID := strings.TrimPrefix(r.URL.Path, "/sse/events/")
			handleSSE(ws, featureID, w, r)
			return
		}
		compatError(w, len(entries), "/sse/project/{id}/events/{featureId}")
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

	// SSE prefix routing: /sse/project/{id}/events/{featureId}
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

	// Browse API：列出指定路徑的子目錄，供前端 folder picker 使用
	mux.HandleFunc("/api/browse", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("path")
		if dir == "" {
			home, _ := os.UserHomeDir()
			dir = home
		}
		if strings.HasPrefix(dir, "~") {
			home, _ := os.UserHomeDir()
			dir = home + dir[1:]
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
			is4x := false
			if info, err := os.Stat(filepath.Join(full, ".4x")); err == nil && info.IsDir() {
				is4x = true
			}
			dirs = append(dirs, dirEntry{Name: e.Name(), Path: full, Is4x: is4x})
		}

		currentIs4x := false
		if info, err := os.Stat(filepath.Join(dir, ".4x")); err == nil && info.IsDir() {
			currentIs4x = true
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"current": dir,
			"is4x":    currentIs4x,
			"dirs":    dirs,
		})
	})

	// Index HTML
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, indexHTML)
	})

	return mux
}

func newProcessManagerFromConfig(ws *protocol.Workspace) *ProcessManager {
	cfg, err := ws.ReadConfig()
	if err != nil {
		return NewProcessManager(ws, 1, "4x")
	}
	return NewProcessManager(ws, cfg.MaxConcurrentRuns, "4x")
}
