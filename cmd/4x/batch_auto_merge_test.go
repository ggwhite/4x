package main

import (
	"testing"

	"github.com/ggwhite/4x/internal/batch"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// F068 / M1：feature 完成（ready-for-review）後 auto-merge 成功 → 標記 done 並計入 completed。
func TestRunBatchSchedule_AutoMergeSuccessMarksDone(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]protocol.Status{"feat-a": "not-started"}

	runFeature := func(next string, f protocol.Feature, s protocol.State) (protocol.Status, error) {
		return protocol.StatusReadyForReview, nil
	}

	var merged []string
	autoMerge := func(featureID string) gitops.MergeResult {
		merged = append(merged, featureID)
		return gitops.MergeResult{} // 成功（無衝突、無錯誤）
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge)

	if len(merged) != 1 || merged[0] != "feat-a" {
		t.Fatalf("autoMerge called with %v, want [feat-a]", merged)
	}
	if statusMap["feat-a"] != protocol.StatusDone {
		t.Errorf("statusMap[feat-a] = %s, want done", statusMap["feat-a"])
	}
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
}

// F068 / M4：非 worktree 模式 merge 被 skipped 時，仍視為成功標記 done。
func TestRunBatchSchedule_AutoMergeSkippedMarksDone(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]protocol.Status{"feat-a": "not-started"}

	runFeature := func(next string, f protocol.Feature, s protocol.State) (protocol.Status, error) {
		return protocol.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{Skipped: true}
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge)

	if statusMap["feat-a"] != protocol.StatusDone {
		t.Errorf("statusMap[feat-a] = %s, want done", statusMap["feat-a"])
	}
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
}

// F068 / M2：auto-merge 衝突 → graceful pause。feature 保持 ready-for-review、不計入 completed、
// 後續 feature 不執行（主迴圈停止）。
func TestRunBatchSchedule_AutoMergeConflictPauses(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)
	batchTestFeature(t, ws, "feat-b", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	statusMap := map[string]protocol.Status{"feat-a": "not-started", "feat-b": "not-started"}

	var executed []string
	runFeature := func(next string, f protocol.Feature, s protocol.State) (protocol.Status, error) {
		executed = append(executed, next)
		return protocol.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{Conflict: true, Files: []string{"main.go"}}
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge)

	if len(executed) != 1 || executed[0] != "feat-a" {
		t.Fatalf("executed = %v, want only [feat-a] (paused before feat-b)", executed)
	}
	if statusMap["feat-a"] != protocol.StatusReadyForReview {
		t.Errorf("statusMap[feat-a] = %s, want ready-for-review (preserved)", statusMap["feat-a"])
	}
	if completed != 0 {
		t.Errorf("completed = %d, want 0 (conflict not counted)", completed)
	}
}

// F068 / M3：auto-merge 非衝突錯誤 → 警告後續跑下一個 feature；錯誤的 feature 仍算 completed。
func TestRunBatchSchedule_AutoMergeErrorContinues(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)
	batchTestFeature(t, ws, "feat-b", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	statusMap := map[string]protocol.Status{"feat-a": "not-started", "feat-b": "not-started"}

	var executed []string
	runFeature := func(next string, f protocol.Feature, s protocol.State) (protocol.Status, error) {
		executed = append(executed, next)
		return protocol.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		if featureID == "feat-a" {
			return gitops.MergeResult{Error: "git push failed"}
		}
		return gitops.MergeResult{}
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge)

	if len(executed) != 2 {
		t.Fatalf("executed = %v, want both features (error does not pause)", executed)
	}
	if statusMap["feat-a"] != protocol.StatusReadyForReview {
		t.Errorf("statusMap[feat-a] = %s, want ready-for-review (merge failed, preserved)", statusMap["feat-a"])
	}
	if statusMap["feat-b"] != protocol.StatusDone {
		t.Errorf("statusMap[feat-b] = %s, want done", statusMap["feat-b"])
	}
	if completed != 2 {
		t.Errorf("completed = %d, want 2 (both count: ready-for-review + done)", completed)
	}
}

// F068：--no-auto-merge → 完全不呼叫 autoMerge，feature 停在 ready-for-review（舊行為）但仍計入 completed。
func TestRunBatchSchedule_NoAutoMergeSkipsMerge(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]protocol.Status{"feat-a": "not-started"}

	runFeature := func(next string, f protocol.Feature, s protocol.State) (protocol.Status, error) {
		return protocol.StatusReadyForReview, nil
	}
	called := false
	autoMerge := func(featureID string) gitops.MergeResult {
		called = true
		return gitops.MergeResult{}
	}

	completed := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, true, autoMerge)

	if called {
		t.Error("autoMerge should not be called when noAutoMerge is true")
	}
	if statusMap["feat-a"] != protocol.StatusReadyForReview {
		t.Errorf("statusMap[feat-a] = %s, want ready-for-review (no auto-merge)", statusMap["feat-a"])
	}
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
}

// F068 / M6：衝突暫停後 resume——人工解完衝突跑 4x merge（feat-a 標 done）後重跑 batch，
// 應跳過已完成的 feat-a、繼續執行並 merge feat-b。
func TestRunBatchSchedule_ResumeAfterConflict(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)
	batchTestFeature(t, ws, "feat-b", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{
		{FeatureID: "feat-a"},
		{FeatureID: "feat-b"},
	}}
	statusMap := map[string]protocol.Status{"feat-a": "not-started", "feat-b": "not-started"}

	runFeature := func(next string, f protocol.Feature, s protocol.State) (protocol.Status, error) {
		return protocol.StatusReadyForReview, nil
	}

	// 第一輪：feat-a auto-merge 衝突 → pause，feat-b 未跑。
	conflictMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{Conflict: true, Files: []string{"main.go"}}
	}
	completed1 := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, conflictMerge)
	if completed1 != 0 {
		t.Fatalf("first run completed = %d, want 0 (paused at conflict)", completed1)
	}
	if statusMap["feat-b"] != "not-started" {
		t.Fatalf("feat-b should not have run in first batch, status = %s", statusMap["feat-b"])
	}

	// 人工解衝突 + 4x merge feat-a → 標記 done（模擬 resume 前置）。
	statusMap["feat-a"] = protocol.StatusDone

	// 第二輪：重跑 batch，feat-a 已 done 跳過，feat-b 正常 auto-merge 成功。
	var merged []string
	successMerge := func(featureID string) gitops.MergeResult {
		merged = append(merged, featureID)
		return gitops.MergeResult{}
	}
	completed2 := runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, successMerge)

	if len(merged) != 1 || merged[0] != "feat-b" {
		t.Fatalf("second run merged %v, want [feat-b] (feat-a already done)", merged)
	}
	if statusMap["feat-b"] != protocol.StatusDone {
		t.Errorf("statusMap[feat-b] = %s, want done", statusMap["feat-b"])
	}
	if completed2 != 1 {
		t.Errorf("second run completed = %d, want 1", completed2)
	}
}

// AC-7：auto-merge 衝突 graceful pause 時寫出 .4x/batch-conflict.json（含 featureId 與 files）。
func TestRunBatchSchedule_ConflictWritesSignal(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]protocol.Status{"feat-a": "not-started"}

	runFeature := func(next string, f protocol.Feature, s protocol.State) (protocol.Status, error) {
		return protocol.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{Conflict: true, Files: []string{"main.go"}, ConflictRepo: "core"}
	}

	runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge)

	conflict, err := ws.ReadBatchConflict()
	if err != nil {
		t.Fatalf("ReadBatchConflict: %v", err)
	}
	if conflict == nil {
		t.Fatal("expected batch-conflict.json after conflict pause, got nil")
	}
	if conflict.FeatureID != "feat-a" {
		t.Errorf("conflict.FeatureID = %s, want feat-a", conflict.FeatureID)
	}
	if len(conflict.Files) != 1 || conflict.Files[0] != "main.go" {
		t.Errorf("conflict.Files = %v, want [main.go]", conflict.Files)
	}
	if conflict.ConflictRepo != "core" {
		t.Errorf("conflict.ConflictRepo = %s, want core", conflict.ConflictRepo)
	}
}

// AC-8：成功 merge（無衝突）時清掉殘留的衝突信號，不留下 batch-conflict.json。
func TestRunBatchSchedule_SuccessClearsStaleSignal(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-a")
	batchTestFeature(t, ws, "feat-a", nil)

	// 模擬上一輪殘留的衝突信號。
	if err := ws.WriteBatchConflict(protocol.BatchConflict{FeatureID: "old"}); err != nil {
		t.Fatalf("WriteBatchConflict: %v", err)
	}

	plan := &batch.BatchPlan{Schedule: []batch.ScheduleEntry{{FeatureID: "feat-a"}}}
	statusMap := map[string]protocol.Status{"feat-a": "not-started"}

	runFeature := func(next string, f protocol.Feature, s protocol.State) (protocol.Status, error) {
		return protocol.StatusReadyForReview, nil
	}
	autoMerge := func(featureID string) gitops.MergeResult {
		return gitops.MergeResult{}
	}

	runBatchSchedule(ws, plan, statusMap, 5, "mock", runFeature, false, autoMerge)

	if conflict, _ := ws.ReadBatchConflict(); conflict != nil {
		t.Errorf("stale conflict signal should be cleared, got %+v", *conflict)
	}
}
