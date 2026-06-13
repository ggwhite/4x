package gitops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

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
	runGit(t, root, "init")

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
	wtPath, err := ops.SetupWorktree("feat-1")
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
	wtPath1, err := ops.SetupWorktree("feat-idem")
	if err != nil {
		t.Fatalf("first SetupWorktree: %v", err)
	}
	wtPath2, err := ops.SetupWorktree("feat-idem")
	if err != nil {
		t.Fatalf("second SetupWorktree (idempotent): %v", err)
	}
	if wtPath1 != wtPath2 {
		t.Errorf("paths differ: %q != %q", wtPath1, wtPath2)
	}
}

func TestMonoRepo_Commit_NoChanges(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-nochange")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if err := ops.Commit(wtPath, "feat-nochange", "No Change", 1); err != nil {
		t.Errorf("Commit with no changes should not fail: %v", err)
	}
}

func TestMonoRepo_CommitAndMerge(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-merge")
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(wtPath, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ops.Commit(wtPath, "feat-merge", "Test Feature", 1); err != nil {
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
	wtPath, err := ops.SetupWorktree("feat-cleanup")
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
	changed := ops.DetectChangedRepos()
	if len(changed) != 0 {
		t.Errorf("expected no changes on fresh repo, got %v", changed)
	}
}
