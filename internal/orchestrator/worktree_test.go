package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestSyncFeatureFromWorktree_Learnings(t *testing.T) {
	featureID := "feat-learnings-sync"
	wt := &protocol.Workspace{Root: t.TempDir()}
	main := &protocol.Workspace{Root: t.TempDir()}

	srcDir := wt.FeatureDir(featureID)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir worktree feature dir: %v", err)
	}
	selContent := []byte(`{"selected":["L1","L2"]}`)
	retroContent := []byte(`[{"text":"keep it simple"}]`)
	if err := os.WriteFile(filepath.Join(srcDir, protocol.SelectedLearningsFile), selContent, 0o644); err != nil {
		t.Fatalf("write selected-learnings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, protocol.RetroLearningsFile), retroContent, 0o644); err != nil {
		t.Fatalf("write retro-learnings: %v", err)
	}

	if err := SyncFeatureFromWorktree(wt, main, featureID, 1); err != nil {
		t.Fatalf("SyncFeatureFromWorktree: %v", err)
	}

	dstDir := main.FeatureDir(featureID)
	got, err := os.ReadFile(filepath.Join(dstDir, protocol.SelectedLearningsFile))
	if err != nil {
		t.Fatalf("selected-learnings not synced back: %v", err)
	}
	if string(got) != string(selContent) {
		t.Errorf("selected-learnings content = %q, want %q", got, selContent)
	}
	got, err = os.ReadFile(filepath.Join(dstDir, protocol.RetroLearningsFile))
	if err != nil {
		t.Fatalf("retro-learnings not synced back: %v", err)
	}
	if string(got) != string(retroContent) {
		t.Errorf("retro-learnings content = %q, want %q", got, retroContent)
	}
}

func TestSyncFeatureToWorktree_SelectedLearnings(t *testing.T) {
	featureID := "feat-learnings-push"
	main := &protocol.Workspace{Root: t.TempDir()}
	wt := &protocol.Workspace{Root: t.TempDir()}

	srcDir := main.FeatureDir(featureID)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir main feature dir: %v", err)
	}
	selContent := []byte(`{"selected":["L9"]}`)
	if err := os.WriteFile(filepath.Join(srcDir, protocol.SelectedLearningsFile), selContent, 0o644); err != nil {
		t.Fatalf("write selected-learnings: %v", err)
	}

	SyncFeatureToWorktree(main, wt, featureID, 0)

	got, err := os.ReadFile(filepath.Join(wt.FeatureDir(featureID), protocol.SelectedLearningsFile))
	if err != nil {
		t.Fatalf("selected-learnings not pushed to worktree: %v", err)
	}
	if string(got) != string(selContent) {
		t.Errorf("selected-learnings content = %q, want %q", got, selContent)
	}
}
