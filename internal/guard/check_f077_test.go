package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// runGitGuard 在指定目錄執行 git 子命令，失敗時呼叫 t.Fatalf。
func runGitGuard(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.io",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.io",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s", args, dir, out)
	}
}

// setupScopeWorkspace 建立含 git repo、.4x、feature YAML 的 mono workspace。
func setupScopeWorkspace(t *testing.T, featureID string, repos []string) (*protocol.Workspace, gitops.Ops) {
	t.Helper()
	root := t.TempDir()
	runGitGuard(t, root, "init")
	runGitGuard(t, root, "config", "user.email", "t@t.io")
	runGitGuard(t, root, "config", "user.name", "t")

	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "scope-test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: featureID, Repos: repos}); err != nil {
		t.Fatal(err)
	}
	writeState(t, ws, featureID, protocol.State{FeatureID: featureID, Phase: protocol.PhaseCoding, Round: 1})

	// coding phase 需要 designer 產出物，補齊以免 required-files 檢查干擾 scope 斷言。
	featureDir := ws.FeatureDir(featureID)
	writeFile(t, filepath.Join(featureDir, protocol.TaskBrief), "# Task Brief\n")
	writeFile(t, filepath.Join(featureDir, protocol.Criteria), "# Acceptance Criteria\n")

	// 建立並提交 scope 內外的子目錄供製造變更。
	writeFile(t, filepath.Join(root, "core", "a.go"), "package core\n")
	writeFile(t, filepath.Join(root, "lib", "b.go"), "package lib\n")
	runGitGuard(t, root, "add", ".")
	runGitGuard(t, root, "commit", "-m", "init core and lib")

	return ws, gitops.New(root, ws, cfg)
}

// TestCheckScope_WorktreeIgnoresMainChanges 是 F077 的端對端回歸測試：
// main workspace 有 out-of-scope 的未提交變更，feature 變更在 worktree 內且落在 scope；
// guard.Check 必須 PASS，不因 main 的無關變更誤報 scope violation。
func TestCheckScope_WorktreeIgnoresMainChanges(t *testing.T) {
	ws, ops := setupScopeWorkspace(t, "feat-scope", []string{"core"})

	wtPath, err := ops.SetupWorktree("feat-scope", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// worktree：scope 內變更（core）。
	writeFile(t, filepath.Join(wtPath, "core", "a.go"), "package core\n// scoped\n")
	// main：out-of-scope 未提交變更（lib），不屬於本 feature。
	writeFile(t, filepath.Join(ws.Root, "lib", "b.go"), "package lib\n// noise\n")

	result := Check(ws, "feat-scope", ops)
	for _, e := range result.Errors {
		if strings.Contains(e, "scope violation") {
			t.Errorf("main workspace change must not trigger scope violation: %q", e)
		}
	}
	if !result.Pass {
		t.Errorf("guard should pass, got errors: %v", result.Errors)
	}
}

// TestCheckScope_WorktreeOutOfScopeStillCaught 驗證修正未削弱 scope 檢查：
// worktree 內若改到 feature.Repos 之外的 repo，仍須回報 scope violation。
func TestCheckScope_WorktreeOutOfScopeStillCaught(t *testing.T) {
	ws, ops := setupScopeWorkspace(t, "feat-scope", []string{"core"})

	wtPath, err := ops.SetupWorktree("feat-scope", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// worktree 內改到 scope 外的 lib → 必須被攔。
	writeFile(t, filepath.Join(wtPath, "lib", "b.go"), "package lib\n// out of scope\n")

	result := Check(ws, "feat-scope", ops)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "lib") {
			found = true
		}
	}
	if !found {
		t.Errorf("worktree change outside feature repos should be flagged, got: %v", result.Errors)
	}
}
