package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestHeadCommit(t *testing.T) {
	root, _, _ := setupMonoWorkspace(t)
	sha := HeadCommit(root)
	if sha == "" {
		t.Fatal("HeadCommit() returned empty for a valid git repo")
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	if want := strings.TrimSpace(string(out)); sha != want {
		t.Errorf("HeadCommit() = %q, want %q", sha, want)
	}
}

func TestHeadCommit_NotGitRepo(t *testing.T) {
	if sha := HeadCommit(t.TempDir()); sha != "" {
		t.Errorf("HeadCommit() = %q, want empty for non-git dir", sha)
	}
}

func TestMonoRepo_GenerateReviewPackage(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	base := HeadCommit(root)

	os.WriteFile(filepath.Join(root, "feature.go"), []byte("package main\n\nfunc Feature() {}\n"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "add feature")

	content, err := ops.GenerateReviewPackage("feat-1", base)
	if err != nil {
		t.Fatalf("GenerateReviewPackage: %v", err)
	}
	for _, want := range []string{"# Review Package", "## Commits", "## File Changes", "## Full Diff", "add feature", "feature.go"} {
		if !strings.Contains(content, want) {
			t.Errorf("GenerateReviewPackage() missing %q in:\n%s", want, content)
		}
	}
}

func TestMonoRepo_GenerateReviewPackage_EmptyBaseCommit(t *testing.T) {
	_, _, ops := setupMonoWorkspace(t)
	if _, err := ops.GenerateReviewPackage("feat-1", ""); err == nil {
		t.Error("GenerateReviewPackage() with empty baseCommit should return error")
	}
}

func TestMonoRepo_GenerateReviewPackage_NoDiff(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	base := HeadCommit(root)
	if _, err := ops.GenerateReviewPackage("feat-1", base); err == nil {
		t.Error("GenerateReviewPackage() with no diff since base should return error")
	}
}

func TestMultiRepo_GenerateReviewPackage(t *testing.T) {
	root, ws, ops := setupMultiWorkspace(t)
	if err := ws.InitFeatureDir("feat-multi-diff"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	if err := ops.CaptureBaseline("feat-multi-diff", []string{"core", "gate"}); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	coreDir := filepath.Join(root, "core")
	os.WriteFile(filepath.Join(coreDir, "feature.go"), []byte("package main\n\nfunc Feature() {}\n"), 0o644)
	runGit(t, coreDir, "add", ".")
	runGit(t, coreDir, "commit", "-m", "add core feature")

	content, err := ops.GenerateReviewPackage("feat-multi-diff", "")
	if err != nil {
		t.Fatalf("GenerateReviewPackage: %v", err)
	}
	for _, want := range []string{"# Review Package", "## Repo: core", "add core feature", "feature.go"} {
		if !strings.Contains(content, want) {
			t.Errorf("GenerateReviewPackage() missing %q in:\n%s", want, content)
		}
	}
	if strings.Contains(content, "## Repo: gate") {
		t.Errorf("GenerateReviewPackage() should not include gate (no diff):\n%s", content)
	}
}

func TestMultiRepo_GenerateReviewPackage_NoBaseline(t *testing.T) {
	_, ws, ops := setupMultiWorkspace(t)
	if err := ws.InitFeatureDir("feat-multi-nobase"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	if _, err := ops.GenerateReviewPackage("feat-multi-nobase", ""); err == nil {
		t.Error("GenerateReviewPackage() without baseline.json should return error")
	}
}

// AC-1：預算內時附各變更檔全文，binary 與已刪除檔跳過。
func TestMonoRepo_GenerateReviewPackage_ChangedFileContents(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)
	base := HeadCommit(root)

	os.WriteFile(filepath.Join(root, "feature.go"), []byte("package main\n\nfunc Feature() {}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "data.bin"), []byte{0x00, 0x01, 0x02, 0x00, 0xff}, 0o644)
	runGit(t, root, "rm", "main.go")
	runGit(t, root, "add", "feature.go", "data.bin")
	runGit(t, root, "commit", "-m", "add feature+binary, delete main")

	content, err := ops.GenerateReviewPackage("feat-1", base)
	if err != nil {
		t.Fatalf("GenerateReviewPackage: %v", err)
	}
	if !strings.Contains(content, "## Changed File Contents") {
		t.Errorf("missing Changed File Contents heading:\n%s", content)
	}
	if !strings.Contains(content, "```feature.go") || !strings.Contains(content, "func Feature()") {
		t.Errorf("feature.go should be inlined:\n%s", content)
	}
	if strings.Contains(content, "```data.bin") {
		t.Errorf("binary file data.bin should be skipped, not inlined:\n%s", content)
	}
	if strings.Contains(content, "```main.go") {
		t.Errorf("deleted file main.go should be skipped, not inlined:\n%s", content)
	}
	if strings.Contains(content, ReviewPackageTruncatedMarker) {
		t.Errorf("small files should not trigger truncation:\n%s", content)
	}
}

// AC-2：變更檔全文超過上限時，未 inline 檔改列路徑並有截斷標記，其內容不被 inline。
func TestMonoRepo_GenerateReviewPackage_Truncation(t *testing.T) {
	root, _, ops := setupMonoWorkspace(t)

	// 先 commit 一個 >100KB 的檔，base 設在此 commit 之後，之後只做小修改，
	// 讓 diff 很小但工作目錄全文超標 → 被列為未 inline，其頭部 token 不會出現在輸出。
	big := "UNIQUEHEADERTOKEN\n" + strings.Repeat("x\n", 60*1024)
	os.WriteFile(filepath.Join(root, "bigfile.txt"), []byte(big), 0o644)
	runGit(t, root, "add", "bigfile.txt")
	runGit(t, root, "commit", "-m", "add big")
	base := HeadCommit(root)

	os.WriteFile(filepath.Join(root, "bigfile.txt"), []byte(big+"MARKERLINE\n"), 0o644)
	runGit(t, root, "add", "bigfile.txt")
	runGit(t, root, "commit", "-m", "modify big")

	content, err := ops.GenerateReviewPackage("feat-1", base)
	if err != nil {
		t.Fatalf("GenerateReviewPackage: %v", err)
	}
	if !strings.Contains(content, ReviewPackageTruncatedMarker) {
		t.Errorf("expected truncation marker:\n%s", content)
	}
	if !strings.Contains(content, "Files Not Inlined") {
		t.Errorf("expected Files Not Inlined section:\n%s", content)
	}
	if !strings.Contains(content, "- bigfile.txt") {
		t.Errorf("bigfile.txt should be listed under Files Not Inlined:\n%s", content)
	}
	if strings.Contains(content, "```bigfile.txt") {
		t.Errorf("over-budget bigfile.txt should NOT be inlined:\n%s", content)
	}
	if strings.Contains(content, "UNIQUEHEADERTOKEN") {
		t.Errorf("over-budget file content must not leak into output:\n%s", content)
	}
}

// setupMultiWorkspaceNamed 建立含指定名稱 repo 的 multi-repo 工作區（供 AC-3 用固定排序名稱）。
func setupMultiWorkspaceNamed(t *testing.T, names ...string) (root string, ws *protocol.Workspace, ops Ops) {
	t.Helper()
	root = t.TempDir()
	repos := map[string]protocol.RepoConfig{}
	for _, n := range names {
		initGitRepo(t, filepath.Join(root, n))
		repos[n] = protocol.RepoConfig{Path: n}
	}
	cfg := protocol.Config{
		Project:   protocol.ProjectConfig{Name: "multi-test"},
		Workspace: protocol.WorkspaceConfig{Repos: repos},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws = &protocol.Workspace{Root: root}
	ops = New(root, ws, cfg)
	return
}

// AC-3：multi-repo 100KB 上限跨 repo 共享，且 repo 依名稱排序 deterministic。
// 排序後第一個 repo（aaa）的檔案 inline、第二個（zzz）落入未 inline 清單；重跑 5 次不 flaky。
func TestMultiRepo_GenerateReviewPackage_SharedBudget(t *testing.T) {
	for i := 0; i < 5; i++ {
		root, ws, ops := setupMultiWorkspaceNamed(t, "aaa", "zzz")
		zzzDir := filepath.Join(root, "zzz")
		aaaDir := filepath.Join(root, "aaa")

		// zzz 的大檔在 baseline 前就存在，之後只做小修改，讓其頭部 token 不進 diff。
		zzzBig := "ZZZUNIQUE\n" + strings.Repeat("z\n", 40*1024)
		os.WriteFile(filepath.Join(zzzDir, "big.txt"), []byte(zzzBig), 0o644)
		runGit(t, zzzDir, "add", "big.txt")
		runGit(t, zzzDir, "commit", "-m", "zzz add big pre-baseline")

		if err := ws.InitFeatureDir("feat-budget"); err != nil {
			t.Fatalf("InitFeatureDir: %v", err)
		}
		if err := ops.CaptureBaseline("feat-budget", []string{"aaa", "zzz"}); err != nil {
			t.Fatalf("CaptureBaseline: %v", err)
		}

		// aaa 排序在前，新增 ~80KB 檔先消耗預算。
		aaaBig := "AAAUNIQUE\n" + strings.Repeat("a\n", 40*1024)
		os.WriteFile(filepath.Join(aaaDir, "big.txt"), []byte(aaaBig), 0o644)
		runGit(t, aaaDir, "add", "big.txt")
		runGit(t, aaaDir, "commit", "-m", "aaa add big")

		// zzz 小修改；工作目錄全文 ~80KB 超過剩餘預算 → 未 inline。
		os.WriteFile(filepath.Join(zzzDir, "big.txt"), []byte(zzzBig+"ZZZMOD\n"), 0o644)
		runGit(t, zzzDir, "add", "big.txt")
		runGit(t, zzzDir, "commit", "-m", "zzz modify big")

		content, err := ops.GenerateReviewPackage("feat-budget", "")
		if err != nil {
			t.Fatalf("GenerateReviewPackage (run %d): %v", i, err)
		}
		if !strings.Contains(content, "AAAUNIQUE") {
			t.Errorf("run %d: aaa file should be inlined:\n%s", i, content)
		}
		if strings.Contains(content, "ZZZUNIQUE") {
			t.Errorf("run %d: zzz file is over shared budget and must NOT be inlined", i)
		}
		if !strings.Contains(content, ReviewPackageTruncatedMarker) {
			t.Errorf("run %d: expected truncation marker (shared budget exhausted)", i)
		}
		if !strings.Contains(content, "Files Not Inlined") {
			t.Errorf("run %d: expected Files Not Inlined section", i)
		}
	}
}
