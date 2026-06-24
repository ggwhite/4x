package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func setupGitWorkspace(t *testing.T, featureID string) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	gitInit(t, root)

	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "symlink-test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: featureID}); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return ws
}

func TestCheckSymlinks_UntrackedSymlink(t *testing.T) {
	ws := setupGitWorkspace(t, "feat-1")

	target := filepath.Join(ws.Root, "real-file.txt")
	writeFile(t, target, "content")
	link := filepath.Join(ws.Root, "bad-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	r := CheckResult{Pass: true}
	checkSymlinks(ws, "feat-1", &r)

	if !r.Pass {
		t.Fatal("symlink should warn, not fail")
	}
	found := false
	for _, w := range r.Warns {
		if strings.Contains(w, "symlink") && strings.Contains(w, "bad-link") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning mentioning bad-link, got warns=%v", r.Warns)
	}
}

func TestCheckSymlinks_TrackedSymlink(t *testing.T) {
	ws := setupGitWorkspace(t, "feat-1")

	target := filepath.Join(ws.Root, "real-file.txt")
	writeFile(t, target, "content")
	link := filepath.Join(ws.Root, "tracked-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"add", "tracked-link"}, {"commit", "-m", "add symlink"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = ws.Root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	r := CheckResult{Pass: true}
	checkSymlinks(ws, "feat-1", &r)

	if !r.Pass {
		t.Fatal("symlink should warn, not fail")
	}
	if len(r.Warns) == 0 {
		t.Fatal("committed symlink should produce a warning (mode 120000)")
	}
}

func TestCheckSymlinks_NoSymlink(t *testing.T) {
	ws := setupGitWorkspace(t, "feat-1")

	writeFile(t, filepath.Join(ws.Root, "normal.txt"), "content")

	r := CheckResult{Pass: true}
	checkSymlinks(ws, "feat-1", &r)

	if !r.Pass {
		t.Fatalf("no symlinks should pass, got errors: %v", r.Errors)
	}
}
