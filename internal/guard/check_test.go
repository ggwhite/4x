package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
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
	// 存一份無 repos 的 feature YAML：F081 後 checkScope 對 YAML 載入失敗 fail-closed，
	// 故每個 workspace 都需有可載入的 feature；無 repos 讓 scope check 早退、不干擾其他斷言。
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: featureID}); err != nil {
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

func TestCheckRequiredFiles_CodingPhaseRequiresDesignReviewWhenProfileEnabled(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	writeState(t, ws, "feat-1", protocol.State{Phase: protocol.PhaseCoding, Profile: "full"})

	dir := ws.FeatureDir("feat-1")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")

	result := Check(ws, "feat-1", nil)
	if result.Pass {
		t.Fatal("coding phase with full profile should require design-review-report.md")
	}
	found := false
	for _, e := range result.Errors {
		if e == "required file missing: "+protocol.DesignReviewReport {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing design review report, got %v", result.Errors)
	}

	writeFile(t, filepath.Join(dir, protocol.DesignReviewReport), "# Design Review\n\n## Verdict\nPASS\n")
	result = Check(ws, "feat-1", nil)
	if !result.Pass {
		t.Fatalf("coding phase with design review report should pass, got %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_AllArtifactsPresent(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS (1.23s)"}}}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("complete tester artifacts should pass, got errors: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_MissingFinalReport(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("missing final-report.md should fail")
	}
	found := false
	for _, err := range result.Errors {
		if err == "required file missing: "+protocol.FinalReport {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing final-report error, got %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_MissingVerifyJSONReportedOnce(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	// 只缺 verify.json，其餘 required 檔齊全。
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("missing verify.json should fail")
	}
	verifyErrs := 0
	for _, err := range result.Errors {
		if strings.Contains(err, protocol.VerifyFile) {
			verifyErrs++
		}
	}
	if verifyErrs != 1 {
		t.Fatalf("missing verify.json should produce exactly 1 error, got %d: %v", verifyErrs, result.Errors)
	}
	// 保留資訊量較高的那條（讀檔區訊息），而非 required 迴圈的 "required file missing"。
	if !strings.Contains(result.Errors[0], "the tester likely could not run") {
		t.Fatalf("should keep the informative not-found message, got %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_MissingTestReportStillReported(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	// verify.json 齊全且通過，但缺 test-report.md → required 迴圈仍須報它。
	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("missing test-report.md should fail")
	}
	found := false
	for _, err := range result.Errors {
		if strings.Contains(err, protocol.TestReport) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing test-report error, got %v", result.Errors)
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

func TestCheckTestingToAccepting_ManualChecksPass(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(featureDir, protocol.TestStratFile),
		"manual_checks:\n  - id: mc-1\n    description: test routing\n    steps:\n      - curl localhost\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{
		Passed:    true,
		Round:     1,
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS (0.5s)"}}},
		ManualCheckResults: []protocol.ManualCheckResult{
			{ID: "mc-1", Passed: true, Evidence: []string{"curl returned 200"}},
		},
	})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("manual checks with results should pass, got errors: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_ManualCheckMissingResult(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(featureDir, protocol.TestStratFile),
		"manual_checks:\n  - id: mc-1\n    description: test routing\n    steps:\n      - curl localhost\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{
		Passed:    true,
		Round:     1,
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS (0.5s)"}}},
	})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("manual check without result should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "mc-1") && strings.Contains(e, "no result") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about missing mc-1 result, got %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_ManualCheckEmptyEvidence(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	writeFile(t, filepath.Join(featureDir, protocol.TestStratFile),
		"manual_checks:\n  - id: mc-1\n    description: test routing\n    steps:\n      - curl localhost\n")

	data, _ := json.Marshal(protocol.VerifyEvidence{
		Passed:    true,
		Round:     1,
		ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS (0.5s)"}}},
		ManualCheckResults: []protocol.ManualCheckResult{
			{ID: "mc-1", Passed: true, Evidence: []string{}},
		},
	})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("manual check with empty evidence should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "mc-1") && strings.Contains(e, "empty evidence") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about empty evidence, got %v", result.Errors)
	}
}

func TestCheckResult_AllRetryable(t *testing.T) {
	t.Run("all retryable", func(t *testing.T) {
		r := CheckResult{Pass: false, Errors: []string{"a", "b"}, RetryableErrors: 2}
		if !r.AllRetryable() {
			t.Fatal("expected AllRetryable true when all errors are retryable")
		}
	})
	t.Run("mixed", func(t *testing.T) {
		r := CheckResult{Pass: false, Errors: []string{"a", "b"}, RetryableErrors: 1}
		if r.AllRetryable() {
			t.Fatal("expected AllRetryable false when not all errors are retryable")
		}
	})
	t.Run("passing", func(t *testing.T) {
		r := CheckResult{Pass: true}
		if r.AllRetryable() {
			t.Fatal("expected AllRetryable false when pass is true")
		}
	})
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
	if err := ws.SaveFeature(feature.Feature{ID: "feat-1", Name: "Feature One", Status: "done"}); err != nil {
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
	if err := ws.SaveFeature(feature.Feature{ID: "feat-1", Name: "Feature One", Status: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: "feat-2", Name: "Feature Two", Status: "done"}); err != nil {
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

	f := feature.Feature{ID: "feat-1", Name: "Test"}
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
	ws.SaveFeature(feature.Feature{ID: "feat-1", Name: "No deps"})

	result := CheckDependencies(ws, "feat-1")
	if !result.Pass {
		t.Errorf("feature with no deps should pass, got errors: %v", result.Errors)
	}
}

func TestCheckDependencies_AllDone(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-a")
	ws.SaveFeature(feature.Feature{ID: "feat-a", Name: "A", Depends: []string{"feat-b", "feat-c"}})
	ws.SaveFeature(feature.Feature{ID: "feat-b", Name: "B", Status: "done"})
	ws.SaveFeature(feature.Feature{ID: "feat-c", Name: "C", Status: "done"})

	result := CheckDependencies(ws, "feat-a")
	if !result.Pass {
		t.Errorf("all deps done should pass, got errors: %v", result.Errors)
	}
}

func TestCheckDependencies_NotDone(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-a")
	ws.SaveFeature(feature.Feature{ID: "feat-a", Name: "A", Depends: []string{"feat-b", "feat-c"}})
	ws.SaveFeature(feature.Feature{ID: "feat-b", Name: "B", Status: "done"})
	ws.SaveFeature(feature.Feature{ID: "feat-c", Name: "C", Status: "coding"})

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
	ws.SaveFeature(feature.Feature{ID: "feat-a", Name: "A", Depends: []string{"nonexistent"}})

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
	// setupGuardWorkspace 預設會存一份 feature YAML；本測試刻意驗證「YAML 缺失」情境，
	// 故移除它，讓 checkDependencies 走 load 失敗的 warn 分支。
	if err := os.Remove(filepath.Join(ws.DotDir(), protocol.FeaturesDir, "feat-a.yaml")); err != nil {
		t.Fatal(err)
	}

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
	ws.SaveFeature(feature.Feature{ID: "feat-a", Name: "A", Depends: []string{"feat-b"}})
	ws.SaveFeature(feature.Feature{ID: "feat-b", Name: "B", Status: feature.StatusInProgress})

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

func TestCheckBuildGate_SkipsNonCodingPhase(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-skip")
	writeState(t, ws, "F999-skip", protocol.State{Phase: protocol.PhaseReviewing, Round: 1})
	r := Check(ws, "F999-skip", nil)
	bgPath := filepath.Join(ws.RoundDir("F999-skip", 1), protocol.BuildGateFile)
	if _, err := os.Stat(bgPath); err == nil {
		t.Fatal("build-gate.json should not exist for reviewing phase")
	}
	_ = r
}

func TestCheckBuildGate_NoBuildLintCommands(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-nobuild")
	writeState(t, ws, "F999-nobuild", protocol.State{Phase: protocol.PhaseCoding, Round: 1})
	dir := ws.FeatureDir("F999-nobuild")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	r := Check(ws, "F999-nobuild", nil)
	if !r.Pass {
		t.Fatalf("expected pass (no build/lint is a warn, not error), got errors: %v", r.Errors)
	}
	found := false
	for _, w := range r.Warns {
		if strings.Contains(w, "build-gate") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a build-gate warning about no commands")
	}
}

func TestCheckBuildGate_CodingPhaseSuccess(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-bgpass")
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
			Lint:  []string{"echo lint-ok"},
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))

	writeState(t, ws, "F999-bgpass", protocol.State{Phase: protocol.PhaseCoding, Round: 1})
	dir := ws.FeatureDir("F999-bgpass")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	os.MkdirAll(ws.RoundDir("F999-bgpass", 1), 0o755)

	r := Check(ws, "F999-bgpass", nil)
	if !r.Pass {
		t.Fatalf("expected pass, got errors: %v", r.Errors)
	}
	bgPath := filepath.Join(ws.RoundDir("F999-bgpass", 1), protocol.BuildGateFile)
	data, err := os.ReadFile(bgPath)
	if err != nil {
		t.Fatalf("build-gate.json not written: %v", err)
	}
	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("invalid build-gate.json: %v", err)
	}
	if !ev.Passed {
		t.Fatal("expected build-gate passed=true")
	}
}

func TestCheckBuildGate_CodingPhaseFail(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-bgfail")
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"false"},
			Lint:  []string{"echo lint-ok"},
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))

	writeState(t, ws, "F999-bgfail", protocol.State{Phase: protocol.PhaseCoding, Round: 1})
	dir := ws.FeatureDir("F999-bgfail")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	os.MkdirAll(ws.RoundDir("F999-bgfail", 1), 0o755)

	r := Check(ws, "F999-bgfail", nil)
	if r.Pass {
		t.Fatal("expected fail when build command fails")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "build-gate") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error mentioning build-gate, got: %v", r.Errors)
	}
	bgPath := filepath.Join(ws.RoundDir("F999-bgfail", 1), protocol.BuildGateFile)
	data, _ := os.ReadFile(bgPath)
	var ev protocol.VerifyEvidence
	json.Unmarshal(data, &ev)
	if ev.Passed {
		t.Fatal("expected build-gate passed=false")
	}
	for _, cmd := range ev.Commands {
		if cmd.Command == "echo lint-ok" && !cmd.Skipped {
			t.Fatal("lint should be skipped when build fails")
		}
	}
}

func TestCheckBuildGate_AmendingPhase(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-amend")
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))

	writeState(t, ws, "F999-amend", protocol.State{Phase: protocol.PhaseAmending, Round: 1})
	dir := ws.FeatureDir("F999-amend")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	os.MkdirAll(ws.RoundDir("F999-amend", 1), 0o755)

	r := Check(ws, "F999-amend", nil)
	if !r.Pass {
		t.Fatalf("expected pass for amending phase, got errors: %v", r.Errors)
	}
	bgPath := filepath.Join(ws.RoundDir("F999-amend", 1), protocol.BuildGateFile)
	if _, err := os.Stat(bgPath); err != nil {
		t.Fatal("build-gate.json should be written for amending phase")
	}
}
