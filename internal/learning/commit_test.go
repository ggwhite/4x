package learning

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo 在指定目錄建立一個乾淨的 git repo，並設定必要的 user config。
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
}

// commitCount 回傳 repo 目前的 commit 數量。
func commitCount(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// 還沒有任何 commit 時 rev-list 會失敗，回傳 0
		return 0
	}
	n := 0
	for _, b := range out {
		if b >= '0' && b <= '9' {
			n = n*10 + int(b-'0')
		}
	}
	return n
}

// TestCommitIfDirty_FileNotExist 驗證當 learnings.json 不存在時，函式不做任何事就返回。
func TestCommitIfDirty_FileNotExist(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	initGitRepo(t, root)

	// dotDir 不存在，learnings.json 也不存在
	CommitIfDirty(root, dotDir)

	if commitCount(t, root) != 0 {
		t.Error("expected no commits, but found some")
	}
}

// TestCommitIfDirty_FileClean 驗證當 learnings.json 存在且已 commit（無 diff）時，不會再建立新 commit。
func TestCommitIfDirty_FileClean(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	initGitRepo(t, root)

	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lpath := filepath.Join(dotDir, "learnings.json")
	if err := os.WriteFile(lpath, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// 先把檔案 commit 進去，讓 working tree 是乾淨的
	rel, _ := filepath.Rel(root, lpath)
	add := exec.Command("git", "add", rel)
	add.Dir = root
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	ci := exec.Command("git", "commit", "-m", "init")
	ci.Dir = root
	if out, err := ci.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	before := commitCount(t, root)
	CommitIfDirty(root, dotDir)
	after := commitCount(t, root)

	if after != before {
		t.Errorf("expected commit count to stay at %d, got %d", before, after)
	}
}

// TestCommitIfDirty_FileDirty 驗證當 learnings.json 有未 commit 的變更時，函式會執行 git add + commit。
func TestCommitIfDirty_FileDirty(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	initGitRepo(t, root)

	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lpath := filepath.Join(dotDir, "learnings.json")

	// 先建立初始 commit
	if err := os.WriteFile(lpath, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(root, lpath)
	add := exec.Command("git", "add", rel)
	add.Dir = root
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	ci := exec.Command("git", "commit", "-m", "init")
	ci.Dir = root
	if out, err := ci.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	// 修改檔案，讓 working tree dirty
	if err := os.WriteFile(lpath, []byte(`{"version":1,"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	before := commitCount(t, root)
	CommitIfDirty(root, dotDir)
	after := commitCount(t, root)

	if after != before+1 {
		t.Errorf("expected %d commits after dirty commit, got %d", before+1, after)
	}

	// 確認 commit message 正確
	msg := exec.Command("git", "log", "-1", "--pretty=%s")
	msg.Dir = root
	out, err := msg.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	got := string(out)
	want := "chore: update learnings.json\n"
	if got != want {
		t.Errorf("commit message: got %q, want %q", got, want)
	}
}

// TestCommitIfDirty_InvalidRoot 驗證傳入不存在的 root 目錄時，函式不會 panic。
func TestCommitIfDirty_InvalidRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nonexistent")
	dotDir := filepath.Join(root, ".4x")

	// 建立 learnings.json 讓 os.Stat 通過，但 root 本身不是 git repo
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lpath := filepath.Join(dotDir, "learnings.json")
	if err := os.WriteFile(lpath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// 函式內部的 git 指令會靜默失敗，不應 panic
	CommitIfDirty(root, dotDir)
}
