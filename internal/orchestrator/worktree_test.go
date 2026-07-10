package orchestrator

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// writeFeatureYAML 在指定 workspace 寫一份合法的 feature YAML，供 repos 合併測試使用。
func writeFeatureYAML(t *testing.T, ws *protocol.Workspace, f feature.Feature) {
	t.Helper()
	if err := ws.SaveFeature(f); err != nil {
		t.Fatalf("write feature YAML: %v", err)
	}
}

func TestSyncFeatureFromWorktree_Learnings(t *testing.T) {
	featureID := "feat-learnings-sync"
	wt := &protocol.Workspace{Root: t.TempDir()}
	main := &protocol.Workspace{Root: t.TempDir()}

	srcDir := wt.FeatureDir(featureID)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir worktree feature dir: %v", err)
	}
	retroContent := []byte(`[{"text":"keep it simple"}]`)
	if err := os.WriteFile(filepath.Join(srcDir, protocol.RetroLearningsFile), retroContent, 0o644); err != nil {
		t.Fatalf("write retro-learnings: %v", err)
	}

	if err := SyncFeatureFromWorktree(wt, main, featureID, 1); err != nil {
		t.Fatalf("SyncFeatureFromWorktree: %v", err)
	}

	dstDir := main.FeatureDir(featureID)
	got, err := os.ReadFile(filepath.Join(dstDir, protocol.RetroLearningsFile))
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

// TestSyncFeatureFromWorktree_ReposMergeBack 驗證 Designer 在 worktree 內對 repos 的
// 變更會合併回主工作區 YAML，且只覆寫 Repos 欄位——name/description 等非 repos 欄位
// 維持主工作區原值，不被 worktree 舊快照覆蓋。
func TestSyncFeatureFromWorktree_ReposMergeBack(t *testing.T) {
	featureID := "feat-repos-merge"
	wt := &protocol.Workspace{Root: t.TempDir()}
	main := &protocol.Workspace{Root: t.TempDir()}

	writeFeatureYAML(t, main, feature.Feature{
		ID:          featureID,
		Name:        "authoritative name",
		Description: "authoritative description",
		Status:      feature.StatusInProgress,
		Repos:       []string{"repoA"},
	})
	// worktree 快照：repos 已被 Designer 加上 repoB，且 name/description 為舊值（模擬過時快照）。
	writeFeatureYAML(t, wt, feature.Feature{
		ID:          featureID,
		Name:        "stale worktree name",
		Description: "stale worktree description",
		Status:      feature.StatusInProgress,
		Repos:       []string{"repoA", "repoB"},
	})

	if err := SyncFeatureFromWorktree(wt, main, featureID, 1); err != nil {
		t.Fatalf("SyncFeatureFromWorktree: %v", err)
	}

	got, err := main.LoadFeature(featureID)
	if err != nil {
		t.Fatalf("reload main feature: %v", err)
	}
	if !slices.Equal(got.Repos, []string{"repoA", "repoB"}) {
		t.Errorf("repos = %v, want [repoA repoB]", got.Repos)
	}
	if got.Name != "authoritative name" {
		t.Errorf("name = %q, want authoritative name (worktree snapshot must not overwrite)", got.Name)
	}
	if got.Description != "authoritative description" {
		t.Errorf("description = %q, want authoritative description (worktree snapshot must not overwrite)", got.Description)
	}
}

// TestSyncFeatureFromWorktree_ReposUnchangedNoRewrite 驗證 repos 相同時不觸發 SaveFeature，
// 避免 live sync 每 2s 無謂改寫主 YAML。以主 YAML bytes 呼叫前後不變為斷言（比 mtime 穩定）。
func TestSyncFeatureFromWorktree_ReposUnchangedNoRewrite(t *testing.T) {
	featureID := "feat-repos-unchanged"
	wt := &protocol.Workspace{Root: t.TempDir()}
	main := &protocol.Workspace{Root: t.TempDir()}

	feat := feature.Feature{
		ID:     featureID,
		Name:   "same",
		Status: feature.StatusInProgress,
		Repos:  []string{"repoA", "repoB"},
	}
	writeFeatureYAML(t, main, feat)
	writeFeatureYAML(t, wt, feat)

	mainYAML := filepath.Join(main.DotDir(), protocol.FeaturesDir, featureID+".yaml")
	before, err := os.ReadFile(mainYAML)
	if err != nil {
		t.Fatalf("read main YAML before: %v", err)
	}

	if err := SyncFeatureFromWorktree(wt, main, featureID, 1); err != nil {
		t.Fatalf("SyncFeatureFromWorktree: %v", err)
	}

	after, err := os.ReadFile(mainYAML)
	if err != nil {
		t.Fatalf("read main YAML after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("main YAML was rewritten despite repos unchanged\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
