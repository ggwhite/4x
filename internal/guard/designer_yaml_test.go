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

// TestDesignerYAMLMod_SharedPaths_Pass 涵蓋 AC-7：Designer 在 designing phase 只新增
// shared_paths 欄位時不應被判為 violation——比照 repos，shared_paths 屬 Designer 可修改欄位
// （刻意不列入 featureSnapshot）。
func TestDesignerYAMLMod_SharedPaths_Pass(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted, Repos: []string{"repo-a"}}
	commitFeatureYAML(t, ws, f)

	f.SharedPaths = []string{"Dockerfile"}
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if !r.Pass {
		t.Errorf("expected pass when only shared_paths added, got errors: %v", r.Errors)
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

func TestDesignerYAMLMod_OrchestratorStatusFlip_Pass(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted}
	commitFeatureYAML(t, ws, f)

	f.Status = feat.StatusInProgress
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{
		Phase: protocol.PhaseDesigning, Round: 1,
		OrchestratorStatusFlipFrom: string(feat.StatusNotStarted),
		OrchestratorStatusFlipTo:   string(feat.StatusInProgress),
	})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if !r.Pass {
		t.Errorf("expected pass for orchestrator status flip not-started → in-progress, got errors: %v", r.Errors)
	}
}

func TestDesignerYAMLMod_EmptyStatusFlip_Pass(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc"}
	commitFeatureYAML(t, ws, f)

	f.Status = feat.StatusInProgress
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{
		Phase: protocol.PhaseDesigning, Round: 1,
		OrchestratorStatusFlipFrom: "",
		OrchestratorStatusFlipTo:   string(feat.StatusInProgress),
	})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if !r.Pass {
		t.Errorf("expected pass for orchestrator status flip \"\" → in-progress, got errors: %v", r.Errors)
	}
}

// TestDesignerYAMLMod_StatusFlipWithoutStateRecord_Fail 是 Finding [2] 的安全回歸測試：
// 即使觀察到的 (orig, current) 數值恰好與 orchestrator 慣用的 not-started→in-progress
// pair 相同，若 state.json 沒有記錄對應的 OrchestratorStatusFlipFrom/To（代表這次 status
// 變更不是本輪 initPhase 寫的——可能是 Designer 自己改的），仍必須被判定為違規。
func TestDesignerYAMLMod_StatusFlipWithoutStateRecord_Fail(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted}
	commitFeatureYAML(t, ws, f)

	f.Status = feat.StatusInProgress
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if r.Pass {
		t.Error("expected fail when status flip matches orchestrator's usual value pair but state.json has no matching OrchestratorStatusFlipFrom/To record")
	}
}

// TestDesignerYAMLMod_StatusFlipMismatchedRecord_Fail 涵蓋 state.json 記錄了某次 flip，
// 但實際觀察到的 (orig, current) 與記錄值不符（例如記錄的是這次 run 的 flip，但磁碟上出現
// 另一個不同的 status 變更）——仍須判定為違規，不可只因欄位非空就整體放行。
func TestDesignerYAMLMod_StatusFlipMismatchedRecord_Fail(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted}
	commitFeatureYAML(t, ws, f)

	f.Status = feat.StatusDone
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{
		Phase: protocol.PhaseDesigning, Round: 1,
		OrchestratorStatusFlipFrom: string(feat.StatusNotStarted),
		OrchestratorStatusFlipTo:   string(feat.StatusInProgress),
	})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if r.Pass {
		t.Error("expected fail when observed status change doesn't match the recorded OrchestratorStatusFlipFrom/To")
	}
}

func TestDesignerYAMLMod_OtherStatusChange_Fail(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted}
	commitFeatureYAML(t, ws, f)

	f.Status = feat.StatusDone
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if r.Pass {
		t.Error("expected fail when status changed to done (not an orchestrator flip)")
	}
}

func TestDesignerYAMLMod_StatusFlipPlusNameChange_Fail(t *testing.T) {
	ws := setupDesignerYAMLWorkspace(t)
	f := feat.Feature{ID: "F001-test", Name: "Test", Description: "desc", Status: feat.StatusNotStarted}
	commitFeatureYAML(t, ws, f)

	f.Status = feat.StatusInProgress
	f.Name = "Modified Name"
	ws.SaveFeature(f)
	writeState(t, ws, "F001-test", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})

	r := CheckResult{Pass: true}
	checkDesignerYAMLMod(ws, "F001-test", &r)
	if r.Pass {
		t.Error("expected fail when name changed even alongside an orchestrator status flip")
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
