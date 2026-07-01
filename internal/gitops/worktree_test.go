package gitops

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile 寫檔並自動建立父目錄，供測試製造變更使用。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// sameRealPath 比較兩個路徑解析 symlink 後是否相同，
// 避開 macOS /var → /private/var 造成的字面差異。
func sameRealPath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = filepath.Clean(a)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = filepath.Clean(b)
	}
	return ra == rb
}

// TestDetectWorktree_MainAndLinked 驗證 DetectWorktree 能區分主工作區與 linked worktree。
func TestDetectWorktree_MainAndLinked(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)

	info, ok := DetectWorktree(root)
	if !ok {
		t.Fatal("main repo should be detected as a worktree")
	}
	if info.IsLinked {
		t.Error("main working tree should not be reported as linked")
	}
	if !sameRealPath(t, info.Root, root) {
		t.Errorf("info.Root = %q, want %q", info.Root, root)
	}

	wtPath, err := ops.SetupWorktree("feat-wt", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	linked, ok := DetectWorktree(wtPath)
	if !ok {
		t.Fatal("linked worktree should be detected")
	}
	if !linked.IsLinked {
		t.Error("git worktree add output should be reported as linked")
	}
	if !sameRealPath(t, linked.Root, wtPath) {
		t.Errorf("linked.Root = %q, want %q", linked.Root, wtPath)
	}
}

// TestDetectWorktree_NotGitRepo 驗證非 git repo 目錄回傳 ok=false。
func TestDetectWorktree_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	if _, ok := DetectWorktree(dir); ok {
		t.Error("non-git dir should not be detected as a worktree")
	}
}

// TestMonoRepo_DetectChangedRepos_WorktreeScoped 是 F077 的核心回歸測試：
// main workspace 有 out-of-scope 的未提交變更，feature 的變更在 worktree 內；
// DetectChangedRepos 必須只回報 worktree 內的變更，不誤報 main 的變更。
func TestMonoRepo_DetectChangedRepos_WorktreeScoped(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)

	// 在 main 建立並提交兩個子目錄，供後續製造變更。
	writeFile(t, filepath.Join(root, "core", "a.go"), "package core\n")
	writeFile(t, filepath.Join(root, "lib", "b.go"), "package lib\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "add core and lib")

	wtPath, err := ops.SetupWorktree("feat-scope", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// worktree 內：scope 內變更（core）。
	writeFile(t, filepath.Join(wtPath, "core", "a.go"), "package core\n// changed in worktree\n")
	// main workspace 內：out-of-scope 的未提交變更（lib），不應被算進 feature scope。
	writeFile(t, filepath.Join(root, "lib", "b.go"), "package lib\n// changed in main\n")

	changed := ops.DetectChangedRepos("feat-scope")

	if !contains(changed, "core") {
		t.Errorf("expected worktree change in 'core' to be reported, got %v", changed)
	}
	if contains(changed, "lib") {
		t.Errorf("main workspace's out-of-scope change in 'lib' must not be reported, got %v", changed)
	}
}

// TestMultiRepo_DetectChangedRepos_WorktreeScoped 驗證 multi-repo 模式下，
// DetectChangedRepos 在各 repo 的 worktree 子目錄掃描，不誤報 main 子 repo 的變更。
func TestMultiRepo_DetectChangedRepos_WorktreeScoped(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)

	wtPath, err := ops.SetupWorktree("feat-multi-scope", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// worktree 的 core 子 repo：scope 內變更（修改既有 tracked 檔 main.go）。
	writeFile(t, filepath.Join(wtPath, "core", "main.go"), "package main\n// worktree change\n")
	// main 的 gate 子 repo：out-of-scope 的未提交變更，不應被算進 feature scope。
	writeFile(t, filepath.Join(root, "gate", "main.go"), "package main\n// main change\n")

	changed := ops.DetectChangedRepos("feat-multi-scope")

	if !contains(changed, "core") {
		t.Errorf("expected worktree change in 'core' to be reported, got %v", changed)
	}
	if contains(changed, "gate") {
		t.Errorf("main workspace's out-of-scope change in 'gate' must not be reported, got %v", changed)
	}
}

// TestScopeRoots_Mono 驗證 mono-repo 下 ScopeRoots 回傳單元素 slice，
// 值與 ScopeRoot 一致（既有行為不變的硬約束）。
func TestScopeRoots_Mono(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)

	wtPath, err := ops.SetupWorktree("feat-scoperoots-mono", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	roots := ScopeRoots(root, "feat-scoperoots-mono")
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %v", roots)
	}
	if !sameRealPath(t, roots[0], wtPath) {
		t.Errorf("roots[0] = %q, want %q", roots[0], wtPath)
	}

	want := ScopeRoot(root, "feat-scoperoots-mono")
	if !sameRealPath(t, roots[0], want) {
		t.Errorf("ScopeRoots result diverges from ScopeRoot: %q vs %q", roots[0], want)
	}
}

// TestScopeRoots_NoWorktree 驗證沒有 worktree 時回退 fallback，維持既有行為。
func TestScopeRoots_NoWorktree(t *testing.T) {
	root, _, _ := setupMonoWorkspace(t)

	roots := ScopeRoots(root, "feat-never-run")
	if len(roots) != 1 || !sameRealPath(t, roots[0], root) {
		t.Errorf("expected fallback [%q], got %v", root, roots)
	}
}

// TestScopeRoots_MultiRepo 是本次修正的核心回歸測試：multi-repo 的 worktree 容器目錄
// 本身不是 git repo，ScopeRoots 必須改為逐一掃描子目錄，回報各 sub-repo 的 linked
// worktree 根目錄，而不是誤判整體非 linked 而 fallback 回 main workspace。
func TestScopeRoots_MultiRepo(t *testing.T) {
	root, _, ops := setupMultiWorkspace(t)

	wtPath, err := ops.SetupWorktree("feat-scoperoots-multi", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	roots := ScopeRoots(root, "feat-scoperoots-multi")
	if len(roots) != 2 {
		t.Fatalf("expected 2 sub-repo roots, got %v", roots)
	}
	wantCore := filepath.Join(wtPath, "core")
	wantGate := filepath.Join(wtPath, "gate")
	for _, want := range []string{wantCore, wantGate} {
		found := false
		for _, r := range roots {
			if sameRealPath(t, r, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("roots %v missing expected sub-repo root %q", roots, want)
		}
		if sameRealPath(t, want, root) {
			t.Errorf("root %q must not resolve to main workspace %q", want, root)
		}
	}
}

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
