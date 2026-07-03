package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	Role       string  `json:"role"`
	Label      string  `json:"label"`
	Content    string  `json:"content"`
	File       string  `json:"file"`
	Round      int     `json:"round,omitempty"`
	Duration   int     `json:"duration,omitempty"` // seconds
	Model      string  `json:"model,omitempty"`
	TokensUsed int     `json:"tokensUsed,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
}

// buildDesignRoundMessages 讀取 design-rounds/round-<round>-<iteration>/ 底下歸檔的
// designer/design-reviewer artifact，依 round、iteration 由小到大組成 message 列表，
// 讓 design-reviewing FAIL 打回 designing 的每一輪都能在 dashboard 顯示出來。
// design-rounds/ 目錄不存在或沒有任何內容時回傳空 slice，呼叫端會退回舊行為。
func buildDesignRoundMessages(dir string, phases map[durationKey]phaseInfo) []messageInfo {
	roundsDir := filepath.Join(dir, protocol.DesignRoundsDir)
	entries, err := os.ReadDir(roundsDir)
	if err != nil {
		return nil
	}

	type cycle struct {
		round, iteration int
		path, name       string
	}
	var cycles []cycle
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var round, iteration int
		if _, err := fmt.Sscanf(entry.Name(), "round-%d-%d", &round, &iteration); err != nil {
			continue
		}
		cycles = append(cycles, cycle{round, iteration, filepath.Join(roundsDir, entry.Name()), entry.Name()})
	}
	sort.Slice(cycles, func(i, j int) bool {
		if cycles[i].round != cycles[j].round {
			return cycles[i].round < cycles[j].round
		}
		return cycles[i].iteration < cycles[j].iteration
	})

	var messages []messageInfo
	for _, c := range cycles {
		if content := readIfExists(filepath.Join(c.path, protocol.TaskBrief)); content != "" {
			p := phases[durationKey{"designer", c.round, c.iteration}]
			messages = append(messages, messageInfo{
				Role: "designer", Label: protocol.TaskBrief, Content: content,
				File:       filepath.Join(protocol.DesignRoundsDir, c.name, protocol.TaskBrief),
				Round:      c.round,
				Duration:   p.duration,
				Model:      p.model,
				TokensUsed: p.tokensUsed,
				CostUSD:    p.costUSD,
			})
		}
		if content := readIfExists(filepath.Join(c.path, protocol.Criteria)); content != "" {
			messages = append(messages, messageInfo{
				Role:    "designer",
				Label:   protocol.Criteria,
				Content: content,
				File:    filepath.Join(protocol.DesignRoundsDir, c.name, protocol.Criteria),
				Round:   c.round,
			})
		}
		if content := readIfExists(filepath.Join(c.path, protocol.DesignReviewReport)); content != "" {
			p := phases[durationKey{"design-reviewer", c.round, c.iteration}]
			messages = append(messages, messageInfo{
				Role: "design-reviewer", Label: protocol.DesignReviewReport, Content: content,
				File:       filepath.Join(protocol.DesignRoundsDir, c.name, protocol.DesignReviewReport),
				Round:      c.round,
				Duration:   p.duration,
				Model:      p.model,
				TokensUsed: p.tokensUsed,
				CostUSD:    p.costUSD,
			})
		}
	}
	return messages
}

func handleMessages(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter) {
	dir := ws.FeatureDir(featureID)
	phases := buildPhaseInfo(ws, featureID)
	var messages []messageInfo

	if designMsgs := buildDesignRoundMessages(dir, phases); len(designMsgs) > 0 {
		messages = append(messages, designMsgs...)
	} else {
		// design-rounds/ 目錄不存在或無內容：feature 是在這次修復之前完成/執行的舊資料，
		// 沒有逐輪歸檔，退回舊行為，直接讀 feature 目錄根目錄下的最新一份 artifact，
		// 避免既有 feature 的 message 區因為這次改動而變成空白。
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
					p := phases[durationKey{"designer", 0, 1}]
					mi.Duration = p.duration
					mi.Model = p.model
					mi.TokensUsed = p.tokensUsed
					mi.CostUSD = p.costUSD
				}
				messages = append(messages, mi)
			}
		}

		drr := readIfExists(filepath.Join(dir, protocol.DesignReviewReport))
		if drr != "" {
			dp := phases[durationKey{"design-reviewer", 0, 1}]
			messages = append(messages, messageInfo{
				Role:       "design-reviewer",
				Label:      protocol.DesignReviewReport,
				Content:    drr,
				File:       protocol.DesignReviewReport,
				Duration:   dp.duration,
				Model:      dp.model,
				TokensUsed: dp.tokensUsed,
				CostUSD:    dp.costUSD,
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
		if _, err := fmt.Sscanf(entry.Name(), "round-%d", &roundNum); err != nil {
			continue
		}
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
			{"fixer-report.md", "fixer"},
		} {
			content := readIfExists(filepath.Join(roundPath, f.name))
			if content != "" {
				rp := phases[durationKey{f.role, roundNum, 1}]
				if f.role == "deep-reviewer" {
					sp := phases[durationKey{"synthesizer", roundNum, 1}]
					rp.costUSD += sp.costUSD
					rp.tokensUsed += sp.tokensUsed
				}
				messages = append(messages, messageInfo{
					Role:       f.role,
					Label:      f.name,
					Content:    content,
					File:       filepath.Join(entry.Name(), f.name),
					Round:      roundNum,
					Duration:   rp.duration,
					Model:      rp.model,
					TokensUsed: rp.tokensUsed,
					CostUSD:    rp.costUSD,
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
			ap := phases[durationKey{"acceptor", s.Round, 1}]
			messages = append(messages, messageInfo{
				Role:       "acceptor",
				Label:      protocol.FinalReport,
				Content:    final,
				File:       protocol.FinalReport,
				Duration:   ap.duration,
				Model:      ap.model,
				TokensUsed: ap.tokensUsed,
				CostUSD:    ap.costUSD,
			})
		}
	}

	totalCost, _ := ws.TotalCost(featureID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messagesResponse{Messages: messages, TotalCostUSD: totalCost})
}

// messagesResponse 是 handleMessages 的回應形狀，供 handler 編碼與測試解碼共用，
// 避免形狀在兩處各寫一份日後漂移。
type messagesResponse struct {
	Messages     []messageInfo `json:"messages"`
	TotalCostUSD float64       `json:"totalCostUSD"`
}

type durationKey struct {
	role      string
	round     int
	iteration int
}

type phaseInfo struct {
	duration   int
	model      string
	tokensUsed int
	costUSD    float64
}

func buildPhaseInfo(ws *protocol.CachedWorkspace, featureID string) map[durationKey]phaseInfo {
	result := make(map[durationKey]phaseInfo)
	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}

	type eventEntry struct {
		Ts         string  `json:"ts"`
		Type       string  `json:"type"`
		Role       string  `json:"role"`
		Round      int     `json:"round"`
		Model      string  `json:"model"`
		TokensUsed int     `json:"tokens_used"`
		CostUSD    float64 `json:"cost_usd"`
	}

	starts := make(map[durationKey]time.Time)
	// iterCount 追蹤同一 (round, role) 累計出現過幾次 phase-start，讓 designer /
	// design-reviewer 這類在 round 不變時仍可能重複執行的 role，每一輪的
	// duration/cost/tokens 各自獨立記錄，不會被下一輪覆蓋（比照 logKeyFromEvent）。
	iterCount := make(map[string]int)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e eventEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		counterKey := fmt.Sprintf("%d-%s", e.Round, e.Role)
		switch e.Type {
		case "phase-start":
			iterCount[counterKey]++
			key := durationKey{e.Role, e.Round, iterCount[counterKey]}
			if t, err := time.Parse(time.RFC3339, e.Ts); err == nil {
				starts[key] = t
			}
			info := result[key]
			if e.Model != "" {
				info.model = e.Model
			}
			result[key] = info
		case "run-end":
			key := durationKey{e.Role, e.Round, iterCount[counterKey]}
			if start, ok := starts[key]; ok {
				if t, err := time.Parse(time.RFC3339, e.Ts); err == nil {
					info := result[key]
					info.duration = int(t.Sub(start).Seconds())
					if e.Model != "" {
						info.model = e.Model
					}
					if e.TokensUsed > 0 {
						info.tokensUsed += e.TokensUsed
					}
					if e.CostUSD > 0 {
						info.costUSD += e.CostUSD
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
