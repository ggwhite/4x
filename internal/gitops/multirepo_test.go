package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/vcshub"
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
	wtPath, err := ops.SetupWorktree("feat-1", nil)
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

func TestMultiRepo_SetupWorktree_OnlyFeatureRepos(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-scoped", []string{"core"})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wtPath, "core")); err != nil {
		t.Error("core should exist in worktree when listed in featureRepos")
	}
	if _, err := os.Stat(filepath.Join(wtPath, "gate")); !os.IsNotExist(err) {
		t.Error("gate should NOT exist in worktree when not in featureRepos")
	}
}

// TestMultiRepo_SetupWorktree_FastForwardsStaleMain 驗證 multi-repo 模式下，每個
// repo 開 worktree 前都會各自 fetch + fast-forward 落後的 origin，跟 mono-repo 行為一致。
func TestMultiRepo_SetupWorktree_FastForwardsStaleMain(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	coreDir := filepath.Join(root, "core")
	branch := gitOutput(coreDir, "symbolic-ref", "--short", "HEAD")

	bareDir := addBareRemote(t, coreDir)
	runGit(t, coreDir, "push", "-u", "origin", branch)

	cloneDir := t.TempDir()
	if out, err := exec.Command("git", "clone", bareDir, cloneDir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %s", out)
	}
	// bare repo 的 HEAD 預設分支名取決於 CI runner 的 git 全域設定，不保證跟 coreDir 的
	// 實際分支一致；明確 checkout 避免 clone 出來的 working tree 停在別的分支上。
	runGit(t, cloneDir, "checkout", branch)
	runGit(t, cloneDir, "config", "user.name", "test")
	runGit(t, cloneDir, "config", "user.email", "test@test")
	os.WriteFile(filepath.Join(cloneDir, "remote.go"), []byte("package main\n"), 0o644)
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "remote change")
	runGit(t, cloneDir, "push", "origin", branch)

	wtPath, err := ops.SetupWorktree("feat-stale", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(coreDir, "remote.go")); err != nil {
		t.Error("local core main should have fast-forwarded to include origin's new commit")
	}
	if _, err := os.Stat(filepath.Join(wtPath, "core", "remote.go")); err != nil {
		t.Error("core worktree branch should be based on the fast-forwarded main")
	}
}

func TestMultiRepo_SetupWorktree_Idempotent(t *testing.T) {
	_, _, ops := setupMultiWorkspace(t)
	wtPath1, err := ops.SetupWorktree("feat-idem2", nil)
	if err != nil {
		t.Fatalf("first SetupWorktree: %v", err)
	}
	wtPath2, err := ops.SetupWorktree("feat-idem2", nil)
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

	_, err := ops2.SetupWorktree("feat-partial", nil)
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
	wtPath, err := ops.SetupWorktree("feat-commit", nil)
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
	wtPath, err := ops.SetupWorktree("feat-nochange2", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := ops.Commit(wtPath, "feat-nochange2", "wip(feat-nochange2): round 1"); err != nil {
		t.Errorf("Commit with no changes should not fail: %v", err)
	}
}

func TestMultiRepo_MergeHappyPath(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-happy", nil)
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

	wtPath, err := ops.SetupWorktree("feat-conflict", nil)
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
	wtPath, err := ops.SetupWorktree("feat-clean2", nil)
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
	wtPath, err := fullOps.SetupWorktree("feat-orphan", nil)
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
	changed := ops.DetectChangedRepos("feat-detect")
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

// worktree 模式下，主工作區非 feature repo 的 dirty files 不應被回報為 changed，
// 否則 guard.checkScope 會誤判 scope violation。
func TestMultiRepo_DetectChangedRepos_WortkreeSkipsNonFeatureRepos(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)

	// 只對 core 建 worktree（模擬 feature.Repos = ["core"]）
	wtPath, err := ops.SetupWorktree("feat-scope", []string{"core"})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// 在主工作區的 gate 加入 dirty file（模擬使用者其他未提交的修改）
	os.WriteFile(filepath.Join(root, "gate", "dirty.txt"), []byte("dirty"), 0o644)
	exec.Command("git", "-C", filepath.Join(root, "gate"), "add", "dirty.txt").Run()

	// 在 worktree 的 core 加入變更（模擬 Coder 的工作）
	os.WriteFile(filepath.Join(wtPath, "core", "new.go"), []byte("package core\n"), 0o644)

	changed := ops.DetectChangedRepos("feat-scope")

	hasCore := false
	hasGate := false
	for _, r := range changed {
		if r == "core" {
			hasCore = true
		}
		if r == "gate" {
			hasGate = true
		}
	}
	if !hasCore {
		t.Error("core should be detected as changed (has worktree changes)")
	}
	if hasGate {
		t.Error("gate must NOT be detected — no worktree for it, main workspace dirty files should be ignored")
	}
}

// worktree 模式下，DetectChangedFiles 同理不應包含非 feature repo 的變更。
func TestMultiRepo_DetectChangedFiles_WorktreeSkipsNonFeatureRepos(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)

	wtPath, err := ops.SetupWorktree("feat-scope2", []string{"core"})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	os.WriteFile(filepath.Join(root, "gate", "dirty.txt"), []byte("dirty"), 0o644)
	os.WriteFile(filepath.Join(wtPath, "core", "new.go"), []byte("package core\n"), 0o644)

	files := ops.DetectChangedFiles("feat-scope2")

	for _, f := range files {
		if len(f.Path) >= 5 && f.Path[:5] == "gate/" {
			t.Errorf("gate file should not appear in DetectChangedFiles, got %q", f.Path)
		}
	}
	if len(files) == 0 {
		t.Error("expected at least core changes")
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

// TestMultiRepo_PushAndOpenMR_Success 涵蓋 AC-12：兩個 repo 皆有 committed commits 領先
// baseline target，push 到各自 bare remote 後透過 fakeHub 開 MR，全部成功時清除 worktree。
func TestMultiRepo_PushAndOpenMR_Success(t *testing.T) {
	root, ws, ops := setupMultiWorkspace(t)
	featureID := "feat-pushmr-multi-ok"

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

	for _, name := range []string{"core", "gate"} {
		addBareRemote(t, filepath.Join(root, name))
	}

	origVcshubNew := vcshubNew
	defer func() { vcshubNew = origVcshubNew }()
	vcshubNew = func(repoPath string) vcshub.Hub { return &fakeHub{} }

	result := ops.PushAndOpenMR(featureID, "Multi Feature")
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	for _, name := range []string{"core", "gate"} {
		if result.MRUrls[name] == "" {
			t.Errorf("MRUrls[%q] should be set", name)
		}
	}
	if _, err := os.Stat(Dir(root, featureID)); !os.IsNotExist(err) {
		t.Error("worktree should be cleaned up after full success")
	}
}

// TestMultiRepo_PushAndOpenMR_PartialFailure 涵蓋 AC-12（D6，硬性要求）：core 有 remote 能
// 成功 push+開 MR，gate 沒有 remote 導致 push 失敗；斷言 MRUrls 只含成功的 core、Error 非空，
// 且 worktree 目錄與 gate 的本地 feature branch 仍存在（未被 Cleanup，可重試）。
func TestMultiRepo_PushAndOpenMR_PartialFailure(t *testing.T) {
	root, ws, ops := setupMultiWorkspace(t)
	featureID := "feat-pushmr-multi-partial"

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

	// core 有 bare remote 能成功 push；gate 沒有 remote，push 會失敗。
	addBareRemote(t, filepath.Join(root, "core"))

	origVcshubNew := vcshubNew
	defer func() { vcshubNew = origVcshubNew }()
	vcshubNew = func(repoPath string) vcshub.Hub { return &fakeHub{} }

	result := ops.PushAndOpenMR(featureID, "Partial Feature")
	if result.Error == "" {
		t.Fatal("expected error from partial failure")
	}
	if result.MRUrls["core"] == "" {
		t.Error("MRUrls[\"core\"] should be set for the succeeding repo")
	}
	if _, ok := result.MRUrls["gate"]; ok {
		t.Error("MRUrls[\"gate\"] should not be set for the failing repo")
	}
	if _, err := os.Stat(Dir(root, featureID)); err != nil {
		t.Error("worktree should be preserved on partial failure")
	}
	out, _ := exec.Command("git", "-C", filepath.Join(root, "gate"), "branch", "--list", Branch(featureID)).Output()
	if len(out) == 0 {
		t.Error("gate's local feature branch should be preserved on partial failure")
	}
}

// TestMultiRepo_PushAndOpenMR_ScopedRepos_IgnoresUnrelatedRepo 涵蓋 deep-review CRITICAL
// 回歸：feat.Repos 為真子集（只含 core）時，gate 從未建立 feature branch，rev-list 在 gate
// 上必然出錯（unknown revision）；PushAndOpenMR 必須只跑 feat.Repos 宣告的子集，不能把
// gate 的 rev-list 錯誤當成整體失敗，否則永遠回傳 Error 卡在 pending-review。
func TestMultiRepo_PushAndOpenMR_ScopedRepos_IgnoresUnrelatedRepo(t *testing.T) {
	root, ws, ops := setupMultiWorkspace(t)
	featureID := "feat-pushmr-scoped"

	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: "Scoped Feature", Status: feature.StatusInProgress, Repos: []string{"core"}}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := ops.CaptureBaseline(featureID, []string{"core"}); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	wtPath, err := ops.SetupWorktree(featureID, []string{"core"})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "gate")); !os.IsNotExist(err) {
		t.Fatal("gate should not be part of the scoped worktree")
	}
	if err := os.WriteFile(filepath.Join(wtPath, "core", "new.go"), []byte("package core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, featureID, "wip"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	addBareRemote(t, filepath.Join(root, "core"))

	origVcshubNew := vcshubNew
	defer func() { vcshubNew = origVcshubNew }()
	vcshubNew = func(repoPath string) vcshub.Hub { return &fakeHub{} }

	result := ops.PushAndOpenMR(featureID, "Scoped Feature")
	if result.Error != "" {
		t.Fatalf("unexpected error from unrelated gate repo: %s", result.Error)
	}
	if result.MRUrls["core"] == "" {
		t.Error("MRUrls[\"core\"] should be set")
	}
	if _, ok := result.MRUrls["gate"]; ok {
		t.Error("MRUrls[\"gate\"] should not be set — gate is outside feat.Repos")
	}
	if _, err := os.Stat(Dir(root, featureID)); !os.IsNotExist(err) {
		t.Error("worktree should be cleaned up after full success")
	}
}
