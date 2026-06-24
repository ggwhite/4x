package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/batch"
	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

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
	LastReport     *protocol.BatchReport   `json:"lastReport,omitempty"`
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
func mergedConfig(ws *protocol.CachedWorkspace) protocol.Config {
	cfg, _ := ws.LoadMergedConfig()
	return cfg
}

// handleBatchStatus 回傳目前 batch 執行狀態、依 PlanBatch schedule 排序的佇列，以及衝突信號（若有）。
// batch 在跑時讀已儲存的 batch-plan.json（這次 batch 的計畫）；
// 未跑時用 pending features 即時 plan（預覽下次會跑什麼）。
func handleBatchStatus(ws *protocol.CachedWorkspace, bm *BatchManager, w http.ResponseWriter) {
	cfg := mergedConfig(ws)

	features, err := ws.ListFeatures()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	featByID := make(map[string]feature.Feature, len(features))
	for _, f := range features {
		featByID[f.ID] = f
	}

	resp := batchStatusResponse{Running: bm.Running(), Queue: []batchQueueItem{}}

	var schedule []batch.ScheduleEntry

	if bm.Running() {
		if saved, err := loadSavedBatchPlan(ws.Workspace); err == nil && saved != nil {
			schedule = saved.Schedule
		}
	}
	if schedule == nil {
		var pending []feature.Feature
		for _, f := range features {
			if f.Status != feature.StatusDone && f.Status != feature.StatusAbandoned {
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
		if f.Status == feature.StatusDone || f.Status == feature.StatusReadyForReview {
			itemState = "done"
		} else if f.Status == feature.StatusNeedsAttention || f.Status == feature.StatusBlocked {
			itemState = "error"
		} else if st, stErr := ws.ReadState(s.FeatureID); stErr == nil {
			if err := ws.ReconcileActive(s.FeatureID, &st); err != nil {
				slog.Warn("failed to reconcile active state", "feature", s.FeatureID, "error", err)
			}
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

	// 附帶上次 batch 報告，前端在 batch 沒在跑時顯示摘要（讀不到就 nil，省一次請求）。
	if report, _ := ws.ReadBatchReport(); report != nil {
		resp.LastReport = report
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePostBatchStart 啟動 batch run；若仍有未解決的 batch-conflict.json 則回 409，要求先 Continue/解衝突。
func handlePostBatchStart(ws *protocol.CachedWorkspace, bm *BatchManager, w http.ResponseWriter, r *http.Request) {
	if conflict, _ := ws.ReadBatchConflict(); conflict != nil {
		writeJSONError(w, http.StatusConflict, "unresolved batch conflict — resolve and continue first")
		return
	}
	startBatch(ws, bm, w, r)
}

// handlePostBatchContinue 在使用者於 worktree 解完衝突後呼叫：先清掉衝突信號再重啟 batch run。
func handlePostBatchContinue(ws *protocol.CachedWorkspace, bm *BatchManager, w http.ResponseWriter, r *http.Request) {
	if err := ws.ClearBatchConflict(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	startBatch(ws, bm, w, r)
}

// startBatch 解析 body（runner/maxRounds，缺省用 config）並透過 BatchManager 啟動 batch run。
func startBatch(ws *protocol.CachedWorkspace, bm *BatchManager, w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength > 0 {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
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

// handlePostBatchReplan 重新產生 batch-plan.json（batch 沒在跑時才允許）。
func handlePostBatchReplan(ws *protocol.CachedWorkspace, bm *BatchManager, w http.ResponseWriter) {
	if bm.Running() {
		writeJSONError(w, http.StatusConflict, "batch is running — stop it first")
		return
	}
	cfg := mergedConfig(ws)
	features, err := ws.ListFeatures()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var pending []feature.Feature
	for _, f := range features {
		if f.Status != feature.StatusDone && f.Status != feature.StatusAbandoned {
			pending = append(pending, f)
		}
	}
	if len(pending) == 0 {
		writeJSONError(w, http.StatusBadRequest, "no pending features")
		return
	}
	plan, err := batch.PlanBatch(pending, protocol.EffectiveHubRepos(cfg), 4)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	planPath := filepath.Join(ws.DotDir(), "batch-plan.json")
	if err := os.WriteFile(planPath, data, 0o644); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
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
