package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

func writeRoundFile(t *testing.T, ws *protocol.Workspace, featureID string, round int, name, content string) {
	t.Helper()
	dir := ws.RoundDir(featureID, round)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir round dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func writeFeatureFile(t *testing.T, ws *protocol.Workspace, featureID, name, content string) {
	t.Helper()
	dir := ws.FeatureDir(featureID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir feature dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

const (
	completeCoderReport = "# Coder Report\n## What Was Done\nstuff\n## Files Changed\n- a.go\n## Verification\n- make test: ok\n"
	partialCoderReport  = "# Coder Report\n## What Was Done\nhalf-written"
	reviewPassReport    = "# Review\n## Verdict\nPASS\n"
	reviewFailReport    = "# Review\n## Verdict\nFAIL\n"
	verifyPassedJSON    = `{"passed":true,"round":1,"role":"tester","commands":[]}`
	verifyFailedJSON    = `{"passed":false,"round":1,"role":"tester","commands":[]}`
)

func seedFullRoundDeepFail(t *testing.T, ws *protocol.Workspace, featureID string, round int) {
	t.Helper()
	writeRoundFile(t, ws, featureID, round, protocol.CoderReport, completeCoderReport)
	writeRoundFile(t, ws, featureID, round, protocol.ReviewReport, reviewPassReport)
	writeRoundFile(t, ws, featureID, round, protocol.TestReport, "# Test\nok\n")
	writeRoundFile(t, ws, featureID, round, protocol.VerifyFile, verifyPassedJSON)
	writeRoundFile(t, ws, featureID, round, protocol.DeepReviewReport, reviewFailReport)
}

func resolveResume(t *testing.T, ws *protocol.Workspace, featureID string, s protocol.State) protocol.State {
	t.Helper()
	if !NeedsResumeRecovery(s) {
		return s
	}
	rp, rr, _ := SmartResumePhase(ws, featureID, s.Round, protocol.Config{}, protocol.ProfileConfig{})
	if rp == s.Phase {
		return s
	}
	ns, err := state.RecoverTo(s, rp, rr)
	if err != nil {
		t.Fatalf("RecoverTo %s → %s: %v", s.Phase, rp, err)
	}
	return ns
}

func TestSmartResume_DeepReviewFail(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-deep"
	seedFullRoundDeepFail(t, ws, fid, 1)

	phase, role, _ := SmartResumePhase(ws, fid, 1, protocol.Config{}, protocol.ProfileConfig{})
	if phase != protocol.PhaseAmending {
		t.Errorf("phase = %s, want %s", phase, protocol.PhaseAmending)
	}
	if role != protocol.RoleCoder {
		t.Errorf("role = %s, want %s", role, protocol.RoleCoder)
	}
}

func TestSmartResume_Paths(t *testing.T) {
	tests := []struct {
		name      string
		round     int
		seed      func(t *testing.T, ws *protocol.Workspace, fid string)
		wantPhase protocol.Phase
		wantRole  protocol.Role
	}{
		{
			name:      "round 0 → designing",
			round:     0,
			seed:      func(t *testing.T, ws *protocol.Workspace, fid string) {},
			wantPhase: protocol.PhaseDesigning,
			wantRole:  protocol.RoleDesigner,
		},
		{
			name:      "missing coder → coding",
			round:     1,
			seed:      func(t *testing.T, ws *protocol.Workspace, fid string) {},
			wantPhase: protocol.PhaseCoding,
			wantRole:  protocol.RoleCoder,
		},
		{
			name:  "incomplete coder → coding",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				writeRoundFile(t, ws, fid, 1, protocol.CoderReport, partialCoderReport)
			},
			wantPhase: protocol.PhaseCoding,
			wantRole:  protocol.RoleCoder,
		},
		{
			name:  "missing review → reviewing",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
			},
			wantPhase: protocol.PhaseReviewing,
			wantRole:  protocol.RoleReviewer,
		},
		{
			name:  "incomplete review → reviewing",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
				writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, "# Review\n(half)")
			},
			wantPhase: protocol.PhaseReviewing,
			wantRole:  protocol.RoleReviewer,
		},
		{
			name:  "review FAIL → amending",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
				writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewFailReport)
			},
			wantPhase: protocol.PhaseAmending,
			wantRole:  protocol.RoleCoder,
		},
		{
			name:  "missing test → testing",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
				writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewPassReport)
			},
			wantPhase: protocol.PhaseTesting,
			wantRole:  protocol.RoleTester,
		},
		{
			name:  "verify FAIL → amending",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
				writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewPassReport)
				writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\n")
				writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, verifyFailedJSON)
			},
			wantPhase: protocol.PhaseAmending,
			wantRole:  protocol.RoleCoder,
		},
		{
			name:  "missing deep-review → deep-reviewing",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
				writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewPassReport)
				writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\n")
				writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, verifyPassedJSON)
			},
			wantPhase: protocol.PhaseDeepReviewing,
			wantRole:  protocol.RoleDeepReviewer,
		},
		{
			name:  "deep-review FAIL → amending",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				seedFullRoundDeepFail(t, ws, fid, 1)
			},
			wantPhase: protocol.PhaseAmending,
			wantRole:  protocol.RoleCoder,
		},
		{
			name:  "all pass → accepting",
			round: 1,
			seed: func(t *testing.T, ws *protocol.Workspace, fid string) {
				writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
				writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewPassReport)
				writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\n")
				writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, verifyPassedJSON)
				writeRoundFile(t, ws, fid, 1, protocol.DeepReviewReport, reviewPassReport)
			},
			wantPhase: protocol.PhaseAccepting,
			wantRole:  protocol.RoleAcceptor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &protocol.Workspace{Root: t.TempDir()}
			const fid = "F-paths"
			tt.seed(t, ws, fid)
			phase, role, _ := SmartResumePhase(ws, fid, tt.round, protocol.Config{}, protocol.ProfileConfig{})
			if phase != tt.wantPhase {
				t.Errorf("phase = %s, want %s", phase, tt.wantPhase)
			}
			if role != tt.wantRole {
				t.Errorf("role = %s, want %s", role, tt.wantRole)
			}
		})
	}
}

// fixingProfile 回傳一個啟用 fixing phase 的 ProfileConfig，供 resume Fixing 判斷測試使用。
func fixingProfile() protocol.ProfileConfig {
	return protocol.ProfileConfig{
		Phases: []protocol.PhaseSpec{{Phase: string(protocol.PhaseFixing)}},
	}
}

// seedDeepReviewPass 寫出一整輪通過到 deep-review PASS 的 artifacts（不含 fixer-report）。
func seedDeepReviewPass(t *testing.T, ws *protocol.Workspace, fid string) {
	t.Helper()
	writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
	writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewPassReport)
	writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\n")
	writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, verifyPassedJSON)
	writeRoundFile(t, ws, fid, 1, protocol.DeepReviewReport, reviewPassReport)
}

func TestSmartResume_DeepReviewPass_FixingProfile(t *testing.T) {
	// profile 啟用 fixing 且尚未跑過 fixer → resume 應對齊 deepTransitionAccepting，走 Fixing。
	t.Run("fixer not run → fixing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-fix"
		seedDeepReviewPass(t, ws, fid)

		phase, role, _ := SmartResumePhase(ws, fid, 1, protocol.Config{}, fixingProfile())
		if phase != protocol.PhaseFixing {
			t.Errorf("phase = %s, want %s", phase, protocol.PhaseFixing)
		}
		if role != protocol.RoleFixer {
			t.Errorf("role = %s, want %s", role, protocol.RoleFixer)
		}
	})

	// fixer-report 已完整 → fixer 跑完，跳過 Fixing 直接進 Accepting（保持冪等）。
	t.Run("fixer done → accepting", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-fix-done"
		seedDeepReviewPass(t, ws, fid)
		writeRoundFile(t, ws, fid, 1, protocol.FixerReport, completeCoderReport)

		phase, role, _ := SmartResumePhase(ws, fid, 1, protocol.Config{}, fixingProfile())
		if phase != protocol.PhaseAccepting {
			t.Errorf("phase = %s, want %s", phase, protocol.PhaseAccepting)
		}
		if role != protocol.RoleAcceptor {
			t.Errorf("role = %s, want %s", role, protocol.RoleAcceptor)
		}
	})

	// profile 未啟用 fixing → 維持既有行為，直接進 Accepting。
	t.Run("fixing disabled → accepting", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-nofix"
		seedDeepReviewPass(t, ws, fid)

		phase, role, _ := SmartResumePhase(ws, fid, 1, protocol.Config{}, protocol.ProfileConfig{})
		if phase != protocol.PhaseAccepting {
			t.Errorf("phase = %s, want %s", phase, protocol.PhaseAccepting)
		}
		if role != protocol.RoleAcceptor {
			t.Errorf("role = %s, want %s", role, protocol.RoleAcceptor)
		}
	})
}

func TestResume_DeepFixCrashScenario(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-crash"
	seedFullRoundDeepFail(t, ws, fid, 1)

	s := protocol.State{FeatureID: fid, Phase: protocol.PhaseCoding, Round: 1, MaxRounds: 5, Active: true}
	s = resolveResume(t, ws, fid, s)

	if s.Phase != protocol.PhaseAmending {
		t.Fatalf("resume phase = %s, want %s", s.Phase, protocol.PhaseAmending)
	}
	if s.Role != protocol.RoleCoder {
		t.Errorf("resume role = %s, want %s", s.Role, protocol.RoleCoder)
	}
	if s.Round != 2 {
		t.Errorf("round = %d, want 2 (amending Round++)", s.Round)
	}

	CleanStaleArtifact(ws, fid, s.Phase, s.Round)

	round1 := ws.RoundDir(fid, 1)
	for _, name := range []string{protocol.CoderReport, protocol.ReviewReport, protocol.TestReport, protocol.VerifyFile, protocol.DeepReviewReport} {
		if _, err := os.Stat(filepath.Join(round1, name)); err != nil {
			t.Errorf("round-1 %s missing after resume: %v", name, err)
		}
	}
}

func TestResumeRecovery_NoInvalidTransition(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-rec"
	seedFullRoundDeepFail(t, ws, fid, 1)

	for _, start := range []protocol.Phase{
		protocol.PhaseBlocked,
		protocol.PhaseNeedsAttention,
		protocol.PhaseCoding,
		protocol.PhaseTesting,
		protocol.PhaseDeepReviewing,
	} {
		s := protocol.State{FeatureID: fid, Phase: start, Round: 1, MaxRounds: 5, Active: true}
		if !NeedsResumeRecovery(s) {
			t.Errorf("NeedsResumeRecovery(%s) = false, want true", start)
			continue
		}
		got := resolveResume(t, ws, fid, s)
		if got.Phase != protocol.PhaseAmending {
			t.Errorf("start %s: resume phase = %s, want amending", start, got.Phase)
		}
	}

	for _, s := range []protocol.State{
		{Phase: protocol.PhaseInit, Round: 0},
		{Phase: protocol.PhaseDesigning, Round: 0},
		{Phase: protocol.PhaseCoding, Round: 0},
		{Phase: protocol.PhasePendingReview, Round: 1},
	} {
		if NeedsResumeRecovery(s) {
			t.Errorf("NeedsResumeRecovery(%s round %d) = true, want false", s.Phase, s.Round)
		}
	}
}

func TestCleanStaleArtifact_PreservesComplete(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-keep"
	writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)

	CleanStaleArtifact(ws, fid, protocol.PhaseAmending, 1)

	path := filepath.Join(ws.RoundDir(fid, 1), protocol.CoderReport)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("complete coder-report removed: %v", err)
	}
	if string(data) != completeCoderReport {
		t.Errorf("coder-report content changed")
	}
}

func TestCleanStaleArtifact_RemovesPartial(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"whitespace only", "   \n\t\n"},
		{"missing terminal section", "# Coder Report\n## What Was Done\nhalf written"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &protocol.Workspace{Root: t.TempDir()}
			const fid = "F-partial"
			writeRoundFile(t, ws, fid, 1, protocol.CoderReport, tt.content)

			CleanStaleArtifact(ws, fid, protocol.PhaseCoding, 1)

			path := filepath.Join(ws.RoundDir(fid, 1), protocol.CoderReport)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("partial coder-report still exists (err=%v), want removed", err)
			}
		})
	}
}

func TestCleanStaleArtifact_PreservesAllTypes(t *testing.T) {
	t.Run("reviewing keeps complete review-report", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-rev"
		writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewFailReport)
		CleanStaleArtifact(ws, fid, protocol.PhaseReviewing, 1)
		if _, err := os.Stat(filepath.Join(ws.RoundDir(fid, 1), protocol.ReviewReport)); err != nil {
			t.Errorf("complete review-report removed: %v", err)
		}
	})

	t.Run("testing keeps parseable verify.json", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-test"
		writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\nok\n")
		writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, verifyFailedJSON)
		CleanStaleArtifact(ws, fid, protocol.PhaseTesting, 1)
		for _, name := range []string{protocol.TestReport, protocol.VerifyFile} {
			if _, err := os.Stat(filepath.Join(ws.RoundDir(fid, 1), name)); err != nil {
				t.Errorf("complete %s removed: %v", name, err)
			}
		}
	})

	t.Run("testing removes unparseable verify.json", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-test2"
		writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\n")
		writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, `{"passed":`)
		CleanStaleArtifact(ws, fid, protocol.PhaseTesting, 1)
		for _, name := range []string{protocol.TestReport, protocol.VerifyFile} {
			if _, err := os.Stat(filepath.Join(ws.RoundDir(fid, 1), name)); !os.IsNotExist(err) {
				t.Errorf("partial %s still exists (err=%v), want removed", name, err)
			}
		}
	})

	t.Run("deep-reviewing keeps complete deep-review-report", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-deep2"
		writeRoundFile(t, ws, fid, 1, protocol.DeepReviewReport, reviewPassReport)
		CleanStaleArtifact(ws, fid, protocol.PhaseDeepReviewing, 1)
		if _, err := os.Stat(filepath.Join(ws.RoundDir(fid, 1), protocol.DeepReviewReport)); err != nil {
			t.Errorf("complete deep-review-report removed: %v", err)
		}
	})

	t.Run("accepting keeps non-empty final-report", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-acc"
		writeFeatureFile(t, ws, fid, protocol.FinalReport, "# Final\ndone\n")
		CleanStaleArtifact(ws, fid, protocol.PhaseAccepting, 1)
		if _, err := os.Stat(filepath.Join(ws.FeatureDir(fid), protocol.FinalReport)); err != nil {
			t.Errorf("complete %s removed: %v", protocol.FinalReport, err)
		}
	})
}

func TestRecoverTo_RejectsNonRecoverable(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseCoding, Round: 1}
	if _, err := state.RecoverTo(s, protocol.PhaseDone, protocol.RoleCoder); err == nil {
		t.Errorf("RecoverTo(done) err = nil, want error")
	}
	ns, err := state.RecoverTo(s, protocol.PhaseAmending, protocol.RoleCoder)
	if err != nil {
		t.Fatalf("RecoverTo(amending) err = %v", err)
	}
	if ns.Round != 2 {
		t.Errorf("amending round = %d, want 2", ns.Round)
	}
}

// --- subphase tests ---

func parallelCfg(reviewers int) protocol.Config {
	return protocol.Config{
		Roles: map[string]protocol.RoleConfig{
			string(protocol.RoleDeepReviewer): {ParallelReviewers: reviewers},
		},
	}
}

const completePartial = "# Deep Review Partial Report\n## Issues\nnone\n## Statistics\n- Reported: 0\n"

func TestDeepPartialComplete(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-partial"

	missing := ws.RoundDir(fid, 1) + "/deep-review-partial-9.md"
	if prompt.DeepPartialComplete(missing) {
		t.Error("nonexistent partial reported complete")
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"whitespace only", "  \n\t\n", false},
		{"missing sentinel", "# Deep Review Partial\n## Issues\nhalf-written", false},
		{"complete with statistics", completePartial, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeRoundFile(t, ws, fid, 1, "deep-review-partial-1.md", tt.content)
			path := ws.RoundDir(fid, 1) + "/deep-review-partial-1.md"
			if got := prompt.DeepPartialComplete(path); got != tt.want {
				t.Errorf("deepPartialComplete = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingDeepPartials(t *testing.T) {
	tests := []struct {
		name    string
		present []int
		want    int
		wantIdx []int
	}{
		{"all missing", nil, 3, []int{1, 2, 3}},
		{"missing middle", []int{1, 3}, 3, []int{2}},
		{"all present", []int{1, 2, 3}, 3, nil},
		{"single agent want 1 none", nil, 1, []int{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &protocol.Workspace{Root: t.TempDir()}
			const fid = "F-miss"
			for _, idx := range tt.present {
				writeRoundFile(t, ws, fid, 1, prompt.DeepReviewPartialName(idx), completePartial)
			}
			got := prompt.MissingDeepPartials(ws.RoundDir(fid, 1), tt.want)
			if len(got) != len(tt.wantIdx) {
				t.Fatalf("missing = %v, want %v", got, tt.wantIdx)
			}
			for i := range got {
				if got[i] != tt.wantIdx[i] {
					t.Errorf("missing[%d] = %d, want %d (%v)", i, got[i], tt.wantIdx[i], tt.wantIdx)
				}
			}
		})
	}
}

func TestDeepResumeSubPhase(t *testing.T) {
	t.Run("missing partial → reviewing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-sub"
		writeRoundFile(t, ws, fid, 1, prompt.DeepReviewPartialName(1), completePartial)
		if got := DeepResumeSubPhase(ws, fid, 1, parallelCfg(3)); got != protocol.SubPhaseReviewing {
			t.Errorf("subPhase = %q, want reviewing", got)
		}
	})

	t.Run("all partials present → synthesizing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-sub"
		for i := 1; i <= 3; i++ {
			writeRoundFile(t, ws, fid, 1, prompt.DeepReviewPartialName(i), completePartial)
		}
		if got := DeepResumeSubPhase(ws, fid, 1, parallelCfg(3)); got != protocol.SubPhaseSynthesizing {
			t.Errorf("subPhase = %q, want synthesizing", got)
		}
	})

	t.Run("single-agent mode → reviewing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-sub"
		if got := DeepResumeSubPhase(ws, fid, 1, protocol.Config{}); got != protocol.SubPhaseReviewing {
			t.Errorf("subPhase = %q, want reviewing", got)
		}
	})
}

func TestSmartResume_DeepSubPhase(t *testing.T) {
	seedToDeepReview := func(t *testing.T, ws *protocol.Workspace, fid string) {
		writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)
		writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewPassReport)
		writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\nok\n")
		writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, verifyPassedJSON)
	}

	t.Run("missing partials → reviewing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-srsub"
		seedToDeepReview(t, ws, fid)
		writeRoundFile(t, ws, fid, 1, prompt.DeepReviewPartialName(1), completePartial)
		phase, role, sub := SmartResumePhase(ws, fid, 1, parallelCfg(3), protocol.ProfileConfig{})
		if phase != protocol.PhaseDeepReviewing || role != protocol.RoleDeepReviewer || sub != protocol.SubPhaseReviewing {
			t.Errorf("got (%s, %s, %s), want (deep-reviewing, deep-reviewer, reviewing)", phase, role, sub)
		}
	})

	t.Run("all partials present → synthesizing", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-srsub"
		seedToDeepReview(t, ws, fid)
		for i := 1; i <= 3; i++ {
			writeRoundFile(t, ws, fid, 1, prompt.DeepReviewPartialName(i), completePartial)
		}
		phase, role, sub := SmartResumePhase(ws, fid, 1, parallelCfg(3), protocol.ProfileConfig{})
		if phase != protocol.PhaseDeepReviewing || role != protocol.RoleSynthesizer || sub != protocol.SubPhaseSynthesizing {
			t.Errorf("got (%s, %s, %s), want (deep-reviewing, synthesizer, synthesizing)", phase, role, sub)
		}
	})

	t.Run("deep-review FAIL → amending, no subPhase", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-srsub"
		seedFullRoundDeepFail(t, ws, fid, 1)
		phase, role, sub := SmartResumePhase(ws, fid, 1, parallelCfg(3), protocol.ProfileConfig{})
		if phase != protocol.PhaseAmending || role != protocol.RoleCoder || sub != "" {
			t.Errorf("got (%s, %s, %q), want (amending, coder, \"\")", phase, role, sub)
		}
	})
}

func TestWriteState_ClearsSubPhaseOutsideDeepReview(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-clear"
	if err := ws.InitFeatureDir(fid); err != nil {
		t.Fatalf("init: %v", err)
	}

	s := protocol.State{FeatureID: fid, Phase: protocol.PhaseAccepting, Role: protocol.RoleAcceptor, SubPhase: protocol.SubPhaseFixing, Round: 1}
	if err := ws.WriteState(fid, s); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ws.ReadState(fid)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SubPhase != "" {
		t.Errorf("SubPhase = %q, want cleared", got.SubPhase)
	}

	s2 := protocol.State{FeatureID: fid, Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, SubPhase: protocol.SubPhaseSynthesizing, Round: 1}
	if err := ws.WriteState(fid, s2); err != nil {
		t.Fatalf("write: %v", err)
	}
	got2, err := ws.ReadState(fid)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got2.SubPhase != protocol.SubPhaseSynthesizing {
		t.Errorf("SubPhase = %q, want synthesizing (preserved in deep-reviewing)", got2.SubPhase)
	}
}

// TestRecoverState_HonorsManualPhase 驗證人為 transition/retry 設定的 phase 被尊重：
// round-1 無 coder-report（SmartResumePhase 會回 coding），但 ManualPhase=true 時
// RecoverState 應照 state.json 的 deep-reviewing 派 deep-reviewer，並在消費後清除旗標。
func TestRecoverState_HonorsManualPhase(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-manual"
	// 不 seed coder-report：確認若走 SmartResumePhase 會被推導回 coding。
	s := protocol.State{
		FeatureID:   fid,
		Phase:       protocol.PhaseDeepReviewing,
		Round:       1,
		ManualPhase: true,
		Active:      true,
	}
	got, err := RecoverState(ws, fid, s, protocol.Config{}, protocol.ProfileConfig{})
	if err != nil {
		t.Fatalf("RecoverState error: %v", err)
	}
	if got.Phase != protocol.PhaseDeepReviewing {
		t.Errorf("Phase = %s, want %s (manual phase honored, not推導回 coding)", got.Phase, protocol.PhaseDeepReviewing)
	}
	if got.Role != protocol.RoleDeepReviewer {
		t.Errorf("Role = %s, want %s", got.Role, protocol.RoleDeepReviewer)
	}
	if got.ManualPhase {
		t.Error("ManualPhase should be cleared after consumption, got true")
	}
}

// TestRecoverState_CrashPathUnchanged 驗證非人為路徑行為不變：ManualPhase=false 時
// 仍經 SmartResumePhase 依 artifacts 推導。coding round-1 無 coder-report → 維持 coding。
func TestRecoverState_CrashPathUnchanged(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-crash"
	s := protocol.State{
		FeatureID:   fid,
		Phase:       protocol.PhaseCoding,
		Round:       1,
		ManualPhase: false,
		Active:      true,
	}
	got, err := RecoverState(ws, fid, s, protocol.Config{}, protocol.ProfileConfig{})
	if err != nil {
		t.Fatalf("RecoverState error: %v", err)
	}
	if got.Phase != protocol.PhaseCoding {
		t.Errorf("Phase = %s, want %s (crash path via SmartResumePhase)", got.Phase, protocol.PhaseCoding)
	}
	// 注意：SmartResumePhase 回的 phase 與輸入相同時，RecoverState 不呼叫 RecoverTo，
	// 故 Role 維持輸入值（既有行為）—— crash path 只驗 phase 不變，不驗 Role。
}
