package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

type taskInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Phase       string   `json:"phase"`
	SubPhase    string   `json:"subPhase,omitempty"`
	Role        string   `json:"role"`
	Round       int      `json:"round"`
	Active      bool     `json:"active"`
	Pid         int      `json:"pid,omitempty"`
	Runner      string   `json:"runner"`
	Runners     []string `json:"runners,omitempty"`
	StopReason  string   `json:"stopReason,omitempty"`
	StopMessage string   `json:"stopMessage,omitempty"`
	Priority    *int     `json:"priority,omitempty"`
	Profile     string   `json:"profile,omitempty"`
	HasSpec     bool     `json:"hasSpec,omitempty"`
	HasPlan     bool     `json:"hasPlan,omitempty"`
	Depends     []string `json:"depends,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

type overviewInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Priority    *int              `json:"priority,omitempty"`
	Repos       []string          `json:"repos,omitempty"`
	Subtasks    []feature.Subtask `json:"subtasks,omitempty"`
	Rules       []string          `json:"rules,omitempty"`
	Depends     []string          `json:"depends,omitempty"`
	Spec        string            `json:"spec"`
	Plan        string            `json:"plan"`
	SpecSource  string            `json:"specSource"`
	PlanSource  string            `json:"planSource"`
}

func handleTasks(ws *protocol.CachedWorkspace, w http.ResponseWriter) {
	features, err := ws.ListFeatures()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var designDocDirs []string
	if cfg, err := ws.ReadConfig(); err == nil {
		designDocDirs = cfg.DesignDocDirs
	}

	var tasks []taskInfo
	for _, f := range features {
		t := taskInfo{
			ID:       f.ID,
			Name:     f.Name,
			Status:   string(f.Status),
			Priority: f.Priority,
		}
		t.HasSpec = protocol.ResolveDesignDoc(ws.Root, f, "spec", designDocDirs...).Source != ""
		t.HasPlan = protocol.ResolveDesignDoc(ws.Root, f, "plan", designDocDirs...).Source != ""
		t.Depends = f.Depends
		t.Warnings = f.Warnings
		if s, err := ws.ReadState(f.ID); err == nil {
			if err := ws.ReconcileActive(f.ID, &s); err != nil {
				slog.Warn("failed to reconcile active state", "feature", f.ID, "error", err)
			}
			t.Phase = string(s.Phase)
			t.SubPhase = string(s.SubPhase)
			t.Role = string(s.Role)
			t.Round = s.Round
			t.Active = s.Active
			t.Pid = s.Pid
			t.Runner = s.Runner
			t.Runners = s.Runners
			if len(t.Runners) == 0 && s.Runner != "" {
				t.Runners = []string{s.Runner}
			}
			t.StopReason = s.StopReason
			t.StopMessage = s.StopMessage
			t.Profile = s.Profile
			if !s.CreatedAt.IsZero() {
				t.CreatedAt = s.CreatedAt.Format(time.RFC3339)
			}
			if !s.UpdatedAt.IsZero() {
				t.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
			}
		}
		if t.Profile == "" {
			t.Profile = f.Profile
		}
		tasks = append(tasks, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func handleOverview(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter) {
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}

	var designDocDirs []string
	if cfg, err := ws.ReadConfig(); err == nil {
		designDocDirs = cfg.DesignDocDirs
	}

	specDoc := protocol.ResolveDesignDoc(ws.Root, f, "spec", designDocDirs...)
	planDoc := protocol.ResolveDesignDoc(ws.Root, f, "plan", designDocDirs...)
	spec, specSource := specDoc.Content, specDoc.Source
	plan, planSource := planDoc.Content, planDoc.Source
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

func handleMessages(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter) {
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

	drr := readIfExists(filepath.Join(dir, protocol.DesignReviewReport))
	if drr != "" {
		messages = append(messages, messageInfo{
			Role:     "design-reviewer",
			Label:    protocol.DesignReviewReport,
			Content:  drr,
			File:     protocol.DesignReviewReport,
			Duration: phases[durationKey{"design-reviewer", 0}].duration,
			Model:    phases[durationKey{"design-reviewer", 0}].model,
		})
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

func buildPhaseInfo(ws *protocol.CachedWorkspace, featureID string) map[durationKey]phaseInfo {
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

// handleEvolveReport 回傳 .4x/evolve-report.md 內容（markdown 字串），供 dashboard surface
// 最近一輪 evolve pipeline 的發現/接受/拒絕/排入摘要。檔不存在時回 exists:false 與空 content。
func handleEvolveReport(ws *protocol.CachedWorkspace, w http.ResponseWriter) {
	content := readIfExists(filepath.Join(ws.DotDir(), protocol.EvolveReportFile))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Content string `json:"content"`
		Exists  bool   `json:"exists"`
	}{Content: content, Exists: content != ""})
}

func handleEvents(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter) {
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
