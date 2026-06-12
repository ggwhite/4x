package server

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

//go:embed static/index.html
var indexHTML string

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
	mux.HandleFunc("/api/messages/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/api/messages/")
		handleMessages(ws, featureID, w)
	})
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/api/events/")
		handleEvents(ws, featureID, w)
	})
	mux.HandleFunc("/sse/events/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/sse/events/")
		handleSSE(ws, featureID, w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, indexHTML)
	})

	return mux
}

// Start 啟動 dashboard web server。
func Start(ws *protocol.Workspace, pm *ProcessManager, port int) error {
	return http.ListenAndServe(fmt.Sprintf(":%d", port), NewMux(ws, pm))
}

// StartMulti 啟動多專案 dashboard server
func StartMulti(reg *ProjectRegistry, port int, recentPath string) error {
	return http.ListenAndServe(fmt.Sprintf(":%d", port), NewMultiMux(reg, recentPath))
}

type taskInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Phase  string `json:"phase"`
	Role   string `json:"role"`
	Round  int    `json:"round"`
	Active bool   `json:"active"`
	Runner string `json:"runner"`
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
		Status:      "not-started",
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
			ID:     f.ID,
			Name:   f.Name,
			Status: f.Status,
		}
		if s, err := ws.ReadState(f.ID); err == nil {
			t.Phase = string(s.Phase)
			t.Role = string(s.Role)
			t.Round = s.Round
			t.Active = s.Active
			t.Runner = s.Runner
		}
		tasks = append(tasks, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

type messageInfo struct {
	Role    string `json:"role"`
	Label   string `json:"label"`
	Content string `json:"content"`
	File    string `json:"file"`
	Round   int    `json:"round,omitempty"`
}

func handleMessages(ws *protocol.Workspace, featureID string, w http.ResponseWriter) {
	dir := ws.FeatureDir(featureID)
	var messages []messageInfo

	for _, f := range []struct {
		name string
		role string
	}{
		{protocol.TaskBrief, "designer"},
		{protocol.Criteria, "designer"},
	} {
		content := readIfExists(filepath.Join(dir, f.name))
		if content != "" {
			messages = append(messages, messageInfo{
				Role:    f.role,
				Label:   f.name,
				Content: content,
				File:    f.name,
			})
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
			{"deep-review-report.md", "deep-reviewer"},
			{"test-report.md", "tester"},
			{"web-test-report.md", "tester"},
			{"gate-test-report.md", "tester"},
		} {
			content := readIfExists(filepath.Join(roundPath, f.name))
			if content != "" {
				messages = append(messages, messageInfo{
					Role:    f.role,
					Label:   f.name,
					Content: content,
					File:    filepath.Join(entry.Name(), f.name),
					Round:   roundNum,
				})
			}
		}
	}

	final := readIfExists(filepath.Join(dir, protocol.FinalReport))
	if final != "" {
		messages = append(messages, messageInfo{
			Role:    "acceptor",
			Label:   protocol.FinalReport,
			Content: final,
			File:    protocol.FinalReport,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
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
			if info.Size() <= lastOffset {
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
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			lastOffset = info.Size()
			f.Close()
			flusher.Flush()
		}
	}
}

func readIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
