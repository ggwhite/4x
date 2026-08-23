package gitops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/vcshub"
)

// writeState 在 root 的 .4x/run/{id}/ 寫入指定 phase 的 state.json。
func writeState(t *testing.T, ws *protocol.Workspace, featureID string, phase protocol.Phase) {
	t.Helper()
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	s := protocol.State{FeatureID: featureID, Phase: phase, Round: 1, Active: true}
	if err := ws.WriteState(featureID, s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
}

// TestMergeAndFinalize_HappyPathFinalizesDone 驗證 AC-5/AC-7(a)：真實 worktree merge 成功且
// re-read 仍為 pending-review 時，共用編排把 state 推進到 done，且回傳 result 無 conflict/error/state-changed。
func TestMergeAndFinalize_HappyPathFinalizesDone(t *testing.T) {
	root, ws, ops := setupMonoWorkspace(t)
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}

	wtPath, err := ops.SetupWorktree("feat-done", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, "feat-done", "wip(feat-done): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	writeState(t, ws, "feat-done", protocol.PhasePendingReview)

	result, err := MergeAndFinalize(root, ws, cfg, "feat-done", "Test Feature")
	if err != nil {
		t.Fatalf("MergeAndFinalize: %v", err)
	}
	if result.Conflict || result.Error != "" || result.StateChanged {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.FinalState.Phase != protocol.PhaseDone {
		t.Errorf("FinalState.Phase = %q, want done", result.FinalState.Phase)
	}

	persisted, err := ws.ReadState("feat-done")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Phase != protocol.PhaseDone {
		t.Errorf("persisted phase = %q, want done", persisted.Phase)
	}
	if persisted.Active || persisted.StopReason != "done" {
		t.Errorf("persisted Active=%v StopReason=%q, want false/done", persisted.Active, persisted.StopReason)
	}
}

// TestMergeAndFinalize_StateChangedSkipsFinalize 驗證 AC-7(b)：merge 後 re-read 偵測到 phase
// 已非 pending-review 時，不 finalize、回報 StateChanged，且 state 維持原 phase（保留防 stale 覆寫的不變式）。
func TestMergeAndFinalize_StateChangedSkipsFinalize(t *testing.T) {
	root, ws, _ := setupMonoWorkspace(t)
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}

	// 無 worktree → Merge 回 Skipped（非 conflict、非 error），仍走到 re-read 守門。
	// root state 為 coding（非 pending-review），模擬 merge 期間 phase 已被其他程序改動。
	writeState(t, ws, "feat-stale", protocol.PhaseCoding)

	result, err := MergeAndFinalize(root, ws, cfg, "feat-stale", "Stale Feature")
	if err != nil {
		t.Fatalf("MergeAndFinalize: %v", err)
	}
	if !result.StateChanged {
		t.Fatalf("StateChanged = false, want true; result=%+v", result)
	}
	if result.FinalState.Phase != protocol.PhaseCoding {
		t.Errorf("FinalState.Phase = %q, want coding", result.FinalState.Phase)
	}

	persisted, err := ws.ReadState("feat-stale")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Phase != protocol.PhaseCoding {
		t.Errorf("persisted phase = %q, want coding (must not finalize to done)", persisted.Phase)
	}
}

// TestMergeAndFinalize_IssueTrackerEnabled_OpensMR 涵蓋 AC-13：cfg.IssueTracker.Enabled 為
// true 時走 PushAndOpenMR 而非 Merge；成功 push+開 MR 後同樣完成既有的 re-read → FinalizeDone
// 序列，FinalState.Phase 變成 done。
func TestMergeAndFinalize_IssueTrackerEnabled_OpensMR(t *testing.T) {
	root, ws, ops := setupMonoWorkspace(t)
	cfg := protocol.Config{
		Project:      protocol.ProjectConfig{Name: "test"},
		IssueTracker: protocol.IssueTrackerConfig{Enabled: true},
	}
	featureID := "feat-done-mr"

	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	if err := ops.CaptureBaseline(featureID, nil); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}
	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	addBareRemote(t, root)

	origVcshubNew := vcshubNew
	defer func() { vcshubNew = origVcshubNew }()
	vcshubNew = func(repoPath string) vcshub.Hub { return &fakeHub{} }

	writeState(t, ws, featureID, protocol.PhasePendingReview)

	result, err := MergeAndFinalize(root, ws, cfg, featureID, "Test Feature")
	if err != nil {
		t.Fatalf("MergeAndFinalize: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.MRUrls["."] == "" {
		t.Fatal("expected MRUrls[\".\"] to be set")
	}
	if result.FinalState.Phase != protocol.PhaseDone {
		t.Errorf("FinalState.Phase = %q, want done", result.FinalState.Phase)
	}
}

// TestMergeAndFinalize_IssueTrackerEnabled_PartialFailureSkipsFinalize 涵蓋 AC-21（D6）：
// multirepo + issue_tracker.enabled 時，某個 repo push/開 MR 失敗會讓 result.Error 非空，
// MergeAndFinalize 在既有的早退條件（Conflict || Error != ""）下不 FinalizeDone，
// FinalState 仍為 pending-review，且 worktree 保留供重試——杜絕「worktree 被清後重跑
// 被誤判 Skipped→靜默 done」的資料遺失漏洞。
func TestMergeAndFinalize_IssueTrackerEnabled_PartialFailureSkipsFinalize(t *testing.T) {
	root, ws, ops := setupMultiWorkspace(t)
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "gate"},
			},
		},
		IssueTracker: protocol.IssueTrackerConfig{Enabled: true},
	}
	featureID := "feat-done-mr-partial"

	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	if err := ops.CaptureBaseline(featureID, nil); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}
	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	for _, name := range []string{"core", "gate"} {
		if err := os.WriteFile(filepath.Join(wtPath, name, "new.go"), []byte("package "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// 只給 core 設 bare remote；gate push 會失敗，觸發 partial-failure。
	addBareRemote(t, filepath.Join(root, "core"))

	origVcshubNew := vcshubNew
	defer func() { vcshubNew = origVcshubNew }()
	vcshubNew = func(repoPath string) vcshub.Hub { return &fakeHub{} }

	writeState(t, ws, featureID, protocol.PhasePendingReview)

	result, err := MergeAndFinalize(root, ws, cfg, featureID, "Partial Feature")
	if err != nil {
		t.Fatalf("MergeAndFinalize: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected result.Error from partial failure")
	}

	persisted, err := ws.ReadState(featureID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Phase == protocol.PhaseDone {
		t.Error("persisted phase must not be done on partial failure")
	}
	if persisted.Phase != protocol.PhasePendingReview {
		t.Errorf("persisted phase = %q, want pending-review", persisted.Phase)
	}
	if _, err := os.Stat(Dir(root, featureID)); err != nil {
		t.Error("worktree should be preserved on partial failure for retry")
	}
}

// TestMergeAndFinalize_SelfManagedDirtyFinalizesDone 驗證 AC-8：端到端重現 F189 的實際失敗——
// state 為 pending-review、主工作區三個 4x 自管路徑皆 tracked-dirty 時，merge 前置 commit 讓
// preflight 不再誤擋，MergeAndFinalize 正常把 phase 推進到 done。
func TestMergeAndFinalize_SelfManagedDirtyFinalizesDone(t *testing.T) {
	root, ws, ops := setupMonoWorkspace(t)
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}
	seedSelfManaged(t, root, "feat-selfdirty")

	wtPath, err := ops.SetupWorktree("feat-selfdirty", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, "feat-selfdirty", "wip(feat-selfdirty): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	writeState(t, ws, "feat-selfdirty", protocol.PhasePendingReview)
	dirtySelfManaged(t, root, "feat-selfdirty")

	result, err := MergeAndFinalize(root, ws, cfg, "feat-selfdirty", "Self Managed Dirty Feature")
	if err != nil {
		t.Fatalf("MergeAndFinalize: %v", err)
	}
	if result.Error != "" || result.Conflict {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.FinalState.Phase != protocol.PhaseDone {
		t.Errorf("FinalState.Phase = %q, want done", result.FinalState.Phase)
	}

	persisted, err := ws.ReadState("feat-selfdirty")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Phase != protocol.PhaseDone {
		t.Errorf("persisted phase = %q, want done", persisted.Phase)
	}
}
