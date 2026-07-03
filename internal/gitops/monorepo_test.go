package gitops

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/vcshub"
)

// fakeHub 是 vcshub.Hub 的測試替身，供 monorepo/multirepo 的 PushAndOpenMR 測試共用。
// onCall（若設定）會在 OpenMR 被呼叫時收到全部參數；openMRErr 非 nil 時 OpenMR 回傳該錯誤。
type fakeHub struct {
	openMRErr error
	onCall    func(repoPath, sourceBranch, targetBranch, title, body string)
}

func (f *fakeHub) Preflight(repoPath string) error { return nil }

func (f *fakeHub) CreateIssue(repoPath, title, body string) (id, url string, err error) {
	return "", "", nil
}

func (f *fakeHub) GetIssue(repoPath, ref string) (id, url string, err error) {
	return "", "", nil
}

func (f *fakeHub) OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (string, error) {
	if f.onCall != nil {
		f.onCall(repoPath, sourceBranch, targetBranch, title, body)
	}
	if f.openMRErr != nil {
		return "", f.openMRErr
	}
	return "https://example.invalid/mr/" + sourceBranch, nil
}

// addBareRemote 在 t.TempDir() 建立一個 bare repo 並設為 repoDir 的 origin remote，供測試 git push。
func addBareRemote(t *testing.T, repoDir string) string {
	t.Helper()
	bareDir := t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %s", out)
	}
	runGit(t, repoDir, "remote", "add", "origin", bareDir)
	return bareDir
}

// runGit 在指定目錄執行 git 子命令，失敗時呼叫 t.Fatalf。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tester",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=tester",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s", args, dir, out)
	}
}

// initGitRepo 在 dir 初始化 git repo 並建立一個初始 commit（含 main.go）。
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "test")
	runGit(t, dir, "config", "user.email", "test@test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
}

// setupMonoWorkspace 建立帶有 git repo 與 .4x/settings.json 的暫存工作區。
// .4x/ 會包含在初始 commit 中，確保 worktree 合併時不會因為 untracked 檔案而失敗。
func setupMonoWorkspace(t *testing.T) (root string, ws *protocol.Workspace, ops Ops) {
	t.Helper()
	root = t.TempDir()
	os.MkdirAll(root, 0o755)
	// PushAndOpenMR 在沒有 baseline.json 時 fallback target 是硬編碼的 "main"（見下方
	// PushAndOpenMR 的註解），故這裡明確指定 initial branch 名稱，不依賴 CI runner 的
	// git init.defaultBranch 全域設定（GitHub Actions runner 預設值與本機不同）。
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "user.email", "test@test")

	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}

	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	ws = &protocol.Workspace{Root: root}
	ops = New(root, ws, cfg)
	return
}

func TestMonoRepo_IsMultiRepo(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	if ops.IsMultiRepo() {
		t.Error("monoRepo.IsMultiRepo() should return false")
	}
}

func TestMonoRepo_SetupWorktree(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-1", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	expected := Dir(root, "feat-1")
	if wtPath != expected {
		t.Errorf("wtPath = %q, want %q", wtPath, expected)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir should exist: %v", err)
	}
	dotCfg := filepath.Join(wtPath, protocol.DirName, protocol.ConfigFile)
	if _, err := os.Stat(dotCfg); err != nil {
		t.Error(".4x/settings.json should be copied to worktree")
	}
}

func TestMonoRepo_SetupWorktree_Idempotent(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	wtPath1, err := ops.SetupWorktree("feat-idem", nil)
	if err != nil {
		t.Fatalf("first SetupWorktree: %v", err)
	}
	wtPath2, err := ops.SetupWorktree("feat-idem", nil)
	if err != nil {
		t.Fatalf("second SetupWorktree (idempotent): %v", err)
	}
	if wtPath1 != wtPath2 {
		t.Errorf("paths differ: %q != %q", wtPath1, wtPath2)
	}
}

func TestMonoRepo_Commit_NoChanges(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-nochange", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := ops.Commit(wtPath, "feat-nochange", "wip(feat-nochange): round 1"); err != nil {
		t.Errorf("Commit with no changes should not fail: %v", err)
	}
}

func TestMonoRepo_CommitAndMerge(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-merge", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, "feat-merge", "wip(feat-merge): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	result := ops.Merge("feat-merge", "Test Feature")
	if result.Conflict {
		t.Fatalf("unexpected conflict: %v", result.Files)
	}
	if result.Error != "" {
		t.Fatalf("merge error: %q", result.Error)
	}
	if result.Skipped {
		t.Error("merge should not be skipped")
	}
}

func TestMonoRepo_Merge_Skipped(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	result := ops.Merge("feat-nonexist", "Nonexistent")
	if !result.Skipped {
		t.Error("Merge without worktree should return Skipped")
	}
}

func TestMonoRepo_Cleanup(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-cleanup", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := ops.Cleanup("feat-cleanup"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed after Cleanup")
	}
	out, _ := exec.Command("git", "-C", root, "branch", "--list", "4x/feat-cleanup").Output()
	if len(out) > 0 {
		t.Errorf("branch 4x/feat-cleanup should be deleted after Cleanup, got: %q", string(out))
	}
}

func TestMonoRepo_CaptureBaseline(t *testing.T) {
	_, ws, ops := setupMonoWorkspace(t)
	if err := ws.InitFeatureDir("feat-baseline"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	if err := ops.CaptureBaseline("feat-baseline", nil); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-baseline"), protocol.BaselineFile))
	if err != nil {
		t.Fatal(err)
	}
	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if len(baseline.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(baseline.Repos))
	}
	if baseline.Repos[0].Head == "" {
		t.Error("baseline HEAD should not be empty")
	}
	if baseline.Repos[0].Branch == "" {
		t.Error("baseline Branch should not be empty")
	}
}

func TestMonoRepo_DetectChangedRepos(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	changed := ops.DetectChangedRepos("feat-detect")
	if len(changed) != 0 {
		t.Errorf("expected no changes on fresh repo, got %v", changed)
	}
}

func TestMonoRepo_MergeConflict(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	_, err := ops.SetupWorktree("feat-conflict", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	wtDir := Dir(root, "feat-conflict")
	os.WriteFile(filepath.Join(wtDir, "conflict.txt"), []byte("from-branch"), 0o644)
	runGit(t, wtDir, "add", "conflict.txt")
	runGit(t, wtDir, "commit", "-m", "branch change")

	os.WriteFile(filepath.Join(root, "conflict.txt"), []byte("from-main"), 0o644)
	runGit(t, root, "add", "conflict.txt")
	runGit(t, root, "commit", "-m", "main change")

	result := ops.Merge("feat-conflict", "Conflict Feature")
	if !result.Conflict {
		t.Fatal("expected conflict")
	}
	if len(result.Files) == 0 {
		t.Error("should report conflicting files")
	}
	// worktree should be preserved on conflict
	if _, err := os.Stat(wtDir); err != nil {
		t.Error("worktree should be preserved on conflict")
	}
	// main index 不應有 staged 或 unmerged 檔案（--squash 不建立 MERGE_HEAD，merge --abort 可能靜默失敗）
	out, _ := exec.Command("git", "-C", root, "diff", "--cached", "--name-only").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("main index should have no staged changes after conflict abort, got:\n%s", out)
	}
	unmerged := conflictFiles(root)
	if len(unmerged) > 0 {
		t.Errorf("main should have no unmerged files after conflict abort, got: %v", unmerged)
	}
}

func TestMonoRepo_CommitExcludesFeatureYAML(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	featureID := "feat-yaml"

	yamlPath := filepath.Join(".4x", "features", featureID+".yaml")
	os.MkdirAll(filepath.Join(root, ".4x", "features"), 0o755)
	os.WriteFile(filepath.Join(root, yamlPath), []byte("id: feat-yaml\nstatus: not-started\n"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "add feature yaml")

	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	os.WriteFile(filepath.Join(wtPath, yamlPath), []byte("id: feat-yaml\nstatus: in-progress\n"), 0o644)
	os.WriteFile(filepath.Join(wtPath, "code.go"), []byte("package main\nfunc A(){}\n"), 0o644)
	if err := ops.Commit(wtPath, featureID, "wip(feat-yaml): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	out, _ := exec.Command("git", "-C", wtPath, "diff", "HEAD~1", "HEAD", "--name-only").Output()
	committed := strings.TrimSpace(string(out))
	if strings.Contains(committed, "features/feat-yaml.yaml") {
		t.Errorf("feature YAML should NOT be in wip commit, got:\n%s", committed)
	}
	if !strings.Contains(committed, "code.go") {
		t.Errorf("code.go should be in wip commit, got:\n%s", committed)
	}
}

func TestMonoRepo_MergeNoConflictAfterExclude(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	featureID := "feat-yaml2"

	yamlPath := filepath.Join(".4x", "features", featureID+".yaml")
	os.MkdirAll(filepath.Join(root, ".4x", "features"), 0o755)
	os.WriteFile(filepath.Join(root, yamlPath), []byte("id: feat-yaml2\nstatus: not-started\n"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "add feature yaml")

	wtPath, err := ops.SetupWorktree(featureID, nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	os.WriteFile(filepath.Join(wtPath, yamlPath), []byte("id: feat-yaml2\nstatus: in-progress\n"), 0o644)
	os.WriteFile(filepath.Join(wtPath, "code.go"), []byte("package main\nfunc A(){}\n"), 0o644)
	if err := ops.Commit(wtPath, featureID, "wip(feat-yaml2): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	os.WriteFile(filepath.Join(root, yamlPath), []byte("id: feat-yaml2\nstatus: ready-for-review\n"), 0o644)
	runGit(t, root, "add", yamlPath)
	runGit(t, root, "commit", "-m", "main: update yaml status")

	result := ops.Merge(featureID, "YAML Feature")
	if result.Conflict {
		t.Fatalf("expected no conflict, got: %v", result.Files)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(root, yamlPath))
	if err != nil {
		t.Fatalf("read yaml: %v", err)
	}
	if !strings.Contains(string(data), "status: ready-for-review") {
		t.Errorf("expected main's status preserved, got:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(root, "code.go")); err != nil {
		t.Error("code.go from branch should be present after merge")
	}
}

// TestMonoRepo_MergeCommitFailCleansStaged 驗證 commit 失敗（非 nothing-to-commit）時，
// Merge 會清理 squash 留下的 staged changes，使 main 回到 merge 前的乾淨狀態（F086 task 7）。
// 用 pre-commit hook 強制 commit 失敗來重現。
func TestMonoRepo_MergeCommitFailCleansStaged(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-commitfail", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(wtPath, "added.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, "feat-commitfail", "wip(feat-commitfail): round 1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// 安裝會拒絕 commit 的 pre-commit hook，迫使 main 上的 squash commit 失敗。
	hookDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "pre-commit"), []byte("#!/bin/sh\necho rejected-by-hook >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := ops.Merge("feat-commitfail", "Commit Fail Feature")
	if result.Conflict {
		t.Errorf("commit-fail should not be reported as conflict: %v", result.Files)
	}
	if result.Error == "" {
		t.Error("commit failure should produce an error")
	}

	// main 應回到乾淨狀態，無 squash 留下的 staged/working tree 殘留（reset --hard 不刪 untracked，
	// 故僅斷言無已追蹤的 staged/modified 殘留）。
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=no").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("main working tree should be clean after commit-fail, got:\n%s", out)
	}

	// 確認 squash 變更沒有真的被 commit 進 main（否則 reset 沒生效卻看似乾淨）。
	tracked, err := exec.Command("git", "-C", root, "ls-files", "added.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if len(tracked) != 0 {
		t.Errorf("added.go should not be committed to main after commit-fail, but it is tracked")
	}
}

// TestMonoRepo_PushAndOpenMR_NoWorktree_Skipped 涵蓋 AC-11：沒有 worktree 時直接 Skipped。
func TestMonoRepo_PushAndOpenMR_NoWorktree_Skipped(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	result := ops.PushAndOpenMR("feat-nonexist-pushmr", "Nonexistent")
	if !result.Skipped {
		t.Errorf("expected Skipped, got %+v", result)
	}
}

// TestMonoRepo_PushAndOpenMR_NoCommitsAhead_Skipped 涵蓋 AC-11（D5）：worktree 存在但 feature
// branch 相對 target 無 commit（rev-list --count == 0）時視為無變更，Skipped 且清理 worktree。
func TestMonoRepo_PushAndOpenMR_NoCommitsAhead_Skipped(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	featureID := "feat-nocommits-ahead"

	if _, err := ops.SetupWorktree(featureID, nil); err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	result := ops.PushAndOpenMR(featureID, "No Commits")
	if !result.Skipped {
		t.Errorf("expected Skipped, got %+v", result)
	}
	if _, err := os.Stat(Dir(root, featureID)); !os.IsNotExist(err) {
		t.Error("worktree should be cleaned up when no commits ahead")
	}
}

// TestMonoRepo_PushAndOpenMR_Success 涵蓋 AC-11：worktree 有 committed commits 領先 target 時，
// push 到 bare remote 後透過 fakeHub 開 MR，成功後回傳 MRUrls["."] 並清除 worktree。
func TestMonoRepo_PushAndOpenMR_Success(t *testing.T) {
	root, ws, ops := setupMonoWorkspace(t)
	featureID := "feat-pushmr-ok"

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
	var gotSource string
	vcshubNew = func(repoPath string) vcshub.Hub {
		return &fakeHub{onCall: func(rp, source, target, title, body string) {
			gotSource = source
		}}
	}

	result := ops.PushAndOpenMR(featureID, "Test Feature")
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.MRUrls["."] == "" {
		t.Fatal("expected MRUrls[\".\"] to be set")
	}
	if gotSource != Branch(featureID) {
		t.Errorf("OpenMR source branch = %q, want %q", gotSource, Branch(featureID))
	}
	if _, err := os.Stat(Dir(root, featureID)); !os.IsNotExist(err) {
		t.Error("worktree should be cleaned up after successful PushAndOpenMR")
	}
}

// TestMonoRepo_PushAndOpenMR_OpenMRFails_PreservesWorktree 涵蓋 AC-11（D6）：push 成功但
// OpenMR 失敗時回傳 Error 且不清理 worktree／local branch，供使用者修好後重跑。
func TestMonoRepo_PushAndOpenMR_OpenMRFails_PreservesWorktree(t *testing.T) {
	root, ws, ops := setupMonoWorkspace(t)
	featureID := "feat-pushmr-fail"

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
	vcshubNew = func(repoPath string) vcshub.Hub {
		return &fakeHub{openMRErr: errors.New("boom")}
	}

	result := ops.PushAndOpenMR(featureID, "Fail Feature")
	if result.Error == "" {
		t.Fatal("expected error from failed OpenMR")
	}
	if _, err := os.Stat(Dir(root, featureID)); err != nil {
		t.Error("worktree should be preserved when OpenMR fails")
	}
	out, _ := exec.Command("git", "-C", root, "branch", "--list", Branch(featureID)).Output()
	if len(out) == 0 {
		t.Error("local feature branch should be preserved when OpenMR fails")
	}
}

func TestMonoRepo_MergeDirtyWorkingTree(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	_, err := ops.SetupWorktree("feat-dirty", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	wtDir := Dir(root, "feat-dirty")
	// branch also modifies main.go so git will refuse to merge with a dirty working tree
	os.WriteFile(filepath.Join(wtDir, "main.go"), []byte("package main\n// from branch\n"), 0o644)
	runGit(t, wtDir, "add", "main.go")
	runGit(t, wtDir, "commit", "-m", "feat")

	// dirty main working tree: unstaged modification to a file the branch also changed
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// dirty\n"), 0o644)

	result := ops.Merge("feat-dirty", "Dirty Feature")
	if result.Conflict {
		t.Error("dirty tree should not be reported as conflict")
	}
	if result.Error == "" {
		t.Error("dirty working tree should produce an error")
	}
}
