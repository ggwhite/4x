package server

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

var settingsMu sync.Mutex
var mergeMu sync.Mutex

//go:embed static
var staticFS embed.FS

var supportedLocales = []string{"en", "zh-TW", "zh-CN", "ja", "ko", "es"}

// NewMux 建立 dashboard 的 HTTP handler。
func NewMux(ws *protocol.Workspace, pm *ProcessManager) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		handleTasks(ws, w)
	})
	if pm != nil {
		mux.HandleFunc("/api/run", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handlePostRun(ws, pm, w, r)
		})
		mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handleGetRuns(pm, w)
		})
		mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handlePostStop(pm, w, r)
		})
		mux.HandleFunc("/api/new", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handlePostNew(ws, w, r)
		})
	}
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetSettings(ws, w)
			return
		}
		if r.Method == http.MethodPut {
			handlePutSettings(ws, w, r)
			reloadProcessManager(ws, pm)
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
		if r.Method == http.MethodGet {
			handleGetMergedConfig(ws, w)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/api/supported-runners", func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(protocol.SupportedRunners())
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
	mux.HandleFunc("/api/done", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostDone(ws, w, r)
	})
	mux.HandleFunc("/api/messages/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/api/messages/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleMessages(ws, featureID, w)
	})
	mux.HandleFunc("/api/overview/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/api/overview/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleOverview(ws, featureID, w)
	})
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/api/events/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleEvents(ws, featureID, w)
	})
	mux.HandleFunc("/sse/events/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/sse/events/")
		if !validFeatureID(featureID) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleSSE(ws, featureID, w, r)
	})
	mux.HandleFunc("/api/logs/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/logs/")
		parts := strings.SplitN(rest, "/", 2)
		if !validFeatureID(parts[0]) {
			http.Error(w, "invalid feature id", http.StatusBadRequest)
			return
		}
		handleLogs(ws, rest, w)
	})
	mux.HandleFunc("/api/features/", func(w http.ResponseWriter, r *http.Request) {
		handleFeatureScreenshots(ws, w, r)
	})
	mux.HandleFunc("/sse/logs/", func(w http.ResponseWriter, r *http.Request) {
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
	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	return mux
}

// Start 啟動 dashboard web server。
func Start(ws *protocol.Workspace, pm *ProcessManager, port int) error {
	return http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), NewMux(ws, pm))
}

// StartMulti 啟動多專案 dashboard server，ctx 取消時 graceful shutdown
func StartMulti(ctx context.Context, reg *ProjectRegistry, port int, recentPath string) error {
	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: NewMultiMux(reg, recentPath),
	}
	go func() {
		<-ctx.Done()
		reg.ShutdownAll()
		srv.Close()
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

type taskInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Phase     string   `json:"phase"`
	Role      string   `json:"role"`
	Round     int      `json:"round"`
	Active    bool     `json:"active"`
	Pid       int      `json:"pid,omitempty"`
	Runner    string   `json:"runner"`
	Runners   []string `json:"runners,omitempty"`
	StopReason string   `json:"stopReason,omitempty"`
	Priority   *int     `json:"priority,omitempty"`
	Profile    string   `json:"profile,omitempty"`
	HasSpec   bool     `json:"hasSpec,omitempty"`
	HasPlan   bool     `json:"hasPlan,omitempty"`
	Depends   []string `json:"depends,omitempty"`
	CreatedAt string   `json:"createdAt,omitempty"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
}

type overviewInfo struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Status      string             `json:"status"`
	Priority    *int               `json:"priority,omitempty"`
	Repos       []string           `json:"repos,omitempty"`
	Subtasks    []protocol.Subtask `json:"subtasks,omitempty"`
	Rules       []string           `json:"rules,omitempty"`
	Depends     []string           `json:"depends,omitempty"`
	Spec        string             `json:"spec"`
	Plan        string             `json:"plan"`
	SpecSource  string             `json:"specSource"`
	PlanSource  string             `json:"planSource"`
}

type runRequest struct {
	FeatureID string `json:"featureId"`
	Runner    string `json:"runner"`
	MaxRounds int    `json:"maxRounds"`
}

func handlePostRun(ws *protocol.Workspace, pm *ProcessManager, w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.FeatureID == "" {
		http.Error(w, "featureId required", http.StatusBadRequest)
		return
	}
	if _, err := ws.LoadFeature(req.FeatureID); err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}
	if s, err := ws.ReadState(req.FeatureID); err == nil {
		if s.Active && protocol.ProcessAlive(s.Pid) {
			http.Error(w, fmt.Sprintf("feature %s is already running (pid %d)", req.FeatureID, s.Pid), http.StatusConflict)
			return
		}
	}
	if req.Runner == "" {
		if cfg, err := ws.ReadConfig(); err == nil {
			req.Runner = cfg.Default
		}
	}
	if req.MaxRounds <= 0 {
		req.MaxRounds = 5
	}

	info, err := pm.Start(req.FeatureID, req.Runner, req.MaxRounds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func handleGetRuns(pm *ProcessManager, w http.ResponseWriter) {
	runs := pm.List()
	if runs == nil {
		runs = []RunInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

type stopRequest struct {
	ID string `json:"id"`
}

func handlePostStop(pm *ProcessManager, w http.ResponseWriter, r *http.Request) {
	var req stopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if err := pm.Stop(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

type newRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type newResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func handlePostNew(ws *protocol.Workspace, w http.ResponseWriter, r *http.Request) {
	var req newRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	next, err := protocol.NextFeatureNumber(ws)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id := protocol.GenerateFeatureID(next, req.Name)

	description := req.Description
	if description == "" {
		description = req.Name
	}
	feature := protocol.Feature{
		ID:          id,
		Name:        req.Name,
		Description: description,
		Status:      protocol.StatusNotStarted,
	}
	if err := ws.SaveFeature(feature); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := ws.InitFeatureDir(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newResponse{ID: id, Name: req.Name})
}

func handleTasks(ws *protocol.Workspace, w http.ResponseWriter) {
	features, err := ws.ListFeatures()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var tasks []taskInfo
	for _, f := range features {
		t := taskInfo{
			ID:       f.ID,
			Name:     f.Name,
			Status:   string(f.Status),
			Priority: f.Priority,
		}
		_, specSource := resolveDoc(ws.Root, f.Spec, f.ID, "spec")
		_, planSource := resolveDoc(ws.Root, f.Plan, f.ID, "plan")
		t.HasSpec = specSource != ""
		t.HasPlan = planSource != ""
		t.Depends = f.Depends
		if s, err := ws.ReadState(f.ID); err == nil {
			ws.ReconcileActive(f.ID, &s)
			t.Phase = string(s.Phase)
			t.Role = string(s.Role)
			t.Round = s.Round
			t.Active = s.Active
			t.Pid = s.Pid
			t.Runner = s.Runner
			t.Runners = s.Runners
			t.StopReason = s.StopReason
			t.Profile = s.Profile
			if !s.CreatedAt.IsZero() {
				t.CreatedAt = s.CreatedAt.Format(time.RFC3339)
			}
			if !s.UpdatedAt.IsZero() {
				t.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
			}
		}
		tasks = append(tasks, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func handleOverview(ws *protocol.Workspace, featureID string, w http.ResponseWriter) {
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}

	spec, specSource := resolveDoc(ws.Root, f.Spec, f.ID, "spec")
	plan, planSource := resolveDoc(ws.Root, f.Plan, f.ID, "plan")
	info := overviewInfo{
		ID:          f.ID,
		Name:        f.Name,
		Description: f.Description,
		Status:      string(f.Status),
		Priority:    f.Priority,
		Repos:       f.Repos,
		Subtasks:    f.Subtasks,
		Rules:       f.Rules,
		Depends:     f.Depends,
		Spec:        spec,
		Plan:        plan,
		SpecSource:  specSource,
		PlanSource:  planSource,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

type messageInfo struct {
	Role     string `json:"role"`
	Label    string `json:"label"`
	Content  string `json:"content"`
	File     string `json:"file"`
	Round    int    `json:"round,omitempty"`
	Duration int    `json:"duration,omitempty"` // seconds
	Model    string `json:"model,omitempty"`
}

func handleMessages(ws *protocol.Workspace, featureID string, w http.ResponseWriter) {
	dir := ws.FeatureDir(featureID)
	phases := buildPhaseInfo(ws, featureID)
	var messages []messageInfo

	for i, name := range []string{protocol.TaskBrief, protocol.Criteria} {
		content := readIfExists(filepath.Join(dir, name))
		if content != "" {
			mi := messageInfo{
				Role:    "designer",
				Label:   name,
				Content: content,
				File:    name,
			}
			if i == 0 {
				mi.Duration = phases[durationKey{"designer", 0}].duration
				mi.Model = phases[durationKey{"designer", 0}].model
			}
			messages = append(messages, mi)
		}
	}

	roundsDir := filepath.Join(dir, protocol.RoundsDir)
	entries, _ := os.ReadDir(roundsDir)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "round-") {
			continue
		}
		roundNum := 0
		fmt.Sscanf(entry.Name(), "round-%d", &roundNum)
		roundPath := filepath.Join(roundsDir, entry.Name())

		for _, f := range []struct {
			name string
			role string
		}{
			{"coder-report.md", "coder"},
			{"review-report.md", "reviewer"},
			{"test-report.md", "tester"},
			{"deep-review-report.md", "deep-reviewer"},
			{"web-test-report.md", "tester"},
			{"gate-test-report.md", "tester"},
		} {
			content := readIfExists(filepath.Join(roundPath, f.name))
			if content != "" {
				messages = append(messages, messageInfo{
					Role:     f.role,
					Label:    f.name,
					Content:  content,
					File:     filepath.Join(entry.Name(), f.name),
					Round:    roundNum,
					Duration: phases[durationKey{f.role, roundNum}].duration,
				Model:    phases[durationKey{f.role, roundNum}].model,
				})
			}
		}
	}

	s, _ := ws.ReadState(featureID)
	showAcceptor := s.Phase == protocol.PhaseAccepting ||
		s.Phase == protocol.PhasePendingReview ||
		s.Phase == protocol.PhaseDone
	if showAcceptor {
		final := readIfExists(filepath.Join(dir, protocol.FinalReport))
		if final != "" {
			messages = append(messages, messageInfo{
				Role:    "acceptor",
				Label:   protocol.FinalReport,
				Content: final,
				File:    protocol.FinalReport,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

type durationKey struct {
	role  string
	round int
}

type phaseInfo struct {
	duration int
	model    string
}

func buildPhaseInfo(ws *protocol.Workspace, featureID string) map[durationKey]phaseInfo {
	result := make(map[durationKey]phaseInfo)
	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}

	type eventEntry struct {
		Ts    string `json:"ts"`
		Type  string `json:"type"`
		Role  string `json:"role"`
		Round int    `json:"round"`
		Model string `json:"model"`
	}

	starts := make(map[durationKey]time.Time)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e eventEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		key := durationKey{e.Role, e.Round}
		switch e.Type {
		case "phase-start":
			if t, err := time.Parse(time.RFC3339, e.Ts); err == nil {
				starts[key] = t
			}
			info := result[key]
			if e.Model != "" {
				info.model = e.Model
			}
			result[key] = info
		case "run-end":
			if start, ok := starts[key]; ok {
				if t, err := time.Parse(time.RFC3339, e.Ts); err == nil {
					info := result[key]
					info.duration = int(t.Sub(start).Seconds())
					if e.Model != "" {
						info.model = e.Model
					}
					result[key] = info
				}
			}
		}
	}
	return result
}

func handleEvents(ws *protocol.Workspace, featureID string, w http.ResponseWriter) {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	content := readIfExists(path)
	if content == "" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "[]")
		return
	}

	var events []json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if line != "" {
			events = append(events, json.RawMessage(line))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// handleSSE 用 polling 方式 tail events.jsonl 並以 SSE 推送
func handleSSE(ws *protocol.Workspace, featureID string, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	var lastOffset int64

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			// 檔案被 truncate/rotate（如 4x transition --to init 重置 events.jsonl）後
			// size 會小於 lastOffset，此時 reset 回 0 從頭重讀，否則 SSE 會永遠卡住不再送新 event。
			if info.Size() < lastOffset {
				lastOffset = 0
			}
			if info.Size() == lastOffset {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			if lastOffset > 0 {
				f.Seek(lastOffset, 0)
			}
			scanner := bufio.NewScanner(f)
			consumed := lastOffset
			for scanner.Scan() {
				line := scanner.Text()
				consumed += int64(len(scanner.Bytes())) + 1
				if line == "" {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			lastOffset = consumed
			f.Close()
			flusher.Flush()
		}
	}
}

type logInfo struct {
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	DurationMs *int64  `json:"durationMs,omitempty"`
	StartedAt  *string `json:"startedAt,omitempty"`
}

type screenshotItem struct {
	Path        string `json:"path"`
	Step        string `json:"step"`
	Description string `json:"description"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
}

type screenshotGroupResponse struct {
	Round       int              `json:"round"`
	Screenshots []screenshotItem `json:"screenshots"`
}

type screenshotsResponse struct {
	Groups []screenshotGroupResponse `json:"groups"`
	Total  int                       `json:"total"`
}

// handleLogs 處理 /api/logs/<featureId> 列表或 /api/logs/<featureId>/<filename> 內容
func handleLogs(ws *protocol.Workspace, rest string, w http.ResponseWriter) {
	parts := strings.SplitN(rest, "/", 2)
	featureID := parts[0]
	logsDir := filepath.Join(ws.FeatureDir(featureID), "logs")

	if len(parts) == 1 || parts[1] == "" {
		entries, _ := os.ReadDir(logsDir)
		timings := parseRoleTimings(ws.FeatureDir(featureID))
		var logs []logInfo
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			li := logInfo{Name: e.Name(), Size: info.Size()}
			if t, ok := timings[e.Name()]; ok {
				li.DurationMs = t.DurationMs
				li.StartedAt = t.StartedAt
			}
			logs = append(logs, li)
		}
		sort.Slice(logs, func(i, j int) bool {
			return logSortKey(logs[i].Name) < logSortKey(logs[j].Name)
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
		return
	}

	filename := filepath.Base(parts[1])
	if !strings.HasSuffix(filename, ".log") {
		http.Error(w, "invalid log file", 400)
		return
	}
	data, err := os.ReadFile(filepath.Join(logsDir, filename))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

var roleOrder = map[string]int{
	"designer":      0,
	"coder":         1,
	"reviewer":      2,
	"tester":        3,
	"deep-reviewer": 4,
	"acceptor":      5,
}

// logSortKey 將 "round-N-role.log" 轉成數字 key，確保按執行順序排列
func logSortKey(name string) int {
	name = strings.TrimSuffix(name, ".log")
	parts := strings.SplitN(name, "-", 3)
	if len(parts) < 3 || parts[0] != "round" {
		return 999999
	}
	round, _ := strconv.Atoi(parts[1])
	role := parts[2]
	order, ok := roleOrder[role]
	if !ok {
		order = 99
	}
	return round*100 + order
}

type roleTiming struct {
	DurationMs *int64
	StartedAt  *string
}

// parseRoleTimings 從 events.jsonl 讀取每個 role 的計時資訊。
// 已結束的 role 帶 DurationMs；仍在執行的 role 帶 StartedAt（供前端動態計算）。
func parseRoleTimings(featureDir string) map[string]roleTiming {
	data, err := os.ReadFile(filepath.Join(featureDir, "events.jsonl"))
	if err != nil {
		return nil
	}
	type event struct {
		Ts    time.Time `json:"ts"`
		Type  string    `json:"type"`
		Role  string    `json:"role"`
		Round int       `json:"round"`
	}
	starts := map[string]time.Time{}
	ended := map[string]bool{}
	timings := map[string]roleTiming{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Role == "" {
			continue
		}
		key := fmt.Sprintf("round-%d-%s.log", ev.Round, ev.Role)
		switch ev.Type {
		case "phase-start":
			starts[key] = ev.Ts
		case "run-end":
			if start, ok := starts[key]; ok {
				ms := ev.Ts.Sub(start).Milliseconds()
				timings[key] = roleTiming{DurationMs: &ms}
				ended[key] = true
			}
		}
	}
	for key, ts := range starts {
		if !ended[key] {
			s := ts.UTC().Format(time.RFC3339)
			timings[key] = roleTiming{StartedAt: &s}
		}
	}
	return timings
}

func handleFeatureScreenshots(ws *protocol.Workspace, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/features/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "screenshots" {
		http.NotFound(w, r)
		return
	}
	featureID := parts[0]
	if strings.ContainsAny(featureID, "/\\") || strings.Contains(featureID, "..") {
		http.Error(w, "invalid feature id", http.StatusBadRequest)
		return
	}
	if len(parts) == 2 || (len(parts) == 3 && parts[2] == "") {
		handleGetScreenshots(ws, featureID, w)
		return
	}
	if len(parts) == 3 {
		handleServeScreenshot(ws, featureID, parts[2], w, r)
		return
	}
	http.NotFound(w, r)
}

// getMergedScreenshotDir 讀取 project config 並合併 user config，回傳 screenshotDir。
func getMergedScreenshotDir(ws *protocol.Workspace) string {
	cfg, err := ws.ReadConfig()
	if err != nil {
		return protocol.DefaultScreenshotDir
	}
	if userCfg, err := protocol.ReadUserConfig(); err == nil {
		cfg = protocol.MergeConfig(userCfg, cfg)
	}
	return protocol.ScreenshotDir(cfg)
}

func handleGetScreenshots(ws *protocol.Workspace, featureID string, w http.ResponseWriter) {
	screenshotDir := getMergedScreenshotDir(ws)
	groups, err := ws.DiscoverScreenshots(featureID, screenshotDir)
	if err != nil {
		http.Error(w, "discover screenshots: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := screenshotsResponse{Groups: make([]screenshotGroupResponse, 0, len(groups))}
	for _, group := range groups {
		items := make([]screenshotItem, 0, len(group.Screenshots))
		for _, shot := range group.Screenshots {
			filename := filepath.Base(shot.Path)
			urlToken := encodeScreenshotToken(shot.Path)
			if urlToken == "" {
				urlToken = filename
			}
			items = append(items, screenshotItem{
				Path:        shot.Path,
				Step:        shot.Step,
				Description: shot.Description,
				Filename:    filename,
				URL:         "/api/features/" + featureID + "/screenshots/" + url.PathEscape(urlToken),
			})
		}
		resp.Groups = append(resp.Groups, screenshotGroupResponse{
			Round:       group.Round,
			Screenshots: items,
		})
		resp.Total += len(items)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleServeScreenshot(ws *protocol.Workspace, featureID, filename string, w http.ResponseWriter, r *http.Request) {
	token, err := url.PathUnescape(filename)
	if err != nil {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	// base64 token 已含完整路徑，直接解碼，不需 re-discover。
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		http.Error(w, "invalid screenshot token", http.StatusBadRequest)
		return
	}
	relPath := protocol.NormalizeScreenshotPath(string(data))
	if relPath == "" || !protocol.IsScreenshotFile(filepath.Base(relPath)) {
		http.Error(w, "unsupported screenshot type", http.StatusBadRequest)
		return
	}

	// 先嘗試以 workspace root 為基準解析路徑（支援 .4x/ 以外的截圖目錄）；
	// 若不存在則 fallback 到 .4x/（NormalizeScreenshotPath 已去除 .4x/ 前綴）。
	abs := filepath.Join(ws.Root, filepath.FromSlash(relPath))
	abs, err = filepath.Abs(abs)
	if err != nil {
		http.Error(w, "invalid screenshot path", http.StatusInternalServerError)
		return
	}
	rootAbs, err := filepath.Abs(ws.Root)
	if err != nil {
		http.Error(w, "invalid workspace path", http.StatusInternalServerError)
		return
	}

	if _, statErr := os.Stat(abs); statErr != nil {
		dotAbs := filepath.Join(rootAbs, protocol.DirName)
		abs2 := filepath.Join(dotAbs, filepath.FromSlash(relPath))
		abs2, err = filepath.Abs(abs2)
		if err == nil {
			abs = abs2
		}
	}

	// 安全檢查：路徑必須在 workspace root 內。
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if _, err := os.Stat(abs); err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", screenshotContentType(filepath.Base(relPath)))
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	http.ServeFile(w, r, abs)
}

func encodeScreenshotToken(path string) string {
	normalized := protocol.NormalizeScreenshotPath(path)
	if normalized == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(normalized))
}

func screenshotContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// handleLogSSE 即時 tail log 檔案。支援 ?file= 指定特定檔案，未指定則追蹤最新的。
func handleLogSSE(ws *protocol.Workspace, featureID string, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	logsDir := filepath.Join(ws.FeatureDir(featureID), "logs")
	pinnedFile := filepath.Base(r.URL.Query().Get("file"))
	var lastFile string
	var lastOffset int64

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			var current string
			if pinnedFile != "" && pinnedFile != "." {
				current = pinnedFile
			} else {
				current = findLatestLog(logsDir)
			}
			if current == "" {
				continue
			}
			if current != lastFile {
				lastFile = current
				lastOffset = 0
			}
			path := filepath.Join(logsDir, current)
			info, err := os.Stat(path)
			if err != nil || info.Size() <= lastOffset {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			if lastOffset > 0 {
				f.Seek(lastOffset, 0)
			}
			buf := make([]byte, info.Size()-lastOffset)
			n, _ := f.Read(buf)
			f.Close()
			if n > 0 {
				chunk, _ := json.Marshal(map[string]string{"file": current, "content": string(buf[:n])})
				fmt.Fprintf(w, "data: %s\n\n", chunk)
			}
			lastOffset += int64(n)
			flusher.Flush()
		}
	}
}

func findLatestLog(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var latest string
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = e.Name()
		}
	}
	return latest
}

type doneRequest struct {
	ID string `json:"id"`
}

func handlePostDone(ws *protocol.Workspace, w http.ResponseWriter, r *http.Request) {
	var req doneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	s, err := ws.ReadState(req.ID)
	if err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}

	if s.Phase != protocol.PhasePendingReview {
		http.Error(w, fmt.Sprintf("feature is in phase %q, not pending-review", s.Phase), http.StatusBadRequest)
		return
	}

	f, err := ws.LoadFeature(req.ID)
	if err != nil {
		http.Error(w, "failed to load feature: "+err.Error(), http.StatusInternalServerError)
		return
	}

	name := req.ID
	if f.Name != "" {
		name = f.Name
	}

	cfg, _ := ws.ReadConfig()
	if userCfg, err := protocol.ReadUserConfig(); err == nil {
		cfg = protocol.MergeConfig(userCfg, cfg)
	}
	ops := gitops.New(ws.Root, ws, cfg)

	mergeMu.Lock()
	result := ops.Merge(req.ID, name)
	mergeMu.Unlock()

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
			http.Error(w, "failed to re-read state: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if fresh.Phase != protocol.PhasePendingReview {
			w.WriteHeader(http.StatusConflict)
			statusJSON, _ := json.Marshal(string(fresh.Phase))
			fmt.Fprintf(w, `{"status":%s,"error":"state changed during merge"}`, statusJSON)
			return
		}

		if _, err := transitionDone(ws, req.ID, fresh, f); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if result.Skipped {
			fmt.Fprint(w, `{"status":"done","merged":false}`)
		} else {
			fmt.Fprint(w, `{"status":"done","merged":true}`)
		}
	}
}

func transitionDone(ws *protocol.Workspace, featureID string, s protocol.State, f protocol.Feature) (protocol.State, error) {
	newState, err := state.Transition(s, protocol.PhaseDone, protocol.RoleDesigner)
	if err != nil {
		return protocol.State{}, err
	}
	newState.Active = false
	newState.StopReason = "done"

	if err := ws.WriteState(featureID, newState); err != nil {
		return protocol.State{}, err
	}

	f.Status = protocol.PhaseToStatus(protocol.PhaseDone)
	if err := ws.SaveFeature(f); err != nil {
		return protocol.State{}, fmt.Errorf("failed to save feature: %w", err)
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

// handleGetSettings 讀取 .4x/settings.json 原始內容並回傳，保留所有欄位（含 Config struct 未定義的）。
func handleGetSettings(ws *protocol.Workspace, w http.ResponseWriter) {
	settingsPath := filepath.Join(ws.DotDir(), protocol.ConfigFile)
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePutSettings 接受完整的設定 JSON，驗證後備份並原子寫入 .4x/settings.json。
// 全量替換：前端送什麼就寫什麼，不做 merge。
func handlePutSettings(ws *protocol.Workspace, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "read error: "+err.Error(), http.StatusBadRequest)
		}
		return
	}

	// 驗證結構相容 protocol.Config
	var cfg protocol.Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.Project.Name == "" {
		http.Error(w, "project.name is required", http.StatusBadRequest)
		return
	}

	// 重新格式化以確保一致的縮排
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		http.Error(w, "marshal error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	newData := append(result, '\n')

	settingsMu.Lock()
	defer settingsMu.Unlock()

	settingsPath := filepath.Join(ws.DotDir(), protocol.ConfigFile)
	oldData, err := os.ReadFile(settingsPath)
	if err != nil {
		http.Error(w, "cannot read current settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 內容沒變就不寫
	if bytes.Equal(oldData, newData) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(result)
		return
	}

	// 備份原始設定
	if err := os.WriteFile(settingsPath+".bak", oldData, 0o644); err != nil {
		http.Error(w, "backup failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 原子寫入：先寫 temp file 再 rename
	tmpPath := settingsPath + ".tmp"
	if err := os.WriteFile(tmpPath, newData, 0o644); err != nil {
		http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmpPath, settingsPath); err != nil {
		http.Error(w, "rename error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}

func reloadProcessManager(ws *protocol.Workspace, pm *ProcessManager) {
	if pm == nil {
		return
	}
	cfg, err := ws.ReadConfig()
	if err != nil {
		return
	}
	if cfg.MaxConcurrentRuns > 0 {
		pm.SetMaxParallel(cfg.MaxConcurrentRuns)
	}
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

// handleGetLocales 回傳支援的語言清單。
func handleGetLocales(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(supportedLocales)
}

// handleGetLocale 回傳對應語言的翻譯 JSON；不存在則 fallback 回 en.json。
func handleGetLocale(w http.ResponseWriter, r *http.Request) {
	lang := strings.TrimPrefix(r.URL.Path, "/api/locales/")
	if strings.ContainsAny(lang, "/\\") || strings.Contains(lang, "..") {
		http.Error(w, "invalid locale", http.StatusBadRequest)
		return
	}
	filename := "static/locales/" + lang + ".json"
	data, err := staticFS.ReadFile(filename)
	if err != nil {
		data, _ = staticFS.ReadFile("static/locales/en.json")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

var userConfigMu sync.Mutex

// handleGetUserConfig 讀取 ~/.4x/settings.json 回傳 user config
func handleGetUserConfig(w http.ResponseWriter) {
	userConfigMu.Lock()
	cfg, err := protocol.ReadUserConfig()
	userConfigMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePutUserConfig 接受 user config JSON，驗證後備份並寫入 ~/.4x/settings.json
func handlePutUserConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "read error: "+err.Error(), http.StatusBadRequest)
		}
		return
	}

	var cfg protocol.UserConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	userConfigMu.Lock()
	defer userConfigMu.Unlock()

	path, err := protocol.UserConfigPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if oldData, err := os.ReadFile(path); err == nil {
		os.WriteFile(path+".bak", oldData, 0o644)
	}

	if err := protocol.WriteUserConfig(cfg); err != nil {
		http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result, _ := json.MarshalIndent(cfg, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}

// handleGetMergedConfig 回傳 user + project merge 後的最終 config
func handleGetMergedConfig(ws *protocol.Workspace, w http.ResponseWriter) {
	projectCfg, err := ws.ReadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	userConfigMu.Lock()
	userCfg, _ := protocol.ReadUserConfig()
	userConfigMu.Unlock()
	merged := protocol.MergeConfig(userCfg, projectCfg)

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// resolveDoc 依優先序讀取設計文件：YAML 指定路徑 > docs/design/ 慣例路徑
func resolveDoc(root, yamlPath, featureID, suffix string) (string, string) {
	if yamlPath != "" {
		abs := yamlPath
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, yamlPath)
		}
		if _, err := os.Stat(abs); err == nil {
			return readIfExists(abs), yamlPath
		}
	}

	source := filepath.Join("docs", "design", featureID+"-"+suffix+".md")
	abs := filepath.Join(root, source)
	if _, err := os.Stat(abs); err == nil {
		return readIfExists(abs), source
	}
	return "", ""
}
