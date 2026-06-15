package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	web "github.com/ggwhite/4x/dashboard/web"
	"github.com/ggwhite/4x/internal/batch"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/logging"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

var settingsMu sync.Mutex
var mergeMu sync.Mutex

var supportedLocales = []string{"en", "zh-TW", "zh-CN", "ja", "ko", "es"}

// NewMux 建立 dashboard 的 HTTP handler，並以當前 4x 執行檔建立預設 BatchManager。
func NewMux(ws *protocol.Workspace, pm *ProcessManager) http.Handler {
	return newMux(ws, pm, NewBatchManager(ws, selfBinary()))
}

// newMux 是 NewMux 的內部實作，額外接受注入的 BatchManager 讓多專案 registry 共用同一實例
// （供 shutdown 停止），測試亦可注入假 binName 的 BatchManager。
func newMux(ws *protocol.Workspace, pm *ProcessManager, bm *BatchManager) http.Handler {
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
	mux.HandleFunc("/api/batch/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleBatchStatus(ws, bm, w)
	})
	mux.HandleFunc("/api/batch/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostBatchStart(ws, bm, w, r)
	})
	mux.HandleFunc("/api/batch/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostBatchStop(bm, w)
	})
	mux.HandleFunc("/api/batch/continue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePostBatchContinue(ws, bm, w, r)
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
	mux.Handle("/", http.FileServer(http.FS(web.Assets)))

	return mux
}

// Start 啟動 dashboard web server。
func Start(ws *protocol.Workspace, pm *ProcessManager, port int) error {
	return http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), logging.Middleware(NewMux(ws, pm)))
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

	slog.Debug("sse connected", "feature", featureID)
	defer slog.Debug("sse disconnected", "feature", featureID)

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
	"synthesizer":   5,
	"deep-fix":      6,
	"deep-reverify": 7,
	"acceptor":      8,
}

// logSortKey 將 "round-N-role.log" 轉成數字 key，確保按執行順序排列。
// deep-fix-1、deep-reverify-2 等帶迭代號的 role 會去掉尾部數字做 base role 比對，
// 迭代號作為子排序依據。
func logSortKey(name string) int {
	name = strings.TrimSuffix(name, ".log")
	parts := strings.SplitN(name, "-", 3)
	if len(parts) < 3 || parts[0] != "round" {
		return 999999
	}
	round, _ := strconv.Atoi(parts[1])
	role := parts[2]
	iter := 0
	if idx := strings.LastIndex(role, "-"); idx > 0 {
		if n, err := strconv.Atoi(role[idx+1:]); err == nil {
			iter = n
			role = role[:idx]
		}
	}
	order, ok := roleOrder[role]
	if !ok {
		order = 99
	}
	return round*1000 + order*10 + iter
}

// logKeyFromEvent 將 events.jsonl 的 role 名稱映射到實際 log 檔名。
// mini-coder / re-verifier 會帶迭代號（deep-fix-1、deep-reverify-2），
// 用 iterCount 追蹤每個 round+role 的出現次數。
func logKeyFromEvent(round int, role, eventType string, iterCount map[string]int) string {
	switch role {
	case "mini-coder":
		counterKey := fmt.Sprintf("%d-mini-coder", round)
		if eventType == "phase-start" {
			iterCount[counterKey]++
		}
		return fmt.Sprintf("round-%d-deep-fix-%d.log", round, iterCount[counterKey])
	case "re-verifier":
		counterKey := fmt.Sprintf("%d-re-verifier", round)
		if eventType == "phase-start" {
			iterCount[counterKey]++
		}
		return fmt.Sprintf("round-%d-deep-reverify-%d.log", round, iterCount[counterKey])
	case "deep-reviewer":
		counterKey := fmt.Sprintf("%d-deep-reviewer", round)
		if eventType == "phase-start" {
			iterCount[counterKey]++
		}
		cnt := iterCount[counterKey]
		return fmt.Sprintf("round-%d-deep-reviewer-%d.log", round, cnt)
	default:
		return fmt.Sprintf("round-%d-%s.log", round, role)
	}
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
	iterCount := map[string]int{}
	var lastEventTs time.Time
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if !ev.Ts.IsZero() {
			lastEventTs = ev.Ts
		}
		if ev.Role == "" {
			continue
		}
		key := logKeyFromEvent(ev.Round, string(ev.Role), ev.Type, iterCount)
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
		if ended[key] {
			continue
		}
		if !lastEventTs.IsZero() && lastEventTs.After(ts) {
			ms := lastEventTs.Sub(ts).Milliseconds()
			timings[key] = roleTiming{DurationMs: &ms}
		} else {
			s := ts.UTC().Format(time.RFC3339)
			timings[key] = roleTiming{StartedAt: &s}
		}
	}
	// 非平行 deep review 的 log 檔名為 round-N-deep-reviewer.log（無後綴），
	// 但 events 的第 1 個 phase-start 會產生 round-N-deep-reviewer-1.log key。
	// 把 -1 的 timing 也寫入無後綴 key，讓兩種模式都能查到。
	for key, t := range timings {
		if strings.Contains(key, "-deep-reviewer-1.log") {
			alt := strings.Replace(key, "-deep-reviewer-1.log", "-deep-reviewer.log", 1)
			if _, exists := timings[alt]; !exists {
				timings[alt] = t
			}
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

	slog.Debug("log sse connected", "feature", featureID)
	defer slog.Debug("log sse disconnected", "feature", featureID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	logsDir := filepath.Join(ws.FeatureDir(featureID), "logs")
	pinnedFile := filepath.Base(r.URL.Query().Get("file"))
	// offsets 記每個正在 tail 的 log 已讀到的位移，跨 tick 持續累進。
	// 未 pin 檔案時可同時 tail 多個活躍 log（平行 sub-reviewer / reviewer+tester），各自獨立 offset。
	offsets := make(map[string]int64)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// 在連線生命週期內共用一個固定 32KB buffer，避免每秒 tick 重新分配造成 GC 壓力。
	buf := make([]byte, 32*1024)

	// tailFile 讀取 current 自上次 offset 起的新增內容並以 SSE message 送出（帶 file 欄位）。
	tailFile := func(current string) {
		path := filepath.Join(logsDir, current)
		info, err := os.Stat(path)
		if err != nil || info.Size() <= offsets[current] {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		if offsets[current] > 0 {
			f.Seek(offsets[current], 0)
		}
		// carry 保留上一個 32KB chunk 尾端不完整的 UTF-8 位元組。
		// 固定切分會把橫跨邊界的多位元組字元切兩半，各自 json.Marshal 會被
		// 替換成不可逆的 U+FFFD；故只 emit 完整 rune，殘段併入下一輪前綴。
		var carry []byte
		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				data := buf[:n]
				if len(carry) > 0 {
					data = append(append(make([]byte, 0, len(carry)+n), carry...), buf[:n]...)
				}
				complete, rest := splitCompleteUTF8(data)
				if len(complete) > 0 {
					chunk, _ := json.Marshal(map[string]string{"file": current, "content": string(complete)})
					fmt.Fprintf(w, "data: %s\n\n", chunk)
				}
				carry = append(carry[:0], rest...)
				offsets[current] += int64(n)
			}
			if readErr != nil {
				// EOF：殘段即使不完整也必須送出，否則檔尾遺失（與舊行為一致）。
				if len(carry) > 0 {
					chunk, _ := json.Marshal(map[string]string{"file": current, "content": string(carry)})
					fmt.Fprintf(w, "data: %s\n\n", chunk)
				}
				break
			}
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			var files []string
			if pinnedFile != "" && pinnedFile != "." {
				files = []string{pinnedFile}
			} else {
				files = findActiveLogs(logsDir)
			}
			for _, current := range files {
				tailFile(current)
			}
			flusher.Flush()
		}
	}
}

// activeLogWindow 是 findActiveLogs 判定 log「最近活躍」的時間窗：mtime 落在最新 log
// mtime 往前 activeLogWindow 內者視為仍在寫入，須一併 tail。窗夠大才能涵蓋平行 runner
// 之間的寫入時間差，又不至於把上一個 phase 的舊 log 拉進來。
const activeLogWindow = 15 * time.Second

// findActiveLogs 回傳目前正在寫入 / 最近活躍的 log 檔名清單（依 logSortKey 排序）。
// 以最新 log 的 mtime 為基準，回傳所有 mtime 落在 activeLogWindow 內的 .log，
// 讓平行 sub-reviewer（或 reviewer+tester）等多個同時寫入的 log 都能被 SSE 一起 tail，
// 而非只追到 mtime 最新的單一檔（修復 ParallelReviewTest 只看得到一個 log 的問題）。
func findActiveLogs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type logEntry struct {
		name string
		mod  time.Time
	}
	var logs []logEntry
	var latest time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		logs = append(logs, logEntry{name: e.Name(), mod: info.ModTime()})
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	if len(logs) == 0 {
		return nil
	}
	cutoff := latest.Add(-activeLogWindow)
	var active []string
	for _, l := range logs {
		if !l.mod.Before(cutoff) {
			active = append(active, l.name)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		return logSortKey(active[i]) < logSortKey(active[j])
	})
	return active
}

// splitCompleteUTF8 將 data 切成「結尾為完整 UTF-8 rune 的前段 complete」與
// 「結尾不完整的殘段 rest」。當 log 的 32KB chunk 邊界剛好落在多位元組字元中間時，
// 殘段（最長 3 bytes）需保留併入下一輪 Read 前綴後再嘗試送出，避免字元被切半損毀。
// 回傳的 complete／rest 共用 data 底層陣列，呼叫端若需跨輪保留 rest 應自行複製。
func splitCompleteUTF8(data []byte) (complete, rest []byte) {
	if len(data) == 0 {
		return data, nil
	}
	// 從尾端往前找最後一個 rune 的起始位元組（非 continuation byte）
	i := len(data) - 1
	for i >= 0 && !utf8.RuneStart(data[i]) {
		i--
	}
	// 整段都是 continuation bytes（理論上不會發生），全部送出避免卡死
	if i < 0 {
		return data, nil
	}
	// 最後一個 rune 已完整 → 整段皆可送出；否則保留該 rune 起點之後為殘段
	if utf8.FullRune(data[i:]) {
		return data, nil
	}
	return data[:i], data[i:]
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

		if _, err := transitionDone(ws, req.ID, fresh); err != nil {
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

type batchRequest struct {
	Runner    string `json:"runner"`
	MaxRounds int    `json:"maxRounds"`
}

type batchQueueItem struct {
	FeatureID string `json:"featureId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Position  int    `json:"position"`
	State     string `json:"state"`
	Priority  *int   `json:"priority,omitempty"`
}

type batchStatusResponse struct {
	Running        bool                    `json:"running"`
	Queue          []batchQueueItem        `json:"queue"`
	CurrentFeature string                  `json:"currentFeature"`
	Conflict       *protocol.BatchConflict `json:"conflict"`
}

// loadSavedBatchPlan 讀取已儲存的 .4x/batch-plan.json。
func loadSavedBatchPlan(ws *protocol.Workspace) (*batch.BatchPlan, error) {
	data, err := os.ReadFile(filepath.Join(ws.DotDir(), "batch-plan.json"))
	if err != nil {
		return nil, err
	}
	var plan batch.BatchPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// mergedConfig 讀取 project config 並套用 user config，供 batch handler 取得 runner / hub repos。
func mergedConfig(ws *protocol.Workspace) protocol.Config {
	cfg, _ := ws.ReadConfig()
	if userCfg, err := protocol.ReadUserConfig(); err == nil {
		cfg = protocol.MergeConfig(userCfg, cfg)
	}
	return cfg
}

// handleBatchStatus 回傳目前 batch 執行狀態、依 PlanBatch schedule 排序的佇列，以及衝突信號（若有）。
// batch 在跑時讀已儲存的 batch-plan.json（這次 batch 的計畫）；
// 未跑時用 pending features 即時 plan（預覽下次會跑什麼）。
func handleBatchStatus(ws *protocol.Workspace, bm *BatchManager, w http.ResponseWriter) {
	cfg := mergedConfig(ws)

	features, err := ws.ListFeatures()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	featByID := make(map[string]protocol.Feature, len(features))
	for _, f := range features {
		featByID[f.ID] = f
	}

	resp := batchStatusResponse{Running: bm.Running(), Queue: []batchQueueItem{}}

	var schedule []batch.ScheduleEntry

	if bm.Running() {
		if saved, err := loadSavedBatchPlan(ws); err == nil && saved != nil {
			schedule = saved.Schedule
		}
	}
	if schedule == nil {
		var pending []protocol.Feature
		for _, f := range features {
			if f.Status != protocol.StatusDone && f.Status != protocol.StatusAbandoned {
				pending = append(pending, f)
			}
		}
		if len(pending) > 0 {
			if plan, planErr := batch.PlanBatch(pending, protocol.EffectiveHubRepos(cfg), 4); planErr == nil {
				schedule = plan.Schedule
			}
		}
	}

	pos := 0
	for _, s := range schedule {
		f, ok := featByID[s.FeatureID]
		if !ok {
			continue
		}
		itemState := "waiting"
		if f.Status == protocol.StatusDone || f.Status == protocol.StatusReadyForReview {
			itemState = "done"
		} else if f.Status == protocol.StatusNeedsAttention || f.Status == protocol.StatusBlocked {
			itemState = "error"
		} else if st, stErr := ws.ReadState(s.FeatureID); stErr == nil {
			ws.ReconcileActive(s.FeatureID, &st)
			if st.Active && st.Phase != protocol.PhaseDone {
				itemState = "running"
				if resp.CurrentFeature == "" {
					resp.CurrentFeature = s.FeatureID
				}
			}
		}
		item := batchQueueItem{
			FeatureID: s.FeatureID,
			Name:      f.Name,
			Status:    string(f.Status),
			State:     itemState,
			Priority:  f.Priority,
		}
		if itemState != "done" && itemState != "error" {
			pos++
			item.Position = pos
		}
		resp.Queue = append(resp.Queue, item)
	}

	conflict, _ := ws.ReadBatchConflict()
	resp.Conflict = conflict

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePostBatchStart 啟動 batch run；若仍有未解決的 batch-conflict.json 則回 409，要求先 Continue/解衝突。
func handlePostBatchStart(ws *protocol.Workspace, bm *BatchManager, w http.ResponseWriter, r *http.Request) {
	if conflict, _ := ws.ReadBatchConflict(); conflict != nil {
		writeJSONError(w, http.StatusConflict, "unresolved batch conflict — resolve and continue first")
		return
	}
	startBatch(ws, bm, w, r)
}

// handlePostBatchContinue 在使用者於 worktree 解完衝突後呼叫：先清掉衝突信號再重啟 batch run。
func handlePostBatchContinue(ws *protocol.Workspace, bm *BatchManager, w http.ResponseWriter, r *http.Request) {
	if err := ws.ClearBatchConflict(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	startBatch(ws, bm, w, r)
}

// startBatch 解析 body（runner/maxRounds，缺省用 config）並透過 BatchManager 啟動 batch run。
func startBatch(ws *protocol.Workspace, bm *BatchManager, w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Runner == "" {
		req.Runner = mergedConfig(ws).Default
	}
	if err := bm.Start(req.Runner, req.MaxRounds); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

// handlePostBatchStop 寫出 batch-stop 信號，讓 batch 跑完當前 feature 後 graceful 停止。
func handlePostBatchStop(bm *BatchManager, w http.ResponseWriter) {
	if err := bm.Stop(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

// writeJSONError 以 JSON `{"error": "..."}` 格式回傳錯誤與對應 HTTP status。
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	payload, _ := json.Marshal(map[string]string{"error": msg})
	w.Write(payload)
}

func transitionDone(ws *protocol.Workspace, featureID string, s protocol.State) (protocol.State, error) {
	newState, err := state.Transition(s, protocol.PhaseDone, protocol.RoleDesigner)
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

// handleGetSettings 讀取 .4x/settings.json 原始內容並回傳，保留所有欄位（含 Config struct 未定義的）。
func handleGetSettings(ws *protocol.Workspace, w http.ResponseWriter) {
	slog.Debug("config loaded", "project", filepath.Base(ws.Root))
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

	slog.Debug("config saved", "project", filepath.Base(ws.Root))
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
	filename := "locales/" + lang + ".json"
	data, err := web.Assets.ReadFile(filename)
	if err != nil {
		data, _ = web.Assets.ReadFile("locales/en.json")
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
