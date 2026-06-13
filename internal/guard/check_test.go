package guard

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func setupGuardWorkspace(t *testing.T, featureID string) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "guard-test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	return ws
}

func writeState(t *testing.T, ws *protocol.Workspace, featureID string, s protocol.State) {
	t.Helper()
	if err := ws.WriteState(featureID, s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCheckRequiredFiles_InitPhase(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})

	result := Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Errorf("init phase should pass with just state.json, got errors: %v", result.Errors)
	}
}

func TestCheckRequiredFiles_CodingPhaseNeedsBrief(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseCoding})

	result := Check(ws, "feat-1", nil)
	if result.Pass {
		t.Error("coding phase without task-brief.md should fail")
	}

	foundBrief := false
	foundCriteria := false
	for _, e := range result.Errors {
		if e == "required file missing: "+protocol.TaskBrief {
			foundBrief = true
		}
		if e == "required file missing: "+protocol.Criteria {
			foundCriteria = true
		}
	}
	if !foundBrief {
		t.Error("expected error about missing task-brief.md")
	}
	if !foundCriteria {
		t.Error("expected error about missing acceptance-criteria.md")
	}
}

func TestCheckRequiredFiles_CodingPhaseWithFiles(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseCoding})

	dir := ws.FeatureDir("feat-1")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")

	result := Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Errorf("coding phase with all files should pass, got errors: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_AllArtifactsPresent(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")
	writeFile(t, filepath.Join(featureDir, protocol.CommitPlan), "# Commit Plan")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("complete tester artifacts should pass, got errors: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_MissingCommitPlan(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("missing commit-plan.md should fail")
	}
	found := false
	for _, err := range result.Errors {
		if err == "required file missing: "+protocol.CommitPlan {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing commit-plan error, got %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_VerifyFailed(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: false, Round: 1})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")
	writeFile(t, filepath.Join(featureDir, protocol.CommitPlan), "# Commit Plan")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("verify.json with passed=false should fail")
	}
	found := false
	for _, err := range result.Errors {
		if err == "verify.json did not pass" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected verify failure error, got %v", result.Errors)
	}
}

func TestCheckBaseline_Missing(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})

	result := Check(ws, "feat-1", nil)
	foundWarn := false
	for _, w := range result.Warns {
		if w == "no baseline.json found, skipping baseline check" {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Error("expected warning about missing baseline.json")
	}
}

func TestCheck_BacklogDriftWarning(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})
	if err := ws.SaveFeature(protocol.Feature{ID: "feat-1", Name: "Feature One", Status: "done"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ws.Root, protocol.BacklogFile), `{"version":1,"features":[{"id":"feat-1","name":"Feature One","status":"todo"}]}`)

	result := Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Fatalf("backlog drift should warn without failing: %v", result.Errors)
	}
	found := false
	for _, w := range result.Warns {
		if w == `feature_list.json mismatch for feature "feat-1" field "status": canonical "done", mirror "todo"` {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want backlog drift warning", result.Warns)
	}
}

func TestCheck_BacklogDriftIgnoresOtherFeatures(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})
	if err := ws.SaveFeature(protocol.Feature{ID: "feat-1", Name: "Feature One", Status: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(protocol.Feature{ID: "feat-2", Name: "Feature Two", Status: "done"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ws.Root, protocol.BacklogFile), `{"version":1,"features":[{"id":"feat-1","name":"Feature One","status":"done"},{"id":"feat-2","name":"Feature Two","status":"todo"}]}`)

	result := Check(ws, "feat-1", nil)
	for _, w := range result.Warns {
		if w == `feature_list.json mismatch for feature "feat-2" field "status": canonical "done", mirror "todo"` {
			t.Fatalf("warnings = %v, should not include drift for unrelated feature", result.Warns)
		}
	}
}

func TestCheckBaseline_InvalidJSON(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.BaselineFile), "not json")

	result := Check(ws, "feat-1", nil)
	if result.Pass {
		t.Error("invalid baseline.json should fail")
	}
}

func TestCheckBaseline_DirtyFiles(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})

	baseline := protocol.Baseline{
		Repos: []protocol.BaselineRepo{
			{Name: "backend", DirtyFiles: []string{"main.go", "go.sum"}},
		},
	}
	data, _ := json.Marshal(baseline)
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.BaselineFile), string(data))

	result := Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Errorf("dirty files should only warn, not fail: %v", result.Errors)
	}
	if len(result.Warns) == 0 {
		t.Error("expected warning about dirty files")
	}
}

func TestCheckBaseline_Clean(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})

	baseline := protocol.Baseline{
		Repos: []protocol.BaselineRepo{
			{Name: "backend", DirtyFiles: nil},
		},
	}
	data, _ := json.Marshal(baseline)
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.BaselineFile), string(data))

	result := Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Errorf("clean baseline should pass: %v", result.Errors)
	}
}

func TestCheckScope_NoRepos(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})

	f := protocol.Feature{ID: "feat-1", Name: "Test"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	result := Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Errorf("no repos defined should pass (no scope restriction): %v", result.Errors)
	}
}

func TestCheckScope_NoFeatureYAML(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})

	result := Check(ws, "feat-1", nil)
	foundWarn := false
	for _, w := range result.Warns {
		if len(w) > 0 {
			foundWarn = true
		}
	}
	_ = foundWarn
}

func TestCheck_MissingStateFile(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	os.MkdirAll(ws.FeatureDir("feat-1"), 0o755)

	result := Check(ws, "feat-1", nil)
	if result.Pass {
		t.Error("missing state.json should fail")
	}
}

func TestCheckTestingToAccepting_UnreadableVerifyJSON(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")
	writeFile(t, filepath.Join(featureDir, protocol.CommitPlan), "# Commit Plan")

	if err := os.Chmod(filepath.Join(roundDir, protocol.VerifyFile), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Chmod(filepath.Join(roundDir, protocol.VerifyFile), 0o644)
	})

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("unreadable verify.json should fail")
	}
	found := false
	for _, e := range result.Errors {
		if len(e) > 0 && e[:len("cannot read ")] == "cannot read " {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'cannot read' error, got %v", result.Errors)
	}
}

func TestCheckRequiredFiles_DonePhaseWithoutRoundDir(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseDone, Round: 1})

	dir := ws.FeatureDir("feat-1")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")

	result := Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Errorf("done phase without round dir should pass (legacy feature), got errors: %v", result.Errors)
	}
}

func TestCheckDependencies_NoDeps(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseInit})
	ws.SaveFeature(protocol.Feature{ID: "feat-1", Name: "No deps"})

	result := CheckDependencies(ws, "feat-1")
	if !result.Pass {
		t.Errorf("feature with no deps should pass, got errors: %v", result.Errors)
	}
}

func TestCheckDependencies_AllDone(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-a")
	ws.SaveFeature(protocol.Feature{ID: "feat-a", Name: "A", Depends: []string{"feat-b", "feat-c"}})
	ws.SaveFeature(protocol.Feature{ID: "feat-b", Name: "B", Status: "done"})
	ws.SaveFeature(protocol.Feature{ID: "feat-c", Name: "C", Status: "done"})

	result := CheckDependencies(ws, "feat-a")
	if !result.Pass {
		t.Errorf("all deps done should pass, got errors: %v", result.Errors)
	}
}

func TestCheckDependencies_NotDone(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-a")
	ws.SaveFeature(protocol.Feature{ID: "feat-a", Name: "A", Depends: []string{"feat-b", "feat-c"}})
	ws.SaveFeature(protocol.Feature{ID: "feat-b", Name: "B", Status: "done"})
	ws.SaveFeature(protocol.Feature{ID: "feat-c", Name: "C", Status: "coding"})

	result := CheckDependencies(ws, "feat-a")
	if result.Pass {
		t.Error("dep not done should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "feat-c") && strings.Contains(e, "coding") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error mentioning feat-c with status coding, got %v", result.Errors)
	}
}

func TestCheckDependencies_MissingDep(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-a")
	ws.SaveFeature(protocol.Feature{ID: "feat-a", Name: "A", Depends: []string{"nonexistent"}})

	result := CheckDependencies(ws, "feat-a")
	if result.Pass {
		t.Error("missing dep should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "nonexistent") && strings.Contains(e, "cannot load") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error about missing dep, got %v", result.Errors)
	}
}

func TestCheckDependencies_NoFeatureYAML(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-a")

	result := CheckDependencies(ws, "feat-a")
	if !result.Pass {
		t.Errorf("missing feature YAML should warn, not fail: errors=%v", result.Errors)
	}
	if len(result.Warns) == 0 {
		t.Error("expected warning about missing feature YAML")
	}
}

func TestCheck_IncludesDependencyCheck(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-a")
	writeState(t, ws, "feat-a", protocol.State{Phase: protocol.PhaseInit})
	ws.SaveFeature(protocol.Feature{ID: "feat-a", Name: "A", Depends: []string{"feat-b"}})
	ws.SaveFeature(protocol.Feature{ID: "feat-b", Name: "B", Status: "coding"})

	result := Check(ws, "feat-a", nil)
	if result.Pass {
		t.Error("Check() should include dependency gate")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "dependencies not done") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dependency error in Check(), got errors: %v", result.Errors)
	}
}

func TestCaptureBaseline_WithGitRepo(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "backend")
	os.MkdirAll(repoDir, 0o755)

	run := func(dir string, args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}

	run(repoDir, "git", "init")
	writeFile(t, filepath.Join(repoDir, "main.go"), "package main")
	run(repoDir, "git", "add", ".")
	run(repoDir, "git", "commit", "-m", "init")

	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir("feat-1"); err != nil {
		t.Fatal(err)
	}

	if err := CaptureBaseline(ws, "feat-1", []string{"backend"}); err != nil {
		t.Fatalf("CaptureBaseline: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.FeatureDir("feat-1"), protocol.BaselineFile))
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var baseline protocol.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	if len(baseline.Repos) != 1 {
		t.Fatalf("repos count = %d, want 1", len(baseline.Repos))
	}
	if baseline.Repos[0].Head == "" {
		t.Error("HEAD should not be empty")
	}
	if len(baseline.Repos[0].DirtyFiles) != 0 {
		t.Errorf("dirty files = %v, want empty", baseline.Repos[0].DirtyFiles)
	}
}
