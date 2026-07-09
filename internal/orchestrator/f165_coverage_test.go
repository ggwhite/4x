package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// --- 2A：run loop 轉換分支（AC-6a）---

// TestNextPhaseAfter_TransitionBranches 以真實 workspace + fixture 檔驗證 NextPhaseAfter
// 在各 phase 的轉換結果 (phase, role, reason)。coding 分支已由 phase_test.go 覆蓋，此處
// 補齊 designing / design-reviewing / reviewing / fixing / accepting / default 分支。
func TestNextPhaseAfter_TransitionBranches(t *testing.T) {
	// designing 成功：task-brief + acceptance-criteria 皆在 → design-reviewing
	t.Run("designing success", func(t *testing.T) {
		id := "F999-des-ok"
		ws := setupPhaseWorkspace(t, id)
		fd := ws.FeatureDir(id)
		writePhaseFile(t, filepath.Join(fd, protocol.TaskBrief), "# brief")
		writePhaseFile(t, filepath.Join(fd, protocol.Criteria), "# criteria")
		next, role, reason := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseDesigning, Round: 1})
		if next != protocol.PhaseDesignReviewing || role != protocol.RoleDesignReviewer || reason != "" {
			t.Fatalf("got (%s,%s,%q), want (design-reviewing, design-reviewer, \"\")", next, role, reason)
		}
	})

	// designing 缺 task-brief → needs-attention + missing-artifact reason
	t.Run("designing missing task-brief", func(t *testing.T) {
		id := "F999-des-miss"
		ws := setupPhaseWorkspace(t, id)
		next, role, reason := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseDesigning, Round: 1})
		if next != protocol.PhaseNeedsAttention || role != "" {
			t.Fatalf("got (%s,%s), want (needs-attention, \"\")", next, role)
		}
		if reason != "missing-artifact: "+protocol.TaskBrief {
			t.Fatalf("reason = %q, want missing-artifact: %s", reason, protocol.TaskBrief)
		}
	})

	// design-reviewing PASS → coding
	t.Run("design-reviewing pass", func(t *testing.T) {
		id := "F999-dr-pass"
		ws := setupPhaseWorkspace(t, id)
		writePhaseFile(t, filepath.Join(ws.FeatureDir(id), protocol.DesignReviewReport),
			"# DR\n## Verdict\nPASS\n")
		next, role, reason := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseDesignReviewing, Round: 1})
		if next != protocol.PhaseCoding || role != protocol.RoleCoder || reason != "" {
			t.Fatalf("got (%s,%s,%q), want (coding, coder, \"\")", next, role, reason)
		}
	})

	// design-reviewing FAIL → 打回 designing
	t.Run("design-reviewing fail", func(t *testing.T) {
		id := "F999-dr-fail"
		ws := setupPhaseWorkspace(t, id)
		writePhaseFile(t, filepath.Join(ws.FeatureDir(id), protocol.DesignReviewReport),
			"# DR\n## Issues\n### [CRITICAL] bad\n## Verdict\nFAIL\n")
		next, role, _ := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseDesignReviewing, Round: 1})
		if next != protocol.PhaseDesigning || role != protocol.RoleDesigner {
			t.Fatalf("got (%s,%s), want (designing, designer)", next, role)
		}
	})

	// reviewing PASS → testing
	t.Run("reviewing pass", func(t *testing.T) {
		id := "F999-rev-pass"
		ws := setupPhaseWorkspace(t, id)
		rd := ws.RoundDir(id, 1)
		writePhaseFile(t, filepath.Join(rd, protocol.ReviewReport), "# R\n## Verdict\nPASS\n")
		next, role, reason := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseReviewing, Round: 1})
		if next != protocol.PhaseTesting || role != protocol.RoleTester || reason != "" {
			t.Fatalf("got (%s,%s,%q), want (testing, tester, \"\")", next, role, reason)
		}
	})

	// reviewing FAIL → amending（失敗恢復路徑）
	t.Run("reviewing fail goes to amending", func(t *testing.T) {
		id := "F999-rev-fail"
		ws := setupPhaseWorkspace(t, id)
		rd := ws.RoundDir(id, 1)
		writePhaseFile(t, filepath.Join(rd, protocol.ReviewReport),
			"# R\n## Issues\n### [CRITICAL] boom\n## Verdict\nFAIL\n")
		next, role, _ := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseReviewing, Round: 1})
		if next != protocol.PhaseAmending || role != protocol.RoleCoder {
			t.Fatalf("got (%s,%s), want (amending, coder)", next, role)
		}
	})

	// fixing 有 fixer-report → accepting
	t.Run("fixing to accepting", func(t *testing.T) {
		id := "F999-fix"
		ws := setupPhaseWorkspace(t, id)
		rd := ws.RoundDir(id, 1)
		writePhaseFile(t, filepath.Join(rd, protocol.FixerReport), "# fix\n## Verification\nok")
		next, role, reason := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseFixing, Round: 1})
		if next != protocol.PhaseAccepting || role != protocol.RoleAcceptor || reason != "" {
			t.Fatalf("got (%s,%s,%q), want (accepting, acceptor, \"\")", next, role, reason)
		}
	})

	// accepting final-report PASS（Status ready-for-review）→ pending-review
	t.Run("accepting final pass", func(t *testing.T) {
		id := "F999-acc-ok"
		ws := setupPhaseWorkspace(t, id)
		writePhaseFile(t, filepath.Join(ws.FeatureDir(id), protocol.FinalReport),
			"# Final\n## Status\nready-for-review\n")
		next, role, reason := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseAccepting, Round: 1})
		if next != protocol.PhasePendingReview || role != "" || reason != "" {
			t.Fatalf("got (%s,%s,%q), want (pending-review, \"\", \"\")", next, role, reason)
		}
	})

	// accepting final-report 非 pass → needs-attention
	t.Run("accepting final needs-attention", func(t *testing.T) {
		id := "F999-acc-na"
		ws := setupPhaseWorkspace(t, id)
		writePhaseFile(t, filepath.Join(ws.FeatureDir(id), protocol.FinalReport),
			"# Final\n## Status\nneeds-attention\n")
		next, _, reason := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseAccepting, Round: 1})
		if next != protocol.PhaseNeedsAttention || reason != "final-report: needs-attention" {
			t.Fatalf("got (%s,%q), want (needs-attention, final-report: needs-attention)", next, reason)
		}
	})

	// default（done 等非工作 phase）→ done
	t.Run("default done", func(t *testing.T) {
		id := "F999-done"
		ws := setupPhaseWorkspace(t, id)
		next, role, reason := NextPhaseAfter(ws, id, protocol.State{Phase: protocol.PhaseDone, Round: 1})
		if next != protocol.PhaseDone || role != "" || reason != "" {
			t.Fatalf("got (%s,%s,%q), want (done, \"\", \"\")", next, role, reason)
		}
	})
}

// --- 2A：ApplyAmendingProgress 三分支（AC-6b）---

// writeReviewReportWithFails 寫出含 n 個 [CRITICAL] tag 的 review-report，使
// ReviewFailCount 讀到 cur=n。
func writeReviewReportWithFails(t *testing.T, ws *protocol.Workspace, id string, round, n int) {
	t.Helper()
	content := "# Review\n## Issues\n"
	for i := 0; i < n; i++ {
		content += "### [CRITICAL] issue\n"
	}
	content += "## Verdict\nFAIL\n"
	writePhaseFile(t, filepath.Join(ws.RoundDir(id, round), protocol.ReviewReport), content)
}

func TestApplyAmendingProgress(t *testing.T) {
	// 首輪基準：LastFailCount==0 && ConsecutiveNoProgress==0 && cur>0
	// → 只設 LastFailCount=cur，ConsecutiveNoProgress 不 increment。
	t.Run("first baseline no increment", func(t *testing.T) {
		id := "F999-amp-base"
		ws := setupPhaseWorkspace(t, id)
		writeReviewReportWithFails(t, ws, id, 1, 2)
		st := protocol.State{LastFailCount: 0, ConsecutiveNoProgress: 0}
		ApplyAmendingProgress(ws, id, &st, 1)
		if st.LastFailCount != 2 {
			t.Fatalf("LastFailCount = %d, want 2", st.LastFailCount)
		}
		if st.ConsecutiveNoProgress != 0 {
			t.Fatalf("ConsecutiveNoProgress = %d, want 0 (baseline no increment)", st.ConsecutiveNoProgress)
		}
	})

	// 無改善：cur >= LastFailCount → ConsecutiveNoProgress++、LastFailCount=cur。
	t.Run("no improvement increments", func(t *testing.T) {
		id := "F999-amp-noimp"
		ws := setupPhaseWorkspace(t, id)
		writeReviewReportWithFails(t, ws, id, 1, 3)
		st := protocol.State{LastFailCount: 2, ConsecutiveNoProgress: 1}
		ApplyAmendingProgress(ws, id, &st, 1)
		if st.ConsecutiveNoProgress != 2 {
			t.Fatalf("ConsecutiveNoProgress = %d, want 2", st.ConsecutiveNoProgress)
		}
		if st.LastFailCount != 3 {
			t.Fatalf("LastFailCount = %d, want 3", st.LastFailCount)
		}
	})

	// 有改善：cur < LastFailCount → ConsecutiveNoProgress 歸零、LastFailCount=cur。
	t.Run("improvement resets", func(t *testing.T) {
		id := "F999-amp-imp"
		ws := setupPhaseWorkspace(t, id)
		writeReviewReportWithFails(t, ws, id, 1, 1)
		st := protocol.State{LastFailCount: 3, ConsecutiveNoProgress: 2}
		ApplyAmendingProgress(ws, id, &st, 1)
		if st.ConsecutiveNoProgress != 0 {
			t.Fatalf("ConsecutiveNoProgress = %d, want 0 (improvement resets)", st.ConsecutiveNoProgress)
		}
		if st.LastFailCount != 1 {
			t.Fatalf("LastFailCount = %d, want 1", st.LastFailCount)
		}
	})
}

// --- 2B：先前 0% 純函式 / 檔案型函式（AC-5）---

func TestSuccessorPhase(t *testing.T) {
	tests := []struct {
		in       protocol.Phase
		wantNext protocol.Phase
		wantRole protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer},
		{protocol.PhaseDesignReviewing, protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseCoding, protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseReviewing, protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseTesting, protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer},
		{protocol.PhaseDeepReviewing, protocol.PhaseFixing, protocol.RoleFixer},
		{protocol.PhaseFixing, protocol.PhaseAccepting, protocol.RoleAcceptor},
		{protocol.PhaseAccepting, protocol.PhasePendingReview, ""},
		// default：非工作 phase 回傳自身 + state.PhaseToRole（done → 空 role）
		{protocol.PhaseDone, protocol.PhaseDone, ""},
	}
	for _, tt := range tests {
		gotNext, gotRole := SuccessorPhase(tt.in)
		if gotNext != tt.wantNext || gotRole != tt.wantRole {
			t.Errorf("SuccessorPhase(%s) = (%s,%s), want (%s,%s)",
				tt.in, gotNext, gotRole, tt.wantNext, tt.wantRole)
		}
	}
}

func TestIsTerminalPhase(t *testing.T) {
	terminal := []protocol.Phase{
		protocol.PhaseDone, protocol.PhasePendingReview, protocol.PhaseBlocked,
		protocol.PhaseNeedsAttention, protocol.PhaseAbandoned,
	}
	for _, p := range terminal {
		if !IsTerminalPhase(p) {
			t.Errorf("IsTerminalPhase(%s) = false, want true", p)
		}
	}
	nonTerminal := []protocol.Phase{
		protocol.PhaseCoding, protocol.PhaseReviewing, protocol.PhaseTesting,
		protocol.PhaseDesigning, protocol.PhaseAccepting,
	}
	for _, p := range nonTerminal {
		if IsTerminalPhase(p) {
			t.Errorf("IsTerminalPhase(%s) = true, want false", p)
		}
	}
}

func TestReadGuardFeedback(t *testing.T) {
	id := "F999-gfb"

	// 檔案不存在 → nil
	t.Run("missing returns nil", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, id)
		if got := ReadGuardFeedback(ws, id, 1); got != nil {
			t.Fatalf("missing file: got %v, want nil", got)
		}
	})

	// 合法 JSON → errors 陣列
	t.Run("valid json returns errors", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, id)
		writePhaseFile(t, filepath.Join(ws.RoundDir(id, 1), protocol.GuardFeedback),
			`{"errors":["e1","e2"]}`)
		got := ReadGuardFeedback(ws, id, 1)
		if len(got) != 2 || got[0] != "e1" || got[1] != "e2" {
			t.Fatalf("got %v, want [e1 e2]", got)
		}
	})

	// 壞 JSON → nil
	t.Run("bad json returns nil", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, id)
		writePhaseFile(t, filepath.Join(ws.RoundDir(id, 1), protocol.GuardFeedback), "{not json")
		if got := ReadGuardFeedback(ws, id, 1); got != nil {
			t.Fatalf("bad json: got %v, want nil", got)
		}
	})
}

func TestPhaseToRole(t *testing.T) {
	tests := []struct {
		phase protocol.Phase
		want  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseAmending, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer},
		{protocol.PhaseFixing, protocol.RoleFixer},
		{protocol.PhaseAccepting, protocol.RoleAcceptor},
		{protocol.PhaseDone, ""},
	}
	for _, tt := range tests {
		if got := PhaseToRole(tt.phase); got != tt.want {
			t.Errorf("PhaseToRole(%s) = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestCheckDependencies(t *testing.T) {
	// 全部 dep done → nil
	t.Run("all deps done", func(t *testing.T) {
		id := "F999-dep-a"
		ws := setupPhaseWorkspace(t, id)
		if err := ws.SaveFeature(feature.Feature{ID: id, Name: "A", Depends: []string{"F999-dep-b"}}); err != nil {
			t.Fatal(err)
		}
		if err := ws.SaveFeature(feature.Feature{ID: "F999-dep-b", Name: "B", Status: feature.StatusDone}); err != nil {
			t.Fatal(err)
		}
		if err := CheckDependencies(ws, id); err != nil {
			t.Fatalf("all deps done should pass, got: %v", err)
		}
	})

	// 有未完成 dep → 非 nil error
	t.Run("unmet dep errors", func(t *testing.T) {
		id := "F999-dep-c"
		ws := setupPhaseWorkspace(t, id)
		if err := ws.SaveFeature(feature.Feature{ID: id, Name: "C", Depends: []string{"F999-dep-d"}}); err != nil {
			t.Fatal(err)
		}
		if err := ws.SaveFeature(feature.Feature{ID: "F999-dep-d", Name: "D", Status: feature.StatusInProgress}); err != nil {
			t.Fatal(err)
		}
		err := CheckDependencies(ws, id)
		if err == nil {
			t.Fatal("unmet dep should error")
		}
		if !contains(err.Error(), id) {
			t.Fatalf("error %q should mention feature id %s", err.Error(), id)
		}
	})
}

func TestParsePhaseOverrides(t *testing.T) {
	// 空輸入 → nil map, nil error
	t.Run("empty returns nil", func(t *testing.T) {
		m, err := ParsePhaseOverrides(nil)
		if err != nil || m != nil {
			t.Fatalf("empty: got (%v,%v), want (nil,nil)", m, err)
		}
	})

	// 合法 phase:runner:model → 解析成 map
	t.Run("valid parses", func(t *testing.T) {
		m, err := ParsePhaseOverrides([]string{"coding:claude:opus"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec, ok := m[protocol.PhaseCoding]
		if !ok {
			t.Fatalf("map missing coding key: %v", m)
		}
		if spec.Runner != "claude" || spec.Model != "opus" {
			t.Fatalf("spec = %+v, want runner=claude model=opus", spec)
		}
	})

	// 非法格式（段數不足）→ error
	t.Run("malformed errors", func(t *testing.T) {
		if _, err := ParsePhaseOverrides([]string{"coding:claude"}); err == nil {
			t.Fatal("malformed entry should error")
		}
	})

	// 非 selectable phase → error
	t.Run("non-selectable phase errors", func(t *testing.T) {
		if _, err := ParsePhaseOverrides([]string{"done:claude:opus"}); err == nil {
			t.Fatal("non-selectable phase should error")
		}
	})
}

func TestWorktreeExitHints(t *testing.T) {
	// wtPath 空 → nil
	if got := WorktreeExitHints("", "F1", protocol.PhaseDone, "per-round"); got != nil {
		t.Fatalf("empty wtPath: got %v, want nil", got)
	}

	// done + per-round → branch + merge 提示
	t.Run("done per-round", func(t *testing.T) {
		got := WorktreeExitHints("/tmp/wt", "F1", protocol.PhaseDone, "per-round")
		if len(got) != 2 {
			t.Fatalf("got %d lines, want 2: %v", len(got), got)
		}
		if !contains(got[0], "4x/F1") {
			t.Errorf("line0 %q should contain branch 4x/F1", got[0])
		}
		if !contains(got[1], "git merge 4x/F1") {
			t.Errorf("line1 %q should contain merge command", got[1])
		}
	})

	// done + never → nil（never 策略不印 merge 提示）
	t.Run("done never", func(t *testing.T) {
		if got := WorktreeExitHints("/tmp/wt", "F1", protocol.PhaseDone, "never"); got != nil {
			t.Fatalf("commit=never: got %v, want nil", got)
		}
	})

	// 非終態成功（needs-attention）→ worktree preserved 提示，含路徑與 feature id
	t.Run("non-done preserved", func(t *testing.T) {
		got := WorktreeExitHints("/tmp/wt", "F1", protocol.PhaseNeedsAttention, "per-round")
		if len(got) != 2 {
			t.Fatalf("got %d lines, want 2: %v", len(got), got)
		}
		if !contains(got[0], "/tmp/wt") || !contains(got[0], string(protocol.PhaseNeedsAttention)) {
			t.Errorf("line0 %q should contain worktree path and state", got[0])
		}
		if !contains(got[1], "/tmp/wt") || !contains(got[1], "4x/F1") {
			t.Errorf("line1 %q should contain path and feature id", got[1])
		}
	})
}

func TestReviewFailCount(t *testing.T) {
	id := "F999-rfc"

	// 無 report → 0
	t.Run("no report returns 0", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, id)
		if got := ReviewFailCount(ws, id, 1); got != 0 {
			t.Fatalf("no report: got %d, want 0", got)
		}
	})

	// 2 critical + 1 warning → 3
	t.Run("counts critical plus warning", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, id)
		writePhaseFile(t, filepath.Join(ws.RoundDir(id, 1), protocol.ReviewReport),
			"# R\n## Issues\n### [CRITICAL] a\n### [CRITICAL] b\n### [WARNING] c\n## Verdict\nFAIL\n")
		if got := ReviewFailCount(ws, id, 1); got != 3 {
			t.Fatalf("got %d, want 3", got)
		}
	})
}

func TestFinalReportPassed(t *testing.T) {
	dir := t.TempDir()

	// ready-for-review → true
	t.Run("ready-for-review true", func(t *testing.T) {
		p := filepath.Join(dir, "pass.md")
		if err := os.WriteFile(p, []byte("# Final\n## Status\nready-for-review\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !FinalReportPassed(p) {
			t.Fatal("ready-for-review should be true")
		}
	})

	// needs-attention → false
	t.Run("needs-attention false", func(t *testing.T) {
		p := filepath.Join(dir, "fail.md")
		if err := os.WriteFile(p, []byte("# Final\n## Status\nneeds-attention\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if FinalReportPassed(p) {
			t.Fatal("needs-attention should be false")
		}
	})

	// 檔案不存在 → false
	t.Run("missing false", func(t *testing.T) {
		if FinalReportPassed(filepath.Join(dir, "nope.md")) {
			t.Fatal("missing file should be false")
		}
	})
}

// contains 是 strings.Contains 的短別名，讓 assert 更精簡。
func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
