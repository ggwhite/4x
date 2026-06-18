package protocol

import (
	"testing"
)

// TestWorktreePath_ScansBeyondFirstLines 驗證 WorktreePath 掃描整個 events.jsonl，
// 即使 worktree 事件被其他事件推到第 5 行之後仍能找到（F086 task 5：原本只看前 4 行）。
func TestWorktreePath_ScansBeyondFirstLines(t *testing.T) {
	ws := setupWorkspace(t)
	featureID := "feat-wt"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	// 前段塞多筆非 worktree 事件，把 worktree 事件推到第 5 行之後。
	for i := 0; i < 6; i++ {
		if err := ws.AppendEvent(featureID, Event{Type: "phase-start", Phase: PhaseCoding}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	want := "/tmp/worktrees/feat-wt"
	if err := ws.AppendEvent(featureID, Event{Type: "run-output", Detail: "worktree: " + want}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	if got := ws.WorktreePath(featureID); got != want {
		t.Errorf("WorktreePath = %q, want %q", got, want)
	}
}

// TestWorktreePath_ReturnsLast 驗證多筆 worktree 事件時回傳「最後一筆」（最近一次 run 的 worktree），
// 避免 re-run 後回到已被移除的舊 worktree（F086 task 5）。
func TestWorktreePath_ReturnsLast(t *testing.T) {
	ws := setupWorkspace(t)
	featureID := "feat-rerun"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	if err := ws.AppendEvent(featureID, Event{Type: "run-output", Detail: "worktree: /tmp/old/feat-rerun"}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := ws.AppendEvent(featureID, Event{Type: "transition", Phase: PhaseReviewing}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	want := "/tmp/new/feat-rerun"
	if err := ws.AppendEvent(featureID, Event{Type: "run-output", Detail: "worktree: " + want}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	if got := ws.WorktreePath(featureID); got != want {
		t.Errorf("WorktreePath = %q, want %q (should return latest)", got, want)
	}
}

// TestWorktreePath_NoWorktree 驗證無 worktree 事件或 events.jsonl 不存在時回傳空字串。
func TestWorktreePath_NoWorktree(t *testing.T) {
	ws := setupWorkspace(t)

	if got := ws.WorktreePath("feat-missing"); got != "" {
		t.Errorf("WorktreePath (no events file) = %q, want empty", got)
	}

	featureID := "feat-nowt"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	if err := ws.AppendEvent(featureID, Event{Type: "phase-start", Phase: PhaseCoding}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if got := ws.WorktreePath(featureID); got != "" {
		t.Errorf("WorktreePath (no worktree event) = %q, want empty", got)
	}
}
