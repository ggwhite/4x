package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lastEnvValue 回傳 env 中 key 最後一次出現的值（os/exec 對重複 key 以最後者為準）。
func lastEnvValue(env []string, key string) (string, bool) {
	prefix := key + "="
	val := ""
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			val = e[len(prefix):]
			found = true
		}
	}
	return val, found
}

// TestApplyWorktreeEnv_NotGitRepo_Unchanged 驗證非 git 目錄不注入任何變數。
func TestApplyWorktreeEnv_NotGitRepo_Unchanged(t *testing.T) {
	env := []string{"PATH=/usr/bin", "FOO=bar"}
	got := ApplyWorktreeEnv(env, t.TempDir())
	if len(got) != len(env) {
		t.Errorf("expected env unchanged for non-git dir, got %v", got)
	}
	if _, ok := lastEnvValue(got, "GOLANGCI_LINT_CACHE"); ok {
		t.Error("GOLANGCI_LINT_CACHE should not be set for non-git dir")
	}
}

// TestApplyWorktreeEnv_MainWorkspace_Unchanged 驗證主工作區（非 linked worktree）不注入。
func TestApplyWorktreeEnv_MainWorkspace_Unchanged(t *testing.T) {
	root, _, _ := setupMonoWorkspace(t)

	got := ApplyWorktreeEnv([]string{"PATH=/usr/bin"}, root)
	if _, ok := lastEnvValue(got, "GOLANGCI_LINT_CACHE"); ok {
		t.Error("GOLANGCI_LINT_CACHE should not be set for main working tree")
	}
}

// TestApplyWorktreeEnv_LinkedWorktree_LintCache 驗證 linked worktree 注入
// worktree-local 的 GOLANGCI_LINT_CACHE，避免跨 worktree 的 lint 快取污染。
func TestApplyWorktreeEnv_LinkedWorktree_LintCache(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-env", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	got := ApplyWorktreeEnv([]string{"PATH=/usr/bin"}, wtPath)
	cache, ok := lastEnvValue(got, "GOLANGCI_LINT_CACHE")
	if !ok {
		t.Fatal("GOLANGCI_LINT_CACHE not set for linked worktree")
	}
	// 快取目錄尚未存在，EvalSymlinks 無法解析，改以已存在的 wtPath 先解析再組路徑，
	// 避開 macOS /var → /private/var 差異。
	resolvedWt, err := filepath.EvalSymlinks(wtPath)
	if err != nil {
		resolvedWt = wtPath
	}
	want := filepath.Join(resolvedWt, ".cache", "golangci-lint")
	if cache != want {
		t.Errorf("GOLANGCI_LINT_CACHE = %q, want %q", cache, want)
	}
	if _, ok := lastEnvValue(got, "FOURX_BIN"); ok {
		t.Error("FOURX_BIN should not be overridden when worktree has no bin/4x")
	}
}

// TestApplyWorktreeEnv_LinkedWorktree_FourxBin 驗證 worktree 內存在可執行的
// bin/4x 時（4x 自我開發情境），FOURX_BIN 覆寫為 worktree-local binary 且其目錄
// 被 prepend 到 PATH 最前面。
func TestApplyWorktreeEnv_LinkedWorktree_FourxBin(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-bin", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	binPath := filepath.Join(wtPath, "bin", "4x")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	env := []string{"PATH=/usr/bin", "FOURX_BIN=/main/exe/4x"}
	got := ApplyWorktreeEnv(env, wtPath)

	bin, ok := lastEnvValue(got, "FOURX_BIN")
	if !ok || !sameRealPath(t, bin, binPath) {
		t.Errorf("FOURX_BIN = %q, want %q (last occurrence wins)", bin, binPath)
	}
	path, _ := lastEnvValue(got, "PATH")
	first := strings.Split(path, string(os.PathListSeparator))[0]
	if !sameRealPath(t, first, filepath.Dir(binPath)) {
		t.Errorf("PATH first entry = %q, want worktree bin dir %q", first, filepath.Dir(binPath))
	}
}

// TestApplyWorktreeEnv_NonExecutableBin_NoOverride 驗證 bin/4x 不可執行時不覆寫
// FOURX_BIN（避免把壞檔案交給 guard-tool hook）。
func TestApplyWorktreeEnv_NonExecutableBin_NoOverride(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	wtPath, err := ops.SetupWorktree("feat-noexec", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	binPath := filepath.Join(wtPath, "bin", "4x")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ApplyWorktreeEnv([]string{"PATH=/usr/bin"}, wtPath)
	if _, ok := lastEnvValue(got, "FOURX_BIN"); ok {
		t.Error("FOURX_BIN should not be overridden when bin/4x is not executable")
	}
}
