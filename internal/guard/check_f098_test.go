package guard

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// fakeSelfModOps 同時實作 ScopeDetector 與 SelfModDetector，供注入可控的檔案層變更。
type fakeSelfModOps struct {
	files []protocol.ChangedFile
}

func (f *fakeSelfModOps) DetectChangedRepos(string) []string { return nil }
func (f *fakeSelfModOps) DetectChangedFiles(string) []protocol.ChangedFile {
	return f.files
}

// prepCodingWorkspace 建立可讓 Check 在 coding phase 不因缺產出物而失敗的 workspace。
func prepCodingWorkspace(t *testing.T, featureID string) *protocol.Workspace {
	t.Helper()
	ws := setupGuardWorkspace(t, featureID)
	writeState(t, ws, featureID, protocol.State{FeatureID: featureID, Phase: protocol.PhaseCoding, Round: 1})
	featureDir := ws.FeatureDir(featureID)
	writeFile(t, filepath.Join(featureDir, protocol.TaskBrief), "# Task Brief\n## Premise Challenge\n- verified\n")
	writeFile(t, filepath.Join(featureDir, protocol.Criteria), "# Acceptance Criteria\n")
	return ws
}

func hasError(errs []string, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

// TestCheck_SelfModTouchedMarked 驗證觸及受保護路徑時 Check 標記 touched 與路徑，
// 但在 budget 內不令 Pass=false（該輪能續跑 review/test）。
func TestCheck_SelfModTouchedMarked(t *testing.T) {
	ws := prepCodingWorkspace(t, "feat-selfmod")
	ops := &fakeSelfModOps{files: []protocol.ChangedFile{
		{Path: "internal/guard/check.go", Lines: 10},
		{Path: "cmd/4x/run.go", Lines: 5},
	}}

	r := Check(ws, "feat-selfmod", ops)

	if !r.SelfModTouched {
		t.Error("SelfModTouched should be true")
	}
	if len(r.SelfModPaths) != 1 || r.SelfModPaths[0] != "internal/guard/check.go" {
		t.Errorf("SelfModPaths = %v, want [internal/guard/check.go]", r.SelfModPaths)
	}
	if r.SelfModDiffLines != 10 {
		t.Errorf("SelfModDiffLines = %d, want 10", r.SelfModDiffLines)
	}
	if hasError(r.Errors, "exceeds budget") {
		t.Errorf("under-budget touch must not fail: %v", r.Errors)
	}
	if !r.Pass {
		t.Errorf("under-budget touch should still pass, errors: %v", r.Errors)
	}
}

// TestCheck_SelfModDiffBudgetExceeded 驗證受保護 diff 超過上限時擋下（Pass=false）。
func TestCheck_SelfModDiffBudgetExceeded(t *testing.T) {
	ws := prepCodingWorkspace(t, "feat-budget")
	ops := &fakeSelfModOps{files: []protocol.ChangedFile{
		{Path: "internal/state/machine.go", Lines: DefaultSelfModMaxDiffLines + 1},
	}}

	r := Check(ws, "feat-budget", ops)

	if r.Pass {
		t.Error("over-budget self-mod must fail")
	}
	if !hasError(r.Errors, "exceeds budget") {
		t.Errorf("expected budget error, got: %v", r.Errors)
	}
}

// TestCheck_SelfModUntouched 驗證未觸及受保護路徑時不標記。
func TestCheck_SelfModUntouched(t *testing.T) {
	ws := prepCodingWorkspace(t, "feat-clean")
	ops := &fakeSelfModOps{files: []protocol.ChangedFile{
		{Path: "cmd/4x/run.go", Lines: 300},
	}}

	r := Check(ws, "feat-clean", ops)

	if r.SelfModTouched {
		t.Error("SelfModTouched should be false for non-protected changes")
	}
}

// TestCheck_SelfMod_Idempotent 驗證對未變動輸入連跑兩次 Check，self-mod 結果穩定一致。
func TestCheck_SelfMod_Idempotent(t *testing.T) {
	ws := prepCodingWorkspace(t, "feat-idem")
	ops := &fakeSelfModOps{files: []protocol.ChangedFile{
		{Path: "internal/guard/check.go", Lines: 10},
		{Path: "internal/protocol/types.go", Lines: 7},
		{Path: "cmd/4x/run.go", Lines: 5},
	}}

	first := Check(ws, "feat-idem", ops)
	second := Check(ws, "feat-idem", ops)

	if first.SelfModTouched != second.SelfModTouched {
		t.Errorf("SelfModTouched not stable: %v vs %v", first.SelfModTouched, second.SelfModTouched)
	}
	if first.SelfModDiffLines != second.SelfModDiffLines {
		t.Errorf("SelfModDiffLines not stable: %d vs %d", first.SelfModDiffLines, second.SelfModDiffLines)
	}
	if first.Pass != second.Pass {
		t.Errorf("Pass not stable: %v vs %v", first.Pass, second.Pass)
	}
	if strings.Join(first.SelfModPaths, ",") != strings.Join(second.SelfModPaths, ",") {
		t.Errorf("SelfModPaths not stable: %v vs %v", first.SelfModPaths, second.SelfModPaths)
	}
}

// TestCheckSelfModTestGate 驗證 test-gate：缺對應測試 FAIL、有對應測試 PASS。
func TestCheckSelfModTestGate(t *testing.T) {
	t.Run("缺測試 → FAIL", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "feat-gate-fail")
		writeState(t, ws, "feat-gate-fail", protocol.State{
			FeatureID:      "feat-gate-fail",
			SelfModTouched: true,
			SelfModPaths:   []string{"internal/guard/check.go"},
		})
		r := CheckResult{Pass: true}
		checkSelfModTestGate(ws, "feat-gate-fail", &r)
		if r.Pass {
			t.Error("missing accompanying test must fail the gate")
		}
		if !hasError(r.Errors, "require accompanying passing tests") {
			t.Errorf("expected test-gate error, got: %v", r.Errors)
		}
	})

	t.Run("有測試 → PASS", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "feat-gate-pass")
		writeState(t, ws, "feat-gate-pass", protocol.State{
			FeatureID:      "feat-gate-pass",
			SelfModTouched: true,
			SelfModPaths:   []string{"internal/guard/check.go", "internal/guard/check_test.go"},
		})
		r := CheckResult{Pass: true}
		checkSelfModTestGate(ws, "feat-gate-pass", &r)
		if !r.Pass {
			t.Errorf("accompanying test present should pass, errors: %v", r.Errors)
		}
	})

	t.Run("未觸及 → 不檢查", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "feat-gate-none")
		writeState(t, ws, "feat-gate-none", protocol.State{FeatureID: "feat-gate-none"})
		r := CheckResult{Pass: true}
		checkSelfModTestGate(ws, "feat-gate-none", &r)
		if !r.Pass {
			t.Errorf("untouched feature must not be gated, errors: %v", r.Errors)
		}
	})
}

// TestCheck_SelfModTestFilesExemptFromBudget 驗證 F155：受保護路徑的 *_test.go
// 行數不計入 diff budget（補測不應觸頂），但路徑仍列入 SelfModPaths 供 test-gate
// 判定「附帶測試」。
func TestCheck_SelfModTestFilesExemptFromBudget(t *testing.T) {
	ws := prepCodingWorkspace(t, "feat-testbudget")
	ops := &fakeSelfModOps{files: []protocol.ChangedFile{
		{Path: "internal/state/machine.go", Lines: DefaultSelfModMaxDiffLines - 10},
		{Path: "internal/state/machine_test.go", Lines: 500},
	}}

	r := Check(ws, "feat-testbudget", ops)

	if hasError(r.Errors, "exceeds budget") {
		t.Errorf("test-file lines must not count toward budget, got: %v", r.Errors)
	}
	if r.SelfModDiffLines != DefaultSelfModMaxDiffLines-10 {
		t.Errorf("SelfModDiffLines = %d, want %d (prod lines only)", r.SelfModDiffLines, DefaultSelfModMaxDiffLines-10)
	}
	if len(r.SelfModPaths) != 2 {
		t.Errorf("SelfModPaths = %v, want both prod and test paths kept", r.SelfModPaths)
	}
}

// TestCheck_SelfModProdOverBudgetWithTests 驗證 production 行數自身超限時，
// 即使附帶測試檔仍要擋下。
func TestCheck_SelfModProdOverBudgetWithTests(t *testing.T) {
	ws := prepCodingWorkspace(t, "feat-prodover")
	ops := &fakeSelfModOps{files: []protocol.ChangedFile{
		{Path: "internal/state/machine.go", Lines: DefaultSelfModMaxDiffLines + 1},
		{Path: "internal/state/machine_test.go", Lines: 10},
	}}

	r := Check(ws, "feat-prodover", ops)

	if r.Pass || !hasError(r.Errors, "exceeds budget") {
		t.Errorf("prod lines over budget must still fail, errors: %v", r.Errors)
	}
}
