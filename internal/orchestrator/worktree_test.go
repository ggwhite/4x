package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
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

// TestSyncFeatureFromWorktree_SharedPathsRoundTrip 涵蓋 F189：Designer 在 worktree 內宣告的
// shared_paths 必須合併回主工作區（repos 未變也要合併），且下一次 SyncFeatureToWorktree
// 以主工作區 YAML 覆寫 worktree 時不得把它洗掉——否則 4x done 這類讀主工作區 YAML 的
// 消費端拿到的 SharedPaths 恆為空。
func TestSyncFeatureFromWorktree_SharedPathsRoundTrip(t *testing.T) {
	featureID := "feat-sharedpaths-roundtrip"
	wt := &protocol.Workspace{Root: t.TempDir()}
	main := &protocol.Workspace{Root: t.TempDir()}

	writeFeatureYAML(t, main, feature.Feature{
		ID:          featureID,
		Name:        "authoritative name",
		Description: "authoritative description",
		Status:      feature.StatusInProgress,
		Repos:       []string{"repoA"},
	})
	// worktree 快照：Designer 只加了 shared_paths（repos 維持原值），name/description 為過時值。
	writeFeatureYAML(t, wt, feature.Feature{
		ID:          featureID,
		Name:        "stale worktree name",
		Description: "stale worktree description",
		Status:      feature.StatusInProgress,
		Repos:       []string{"repoA"},
		SharedPaths: []string{"Dockerfile", "docker-compose.yml"},
	})

	if err := SyncFeatureFromWorktree(wt, main, featureID, 1); err != nil {
		t.Fatalf("SyncFeatureFromWorktree: %v", err)
	}

	got, err := main.LoadFeature(featureID)
	if err != nil {
		t.Fatalf("reload main feature: %v", err)
	}
	if !slices.Equal(got.SharedPaths, []string{"Dockerfile", "docker-compose.yml"}) {
		t.Errorf("shared_paths = %v, want [Dockerfile docker-compose.yml]", got.SharedPaths)
	}
	if !slices.Equal(got.Repos, []string{"repoA"}) {
		t.Errorf("repos = %v, want [repoA]", got.Repos)
	}
	if got.Name != "authoritative name" || got.Description != "authoritative description" {
		t.Errorf("worktree snapshot overwrote authoritative fields: name=%q description=%q", got.Name, got.Description)
	}

	// 下一個 role 起跑前的反向 sync：worktree YAML 被主工作區覆寫，shared_paths 須存活。
	SyncFeatureToWorktree(main, wt, featureID, 1)
	wtGot, err := wt.LoadFeature(featureID)
	if err != nil {
		t.Fatalf("reload worktree feature: %v", err)
	}
	if !slices.Equal(wtGot.SharedPaths, []string{"Dockerfile", "docker-compose.yml"}) {
		t.Errorf("shared_paths after SyncFeatureToWorktree = %v, want [Dockerfile docker-compose.yml]", wtGot.SharedPaths)
	}
}

// TestSyncFeatureFromWorktree_SharedPathsUnchangedNoRewrite 驗證 repos 與 shared_paths 都相同時
// 不觸發 SaveFeature——live sync 每 2s 呼叫一次，無謂改寫會污染主 YAML 的 mtime / cache。
func TestSyncFeatureFromWorktree_SharedPathsUnchangedNoRewrite(t *testing.T) {
	featureID := "feat-sharedpaths-unchanged"
	wt := &protocol.Workspace{Root: t.TempDir()}
	main := &protocol.Workspace{Root: t.TempDir()}

	feat := feature.Feature{
		ID:          featureID,
		Name:        "same",
		Status:      feature.StatusInProgress,
		Repos:       []string{"repoA"},
		SharedPaths: []string{"Dockerfile"},
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
		t.Errorf("main YAML was rewritten despite repos/shared_paths unchanged\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestSyncFeatureFromWorktree_SharedPathsRemoval 驗證 Designer 於 worktree 移除 shared_paths 時，
// 主工作區同步清空——合併語意對稱，不能只允許新增而讓已移除的路徑殘留在主 YAML。
func TestSyncFeatureFromWorktree_SharedPathsRemoval(t *testing.T) {
	featureID := "feat-sharedpaths-removal"
	wt := &protocol.Workspace{Root: t.TempDir()}
	main := &protocol.Workspace{Root: t.TempDir()}

	writeFeatureYAML(t, main, feature.Feature{
		ID:          featureID,
		Name:        "same",
		Status:      feature.StatusInProgress,
		Repos:       []string{"repoA"},
		SharedPaths: []string{"Dockerfile", "docker-compose.yml"},
	})
	writeFeatureYAML(t, wt, feature.Feature{
		ID:     featureID,
		Name:   "same",
		Status: feature.StatusInProgress,
		Repos:  []string{"repoA"},
	})

	if err := SyncFeatureFromWorktree(wt, main, featureID, 1); err != nil {
		t.Fatalf("SyncFeatureFromWorktree: %v", err)
	}

	got, err := main.LoadFeature(featureID)
	if err != nil {
		t.Fatalf("reload main feature: %v", err)
	}
	if len(got.SharedPaths) != 0 {
		t.Errorf("shared_paths = %v, want empty after worktree removal", got.SharedPaths)
	}
}

// sharedPathsGit 在 dir 執行 git 子命令，失敗即 t.Fatalf。
func sharedPathsGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

// TestSharedPaths_DeclaredDuringRunSurvivesDone 端到端鎖住「Designer 於 designing 才宣告」
// 這條鏈路：主工作區 YAML 一開始不含 shared_paths，宣告寫在 worktree 側，經
// SyncFeatureFromWorktree 合併回主工作區、UpsertSharedPathsBaseline 取樣，
// 再由 Merge 把 worktree 內的改動 merge-back 進主工作區與 root repo 的 HEAD。
//
// F189 的三個測試只驗到 sync 本身、AC-2/AC-10 的 fixture 則預先把宣告寫進主工作區 YAML，
// 只有本測試把「宣告 → sync → 基線 → merge-back」整條串起來。
// 放在 orchestrator package：internal/orchestrator 已 import internal/gitops，
// 反向放進 gitops 會造成 import 循環。
func TestSharedPaths_DeclaredDuringRunSurvivesDone(t *testing.T) {
	const composeFile = "docker-compose.yml"
	featureID := "feat-sp-declared-during-run"

	root := t.TempDir()
	repoDir := filepath.Join(root, "core")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	sharedPathsGit(t, repoDir, "init")
	sharedPathsGit(t, repoDir, "config", "user.name", "test")
	sharedPathsGit(t, repoDir, "config", "user.email", "test@test")
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	sharedPathsGit(t, repoDir, "add", "--", "main.go")
	sharedPathsGit(t, repoDir, "commit", "-m", "init")

	if err := os.WriteFile(filepath.Join(root, composeFile), []byte("services:\n  app:\n    image: base\n"), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	sharedPathsGit(t, root, "init")
	sharedPathsGit(t, root, "config", "user.name", "test")
	sharedPathsGit(t, root, "config", "user.email", "test@test")
	sharedPathsGit(t, root, "add", "--", composeFile)
	sharedPathsGit(t, root, "commit", "-m", "add compose")

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "sp-test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{"core": {Path: "core"}},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("protocol.Init: %v", err)
	}
	main := &protocol.Workspace{Root: root}
	// 主工作區 YAML 一開始沒有 shared_paths——宣告是 Designer 在 designing phase 才寫的。
	writeFeatureYAML(t, main, feature.Feature{
		ID: featureID, Name: "Declared During Run", Description: "desc",
		Status: feature.StatusInProgress,
	})

	ops := gitops.New(root, main, cfg)
	wtDir, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if _, err := os.Stat(gitops.SharedPathsBaselineFile(root, featureID)); !os.IsNotExist(err) {
		t.Fatalf("baseline must not exist before the declaration: %v", err)
	}

	// role 起跑前 orchestrator 會先把主工作區的 feature 檔同步進 worktree。
	wt := &protocol.Workspace{Root: wtDir}
	SyncFeatureToWorktree(main, wt, featureID, 0)

	// Designer 在 worktree 側寫下宣告（templates/designer.md.tmpl 的行為）。
	wtFeat, err := wt.LoadFeature(featureID)
	if err != nil {
		t.Fatalf("load worktree feature: %v", err)
	}
	wtFeat.SharedPaths = []string{composeFile}
	writeFeatureYAML(t, wt, wtFeat)

	if err := SyncFeatureFromWorktree(wt, main, featureID, 1); err != nil {
		t.Fatalf("SyncFeatureFromWorktree: %v", err)
	}
	mainFeat, err := main.LoadFeature(featureID)
	if err != nil {
		t.Fatalf("reload main feature: %v", err)
	}
	if !slices.Equal(mainFeat.SharedPaths, []string{composeFile}) {
		t.Fatalf("shared_paths not merged back to main YAML: %v", mainFeat.SharedPaths)
	}
	if err := gitops.UpsertSharedPathsBaseline(root, featureID, mainFeat.SharedPaths); err != nil {
		t.Fatalf("UpsertSharedPathsBaseline: %v", err)
	}

	const coderVersion = "services:\n  app:\n    image: declared-during-run\n"
	if err := os.WriteFile(filepath.Join(wtDir, composeFile), []byte(coderVersion), 0o644); err != nil {
		t.Fatalf("write worktree compose: %v", err)
	}

	result := ops.Merge(featureID, "Declared During Run")
	if result.Error != "" {
		t.Fatalf("merge error: %s", result.Error)
	}
	if len(result.SharedPathsMerged) == 0 {
		t.Fatalf("SharedPathsMerged should be non-empty, notes: %v", result.SharedPathsNotes)
	}
	got, err := os.ReadFile(filepath.Join(root, composeFile))
	if err != nil {
		t.Fatalf("read main compose: %v", err)
	}
	if string(got) != coderVersion {
		t.Errorf("main workspace %s = %q, want %q", composeFile, got, coderVersion)
	}
	out, err := exec.Command("git", "-C", root, "show", "HEAD:"+composeFile).Output()
	if err != nil {
		t.Fatalf("git show HEAD:%s: %v", composeFile, err)
	}
	if string(out) != coderVersion {
		t.Errorf("root repo HEAD:%s = %q, want %q", composeFile, out, coderVersion)
	}
}
