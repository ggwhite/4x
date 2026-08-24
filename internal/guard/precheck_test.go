package guard

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// swallowedCmd 是 pipe 吞掉 exit code 的違規命令，供多個 precheck 測試共用。
const swallowedCmd = "go test ./... 2>&1 | grep -q PASS"

// writePrecheckStrategy 把 test-strategy.yaml 內容寫進 feature 目錄。
func writePrecheckStrategy(t *testing.T, ws *protocol.Workspace, featureID, content string) {
	t.Helper()
	writeFile(t, filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile), content)
}

// TestPrecheckTestStrategyParseError 壞掉的 YAML（ac_ref 誤寫成陣列）必須是阻擋級錯誤，
// 不能像 checkTestStrategyVerifyTypes / checkACChecksSchema 那樣靜默 return。（AC-8）
func TestPrecheckTestStrategyParseError(t *testing.T) {
	ws := setupGuardWorkspace(t, "F188-parse")
	writePrecheckStrategy(t, ws, "F188-parse",
		"manual_checks: [{id: mc-1, ac_ref: [AC-1, AC-2], description: d, steps: [s]}]\n")

	r := PrecheckTestStrategy(ws, "F188-parse")
	if r.Pass {
		t.Fatal("unparseable test-strategy.yaml must block")
	}
	if r.RetryableErrors <= 0 {
		t.Fatalf("RetryableErrors = %d, want > 0", r.RetryableErrors)
	}
	if !strings.Contains(strings.Join(r.Errors, "\n"), "parse failed") {
		t.Fatalf("expected an error containing %q, got %v", "parse failed", r.Errors)
	}
}

// TestPrecheckTestStrategyCommandViolation 違規命令的錯誤訊息必須同時點出命令原文、
// rule ID 與來源標記，Designer 才能直接定位要改哪一條。（AC-9）
func TestPrecheckTestStrategyCommandViolation(t *testing.T) {
	ws := setupGuardWorkspace(t, "F188-cmd")
	writePrecheckStrategy(t, ws, "F188-cmd",
		"verify_commands:\n  - make test\nac_checks:\n  AC-1: [\""+swallowedCmd+"\"]\n")

	r := PrecheckTestStrategy(ws, "F188-cmd")
	if r.Pass {
		t.Fatal("exit-code-swallowing ac_check must block")
	}
	if r.RetryableErrors != len(r.Errors) {
		t.Fatalf("RetryableErrors = %d, want %d (all precheck errors are retryable)", r.RetryableErrors, len(r.Errors))
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, swallowedCmd) && strings.Contains(e, "exit-code-swallowed") && strings.Contains(e, "ac_checks[AC-1]") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected one error containing command + rule ID + source, got %v", r.Errors)
	}
}

// TestPrecheckTestStrategyAbsent test-strategy.yaml 不存在時不擋（向下相容）。（AC-10）
func TestPrecheckTestStrategyAbsent(t *testing.T) {
	ws := setupGuardWorkspace(t, "F188-absent")

	r := PrecheckTestStrategy(ws, "F188-absent")
	if !r.Pass || len(r.Errors) != 0 {
		t.Fatalf("missing test-strategy.yaml must pass, got Pass=%v errors=%v", r.Pass, r.Errors)
	}
}

// TestPrecheckTestStrategySources 三個來源各一筆違規時，Source 標記各自正確。（AC-6 的 guard 側覆核）
func TestPrecheckTestStrategySources(t *testing.T) {
	ws := setupGuardWorkspace(t, "F188-src")
	writePrecheckStrategy(t, ws, "F188-src",
		"verify_groups:\n  core: [\""+swallowedCmd+"\"]\nac_checks:\n  AC-1: [\""+swallowedCmd+"\"]\n")

	r := PrecheckTestStrategy(ws, "F188-src")
	joined := strings.Join(r.Errors, "\n")
	for _, want := range []string{"verify_groups[core]", "ac_checks[AC-1]"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected source %q in errors, got %v", want, r.Errors)
		}
	}
}

// TestCheckRegistersVerifyPrecheck guard.Check 已註冊 precheck：手跑 4x check 看得到同一批錯誤。（AC-11）
func TestCheckRegistersVerifyPrecheck(t *testing.T) {
	ws := setupGuardWorkspace(t, "F188-reg")
	writeState(t, ws, "F188-reg", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})
	writePrecheckStrategy(t, ws, "F188-reg", "ac_checks:\n  AC-1: [\""+swallowedCmd+"\"]\n")

	r := Check(ws, "F188-reg", nil)
	for _, e := range r.Errors {
		if strings.Contains(e, swallowedCmd) && strings.Contains(e, "exit-code-swallowed") {
			return
		}
	}
	t.Fatalf("guard.Check must surface the precheck error, got %v", r.Errors)
}

// TestCheckSkipsVerifyPrecheckOutsideDesigning coding phase 的 guard.Check 不得受
// test-strategy.yaml 的違規命令影響——Coder 不能改該檔，這裡若擋下就是不可自癒的停機。
func TestCheckSkipsVerifyPrecheckOutsideDesigning(t *testing.T) {
	ws := setupGuardWorkspace(t, "F188-skip")
	writeState(t, ws, "F188-skip", protocol.State{Phase: protocol.PhaseCoding, Round: 1})
	writePrecheckStrategy(t, ws, "F188-skip", "ac_checks:\n  AC-1: [\""+swallowedCmd+"\"]\n")

	for _, e := range Check(ws, "F188-skip", nil).Errors {
		if strings.Contains(e, "exit-code-swallowed") {
			t.Fatalf("precheck must not run outside designing, got %q", e)
		}
	}
}
