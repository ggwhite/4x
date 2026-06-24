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
	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/logging"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
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

// NewMux 建立 dashboard 的 HTTP handler。每個需要資料的 handler 透過傳入的 resolver
// 解析該請求對應的 workspace/process manager/batch manager，使路由表只定義一次，
// 由 singleResolver（單一專案）或 multiResolver（多專案）共用。
func NewMux(resolver WorkspaceResolver) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		handleTasks(ws, w)
	})
	mux.HandleFunc("/api/run", func(w http.ResponseWriter, r *http.Request) {
		ws, pm, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if pm == nil {
			http.Error(w, "run not available", http.StatusServiceUnavailable)
			return
		}
		handlePostRun(ws, pm, w, r)
	})
	mux.HandleFunc("/api/run/preview", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostRunPreview(ws, w, r)
	})
	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		_, pm, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if pm == nil {
			http.Error(w, "runs not available", http.StatusServiceUnavailable)
			return
		}
		handleGetRuns(pm, w)
	})
	mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		_, pm, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if pm == nil {
			http.Error(w, "stop not available", http.StatusServiceUnavailable)
			return
		}
		handlePostStop(pm, w, r)
	})
	mux.HandleFunc("/api/new", func(w http.ResponseWriter, r *http.Request) {
		ws, pm, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if pm == nil {
			http.Error(w, "new not available", http.StatusServiceUnavailable)
			return
		}
		handlePostNew(ws, w, r)
	})
	mux.HandleFunc("/api/settings/profiles/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodPut {
			handlePutProfile(ws, w, r)
			return
		}
		if r.Method == http.MethodDelete {
			handleDeleteProfile(ws, w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/api/settings/roles/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodPut {
			handlePutRole(ws, w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			ws, _, _, err := resolver(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handleGetSettings(ws, w)
			return
		}
		if r.Method == http.MethodPut {
			ws, pm, _, err := resolver(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handlePutSettings(ws, w, r)
			reloadProcessManager(ws, pm)
			return
		}
		if r.Method == http.MethodPatch {
			ws, _, _, err := resolver(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handlePatchSettings(ws, w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		handleGetMergedConfig(ws, w)
	})
	mux.HandleFunc("/api/supported-runners", func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(protocol.SupportedRunners())
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
	mux.HandleFunc("/api/done", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostDone(ws, w, r)
	})
	mux.HandleFunc("/api/clean", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostClean(ws.Workspace, w)
	})
	mux.HandleFunc("/api/batch/status", func(w http.ResponseWriter, r *http.Request) {
		ws, _, bm, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleBatchStatus(ws, bm, w)
	})
	mux.HandleFunc("/api/batch/start", func(w http.ResponseWriter, r *http.Request) {
		ws, _, bm, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostBatchStart(ws, bm, w, r)
	})
	mux.HandleFunc("/api/batch/stop", func(w http.ResponseWriter, r *http.Request) {
		_, _, bm, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostBatchStop(bm, w)
	})
	mux.HandleFunc("/api/batch/continue", func(w http.ResponseWriter, r *http.Request) {
		ws, _, bm, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostBatchContinue(ws, bm, w, r)
	})
	mux.HandleFunc("/api/batch/replan", func(w http.ResponseWriter, r *http.Request) {
		ws, _, bm, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostBatchReplan(ws, bm, w)
	})
	mux.HandleFunc("/api/evolve-report", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleEvolveReport(ws, w)
	})
	mux.HandleFunc("/api/messages/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		featureID := strings.TrimPrefix(r.URL.Path, "/api/messages/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleMessages(ws, featureID, w)
	})
	mux.HandleFunc("/api/overview/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		featureID := strings.TrimPrefix(r.URL.Path, "/api/overview/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleOverview(ws, featureID, w)
	})
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
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
	mux.HandleFunc("/sse/events/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
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
	mux.HandleFunc("/api/logs/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
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
		ws, _, _, err := resolver(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		handleFeatureScreenshots(ws, w, r)
	})
	mux.HandleFunc("/sse/logs/", func(w http.ResponseWriter, r *http.Request) {
		ws, _, _, err := resolver(r)
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

// newMux 是接受固定 ws/pm/bm 的 thin wrapper，讓注入特定 BatchManager 的測試與內部呼叫者
// 不必自行組 resolver。
func newMux(ws *protocol.CachedWorkspace, pm *ProcessManager, bm *BatchManager) http.Handler {
	return NewMux(staticResolver(ws, pm, bm))
}

// Start 啟動 dashboard web server。
func Start(ws *protocol.CachedWorkspace, pm *ProcessManager, port int) error {
	return http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), logging.Middleware(NewMux(singleResolver(ws, pm))))
}

// StartMulti 啟動多專案 dashboard server，ctx 取消時 graceful shutdown。
func StartMulti(ctx context.Context, reg *ProjectRegistry, port int, recentPath string) error {
	_, err := StartMultiOnListener(ctx, reg, port, recentPath)
	return err
}

// ListenForMulti 在指定 port 建立 TCP listener，port 為 0 時由 OS 自動分配可用 port。
func ListenForMulti(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

// StartMultiOnListener 在已建立的 listener 上啟動 server（由 ListenForMulti 取得）。
// 回傳實際使用的 port。若傳入 port 而非 listener，使用 StartMulti。
func StartMultiOnListener(ctx context.Context, reg *ProjectRegistry, port int, recentPath string) (int, error) {
	ln, err := ListenForMulti(port)
	if err != nil {
		return 0, err
	}
	return ServeMulti(ctx, reg, ln, recentPath)
}

// ServeMulti 在已建立的 listener 上啟動 dashboard server。
func ServeMulti(ctx context.Context, reg *ProjectRegistry, ln net.Listener, recentPath string) (int, error) {
	actualPort := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{
		Handler: NewMultiMux(reg, recentPath),
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
	ops := gitops.New(ws.Root, ws.Workspace, cfg)

	mergeLock := mergeMu.get(ws.Root)
	mergeLock.Lock()
	result := ops.Merge(req.ID, name)
	mergeLock.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if result.Conflict {
		filesJSON, _ := json.Marshal(result.Files)
		fmt.Fprintf(w, `{"status":"pending-review","merge_conflict":true,"conflicts":%s}`, filesJSON)
	} else if result.Error != "" {
		errJSON, _ := json.Marshal(result.Error)
		fmt.Fprintf(w, `{"status":"pending-review","merge_error":%s}`, errJSON)
	} else {
		// merge 可能耗時數秒，期間 runner 或 ensureInactive 可能改過 state。
		// 重新讀取最新 state，避免用 merge 前的 stale 值覆蓋其他欄位更新。
		fresh, err := ws.ReadState(req.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to re-read state: "+err.Error())
			return
		}
		if fresh.Phase != protocol.PhasePendingReview {
			w.WriteHeader(http.StatusConflict)
			statusJSON, _ := json.Marshal(string(fresh.Phase))
			fmt.Fprintf(w, `{"status":%s,"error":"state changed during merge"}`, statusJSON)
			return
		}

		if _, err := transitionDone(ws, req.ID, fresh); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		learning.CommitIfDirty(ws.Root, ws.DotDir())

		if result.Skipped {
			fmt.Fprint(w, `{"status":"done","merged":false}`)
		} else {
			fmt.Fprint(w, `{"status":"done","merged":true}`)
		}
	}
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

func transitionDone(ws *protocol.CachedWorkspace, featureID string, s protocol.State) (protocol.State, error) {
	// PhaseDone 的 role 與 CLI finalizeDone（cmd/4x/done.go）一致用空字串，
	// 使 server 與 CLI 寫出的 state.json Role 欄位相同。
	newState, err := state.Transition(s, protocol.PhaseDone, "")
	if err != nil {
		return protocol.State{}, err
	}
	newState.Active = false
	newState.StopReason = "done"

	if err := ws.WriteState(featureID, newState); err != nil {
		return protocol.State{}, err
	}

	if err := ws.SyncFeatureStatus(featureID, protocol.PhaseDone); err != nil {
		return protocol.State{}, fmt.Errorf("failed to sync feature status: %w", err)
	}

	if err := ws.AppendEvent(featureID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	}); err != nil {
		return protocol.State{}, err
	}

	return newState, nil
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
