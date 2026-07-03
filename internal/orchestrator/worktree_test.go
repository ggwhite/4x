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

// TestSyncFeatureFromWorktree_RoundSubdir 重現 ws-126 事故：Tester 把截圖寫進
// rounds/round-N/ 底下的任意子目錄（而非固定的 e2e/screenshots 特例路徑），
// round 收尾同步時該子目錄必須完整複製回主 repo，不能被舊版「只複製頂層檔案」的
// 白名單邏輯靜默丟棄——否則 4x done 清掉 worktree 後這些檔案就永久遺失。
func TestSyncFeatureFromWorktree_RoundSubdir(t *testing.T) {
	featureID := "feat-round-subdir-sync"
	wt := &protocol.Workspace{Root: t.TempDir()}
	main := &protocol.Workspace{Root: t.TempDir()}

	srcRound := wt.RoundDir(featureID, 1)
	srcScreens := filepath.Join(srcRound, "screenshots")
	if err := os.MkdirAll(srcScreens, 0o755); err != nil {
		t.Fatalf("mkdir round screenshots dir: %v", err)
	}
	content := []byte("fake-png-bytes")
	if err := os.WriteFile(filepath.Join(srcScreens, "page1-first-pay-search.png"), content, 0o644); err != nil {
		t.Fatalf("write screenshot: %v", err)
	}

	if err := SyncFeatureFromWorktree(wt, main, featureID, 1); err != nil {
		t.Fatalf("SyncFeatureFromWorktree: %v", err)
	}

	dstFile := filepath.Join(main.RoundDir(featureID, 1), "screenshots", "page1-first-pay-search.png")
	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("round subdir screenshot not synced back: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("screenshot content = %q, want %q", got, content)
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
