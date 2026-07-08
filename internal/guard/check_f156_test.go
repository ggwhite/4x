package guard

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// writeTestStrategyF156 寫入指定內容的 test-strategy.yaml 到 feature dir。
func writeTestStrategyF156(t *testing.T, ws *protocol.Workspace, featureID, content string) {
	t.Helper()
	writeFile(t, filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile), content)
}

// hasErrContaining 回傳 errors 中是否有任一項包含 sub。
func hasErrContaining(errs []string, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}

// TestCheckACChecksSchema 驗證 guard 對 ac_checks 的假驗證 lint：假驗證命令 → 擋且 error 指名該 AC；
// 合法命令 → 不因此擋。（AC-6）
func TestCheckACChecksSchema(t *testing.T) {
	t.Run("fake-verification-blocked", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-fake")
		writeTestStrategyF156(t, ws, "F156-fake", "verify_commands:\n  - make test\nac_checks:\n  AC-1: [\"echo PASS\"]\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-fake", &r)
		if r.Pass {
			t.Fatal("expected fake-verification command to block")
		}
		if !hasErrContaining(r.Errors, "AC-1") {
			t.Fatalf("expected error naming AC-1, got %v", r.Errors)
		}
	})

	t.Run("empty-command-list-blocked", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-empty")
		writeTestStrategyF156(t, ws, "F156-empty", "verify_commands:\n  - make test\nac_checks:\n  AC-1: []\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-empty", &r)
		if r.Pass {
			t.Fatal("expected empty command list to block")
		}
		if !hasErrContaining(r.Errors, "AC-1") {
			t.Fatalf("expected error naming AC-1, got %v", r.Errors)
		}
	})

	t.Run("legit-command-not-blocked", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-legit")
		writeTestStrategyF156(t, ws, "F156-legit", "verify_commands:\n  - make test\nac_checks:\n  AC-1: [\"go test ./...\"]\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-legit", &r)
		if !r.Pass {
			t.Fatalf("legit ac_checks command should not block, got errors: %v", r.Errors)
		}
	})

	t.Run("allowlist-token-not-blocked", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-allow")
		writeTestStrategyF156(t, ws, "F156-allow", "verify_commands:\n  - make test\nac_checks:\n  AC-1: [\"grep -q sym gen.go # 4x-lint:allow\"]\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-allow", &r)
		if !r.Pass {
			t.Fatalf("4x-lint:allow should bypass linter, got errors: %v", r.Errors)
		}
	})
}

// TestCheckACChecksConsistency 驗證 guard 對綁定 ac_checks 的 AC 以 exit code 為權威：
// (a) checks 為空 → 擋；(b) passed 與重算不一致 → 擋；(c) 一致且純文字 evidence → 豁免正則、通過。（AC-7）
func TestCheckACChecksConsistency(t *testing.T) {
	ts := protocol.TestStrategy{
		ACVerifyMap: map[string]string{"AC-1": "unit-test"},
		ACChecks:    map[string][]string{"AC-1": {"go test ./..."}},
	}

	t.Run("missing-checks-blocked", func(t *testing.T) {
		ev := protocol.VerifyEvidence{
			ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"looks fine"}}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, ev, &r)
		if r.Pass {
			t.Fatal("expected block when ac_checks-bound AC has no checks")
		}
		if !hasErrContaining(r.Errors, "AC-1") || !hasErrContaining(r.Errors, "re-run 4x verify") {
			t.Fatalf("expected re-run hint naming AC-1, got %v", r.Errors)
		}
	})

	t.Run("liar-passed-blocked", func(t *testing.T) {
		// checks 全 exit 0 但 ac.passed=false → 不一致 → 擋。
		ev := protocol.VerifyEvidence{
			ACResults: []protocol.ACEvidence{{
				ID:       "AC-1",
				Passed:   false,
				Evidence: []string{"$ go test → exit 0"},
				Checks:   []protocol.VerifyCommand{{Command: "go test ./...", ExitCode: 0}},
			}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, ev, &r)
		if r.Pass {
			t.Fatal("expected block when passed disagrees with exit codes")
		}
		if !hasErrContaining(r.Errors, "AC-1") {
			t.Fatalf("expected error naming AC-1, got %v", r.Errors)
		}
	})

	t.Run("missing-entry-blocked", func(t *testing.T) {
		// test-strategy 宣告 AC-1 的 ac_checks，但 verify.json 只留另一條 passing AC、
		// 整筆刪掉 AC-1 → 不可讓刪 entry 繞過 exit-code 重算，須擋。
		ev := protocol.VerifyEvidence{
			ACResults: []protocol.ACEvidence{{ID: "AC-2", Passed: true, Evidence: []string{"unrelated"}}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, ev, &r)
		if r.Pass {
			t.Fatal("expected block when ac_checks-bound AC entry is entirely missing from verify.json")
		}
		if !hasErrContaining(r.Errors, "AC-1") || !hasErrContaining(r.Errors, "re-run 4x verify") {
			t.Fatalf("expected re-run hint naming AC-1, got %v", r.Errors)
		}
	})

	t.Run("consistent-plain-evidence-exempt", func(t *testing.T) {
		// checks 全 exit 0、passed=true、evidence 為純文字（無 $/PASS 樣式）→ 豁免 executionPattern，通過。
		ev := protocol.VerifyEvidence{
			ACResults: []protocol.ACEvidence{{
				ID:       "AC-1",
				Passed:   true,
				Evidence: []string{"the checks all succeeded"},
				Checks:   []protocol.VerifyCommand{{Command: "go test ./...", ExitCode: 0}},
			}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, ev, &r)
		if !r.Pass {
			t.Fatalf("ac_checks-bound AC should be exempt from prose executionPattern, got errors: %v", r.Errors)
		}
	})
}

// TestACChecksFailingACBlocks 驗證綁定 ac_checks 的 AC 重算失敗時一律擋，
// 且 top-level passed=true 時額外報謊報 error——手改 top-level passed 不可繞過 guard。
// （review Blocker 2）
func TestACChecksFailingACBlocks(t *testing.T) {
	ts := protocol.TestStrategy{
		ACChecks: map[string][]string{"AC-1": {"go test ./..."}},
	}

	t.Run("consistent-failure-still-blocks", func(t *testing.T) {
		// ac.Passed=false 與 checks exit 1 一致（無謊報），但 AC 實際失敗 → 仍須擋。
		ev := protocol.VerifyEvidence{
			Passed: true, // 手改 top-level
			ACResults: []protocol.ACEvidence{{
				ID:       "AC-1",
				Passed:   false,
				Evidence: []string{"$ go test → exit 1"},
				Checks:   []protocol.VerifyCommand{{Command: "go test ./...", ExitCode: 1}},
			}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, ev, &r)
		if r.Pass {
			t.Fatal("expected block when an ac_checks-bound AC fails by exit code")
		}
		if !hasErrContaining(r.Errors, "AC-1") {
			t.Fatalf("expected error naming AC-1, got %v", r.Errors)
		}
	})

	t.Run("top-level-passed-lie-blocked", func(t *testing.T) {
		ev := protocol.VerifyEvidence{
			Passed: true,
			ACResults: []protocol.ACEvidence{{
				ID:       "AC-1",
				Passed:   false,
				Evidence: []string{"$ go test → exit 1"},
				Checks:   []protocol.VerifyCommand{{Command: "go test ./...", ExitCode: 1}},
			}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, ev, &r)
		if !hasErrContaining(r.Errors, "claims passed=true") {
			t.Fatalf("expected top-level passed lie error, got %v", r.Errors)
		}
	})
}

// TestACChecksSkippedBypassBlocked 驗證把 checks 全標 skipped 不能充當通過：
// 重算採「全部執行且 exit 0」語意，skipped 條目視為未達成。（review Blocker 3）
func TestACChecksSkippedBypassBlocked(t *testing.T) {
	ts := protocol.TestStrategy{
		ACChecks: map[string][]string{"AC-1": {"go test ./..."}},
	}
	ev := protocol.VerifyEvidence{
		Passed: true,
		ACResults: []protocol.ACEvidence{{
			ID:       "AC-1",
			Passed:   true,
			Evidence: []string{"$ go test → skipped"},
			Checks:   []protocol.VerifyCommand{{Command: "go test ./...", ExitCode: 1, Skipped: true}},
		}},
	}
	r := CheckResult{Pass: true}
	checkACEvidence(ts, ev, &r)
	if r.Pass {
		t.Fatal("expected block when all checks are marked skipped")
	}
	if !hasErrContaining(r.Errors, "AC-1") {
		t.Fatalf("expected error naming AC-1, got %v", r.Errors)
	}
}

// TestACChecksCommandSubstitutionBlocked 驗證 verify.json 記錄的 check 命令必須與
// test-strategy.yaml 宣告的 ac_checks 一致（SoT 比對）——整組換成假命令即擋。（review Finding 4）
func TestACChecksCommandSubstitutionBlocked(t *testing.T) {
	ts := protocol.TestStrategy{
		ACChecks: map[string][]string{"AC-1": {"go test ./..."}},
	}

	t.Run("substituted-command-blocked", func(t *testing.T) {
		ev := protocol.VerifyEvidence{
			Passed: true,
			ACResults: []protocol.ACEvidence{{
				ID:       "AC-1",
				Passed:   true,
				Evidence: []string{"$ true → exit 0"},
				Checks:   []protocol.VerifyCommand{{Command: "true", ExitCode: 0}},
			}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, ev, &r)
		if r.Pass {
			t.Fatal("expected block when recorded check commands differ from test-strategy ac_checks")
		}
		if !hasErrContaining(r.Errors, "AC-1") || !hasErrContaining(r.Errors, "do not match") {
			t.Fatalf("expected command-mismatch error naming AC-1, got %v", r.Errors)
		}
	})

	t.Run("count-mismatch-blocked", func(t *testing.T) {
		ev := protocol.VerifyEvidence{
			Passed: true,
			ACResults: []protocol.ACEvidence{{
				ID:       "AC-1",
				Passed:   true,
				Evidence: []string{"$ go test → exit 0"},
				Checks: []protocol.VerifyCommand{
					{Command: "go test ./...", ExitCode: 0},
					{Command: "true", ExitCode: 0},
				},
			}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, ev, &r)
		if r.Pass {
			t.Fatal("expected block when recorded check count differs from test-strategy ac_checks")
		}
	})
}

// TestACChecksTimeoutExplicitError 驗證 ctx 超時導致的 check 失敗給明確 timeout 訊息，
// 而非籠統的 AC failed。（review Finding 7）
func TestACChecksTimeoutExplicitError(t *testing.T) {
	ts := protocol.TestStrategy{
		ACChecks: map[string][]string{"AC-1": {"go test ./..."}},
	}
	ev := protocol.VerifyEvidence{
		Passed: false,
		ACResults: []protocol.ACEvidence{{
			ID:       "AC-1",
			Passed:   false,
			Evidence: []string{"$ go test → exit -1"},
			Checks:   []protocol.VerifyCommand{{Command: "go test ./...", ExitCode: -1, Error: "timeout"}},
		}},
	}
	r := CheckResult{Pass: true}
	checkACEvidence(ts, ev, &r)
	if r.Pass {
		t.Fatal("expected block when a check timed out")
	}
	if !hasErrContaining(r.Errors, "timeout") {
		t.Fatalf("expected explicit timeout error, got %v", r.Errors)
	}
}

// TestBackwardCompatNoACChecks 反向回歸 pin：test-strategy 無 ac_checks 時，checkACChecksSchema 立即
// return（不擋），checkACEvidence 走既有 prose-evidence 路徑（execution 類需執行輸出）。（AC-8 guard 部分）
func TestBackwardCompatNoACChecks(t *testing.T) {
	t.Run("schema-returns-immediately", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-bc")
		// 有 ac_verify_map 的 execution 類 AC，但完全未宣告 ac_checks → 不應觸發完整性強制。
		writeTestStrategyF156(t, ws, "F156-bc", "verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-bc", &r)
		if !r.Pass {
			t.Fatalf("no ac_checks should not block (backward compat), got errors: %v", r.Errors)
		}
		if len(r.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", r.Errors)
		}
	})

	t.Run("evidence-uses-prose-path", func(t *testing.T) {
		ts := protocol.TestStrategy{ACVerifyMap: map[string]string{"AC-1": "unit-test"}}
		// execution 類 AC 需要執行輸出樣式 evidence——純文字 evidence 應被既有路徑擋下。
		evProse := protocol.VerifyEvidence{
			ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"just words"}}},
		}
		r := CheckResult{Pass: true}
		checkACEvidence(ts, evProse, &r)
		if r.Pass {
			t.Fatal("execution-type AC with no execution output should block (prose path unchanged)")
		}

		// 含執行輸出樣式 → 通過。
		evExec := protocol.VerifyEvidence{
			ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"$ go test → PASS (0.5s)"}}},
		}
		r2 := CheckResult{Pass: true}
		checkACEvidence(ts, evExec, &r2)
		if !r2.Pass {
			t.Fatalf("execution-type AC with execution output should pass, got errors: %v", r2.Errors)
		}
	})
}

// TestCheckACChecksRequired 完整性強制（DR-3 presence-based 契約）：宣告 ac_checks 時每條 execution
// 類 AC 必須綁 check，缺者擋；inspection 類豁免；未宣告則不觸發。（AC-11）
func TestCheckACChecksRequired(t *testing.T) {
	t.Run("unbound-execution-ac-blocked", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-req-a")
		// 宣告 ac_checks（AC-1）但 ac_verify_map 的 AC-2（unit-test）未綁 → 擋、error 含 AC-2。
		writeTestStrategyF156(t, ws, "F156-req-a", "verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n  AC-2: unit-test\nac_checks:\n  AC-1: [\"go test ./...\"]\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-req-a", &r)
		if r.Pass {
			t.Fatal("expected block when an execution-type AC lacks ac_checks")
		}
		if !hasErrContaining(r.Errors, "AC-2") {
			t.Fatalf("expected error naming AC-2, got %v", r.Errors)
		}
	})

	t.Run("all-bound-not-blocked", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-req-b")
		writeTestStrategyF156(t, ws, "F156-req-b", "verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n  AC-2: integration\nac_checks:\n  AC-1: [\"go test ./...\"]\n  AC-2: [\"bin/app --help | grep -q x\"]\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-req-b", &r)
		if !r.Pass {
			t.Fatalf("all execution ACs bound should not block, got errors: %v", r.Errors)
		}
	})

	t.Run("inspection-ac-exempt", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-req-c")
		// AC-2 改標 inspection → 缺 check 不擋。
		writeTestStrategyF156(t, ws, "F156-req-c", "verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n  AC-2: inspection\nac_checks:\n  AC-1: [\"go test ./...\"]\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-req-c", &r)
		if !r.Pass {
			t.Fatalf("inspection-type AC without check should not block, got errors: %v", r.Errors)
		}
	})

	t.Run("no-ac-checks-not-triggered", func(t *testing.T) {
		ws := setupGuardWorkspace(t, "F156-req-d")
		writeTestStrategyF156(t, ws, "F156-req-d", "verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n  AC-2: unit-test\n")
		r := CheckResult{Pass: true}
		checkACChecksSchema(ws, "F156-req-d", &r)
		if !r.Pass {
			t.Fatalf("completeness enforcement must not trigger without ac_checks, got errors: %v", r.Errors)
		}
	})
}
