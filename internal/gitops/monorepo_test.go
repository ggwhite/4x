package gitops

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/vcshub"
)

// captureStderr 暫時把 os.Stderr 換成 pipe，執行 fn 後回傳擷取到的內容，供驗證
// syncUpstream 印到 stderr 的警告訊息（fetch/merge 是同行程呼叫，不是走 subprocess）。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

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

// TestMonoRepo_SetupWorktree_GoWorkUntouched 驗證 AC-6：monorepo 完全不觸碰 go.work——
// whole-repo checkout 會原樣帶入 committed 的 go.work，不裁切、不新建、不刪除。
func TestMonoRepo_SetupWorktree_GoWorkUntouched(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	original := "go 1.26\n\nuse ./sub-a\nuse ./sub-b\n"
	os.WriteFile(filepath.Join(root, "go.work"), []byte(original), 0o644)
	runGit(t, root, "add", "go.work")
	runGit(t, root, "commit", "-m", "add go.work")

	wtPath, err := ops.SetupWorktree("feat-gowork-mono", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(wtPath, "go.work"))
	if err != nil {
		t.Fatalf("go.work should be present in whole-repo checkout: %v", err)
	}
	if string(data) != original {
		t.Errorf("monorepo go.work should be untouched:\ngot:  %q\nwant: %q", string(data), original)
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

// TestMonoRepo_SetupWorktree_FastForwardsStaleMain 驗證開 worktree 前，本地落後
// origin main 時會先 fetch + fast-forward，讓新 feature 從最新的 main 分岔，避免
// issue-first MR 流程下本地 main 忘記 git pull 而讓 worktree 從舊 base 開出。
func TestMonoRepo_SetupWorktree_FastForwardsStaleMain(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	bareDir := addBareRemote(t, root)
	runGit(t, root, "push", "-u", "origin", "main")

	// 模擬別人已經把新 commit push 到 origin main，本地 root 尚未同步。
	cloneDir := t.TempDir()
	if out, err := exec.Command("git", "clone", bareDir, cloneDir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %s", out)
	}
	// bare repo 的 HEAD 預設分支名取決於 CI runner 的 git 全域設定，不保證跟 setupMonoWorkspace
	// 明確指定的 "main" 一致；明確 checkout 避免 clone 出來的 working tree 停在別的分支上。
	runGit(t, cloneDir, "checkout", "main")
	runGit(t, cloneDir, "config", "user.name", "test")
	runGit(t, cloneDir, "config", "user.email", "test@test")
	os.WriteFile(filepath.Join(cloneDir, "remote.go"), []byte("package main\n"), 0o644)
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "remote change")
	runGit(t, cloneDir, "push", "origin", "main")

	wtPath, err := ops.SetupWorktree("feat-stale", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "remote.go")); err != nil {
		t.Error("local main should have fast-forwarded to include origin's new commit")
	}
	if _, err := os.Stat(filepath.Join(wtPath, "remote.go")); err != nil {
		t.Error("worktree branch should be based on the fast-forwarded main")
	}
}

// TestMonoRepo_SetupWorktree_DivergedMainPreserved 驗證本地 main 與 origin 已分岔
// （雙方都有對方沒有的 commit）時，SetupWorktree 不會失敗、也不會強制覆蓋本地 commit
// ——fetch+ff-only 在分岔時本就會乾淨失敗，worktree 照樣用現有本地 ref 開出。
func TestMonoRepo_SetupWorktree_DivergedMainPreserved(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	bareDir := addBareRemote(t, root)
	runGit(t, root, "push", "-u", "origin", "main")

	cloneDir := t.TempDir()
	if out, err := exec.Command("git", "clone", bareDir, cloneDir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %s", out)
	}
	runGit(t, cloneDir, "checkout", "main")
	runGit(t, cloneDir, "config", "user.name", "test")
	runGit(t, cloneDir, "config", "user.email", "test@test")
	os.WriteFile(filepath.Join(cloneDir, "remote.go"), []byte("package main\n"), 0o644)
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "remote change")
	runGit(t, cloneDir, "push", "origin", "main")

	// root 自己也有一筆未推送的 local commit，跟 origin 分岔。
	os.WriteFile(filepath.Join(root, "local.go"), []byte("package main\n"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "local change")
	localHead := gitOutput(root, "rev-parse", "HEAD")

	wtPath, err := ops.SetupWorktree("feat-diverged", nil)
	if err != nil {
		t.Fatalf("SetupWorktree should not fail on diverged main: %v", err)
	}

	if afterHead := gitOutput(root, "rev-parse", "HEAD"); afterHead != localHead {
		t.Errorf("local main HEAD should be unchanged when diverged, got %s want %s", afterHead, localHead)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "local.go")); err != nil {
		t.Error("worktree should be based on preserved local commit")
	}
}

// TestMonoRepo_SetupWorktree_LocalAheadOfRemote 驗證本地領先 origin（有未推送的本地
// commit，origin 沒有新 commit，非分岔）時，SetupWorktree 用本地 HEAD 當 base（fast-
// forward 對此是 no-op），但會印警告讓使用者知道有未推送的 commit 會被當成新 feature
// 的起點——不只落後 remote 要處理，超前 remote 也要讓使用者看得到，而非悄悄略過。
func TestMonoRepo_SetupWorktree_LocalAheadOfRemote(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	addBareRemote(t, root)
	runGit(t, root, "push", "-u", "origin", "main")

	os.WriteFile(filepath.Join(root, "local.go"), []byte("package main\n"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "local change")
	localHead := gitOutput(root, "rev-parse", "HEAD")

	var wtPath string
	var err error
	stderr := captureStderr(t, func() {
		wtPath, err = ops.SetupWorktree("feat-ahead", nil)
	})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if !strings.Contains(stderr, "ahead") {
		t.Errorf("expected a warning about local commits ahead of origin, got stderr: %q", stderr)
	}
	if afterHead := gitOutput(root, "rev-parse", "HEAD"); afterHead != localHead {
		t.Errorf("local main HEAD should be unchanged, got %s want %s", afterHead, localHead)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "local.go")); err != nil {
		t.Error("worktree should be based on local HEAD including the unpushed commit")
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
	writeFile(t, filepath.Join(wtDir, "main.go"), "package main\n// from branch\n")
	runGit(t, wtDir, "add", "main.go")
	runGit(t, wtDir, "commit", "-m", "feat")

	// dirty main working tree: unstaged modification to a file the branch also changed.
	// 這正是 kairos/F181 資料遺失案例的精確重現：main 有與本次 merge 重疊的 tracked 修改，
	// pre-fix 時 git 會拒絕 merge、走 reset --hard HEAD 抹除這筆髒內容。
	const dirtyContent = "package main\n// dirty\n"
	writeFile(t, filepath.Join(root, "main.go"), dirtyContent)

	// 擷取 Merge 前的 git status（含 untracked），供比對 preflight 未觸碰任何檔案。
	statusBefore, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status before: %v", err)
	}

	result := ops.Merge("feat-dirty", "Dirty Feature")
	if result.Conflict {
		t.Error("dirty tree should not be reported as conflict")
	}
	// preflight 應提前攔截並回傳固定錯誤字串，而非走 merge --squash 失敗分支。
	if result.Error != "main workspace has uncommitted changes, aborting merge" {
		t.Errorf("unexpected error: %q", result.Error)
	}

	// 核心回歸斷言：main.go 的髒內容未被 reset --hard 抹除，仍原樣存在。
	got, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if string(got) != dirtyContent {
		t.Errorf("dirty main.go content was destroyed:\ngot:  %q\nwant: %q", string(got), dirtyContent)
	}

	// git status --porcelain 前後位元組完全一致，證明 preflight 未觸碰任何檔案。
	statusAfter, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status after: %v", err)
	}
	if !bytes.Equal(statusBefore, statusAfter) {
		t.Errorf("git status changed across Merge:\nbefore: %q\nafter:  %q", statusBefore, statusAfter)
	}
}

// TestMonoRepo_Merge_DirtyMainAborts 驗證 AC-2：worktree 有一筆可 merge 的 commit、main 有一個
// 被修改的 tracked 檔（與 branch 變更不重疊，pre-fix 時 git 不會拒絕 merge）時，preflight 仍提前
// 攔截、不觸碰任何檔案：Error 為固定字串、git status 前後一致、feature commit 未進 main、worktree
// 未被清理。這釘住 R7「對無關 tracked 髒檔一律攔截」的較廣設計面。
func TestMonoRepo_Merge_DirtyMainAborts(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	wtDir, err := ops.SetupWorktree("feat-dirtyabort", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// worktree 有一筆可 merge 的 commit（新檔，與 main 髒檔不重疊）。
	writeFile(t, filepath.Join(wtDir, "feature.go"), "package main\n")
	runGit(t, wtDir, "add", "feature.go")
	runGit(t, wtDir, "commit", "-m", "feat")

	// main 有一個被修改的 tracked 檔（unstaged）。
	writeFile(t, filepath.Join(root, "main.go"), "package main\n// dirty main\n")

	statusBefore, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status before: %v", err)
	}

	result := ops.Merge("feat-dirtyabort", "Dirty Abort Feature")
	if result.Error != "main workspace has uncommitted changes, aborting merge" {
		t.Errorf("unexpected error: %q", result.Error)
	}
	if result.Conflict {
		t.Error("dirty preflight abort should not be reported as conflict")
	}

	statusAfter, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status after: %v", err)
	}
	if !bytes.Equal(statusBefore, statusAfter) {
		t.Errorf("git status changed across Merge:\nbefore: %q\nafter:  %q", statusBefore, statusAfter)
	}

	// feature commit 未進入 main：feature.go 不應被 tracked。
	tracked, err := exec.Command("git", "-C", root, "ls-files", "feature.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if len(tracked) != 0 {
		t.Errorf("feature.go should NOT be merged into main, but it is tracked")
	}

	// worktree 目錄仍存在（未被 Cleanup）。
	if _, err := os.Stat(wtDir); err != nil {
		t.Errorf("worktree should be preserved when preflight aborts: %v", err)
	}
}

// TestMonoRepo_Merge_UntrackedMainProceeds 驗證 AC-3（釘住 R2）：main 僅有 untracked 檔時
// merge 照舊成功——git reset --hard 不會刪除 untracked，擋下它沒有安全收益反而誤擋正常流程。
func TestMonoRepo_Merge_UntrackedMainProceeds(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	wtDir, err := ops.SetupWorktree("feat-untracked", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// worktree 有一筆可 merge 的 commit。
	writeFile(t, filepath.Join(wtDir, "feature.go"), "package main\n")
	runGit(t, wtDir, "add", "feature.go")
	runGit(t, wtDir, "commit", "-m", "feat")

	// main 只有一個 untracked 檔（reset --hard 不會刪它，故不算髒）。
	scratch := filepath.Join(root, "scratch.txt")
	writeFile(t, scratch, "unrelated scratch\n")

	result := ops.Merge("feat-untracked", "Untracked Main Feature")
	if result.Error != "" {
		t.Errorf("merge should proceed with untracked-only main, got error: %q", result.Error)
	}
	if result.Conflict {
		t.Error("merge should not conflict with untracked-only main")
	}

	// untracked 檔在 merge 後仍存在。
	if _, err := os.Stat(scratch); err != nil {
		t.Errorf("untracked scratch file should survive merge: %v", err)
	}
	// feature 確實 merge 進 main。
	if _, err := os.Stat(filepath.Join(root, "feature.go")); err != nil {
		t.Errorf("feature.go should be merged into main: %v", err)
	}
}

// TestMonoRepo_Merge_SelfManagedDirtyProceeds 驗證 AC-3：主工作區只有 4x 自管路徑
// （feature YAML、learnings.json、learnings-context.md）為 tracked-dirty 時，Merge 前置
// commit 會把它們收乾淨，preflight 不再誤擋，merge 照常完成並留下一筆 pipeline state commit。
func TestMonoRepo_Merge_SelfManagedDirtyProceeds(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	seedSelfManaged(t, root, "feat-x")

	wtDir, err := ops.SetupWorktree("feat-x", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	writeFile(t, filepath.Join(wtDir, "feature.go"), "package main\n")
	runGit(t, wtDir, "add", "--", "feature.go")
	runGit(t, wtDir, "commit", "-m", "feat")

	dirtySelfManaged(t, root, "feat-x")

	result := ops.Merge("feat-x", "Self Managed Feature")
	if result.Error != "" {
		t.Fatalf("merge should proceed with self-managed-only dirt, got error: %q", result.Error)
	}
	if result.Conflict {
		t.Error("merge should not conflict with self-managed-only dirt")
	}

	if _, err := os.Stat(filepath.Join(root, "feature.go")); err != nil {
		t.Errorf("feature.go should be merged into main: %v", err)
	}

	const want = "chore(feat-x): 4x pipeline state"
	found := false
	for _, line := range strings.Split(gitOutput(root, "log", "--format=%s"), "\n") {
		if line == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("git log should contain %q, got:\n%s", want, gitOutput(root, "log", "--format=%s"))
	}
}

// TestMonoRepo_Merge_UserDirtyStillAborts 驗證 AC-4：前置 commit 只認 4x 自管路徑，使用者
// 自己的 tracked-dirty 檔仍會讓 preflight 中止，且該檔內容逐位元組不變、worktree 保留。
func TestMonoRepo_Merge_UserDirtyStillAborts(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	wtDir, err := ops.SetupWorktree("feat-userdirty", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	writeFile(t, filepath.Join(wtDir, "feature.go"), "package main\n")
	runGit(t, wtDir, "add", "--", "feature.go")
	runGit(t, wtDir, "commit", "-m", "feat")

	const dirtyContent = "package main\n// user edit\n"
	writeFile(t, filepath.Join(root, "main.go"), dirtyContent)

	result := ops.Merge("feat-userdirty", "User Dirty Feature")
	if result.Error != "main workspace has uncommitted changes, aborting merge" {
		t.Errorf("unexpected error: %q", result.Error)
	}
	if result.Conflict {
		t.Error("preflight abort should not be reported as conflict")
	}

	got, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !bytes.Equal(got, []byte(dirtyContent)) {
		t.Errorf("user dirty main.go was modified:\ngot:  %q\nwant: %q", got, dirtyContent)
	}
	if _, err := os.Stat(wtDir); err != nil {
		t.Errorf("worktree should be preserved when preflight aborts: %v", err)
	}
}

// TestMonoRepo_Merge_MixedDirtyAbortsButCommitsSelfManaged 驗證 AC-5：4x 自管路徑與使用者
// 檔同時髒時，前置 commit 只收自管路徑，preflight 仍因使用者檔中止且不觸碰該檔。
func TestMonoRepo_Merge_MixedDirtyAbortsButCommitsSelfManaged(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	seedSelfManaged(t, root, "feat-mixed")

	wtDir, err := ops.SetupWorktree("feat-mixed", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	writeFile(t, filepath.Join(wtDir, "feature.go"), "package main\n")
	runGit(t, wtDir, "add", "--", "feature.go")
	runGit(t, wtDir, "commit", "-m", "feat")

	writeFile(t, filepath.Join(root, protocol.DirName, protocol.LearningsFile), `{"learnings": [{"id": "L1"}]}`+"\n")
	const dirtyContent = "package main\n// user edit\n"
	writeFile(t, filepath.Join(root, "main.go"), dirtyContent)

	result := ops.Merge("feat-mixed", "Mixed Dirty Feature")
	if result.Error != "main workspace has uncommitted changes, aborting merge" {
		t.Errorf("unexpected error: %q", result.Error)
	}

	got, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !bytes.Equal(got, []byte(dirtyContent)) {
		t.Errorf("user dirty main.go was modified:\ngot:  %q\nwant: %q", got, dirtyContent)
	}

	if status := gitOutput(root, "status", "--porcelain", "--", ".4x/learnings.json"); status != "" {
		t.Errorf("learnings.json should have been committed by the pre-merge commit, git status = %q", status)
	}
}
