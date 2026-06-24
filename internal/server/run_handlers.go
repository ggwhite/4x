package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

type taskInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Phase      string   `json:"phase"`
	SubPhase   string   `json:"subPhase,omitempty"`
	Role       string   `json:"role"`
	Round      int      `json:"round"`
	Active     bool     `json:"active"`
	Pid        int      `json:"pid,omitempty"`
	Runner     string   `json:"runner"`
	Runners    []string `json:"runners,omitempty"`
	StopReason  string   `json:"stopReason,omitempty"`
	StopMessage string   `json:"stopMessage,omitempty"`
	Priority   *int     `json:"priority,omitempty"`
	Profile    string   `json:"profile,omitempty"`
	HasSpec    bool     `json:"hasSpec,omitempty"`
	HasPlan    bool     `json:"hasPlan,omitempty"`
	Depends    []string `json:"depends,omitempty"`
	CreatedAt  string   `json:"createdAt,omitempty"`
	UpdatedAt  string   `json:"updatedAt,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
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

type runRequest struct {
	FeatureID string             `json:"featureId"`
	Runner    string             `json:"runner"` // 保留向後相容（全域 --runner）
	Profile   string             `json:"profile,omitempty"`
	Overrides []phaseOverrideReq `json:"overrides,omitempty"`
	MaxRounds int                `json:"maxRounds"`
}

// phaseOverrideReq 是 dashboard 送來的單筆 per-phase 臨時覆寫，只影響本次 run、不落地。
type phaseOverrideReq struct {
	Phase  string `json:"phase"`
	Runner string `json:"runner,omitempty"`
	Model  string `json:"model,omitempty"`
}

func handlePostRun(ws *protocol.CachedWorkspace, pm *ProcessManager, w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.FeatureID == "" {
		http.Error(w, "featureId required", http.StatusBadRequest)
		return
	}
	feature, err := ws.LoadFeature(req.FeatureID)
	if err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}
	if s, err := ws.ReadState(req.FeatureID); err == nil {
		if s.Active && protocol.ProcessAlive(s.Pid) {
			http.Error(w, fmt.Sprintf("feature %s is already running (pid %d)", req.FeatureID, s.Pid), http.StatusConflict)
			return
		}
	}

	cfg := mergedConfig(ws)
	// 驗證 profile（若非空，沿用 CLI 早期驗證語意：存在 + 含 coding phase）。
	if req.Profile != "" {
		if _, _, err := protocol.ResolveProfile(cfg, feature, req.Profile); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	// 驗證每筆 override 的 phase 屬於可選白名單。
	for _, ov := range req.Overrides {
		if !protocol.IsSelectablePhase(protocol.Phase(ov.Phase)) {
			http.Error(w, fmt.Sprintf("invalid override phase %q", ov.Phase), http.StatusBadRequest)
			return
		}
	}

	if req.Runner == "" {
		req.Runner = cfg.Default
	}
	if req.MaxRounds <= 0 {
		req.MaxRounds = 5
	}

	info, err := pm.Start(req.FeatureID, req.Runner, req.MaxRounds, req.Profile, req.Overrides)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

type runPreviewRequest struct {
	FeatureID string             `json:"featureId"`
	Profile   string             `json:"profile,omitempty"`
	Overrides []phaseOverrideReq `json:"overrides,omitempty"`
}

// handlePostRunPreview 解析給定 profile + 臨時覆寫下的完整 pipeline 並回傳 []protocol.ResolvedPhase，
// 供 dashboard run dialog 顯示每個 phase 合併所有覆寫層級後的最終 runner/model。
// 與實際 run loop 共用 protocol.ResolvePipeline，確保預覽與執行結果一致。
// 解析錯誤（unknown profile/runner、bad phase）回 400 並帶訊息。
func handlePostRunPreview(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
	var req runPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.FeatureID == "" {
		http.Error(w, "featureId required", http.StatusBadRequest)
		return
	}
	feature, err := ws.LoadFeature(req.FeatureID)
	if err != nil {
		http.Error(w, "feature not found", http.StatusNotFound)
		return
	}

	overridesMap := make(map[protocol.Phase]protocol.PhaseSpec, len(req.Overrides))
	for _, ov := range req.Overrides {
		phase := protocol.Phase(ov.Phase)
		if !protocol.IsSelectablePhase(phase) {
			http.Error(w, fmt.Sprintf("invalid override phase %q", ov.Phase), http.StatusBadRequest)
			return
		}
		overridesMap[phase] = protocol.PhaseSpec{Phase: ov.Phase, Runner: ov.Runner, Model: ov.Model}
	}

	cfg := mergedConfig(ws)
	pipeline, err := protocol.ResolvePipeline(cfg, feature, req.Profile, "", overridesMap)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if pipeline == nil {
		pipeline = []protocol.ResolvedPhase{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pipeline); err != nil {
		slog.Error("encode run preview pipeline", "error", err)
	}
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
	Name        string            `json:"name"`
	Description string            `json:"description"`
	CustomID    string            `json:"customId,omitempty"`
	Subtasks    []feature.Subtask `json:"subtasks,omitempty"`
	Rules       []string          `json:"rules,omitempty"`
	Depends     []string          `json:"depends,omitempty"`
	Priority    *int              `json:"priority,omitempty"`
}

type newResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func handlePostNew(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
	var req newRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	// 與 CLI 共用 feature.Create；*protocol.CachedWorkspace 隱式滿足 feature.Store。
	// 只傳 name(+description) 的舊 request 維持向下相容（其餘欄位為零值）。
	f, err := feature.Create(ws, feature.CreateOpts{
		Name:        req.Name,
		Description: req.Description,
		CustomID:    req.CustomID,
		Subtasks:    req.Subtasks,
		Rules:       req.Rules,
		Depends:     req.Depends,
		Priority:    req.Priority,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newResponse{ID: f.ID, Name: f.Name})
}
