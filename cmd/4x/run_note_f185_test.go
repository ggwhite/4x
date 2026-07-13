package main

import (
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestBuildRunBgArgs_Note 驗證 AC-6：--json 背景轉發在 note 非空時帶相鄰 --note <note>，空時不帶。
func TestBuildRunBgArgs_Note(t *testing.T) {
	got := buildRunBgArgs("F185-bg", "", "", nil, 0, 0, false, false, "focus X")
	if !adjacentArgs(got, "--note", "focus X") {
		t.Errorf("buildRunBgArgs with note = %v, want adjacent --note \"focus X\"", got)
	}

	empty := buildRunBgArgs("F185-bg", "", "", nil, 0, 0, false, false, "")
	for _, a := range empty {
		if a == "--note" {
			t.Errorf("buildRunBgArgs(empty note) should not contain --note: %v", empty)
		}
	}
}

// TestFirstRunRole 驗證 AC-11(a)：firstRunRole 對各 phase 回傳本次實際先跑的 role，與真實
// RunLoop 對齊（init→designing）。
func TestFirstRunRole(t *testing.T) {
	cases := []struct {
		phase protocol.Phase
		want  protocol.Role
	}{
		{protocol.PhaseInit, protocol.RoleDesigner},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseAmending, protocol.RoleCoder},
		{protocol.PhaseFixing, protocol.RoleFixer},
	}
	for _, c := range cases {
		if got := firstRunRole(protocol.State{Phase: c.phase}); got != c.want {
			t.Errorf("firstRunRole(%s) = %q, want %q", c.phase, got, c.want)
		}
	}
}

// TestDryRunLoop_NoteRole 驗證 AC-11(b)：Phase==reviewing + 非空 RunNote 時，dryRunLoop 產出的
// note 只出現在 Reviewer render 段落，不出現在 Designer 段落（證明 note 綁到正確角色而非固定第一項）。
func TestDryRunLoop_NoteRole(t *testing.T) {
	ws := setupLoopWorkspace(t, "F185-dry")
	feature, _ := ws.LoadFeature("F185-dry")
	cfg, _ := ws.ReadConfig()

	const note = "FOCUS-REVIEWER-NOTE"
	s := protocol.State{FeatureID: "F185-dry", Phase: protocol.PhaseReviewing, Round: 1, RunNote: note}

	out := captureStdout(t, func() {
		if err := dryRunLoop(ws, feature, cfg, s); err != nil {
			t.Fatalf("dryRunLoop: %v", err)
		}
	})

	designer := sectionBetween(out, "=== designing (designer) ===", "=== coding (coder) ===")
	reviewer := sectionBetween(out, "=== reviewing (reviewer) ===", "=== testing (tester) ===")
	if strings.Contains(designer, note) {
		t.Errorf("Designer section should NOT contain note\n---designer---\n%s", designer)
	}
	if !strings.Contains(reviewer, note) {
		t.Errorf("Reviewer section should contain note\n---reviewer---\n%s", reviewer)
	}
}

// sectionBetween 回傳 out 中 start 標頭（含）到 end 標頭（不含）之間的內容；找不到 end 時取到結尾。
func sectionBetween(out, start, end string) string {
	i := strings.Index(out, start)
	if i < 0 {
		return ""
	}
	rest := out[i:]
	if j := strings.Index(rest[len(start):], end); j >= 0 {
		return rest[:len(start)+j]
	}
	return rest
}

// TestRunCmd_RunNoteDryRun 驗證 AC-7：`4x run <id> --note "focus X" --dry-run` in-process 執行後
// state.json 含 runNote（dry-run 不進 RunLoop 故不清除），且 stdout 含 note 文字。
func TestRunCmd_RunNoteDryRun(t *testing.T) {
	ws := setupLoopWorkspace(t, "F185-e2e")
	t.Chdir(ws.Root)

	out := captureStdout(t, func() {
		cmd := newRunCmd()
		cmd.SetArgs([]string{"F185-e2e", "--note", "focus X", "--dry-run"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("run --dry-run: %v", err)
		}
	})

	got, err := ws.ReadState("F185-e2e")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.RunNote != "focus X" {
		t.Errorf("RunNote = %q, want \"focus X\" (dry-run should not clear it)", got.RunNote)
	}
	if !strings.Contains(out, "focus X") {
		t.Errorf("stdout should contain note text\n---\n%s", out)
	}
}
