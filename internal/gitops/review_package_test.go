package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
