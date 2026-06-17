package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// setupMultiWorkspace 建立含兩個子 git repo 的 multi-repo workspace。
// 回傳 workspace root、Workspace pointer 及 Ops 實作。
func setupMultiWorkspace(t *testing.T) (root string, ws *protocol.Workspace, ops Ops) {
	t.Helper()
	root = t.TempDir()

	for _, name := range []string{"core", "gate"} {
		initGitRepo(t, filepath.Join(root, name))
	}

	os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26\nuse ./core\nuse ./gate\n"), 0o644)

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "multi-test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "gate"},
			},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws = &protocol.Workspace{Root: root}
	ops = New(root, ws, cfg)
	return
}

func TestMultiRepo_IsMultiRepo(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	if !ops.IsMultiRepo() {
		t.Error("multiRepo.IsMultiRepo() should return true")
	}
}

func TestMultiRepo_SetupWorktree(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	expected := Dir(root, "feat-1")
	if wtPath != expected {
		t.Errorf("wtPath = %q, want %q", wtPath, expected)
	}

	for _, name := range []string{"core", "gate"} {
		repoDir := filepath.Join(wtPath, name)
		if _, err := os.Stat(repoDir); err != nil {
			t.Errorf("repo %s should exist in worktree: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(wtPath, "go.work")); err != nil {
		t.Error("go.work should be copied to worktree")
	}
	dotCfg := filepath.Join(wtPath, protocol.DirName, protocol.ConfigFile)
	if _, err := os.Stat(dotCfg); err != nil {
		t.Error(".4x/settings.json should be copied to worktree")
	}
}

func TestMultiRepo_SetupWorktree_Idempotent(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	wtPath1, err := ops.SetupWorktree("feat-idem2")
	if err != nil {
		t.Fatalf("first SetupWorktree: %v", err)
	}
	wtPath2, err := ops.SetupWorktree("feat-idem2")
	if err != nil {
		t.Fatalf("second SetupWorktree (idempotent): %v", err)
	}
	if wtPath1 != wtPath2 {
		t.Errorf("paths differ: %q != %q", wtPath1, wtPath2)
	}
}

func TestMultiRepo_SetupWorktree_CleanupPartialOnFailure(t *testing.T) {
	root, ws, _ := setupMultiWorkspace(t)
	_ = ws

	// 第三個 repo 目錄存在但不是 git repo，讓 worktree add 失敗以觸發 cleanupPartial
	badRepoDir := filepath.Join(root, "bad")
	os.MkdirAll(badRepoDir, 0o755)

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "multi-test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "gate"},
				"bad":  {Path: "bad"},
			},
		},
	}
	ops2 := New(root, &protocol.Workspace{Root: root}, cfg)

	_, err := ops2.SetupWorktree("feat-partial")
	if err == nil {
		t.Fatal("SetupWorktree should fail when one repo is invalid")
	}

	// cleanupPartial 應移除已建立的 worktree 目錄
	wtDir := Dir(root, "feat-partial")
	if _, statErr := os.Stat(wtDir); !os.IsNotExist(statErr) {
		t.Error("worktree dir should be removed by cleanupPartial on failure")
	}

	// 已建立的 repo worktree branch 應被清理
	for _, name := range []string{"core", "gate"} {
		repoPath := filepath.Join(root, name)
		out, _ := exec.Command("git", "-C", repoPath, "branch", "--list", "4x/feat-partial").Output()
		if len(out) > 0 {
			t.Errorf("branch 4x/feat-partial in %s should be cleaned up after partial failure", name)
		}
	}
}

func TestMultiRepo_Commit(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-commit")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// 在 core worktree 寫入變更
	os.WriteFile(filepath.Join(wtPath, "core", "new.go"), []byte("package core\n"), 0o644)

	if err := ops.Commit(wtPath, "feat-commit", "wip(feat-commit): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// core 應有新 commit，gate 沒有（無變更）
	coreLog := gitOutput(filepath.Join(wtPath, "core"), "log", "--oneline", "-1")
	if coreLog == "" {
		t.Error("core should have at least one commit")
	}
	gateLog := gitOutput(filepath.Join(wtPath, "gate"), "log", "--oneline", "-1")
	if gateLog == "" {
		t.Error("gate should have at least the initial commit")
	}
}

func TestMultiRepo_Commit_NoChanges(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-nochange2")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := ops.Commit(wtPath, "feat-nochange2", "wip(feat-nochange2): round 1"); err != nil {
		t.Errorf("Commit with no changes should not fail: %v", err)
	}
}

func TestMultiRepo_MergeHappyPath(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-happy")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	os.WriteFile(filepath.Join(wtPath, "core", "feature.go"), []byte("package core\n"), 0o644)
	os.WriteFile(filepath.Join(wtPath, "gate", "feature.go"), []byte("package gate\n"), 0o644)

	if err := ops.Commit(wtPath, "feat-happy", "wip(feat-happy): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	result := ops.Merge("feat-happy", "Happy Path Feature")
	if result.Conflict {
		t.Fatalf("unexpected conflict in repo %q: %v", result.ConflictRepo, result.Files)
	}
	if result.Error != "" {
		t.Fatalf("merge error: %q", result.Error)
	}
	if result.Skipped {
		t.Error("merge should not be skipped")
	}

	// worktree 目錄應已清除
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed after successful merge")
	}

	// 兩個 main repo 應有合併後的檔案
	for _, name := range []string{"core", "gate"} {
		merged := filepath.Join(root, name, "feature.go")
		if _, err := os.Stat(merged); err != nil {
			t.Errorf("feature.go should exist in main %s repo after merge: %v", name, err)
		}
	}
}

// TestMultiRepo_MergeAllOrNothingRollback 驗證 all-or-nothing：
// 一個 repo merge 衝突時，已成功 merge 的 repo 應 reset 回原始 HEAD。
func TestMultiRepo_MergeAllOrNothingRollback(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)

	// 先在兩個 main repo 各建立一個會衝突的基礎
	for _, name := range []string{"core", "gate"} {
		repoPath := filepath.Join(root, name)
		os.WriteFile(filepath.Join(repoPath, "conflict.go"), []byte(fmt.Sprintf("package %s\n// original\n", name)), 0o644)
		runGit(t, repoPath, "add", ".")
		runGit(t, repoPath, "commit", "-m", "add conflict.go to main")
	}

	wtPath, err := ops.SetupWorktree("feat-conflict")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// 在兩個 worktree 中都修改 conflict.go（產生衝突）
	for _, name := range []string{"core", "gate"} {
		content := fmt.Sprintf("package %s\n// worktree version\n", name)
		os.WriteFile(filepath.Join(wtPath, name, "conflict.go"), []byte(content), 0o644)
	}
	if err := ops.Commit(wtPath, "feat-conflict", "wip(feat-conflict): round 1"); err != nil {
		t.Fatalf("Commit in worktrees: %v", err)
	}

	// 在兩個 main repo 再做不同修改（讓所有 repo 都有衝突，排除非確定性）
	for _, name := range []string{"core", "gate"} {
		repoPath := filepath.Join(root, name)
		content := fmt.Sprintf("package %s\n// main different version\n", name)
		os.WriteFile(filepath.Join(repoPath, "conflict.go"), []byte(content), 0o644)
		runGit(t, repoPath, "add", ".")
		runGit(t, repoPath, "commit", "-m", "update conflict.go in main")
	}

	// 記錄 merge 前各 main repo 的 HEAD
	preHeads := make(map[string]string)
	for _, name := range []string{"core", "gate"} {
		preHeads[name] = gitOutput(filepath.Join(root, name), "rev-parse", "HEAD")
	}

	result := ops.Merge("feat-conflict", "Conflict Test Feature")
	if !result.Conflict {
		t.Fatalf("expected conflict, got result: %+v", result)
	}
	if result.ConflictRepo == "" {
		t.Error("ConflictRepo should be set")
	}

	// all-or-nothing：兩個 main repo 的 HEAD 都應回到 merge 前
	for _, name := range []string{"core", "gate"} {
		afterHead := gitOutput(filepath.Join(root, name), "rev-parse", "HEAD")
		if afterHead != preHeads[name] {
			t.Errorf("%s HEAD changed after failed merge: was %s, now %s", name, preHeads[name], afterHead)
		}
	}
}

func TestMultiRepo_Cleanup(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-clean2")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := ops.Cleanup("feat-clean2"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed after Cleanup")
	}
	for _, name := range []string{"core", "gate"} {
		repoPath := filepath.Join(root, name)
		out, _ := exec.Command("git", "-C", repoPath, "branch", "--list", "4x/feat-clean2").Output()
		if len(out) > 0 {
			t.Errorf("branch 4x/feat-clean2 in %s should be deleted after Cleanup", name)
		}
	}
}

// TestMultiRepo_Cleanup_OrphanedWorktree 驗證 Cleanup 能清理不在當前 config 裡的殘留 worktree。
func TestMultiRepo_Cleanup_OrphanedWorktree(t *testing.T) {
	root, ws, _ := setupMultiWorkspace(t)

	// 用完整 3-repo config 建立 worktree
	orphanRepo := filepath.Join(root, "orphan")
	initGitRepo(t, orphanRepo)

	fullCfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "multi-test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core":   {Path: "core"},
				"gate":   {Path: "gate"},
				"orphan": {Path: "orphan"},
			},
		},
	}
	fullOps := New(root, ws, fullCfg)
	wtPath, err := fullOps.SetupWorktree("feat-orphan")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// 用只有 core+gate 的 config 做 Cleanup（orphan 不在 config 裡）
	partialCfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "multi-test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "gate"},
			},
		},
	}
	partialOps := New(root, ws, partialCfg)
	if err := partialOps.Cleanup("feat-orphan"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree dir should be fully removed including orphaned repos")
	}

	// orphan repo 的 branch 也應被清理
	out, _ := exec.Command("git", "-C", orphanRepo, "branch", "--list", "4x/feat-orphan").Output()
	if len(out) > 0 {
		t.Error("orphaned repo's feature branch should be deleted")
	}
}

func TestMultiRepo_DetectChangedRepos(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	changed := ops.DetectChangedRepos()
	if len(changed) != 0 {
		t.Errorf("expected no changes on fresh repos, got %v", changed)
	}
}

func TestMultiRepo_CaptureBaseline(t *testing.T) {
	_, ws, ops := setupMultiWorkspace(t)
	if err := ws.InitFeatureDir("feat-base2"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	if err := ops.CaptureBaseline("feat-base2", []string{"core", "gate"}); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-base2"), protocol.BaselineFile))
	if err != nil {
		t.Fatal(err)
	}
	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if len(baseline.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(baseline.Repos))
	}
	for _, repo := range baseline.Repos {
		if repo.Head == "" {
			t.Errorf("repo %s HEAD should not be empty", repo.Name)
		}
		if repo.Branch == "" {
			t.Errorf("repo %s Branch should not be empty", repo.Name)
		}
	}
}

// TestMultiRepo_MergeBranchMissingInSomeRepos 驗證部分 repo 不存在 feature branch 時，
// merge 應跳過該 repo 而非回報錯誤。
func TestMultiRepo_MergeBranchMissingInSomeRepos(t *testing.T) {
	root, ws, _ := setupMultiWorkspace(t)
	_ = ws

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "multi-test"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
				"gate": {Path: "gate"},
			},
		},
	}
	ops := New(root, &protocol.Workspace{Root: root}, cfg)

	// 只在 core 建立 feature branch 並加 commit
	branch := Branch("feat-partial-branch")
	coreRepo := filepath.Join(root, "core")
	defaultBranch := gitOutput(coreRepo, "rev-parse", "--abbrev-ref", "HEAD")
	runGit(t, coreRepo, "checkout", "-b", branch)
	os.WriteFile(filepath.Join(coreRepo, "partial.go"), []byte("package core\n"), 0o644)
	runGit(t, coreRepo, "add", ".")
	runGit(t, coreRepo, "commit", "-m", "add partial.go")
	runGit(t, coreRepo, "checkout", defaultBranch)

	// gate 不建立 branch — 模擬 feature 只改了部分 repo 的場景

	// 手動建立 worktree 目錄讓 Merge 不視為 skipped
	wtDir := Dir(root, "feat-partial-branch")
	os.MkdirAll(wtDir, 0o755)

	result := ops.Merge("feat-partial-branch", "Partial Branch Feature")
	if result.Error != "" {
		t.Fatalf("merge should succeed when branch missing in some repos, got error: %s", result.Error)
	}
	if result.Conflict {
		t.Fatal("unexpected conflict")
	}

	// core 應有合併後的檔案
	if _, err := os.Stat(filepath.Join(root, "core", "partial.go")); err != nil {
		t.Error("partial.go should exist in core after merge")
	}
}

func TestMultiRepo_CaptureBaseline_AllReposWhenEmpty(t *testing.T) {
	_, ws, ops := setupMultiWorkspace(t)
	if err := ws.InitFeatureDir("feat-base3"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	// featureRepos 為空時應抓取所有 workspace repos
	if err := ops.CaptureBaseline("feat-base3", nil); err != nil {
		t.Fatalf("CaptureBaseline with nil repos: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-base3"), protocol.BaselineFile))
	if err != nil {
		t.Fatal(err)
	}
	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if len(baseline.Repos) != 2 {
		t.Fatalf("nil repos should capture all 2 workspace repos, got %d", len(baseline.Repos))
	}
}
