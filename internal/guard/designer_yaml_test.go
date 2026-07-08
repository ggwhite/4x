package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func setupDesignerYAMLWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	tmp := t.TempDir()
	dotDir := filepath.Join(tmp, ".4x")
	featDir := filepath.Join(dotDir, "features")
	runDir := filepath.Join(dotDir, "run", "F001-test")
	os.MkdirAll(featDir, 0o755)
	os.MkdirAll(runDir, 0o755)

	cmd := exec.Command("git", "init")
	cmd.Dir = tmp
	cmd.Run()
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = tmp
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = tmp
	cmd.Run()

	return &protocol.Workspace{Root: tmp}
}

func commitFeatureYAML(t *testing.T, ws *protocol.Workspace, f feat.Feature) {
	t.Helper()
	ws.SaveFeature(f)
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = ws.Root
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = ws.Root
	cmd.Run()
}

func TestDesignerYAMLMod_ReposOnly_Pass(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted, Repos: []string{"repo-a"}}
	commitFeatureYAML(t, ws, f)

	f.Repos = []string{"repo-a", "repo-b"}
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if !r.Pass {
		t.Errorf("expected pass when only repos changed, got errors: %v", r.Errors)
	}
}

func TestDesignerYAMLMod_NameChanged_Fail(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted}
	commitFeatureYAML(t, ws, f)

	f.Name = "Modified Name"
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if r.Pass {
		t.Error("expected fail when name changed")
	}
	if len(r.Errors) == 0 || r.RetryableErrors != 1 {
		t.Errorf("expected 1 retryable error, got %d errors, %d retryable", len(r.Errors), r.RetryableErrors)
	}
}

func TestDesignerYAMLMod_PriorityChanged_Fail(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	p := 1
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted, Priority: &p}
	commitFeatureYAML(t, ws, f)

	p2 := 2
	f.Priority = &p2
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if r.Pass {
		t.Error("expected fail when priority changed")
	}
}

func TestDesignerYAMLMod_NotDesigning_Skip(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted}
	commitFeatureYAML(t, ws, f)

	f.Name = "Modified"
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseCoding, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if !r.Pass {
		t.Error("expected skip (pass) when not in designing phase")
	}
}

func TestDesignerYAMLMod_NoChange_Pass(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted, Repos: []string{"repo-a"}}
	commitFeatureYAML(t, ws, f)

	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if !r.Pass {
		t.Errorf("expected pass when YAML unchanged, got errors: %v", r.Errors)
	}
}
