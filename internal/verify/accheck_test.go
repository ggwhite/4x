package verify

import (
	"context"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestRunACChecks 用真實子程序命令驗證：全 exit 0 → Passed=true；含失敗命令 → Passed=false
// 且首失敗後其餘標 Skipped；每條 Checks 記錄真實 exit code；AC ID 排序後執行使輸出穩定。（AC-3）
func TestRunACChecks(t *testing.T) {
	acChecks := map[string][]string{
		"AC-2": {"sh -c 'exit 1'", "true"},
		"AC-1": {"true", "sh -c 'exit 0'"},
		"AC-3": {"true"},
	}
	results := RunACChecks(context.Background(), acChecks, t.TempDir())

	if len(results) != 3 {
		t.Fatalf("expected 3 AC results, got %d", len(results))
	}

	// 排序穩定性：AC ID 需依序 AC-1, AC-2, AC-3。
	wantOrder := []string{"AC-1", "AC-2", "AC-3"}
	for i, r := range results {
		if r.ID != wantOrder[i] {
			t.Fatalf("result[%d].ID = %q, want %q (must be sorted)", i, r.ID, wantOrder[i])
		}
	}

	byID := map[string]int{}
	for i, r := range results {
		byID[r.ID] = i
	}

	// AC-1：兩條皆 exit 0 → Passed=true，兩條 exit code 均 0。
	ac1 := results[byID["AC-1"]]
	if !ac1.Passed {
		t.Errorf("AC-1 should pass (all exit 0), got Passed=%v", ac1.Passed)
	}
	if len(ac1.Checks) != 2 {
		t.Fatalf("AC-1 expected 2 checks, got %d", len(ac1.Checks))
	}
	for _, c := range ac1.Checks {
		if c.ExitCode != 0 {
			t.Errorf("AC-1 check %q exit code = %d, want 0", c.Command, c.ExitCode)
		}
	}

	// AC-2：首條 exit 1 → Passed=false，第二條被標 Skipped。
	ac2 := results[byID["AC-2"]]
	if ac2.Passed {
		t.Errorf("AC-2 should fail (first check exit 1), got Passed=%v", ac2.Passed)
	}
	if len(ac2.Checks) != 2 {
		t.Fatalf("AC-2 expected 2 checks, got %d", len(ac2.Checks))
	}
	if ac2.Checks[0].ExitCode != 1 {
		t.Errorf("AC-2 first check exit code = %d, want 1", ac2.Checks[0].ExitCode)
	}
	if !ac2.Checks[1].Skipped {
		t.Errorf("AC-2 second check should be skipped after first failure")
	}

	// AC-3：單條 exit 0 → Passed=true。
	ac3 := results[byID["AC-3"]]
	if !ac3.Passed {
		t.Errorf("AC-3 should pass, got Passed=%v", ac3.Passed)
	}
	if len(ac3.Evidence) == 0 {
		t.Errorf("AC-3 should have machine-generated evidence lines")
	}
}

// TestLintACCheck 表格驅動涵蓋假驗證 pattern 與合法命令，並驗證 4x-lint:allow 逃生口。（AC-5）
func TestLintACCheck(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		violate bool // true = 應回非空原因
	}{
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"true", "true", true},
		{"colon", ":", true},
		{"echo-only", "echo PASS", true},
		{"printf-only", "printf ok", true},
		{"grep-go-source", "grep foo internal/x.go", true},
		{"grep-pkg-dir", "grep -r foo cmd/4x", true},
		{"go-test", "go test ./...", false},
		{"piped-grep", "bin/app | grep -q x", false},
		{"grep-non-source", "grep foo out.txt", false},
		{"grep-docs", "grep -q ac_checks docs/architecture/protocol.md", false},
		{"allow-escape", "grep -q sym gen.go # 4x-lint:allow", false},
		{"echo-piped", "echo hi | grep hi", false},
		{"negated-grep-source", "! grep -q foo internal/x.go", true},
		{"negated-grep-output", "! grep -q foo out.txt", false},
		{"paren-grep-source", "(grep foo internal/x.go)", true},
		{"compound-echo-then-real", "echo case-1 && bin/4x status", false},
		{"compound-all-echo", "echo a && echo b", true},
		{"compound-echo-semicolon", "echo start; printf done", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := LintACCheck(tc.cmd)
			if tc.violate && reason == "" {
				t.Errorf("LintACCheck(%q) = \"\" (allowed), want a violation reason", tc.cmd)
			}
			if !tc.violate && reason != "" {
				t.Errorf("LintACCheck(%q) = %q (rejected), want \"\" (allowed)", tc.cmd, reason)
			}
		})
	}
}

// TestACChecksPassed 驗證 ac_checks 的嚴格通過語意：全部執行且 exit 0 才算過，
// skipped 條目視為未達成（不得充當通過），空清單不算過。（review Blocker 3）
func TestACChecksPassed(t *testing.T) {
	cases := []struct {
		name string
		cmds []protocol.VerifyCommand
		want bool
	}{
		{"all-exit-zero", []protocol.VerifyCommand{{Command: "a", ExitCode: 0}, {Command: "b", ExitCode: 0}}, true},
		{"one-fails", []protocol.VerifyCommand{{Command: "a", ExitCode: 1}}, false},
		{"skipped-not-pass", []protocol.VerifyCommand{{Command: "a", ExitCode: 1, Skipped: true}}, false},
		{"skipped-zero-not-pass", []protocol.VerifyCommand{{Command: "a", ExitCode: 0, Skipped: true}}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ACChecksPassed(tc.cmds); got != tc.want {
				t.Errorf("ACChecksPassed(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestRunACChecksTimeoutMarked 驗證 ctx 超時導致的失敗在結果上可辨識：
// check 的 Error 欄位標 "timeout"，該 AC Passed=false。（review Finding 7）
func TestRunACChecksTimeoutMarked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	results := RunACChecks(ctx, map[string][]string{"AC-1": {"sleep 5"}}, t.TempDir())
	if len(results) != 1 {
		t.Fatalf("expected 1 AC result, got %d", len(results))
	}
	ac := results[0]
	if ac.Passed {
		t.Error("timed-out check must not pass")
	}
	if len(ac.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(ac.Checks))
	}
	if ac.Checks[0].Error != "timeout" {
		t.Errorf("check Error = %q, want \"timeout\"", ac.Checks[0].Error)
	}
}

// TestBackwardCompatNoACChecks 反向回歸 pin：無 ac_checks 時，RunACChecks 空輸入回空結果、
// 既有 RunGroups 路徑不變（AC-8 的 internal/verify 部分）。
func TestBackwardCompatNoACChecks(t *testing.T) {
	// 空 ac_checks map → 回空 slice，不執行任何子程序。
	results := RunACChecks(context.Background(), nil, t.TempDir())
	if len(results) != 0 {
		t.Fatalf("RunACChecks(nil) should return empty, got %d", len(results))
	}

	// 既有 RunGroups 路徑不受 ac_checks 影響：一組簡單命令仍照舊執行、Passed 由 exit code 決定。
	ev := RunGroups(context.Background(), []Group{{Name: "default", Commands: []string{"true"}}}, t.TempDir())
	if !ev.Passed {
		t.Errorf("RunGroups with exit-0 command should pass, got Passed=%v", ev.Passed)
	}
	if len(ev.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(ev.Commands))
	}
	if len(ev.ACResults) != 0 {
		t.Errorf("RunGroups should not synthesize ac_results, got %d", len(ev.ACResults))
	}
}
