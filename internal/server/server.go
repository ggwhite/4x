package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	web "github.com/ggwhite/4x/dashboard/web"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/logging"
	"github.com/ggwhite/4x/internal/protocol"
)

// settingsMu / mergeMu 為 per-project 鎖：以專案 root 為 key 取得對應 mutex，
// 使同一專案的 settings 寫入 / merge 仍序列化，不同專案則互不阻塞。
var settingsMu keyedMutex
var mergeMu keyedMutex

// keyedMutex 提供以字串 key 取得對應 *sync.Mutex 的並行安全機制。
// 用於將原本的全域鎖改為 per-project 鎖：相同 key（專案 root）序列化，
// 不同 key 互不阻塞；取鎖過程本身由 sync.Map 保證 thread-safe。
type keyedMutex struct {
	mutexes sync.Map // map[string]*sync.Mutex
}

// get 回傳 key 對應的 *sync.Mutex，key 首次出現時建立新鎖。
// 呼叫者取得後自行 Lock/Unlock。
func (k *keyedMutex) get(key string) *sync.Mutex {
	m, _ := k.mutexes.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex)
}

var supportedLocales = []string{"en", "zh-TW", "zh-CN", "ja", "ko", "es"}

// wsRoute 註冊只需 workspace 的單一 HTTP method 路由。
// 自動處理 method 檢查與 workspace 解析。
func wsRoute(mux *http.ServeMux, method, pattern string, resolver WorkspaceResolver, handler func(*protocol.CachedWorkspace, http.ResponseWriter, *http.Request)) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		handler(ws, w, r)
	})
}

// pmRoute 註冊需要 workspace + ProcessManager 的單一 HTTP method 路由。
// pm 為 nil 時回傳 503。
func pmRoute(mux *http.ServeMux, method, pattern string, resolver WorkspaceResolver, handler func(*protocol.CachedWorkspace, *ProcessManager, http.ResponseWriter, *http.Request)) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ws, pm, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if pm == nil {
			http.Error(w, strings.TrimPrefix(pattern, "/api/")+" not available", http.StatusServiceUnavailable)
			return
		}
		handler(ws, pm, w, r)
	})
}

// featureRoute 註冊以 URL 尾段作為 feature ID 的 GET 路由，自動驗證 ID 格式。
func featureRoute(mux *http.ServeMux, pattern string, resolver WorkspaceResolver, handler func(*protocol.CachedWorkspace, string, http.ResponseWriter, *http.Request)) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		featureID := strings.TrimPrefix(r.URL.Path, pattern)
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handler(ws, featureID, w, r)
	})
}

// NewMux 建立 dashboard 的 HTTP handler。每個需要資料的 handler 透過傳入的 resolver
// 解析該請求對應的 workspace/process manager/batch manager，使路由表只定義一次，
// 由 singleResolver（單一專案）或 multiResolver（多專案）共用。
func NewMux(resolver WorkspaceResolver) http.Handler {
	mux := http.NewServeMux()

	// Feature CRUD
	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		handleTasks(ws, w)
	})
	pmRoute(mux, http.MethodPost, "/api/new", resolver, func(ws *protocol.CachedWorkspace, _ *ProcessManager, w http.ResponseWriter, r *http.Request) {
		handlePostNew(ws, w, r)
	})
	wsRoute(mux, http.MethodPost, "/api/done", resolver, func(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
		handlePostDone(ws, w, r)
	})
	wsRoute(mux, http.MethodPost, "/api/clean", resolver, func(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
		handlePostClean(ws.Workspace, w)
	})

	// Run operations
	pmRoute(mux, http.MethodPost, "/api/run", resolver, handlePostRun)
	wsRoute(mux, http.MethodPost, "/api/run/preview", resolver, func(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
		handlePostRunPreview(ws, w, r)
	})
	pmRoute(mux, http.MethodGet, "/api/runs", resolver, func(_ *protocol.CachedWorkspace, pm *ProcessManager, w http.ResponseWriter, _ *http.Request) {
		handleGetRuns(pm, w)
	})
	pmRoute(mux, http.MethodPost, "/api/stop", resolver, func(_ *protocol.CachedWorkspace, pm *ProcessManager, w http.ResponseWriter, r *http.Request) {
		handlePostStop(pm, w, r)
	})

	// Settings (multi-method)
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ws, _, err := resolver(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handleGetSettings(ws, w)
		case http.MethodPut:
			ws, pm, err := resolver(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handlePutSettings(ws, w, r)
			reloadProcessManager(ws, pm)
		case http.MethodPatch:
			ws, _, err := resolver(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handlePatchSettings(ws, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/settings/profiles/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			handlePutProfile(ws, w, r)
		case http.MethodDelete:
			handleDeleteProfile(ws, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/settings/roles/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePutRole(ws, w, r)
	})
	mux.HandleFunc("/api/user-config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetUserConfig(w)
		case http.MethodPut:
			handlePutUserConfig(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	wsRoute(mux, http.MethodGet, "/api/merged-config", resolver, func(ws *protocol.CachedWorkspace, w http.ResponseWriter, _ *http.Request) {
		handleGetMergedConfig(ws, w)
	})
	mux.HandleFunc("/api/supported-runners", func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(protocol.SupportedRunners())
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	// Feature detail views (feature ID in URL path)
	featureRoute(mux, "/api/messages/", resolver, func(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter, _ *http.Request) {
		handleMessages(ws, featureID, w)
	})
	featureRoute(mux, "/api/overview/", resolver, func(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter, _ *http.Request) {
		handleOverview(ws, featureID, w)
	})
	wsRoute(mux, http.MethodGet, "/api/evolve-report", resolver, func(ws *protocol.CachedWorkspace, w http.ResponseWriter, _ *http.Request) {
		handleEvolveReport(ws, w)
	})
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		featureID := strings.TrimPrefix(r.URL.Path, "/api/events/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleEvents(ws, featureID, w)
	})
	mux.HandleFunc("/api/logs/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/logs/")
		parts := strings.SplitN(rest, "/", 2)
		if !validFeatureID(parts[0]) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleLogs(ws, rest, w)
	})
	mux.HandleFunc("/api/features/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		handleFeatureScreenshots(ws, w, r)
	})

	// SSE streams
	mux.HandleFunc("/sse/events/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		featureID := strings.TrimPrefix(r.URL.Path, "/sse/events/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleSSE(ws, featureID, w, r)
	})
	mux.HandleFunc("/sse/logs/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		featureID := strings.TrimPrefix(r.URL.Path, "/sse/logs/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleLogSSE(ws, featureID, w, r)
	})

	// Locales & static assets
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
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		handleVersion(w, r)
	})
	mux.Handle("/", http.FileServer(http.FS(web.Assets)))

	return mux
}

// Start 啟動 dashboard web server。opts 為可選 functional option（如 WithAuth）。
func Start(ws *protocol.CachedWorkspace, pm *ProcessManager, port int, opts ...ServeOption) error {
	return http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), logging.Middleware(wrapAuth(NewMux(singleResolver(ws, pm)), opts...)))
}

// StartMulti 啟動多專案 dashboard server，ctx 取消時 graceful shutdown。
// opts 為可選 functional option（如 WithAuth）。
func StartMulti(ctx context.Context, reg *ProjectRegistry, port int, recentPath string, opts ...ServeOption) error {
	_, err := StartMultiOnListener(ctx, reg, port, recentPath, opts...)
	return err
}

// DefaultPort 是 dashboard server 的預設監聽 port，為 CLI（`4x live`）與桌面殼
// （macOS Swift、Windows/Linux Tauri）啟動時的預設猜測值之單一事實來源。
// Swift（dashboard/macos/Sources/main.swift）與 Rust（dashboard/tauri/src-tauri/src/main.rs）
// 各自維護一份等值字面常量（無法跨語言直接讀取此常量），由
// internal/server/port_sync_test.go 的一致性測試守護三者不漂移；異動此值須同步更新該兩處字面常量。
const DefaultPort = 4567

// ListenForMulti 在指定 port 建立 TCP listener，port 為 0 時由 OS 自動分配可用 port。
func ListenForMulti(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

// StartMultiOnListener 在已建立的 listener 上啟動 server（由 ListenForMulti 取得）。
// 回傳實際使用的 port。若傳入 port 而非 listener，使用 StartMulti。
// opts 為可選 functional option（如 WithAuth）。
func StartMultiOnListener(ctx context.Context, reg *ProjectRegistry, port int, recentPath string, opts ...ServeOption) (int, error) {
	ln, err := ListenForMulti(port)
	if err != nil {
		return 0, err
	}
	return ServeMulti(ctx, reg, ln, recentPath, opts...)
}

// ServeMulti 在已建立的 listener 上啟動 dashboard server。
// opts 為可選 functional option（如 WithAuth）；不傳時行為與加 auth 前完全一致。
func ServeMulti(ctx context.Context, reg *ProjectRegistry, ln net.Listener, recentPath string, opts ...ServeOption) (int, error) {
	actualPort := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{
		Handler: NewMultiMux(reg, recentPath, opts...),
	}
	go func() {
		<-ctx.Done()
		reg.ShutdownAll()
		srv.Close()
	}()
	err := srv.Serve(ln)
	if err == http.ErrServerClosed {
		return actualPort, nil
	}
	return actualPort, err
}

type doneRequest struct {
	ID string `json:"id"`
}

type doneResponse struct {
	Status        string   `json:"status"`
	Merged        *bool    `json:"merged,omitempty"`
	MergeConflict bool     `json:"merge_conflict,omitempty"`
	Conflicts     []string `json:"conflicts,omitempty"`
	MergeError    string   `json:"merge_error,omitempty"`
	Error         string   `json:"error,omitempty"`
}

func boolPtr(v bool) *bool { return &v }

func handlePostDone(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
	var req doneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}

	s, err := ws.ReadState(req.ID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "feature not found")
		return
	}

	if s.Phase != protocol.PhasePendingReview {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("feature is in phase %q, not pending-review", s.Phase))
		return
	}

	f, err := ws.LoadFeature(req.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load feature: "+err.Error())
		return
	}

	name := req.ID
	if f.Name != "" {
		name = f.Name
	}

	cfg, _ := ws.LoadMergedConfig()

	// mergeMu 包住整個共用編排（merge → re-read → finalize → commit），序列化同一專案的 merge；
	// 共用函式本身不持鎖，鎖留在 server 呼叫端（CLI 單程序不需鎖）。傳底層 *Workspace，
	// 不繞過 CachedWorkspace 讀取側 cache（共用函式僅走未被 cache 覆寫的寫入側方法）。
	mergeLock := mergeMu.get(ws.Root)
	mergeLock.Lock()
	result, err := gitops.MergeAndFinalize(ws.Root, ws.Workspace, cfg, req.ID, name)
	mergeLock.Unlock()
	if err != nil {
		// 真正 fatal 的 re-read / finalize 失敗維持 HTTP 500。SyncFeatureStatus 失敗已在
		// state.FinalizeDone 內降為 non-fatal，不會走到這裡。
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var resp doneResponse
	switch {
	case result.Conflict:
		resp = doneResponse{Status: "pending-review", MergeConflict: true, Conflicts: result.Files}
	case result.StateChanged:
		w.WriteHeader(http.StatusConflict)
		resp = doneResponse{Status: string(result.FinalState.Phase), Error: "state changed during merge"}
	case result.Error != "":
		resp = doneResponse{Status: "pending-review", MergeError: result.Error}
	case result.Skipped:
		resp = doneResponse{Status: "done", Merged: boolPtr(false)}
	default:
		resp = doneResponse{Status: "done", Merged: boolPtr(true)}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// cleanResponse 是 POST /api/clean 的回應，回報清理數量、釋放空間與被清的 feature 清單。
type cleanResponse struct {
	Cleaned   int      `json:"cleaned"`
	Freed     int64    `json:"freed"`
	FreedText string   `json:"freed_human"`
	Features  []string `json:"features"`
}

// handlePostClean 清理所有可清理的 feature workspace，回傳 JSON 統計。
// 逐一呼叫 CleanFeature，個別失敗（如 race 中變為 active）僅跳過不中斷整批。
func handlePostClean(ws *protocol.Workspace, w http.ResponseWriter) {
	candidates, err := ws.CleanableFeatures()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := cleanResponse{Features: []string{}}
	for _, c := range candidates {
		freed, err := ws.CleanFeature(c.FeatureID)
		if err != nil {
			continue
		}
		resp.Cleaned++
		resp.Freed += freed
		resp.Features = append(resp.Features, c.FeatureID)
	}
	resp.FreedText = protocol.HumanSize(resp.Freed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeJSONError 以 JSON `{"error": "..."}` 格式回傳錯誤與對應 HTTP status。
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	payload, _ := json.Marshal(map[string]string{"error": msg})
	w.Write(payload)
}

func mergedConfig(ws *protocol.CachedWorkspace) protocol.Config {
	cfg, _ := ws.LoadMergedConfig()
	return cfg
}

func readIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// validFeatureID 檢查 featureID 是否安全，拒絕 path traversal 攻擊
func validFeatureID(id string) bool {
	return id != "" && !strings.ContainsAny(id, "/\\") && !strings.Contains(id, "..")
}
