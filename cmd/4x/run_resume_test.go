package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

// writeRoundFile 在指定 round 目錄寫入一個 artifact，必要時建立目錄。
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

// writeFeatureFile 在 feature 目錄（非 round 目錄）寫入 artifact，供 accepting phase 用。
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
	// partialCoderReport 模擬 crash 寫到一半的 coder-report：有 header 但缺終止區段 `## Verification`。
	partialCoderReport = "# Coder Report\n## What Was Done\nhalf-written"
	reviewPassReport   = "# Review\n## Verdict\nPASS\n"
	reviewFailReport    = "# Review\n## Verdict\nFAIL\n"
	verifyPassedJSON    = `{"passed":true,"round":1,"role":"tester","commands":[]}`
	verifyFailedJSON    = `{"passed":false,"round":1,"role":"tester","commands":[]}`
)

// seedFullRoundDeepFail 組裝一個完整跑到 deep-review FAIL 的 round（coding→review PASS→
// test/verify PASS→deep-review FAIL），重現 deep-fix 斷線場景。
func seedFullRoundDeepFail(t *testing.T, ws *protocol.Workspace, featureID string, round int) {
	t.Helper()
	writeRoundFile(t, ws, featureID, round, protocol.CoderReport, completeCoderReport)
	writeRoundFile(t, ws, featureID, round, protocol.ReviewReport, reviewPassReport)
	writeRoundFile(t, ws, featureID, round, protocol.TestReport, "# Test\nok\n")
	writeRoundFile(t, ws, featureID, round, protocol.VerifyFile, verifyPassedJSON)
	writeRoundFile(t, ws, featureID, round, protocol.DeepReviewReport, reviewFailReport)
}

// resolveResume 複製 run.go resume 區塊的校正邏輯，供測試模擬 crash 後 resume 的落地。
func resolveResume(t *testing.T, ws *protocol.Workspace, featureID string, s protocol.State) protocol.State {
	t.Helper()
	if !needsResumeRecovery(s) {
		return s
	}
	rp, rr := smartResumePhase(ws, featureID, s.Round)
	if rp == s.Phase {
		return s
	}
	ns, err := state.RecoverTo(s, rp, rr)
	if err != nil {
		t.Fatalf("RecoverTo %s → %s: %v", s.Phase, rp, err)
	}
	return ns
}

// TestSmartResume_DeepReviewFail 對應 AC-1：deep-review FAIL 時回 amending（不再回 coding）。
func TestSmartResume_DeepReviewFail(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-deep"
	seedFullRoundDeepFail(t, ws, fid, 1)

	phase, role := smartResumePhase(ws, fid, 1)
	if phase != protocol.PhaseAmending {
		t.Errorf("phase = %s, want %s", phase, protocol.PhaseAmending)
	}
	if role != protocol.RoleCoder {
		t.Errorf("role = %s, want %s", role, protocol.RoleCoder)
	}
}

// TestSmartResume_Paths 對應 AC-2：table-driven 覆蓋 smartResumePhase 各條既有路徑不回歸。
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
			// crash 寫到一半：coder-report 存在但缺 `## Verification`，必須回 coding 重跑，
			// 不可因檔案存在就推進到 reviewing（對應 deep-review [WARNING] 修正）。
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
			// review-report 存在但缺 `## Verdict`（半成品）→ 回 reviewing 重跑。
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
			phase, role := smartResumePhase(ws, fid, tt.round)
			if phase != tt.wantPhase {
				t.Errorf("phase = %s, want %s", phase, tt.wantPhase)
			}
			if role != tt.wantRole {
				t.Errorf("role = %s, want %s", role, tt.wantRole)
			}
		})
	}
}

// TestResume_DeepFixCrashScenario 對應 AC-3 / AC-8：state.json 停在 coding，但 round-1
// artifacts 已跑到 deep-review FAIL。resume 後 (a) phase=amending、(b) coder-report 仍在、
// (c) review/test/deep-review 報告未被刪。
func TestResume_DeepFixCrashScenario(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-crash"
	seedFullRoundDeepFail(t, ws, fid, 1)

	// state.json 盲信 coding（Bug 2 的退步表現），實際 artifacts 已到 deep-review FAIL。
	s := protocol.State{FeatureID: fid, Phase: protocol.PhaseCoding, Round: 1, MaxRounds: 5, Active: true}
	s = resolveResume(t, ws, fid, s)

	// (a) phase 校正為 amending。
	if s.Phase != protocol.PhaseAmending {
		t.Fatalf("resume phase = %s, want %s", s.Phase, protocol.PhaseAmending)
	}
	if s.Role != protocol.RoleCoder {
		t.Errorf("resume role = %s, want %s", s.Role, protocol.RoleCoder)
	}
	// amending 套用 Round++，amend 在新 round 進行、保留 round-1。
	if s.Round != 2 {
		t.Errorf("round = %d, want 2 (amending Round++)", s.Round)
	}

	// resume 後對校正落地的 phase 跑 cleanStaleArtifact——必須不動到 round-1 完整報告。
	cleanStaleArtifact(ws, fid, s.Phase, s.Round)

	round1 := ws.RoundDir(fid, 1)
	for _, name := range []string{protocol.CoderReport, protocol.ReviewReport, protocol.TestReport, protocol.VerifyFile, protocol.DeepReviewReport} {
		if _, err := os.Stat(filepath.Join(round1, name)); err != nil {
			t.Errorf("round-1 %s missing after resume: %v", name, err)
		}
	}
}

// TestResumeRecovery_NoInvalidTransition 對應 AC-4：phase 校正落地不產生 invalid transition，
// 且 blocked / needs-attention 既有 recovery 行為維持不變。
func TestResumeRecovery_NoInvalidTransition(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-rec"
	seedFullRoundDeepFail(t, ws, fid, 1)

	// 各種 crash 起點 phase，落地不得 error，且最終 phase = smartResumePhase 推斷（amending）。
	for _, start := range []protocol.Phase{
		protocol.PhaseBlocked,
		protocol.PhaseNeedsAttention,
		protocol.PhaseCoding,
		protocol.PhaseTesting,
		protocol.PhaseDeepReviewing,
	} {
		s := protocol.State{FeatureID: fid, Phase: start, Round: 1, MaxRounds: 5, Active: true}
		if !needsResumeRecovery(s) {
			t.Errorf("needsResumeRecovery(%s) = false, want true", start)
			continue
		}
		got := resolveResume(t, ws, fid, s)
		if got.Phase != protocol.PhaseAmending {
			t.Errorf("start %s: resume phase = %s, want amending", start, got.Phase)
		}
	}

	// round 0 / init / pending-review 不觸發校正。
	for _, s := range []protocol.State{
		{Phase: protocol.PhaseInit, Round: 0},
		{Phase: protocol.PhaseDesigning, Round: 0},
		{Phase: protocol.PhaseCoding, Round: 0},
		{Phase: protocol.PhasePendingReview, Round: 1},
	} {
		if needsResumeRecovery(s) {
			t.Errorf("needsResumeRecovery(%s round %d) = true, want false", s.Phase, s.Round)
		}
	}
}

// TestCleanStaleArtifact_PreservesComplete 對應 AC-5：完整 coder-report 不被刪除。
func TestCleanStaleArtifact_PreservesComplete(t *testing.T) {
	ws := &protocol.Workspace{Root: t.TempDir()}
	const fid = "F-keep"
	writeRoundFile(t, ws, fid, 1, protocol.CoderReport, completeCoderReport)

	cleanStaleArtifact(ws, fid, protocol.PhaseAmending, 1)

	path := filepath.Join(ws.RoundDir(fid, 1), protocol.CoderReport)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("complete coder-report removed: %v", err)
	}
	if string(data) != completeCoderReport {
		t.Errorf("coder-report content changed")
	}
}

// TestCleanStaleArtifact_RemovesPartial 對應 AC-6：半成品（空檔 / 缺 ## Verification）被清除。
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

			cleanStaleArtifact(ws, fid, protocol.PhaseCoding, 1)

			path := filepath.Join(ws.RoundDir(fid, 1), protocol.CoderReport)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("partial coder-report still exists (err=%v), want removed", err)
			}
		})
	}
}

// TestCleanStaleArtifact_PreservesAllTypes 對應 AC-7：各 artifact 種類完整時不丟失。
func TestCleanStaleArtifact_PreservesAllTypes(t *testing.T) {
	t.Run("reviewing keeps complete review-report", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-rev"
		writeRoundFile(t, ws, fid, 1, protocol.ReviewReport, reviewFailReport) // FAIL 仍是完整 verdict
		cleanStaleArtifact(ws, fid, protocol.PhaseReviewing, 1)
		if _, err := os.Stat(filepath.Join(ws.RoundDir(fid, 1), protocol.ReviewReport)); err != nil {
			t.Errorf("complete review-report removed: %v", err)
		}
	})

	t.Run("testing keeps parseable verify.json", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-test"
		writeRoundFile(t, ws, fid, 1, protocol.TestReport, "# Test\nok\n")
		writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, verifyFailedJSON) // FAIL 但可解析→完整
		cleanStaleArtifact(ws, fid, protocol.PhaseTesting, 1)
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
		writeRoundFile(t, ws, fid, 1, protocol.VerifyFile, `{"passed":`) // 半成品 JSON
		cleanStaleArtifact(ws, fid, protocol.PhaseTesting, 1)
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
		cleanStaleArtifact(ws, fid, protocol.PhaseDeepReviewing, 1)
		if _, err := os.Stat(filepath.Join(ws.RoundDir(fid, 1), protocol.DeepReviewReport)); err != nil {
			t.Errorf("complete deep-review-report removed: %v", err)
		}
	})

	t.Run("accepting keeps non-empty final-report", func(t *testing.T) {
		ws := &protocol.Workspace{Root: t.TempDir()}
		const fid = "F-acc"
		writeFeatureFile(t, ws, fid, protocol.FinalReport, "# Final\ndone\n")
		cleanStaleArtifact(ws, fid, protocol.PhaseAccepting, 1)
		for _, name := range []string{protocol.FinalReport} {
			if _, err := os.Stat(filepath.Join(ws.FeatureDir(fid), name)); err != nil {
				t.Errorf("complete %s removed: %v", name, err)
			}
		}
	})
}

// TestRecoverTo_RejectsNonRecoverable 確認 RecoverTo 對非工作 phase 回 error，守住合法性判準。
func TestRecoverTo_RejectsNonRecoverable(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseCoding, Round: 1}
	if _, err := state.RecoverTo(s, protocol.PhaseDone, protocol.RoleCoder); err == nil {
		t.Errorf("RecoverTo(done) err = nil, want error")
	}
	// amending 套 Round++。
	ns, err := state.RecoverTo(s, protocol.PhaseAmending, protocol.RoleCoder)
	if err != nil {
		t.Fatalf("RecoverTo(amending) err = %v", err)
	}
	if ns.Round != 2 {
		t.Errorf("amending round = %d, want 2", ns.Round)
	}
}
