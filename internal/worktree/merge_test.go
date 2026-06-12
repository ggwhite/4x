package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) (mainDir string, wtDir string, featureID string) {
	t.Helper()
	featureID = "F099-test"
	mainDir = t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", mainDir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	run("init")
	run("commit", "--allow-empty", "-m", "init")

	wtDir = filepath.Join(mainDir, ".worktrees", "4x", featureID)
	os.MkdirAll(filepath.Dir(wtDir), 0o755)
	run("worktree", "add", wtDir, "-b", "4x/"+featureID)

	wtRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", wtDir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	os.WriteFile(filepath.Join(wtDir, "new.txt"), []byte("hello"), 0o644)
	wtRun("add", "new.txt")
	wtRun("commit", "-m", "feat: add new.txt")

	return mainDir, wtDir, featureID
}

func TestMerge_Success(t *testing.T) {
	mainDir, _, featureID := setupTestRepo(t)

	result := Merge(mainDir, featureID, "Test Feature")
	if result.Conflict {
		t.Fatalf("expected no conflict, got conflicts: %v", result.Files)
	}
	if result.Skipped {
		t.Fatal("should not be skipped when worktree exists")
	}

	wtDir := filepath.Join(mainDir, ".worktrees", "4x", featureID)
	if _, err := os.Stat(wtDir); err == nil {
		t.Error("worktree directory should be removed")
	}

	if _, err := os.Stat(filepath.Join(mainDir, "new.txt")); err != nil {
		t.Error("new.txt should exist on main after merge")
	}
}

func TestMerge_NoWorktree(t *testing.T) {
	mainDir := t.TempDir()
	cmd := exec.Command("git", "-C", mainDir, "init")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	cmd.Run()
	cmd2 := exec.Command("git", "-C", mainDir, "commit", "--allow-empty", "-m", "init")
	cmd2.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	cmd2.Run()

	result := Merge(mainDir, "F099-nonexistent", "Test")
	if result.Conflict {
		t.Error("no worktree should mean no conflict")
	}
	if !result.Skipped {
		t.Error("should be skipped when no worktree exists")
	}
}

func TestMerge_Conflict(t *testing.T) {
	mainDir, _, featureID := setupTestRepo(t)

	mainEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)

	os.WriteFile(filepath.Join(mainDir, "new.txt"), []byte("conflict"), 0o644)
	addCmd := exec.Command("git", "-C", mainDir, "add", "new.txt")
	addCmd.Env = mainEnv
	addCmd.Run()
	commitCmd := exec.Command("git", "-C", mainDir, "commit", "-m", "conflict on main")
	commitCmd.Env = mainEnv
	commitCmd.Run()

	result := Merge(mainDir, featureID, "Test Feature")
	if !result.Conflict {
		t.Fatal("expected conflict")
	}
	if len(result.Files) == 0 {
		t.Error("should report conflicting files")
	}

	wtDir := filepath.Join(mainDir, ".worktrees", "4x", featureID)
	if _, err := os.Stat(wtDir); err != nil {
		t.Error("worktree should be preserved on conflict")
	}
}

func TestCleanup_Success(t *testing.T) {
	mainDir, wtDir, featureID := setupTestRepo(t)

	err := Cleanup(mainDir, featureID)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if _, err := os.Stat(wtDir); err == nil {
		t.Error("worktree directory should be removed")
	}
}
