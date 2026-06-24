package gitops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
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
