package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
)

// newBareRunner 用給定 workspace + featureID 組出最小 Runner，供不 spawn 子程序的
// Runner method 單元測試使用（Ops 用 fakeConvergeOps，不接觸真實 git）。
func newBareRunner(ws *protocol.Workspace, id string) *Runner {
	return &Runner{Config: Config{
		Ws:       ws,
		RunnerWs: ws,
		Feature:  feat.Feature{ID: id, Name: id},
		Ops:      fakeConvergeOps{},
	}}
}

func TestClearStaleEscalation(t *testing.T) {
	id := "F999-cse"
	ws := setupPhaseWorkspace(t, id)
	r := newBareRunner(ws, id)
	escPath := filepath.Join(ws.RoundDir(id, 1), protocol.EscalationFile)

	// coding phase → 移除殘留 escalation
	t.Run("coding removes", func(t *testing.T) {
		writePhaseFile(t, escPath, `{"needed":true}`)
		r.clearStaleEscalation(protocol.PhaseCoding, &protocol.State{Round: 1})
		if _, err := os.Stat(escPath); !os.IsNotExist(err) {
			t.Fatal("coding phase should remove stale escalation.json")
		}
	})

	// reviewing phase → 保留 escalation（非 coder 類 phase）
	t.Run("reviewing preserves", func(t *testing.T) {
		writePhaseFile(t, escPath, `{"needed":true}`)
		r.clearStaleEscalation(protocol.PhaseReviewing, &protocol.State{Round: 1})
		if _, err := os.Stat(escPath); err != nil {
			t.Fatal("reviewing phase should preserve escalation.json")
		}
	})
}

func TestClearStaleFinalReport(t *testing.T) {
	id := "F999-csfr"
	ws := setupPhaseWorkspace(t, id)
	r := newBareRunner(ws, id)
	frPath := filepath.Join(ws.FeatureDir(id), protocol.FinalReport)

	// testing phase → 移除殘留 final-report
	t.Run("testing removes", func(t *testing.T) {
		writePhaseFile(t, frPath, "# Final\nstale")
		r.clearStaleFinalReport(protocol.PhaseTesting, &protocol.State{Round: 1})
		if _, err := os.Stat(frPath); !os.IsNotExist(err) {
			t.Fatal("testing phase should remove stale final-report.md")
		}
	})

	// coding phase → 保留
	t.Run("coding preserves", func(t *testing.T) {
		writePhaseFile(t, frPath, "# Final\nstale")
		r.clearStaleFinalReport(protocol.PhaseCoding, &protocol.State{Round: 1})
		if _, err := os.Stat(frPath); err != nil {
			t.Fatal("coding phase should preserve final-report.md")
		}
	})
}

// errCaptureOps 讓 CaptureBaseline 回傳錯誤，其餘沿用 fakeConvergeOps 的 no-op。
type errCaptureOps struct{ fakeConvergeOps }

func (errCaptureOps) CaptureBaseline(string, []string) error {
	return errors.New("capture failed")
}

func TestCaptureBaselineOnce(t *testing.T) {
	id := "F999-cbo"

	// baseline 已存在 → 直接跳過回 nil（不呼叫 ops）
	t.Run("existing baseline skips", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, id)
		writePhaseFile(t, filepath.Join(ws.FeatureDir(id), protocol.BaselineFile), "{}")
		if err := CaptureBaselineOnce(ws, errCaptureOps{}, id, nil); err != nil {
			t.Fatalf("existing baseline should skip ops and return nil, got: %v", err)
		}
	})

	// 無 baseline + ops 成功 → nil
	t.Run("captures when missing", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, id)
		if err := CaptureBaselineOnce(ws, fakeConvergeOps{}, id, nil); err != nil {
			t.Fatalf("successful capture should return nil, got: %v", err)
		}
	})

	// 無 baseline + ops 失敗 → error
	t.Run("propagates ops error", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, id)
		if err := CaptureBaselineOnce(ws, errCaptureOps{}, id, nil); err == nil {
			t.Fatal("ops error should propagate")
		}
	})
}

func TestCaptureBaseline_PhaseGate(t *testing.T) {
	id := "F999-cb"
	ws := setupPhaseWorkspace(t, id)
	r := newBareRunner(ws, id)

	// 非 coding round-1 → no-op（不建立 baseline）
	if err := r.captureBaseline(&protocol.State{Phase: protocol.PhaseReviewing, Round: 1}); err != nil {
		t.Fatalf("reviewing phase should no-op, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.FeatureDir(id), protocol.BaselineFile)); !os.IsNotExist(err) {
		t.Fatal("non-coding phase should not create baseline")
	}

	// coding round-1 → 走 CaptureBaselineOnce（fake ops 成功）+ captureBaseCommit（非 git 目錄 HEAD 為空即早退）
	if err := r.captureBaseline(&protocol.State{Phase: protocol.PhaseCoding, Round: 1}); err != nil {
		t.Fatalf("coding round-1 capture should succeed, got: %v", err)
	}
}

func TestDeepTransitionAccepting(t *testing.T) {
	base := protocol.State{
		FeatureID: "F999-dta", Phase: protocol.PhaseDeepReviewing,
		Role: protocol.RoleDeepReviewer, Round: 1, Active: true,
	}

	// profile 未啟用 fixing → 轉 accepting
	t.Run("no fixing goes to accepting", func(t *testing.T) {
		id := "F999-dta"
		ws := setupPhaseWorkspace(t, id)
		s := base
		pc := protocol.ProfileConfig{Phases: []protocol.PhaseSpec{{Phase: string(protocol.PhaseAccepting)}}}
		cont, err := deepTransitionAccepting(ws, id, &s, pc)
		if err != nil || !cont {
			t.Fatalf("got (cont=%v, err=%v), want (true, nil)", cont, err)
		}
		if s.Phase != protocol.PhaseAccepting || s.Role != protocol.RoleAcceptor {
			t.Fatalf("phase/role = (%s,%s), want (accepting, acceptor)", s.Phase, s.Role)
		}
	})

	// profile 啟用 fixing → 轉 fixing
	t.Run("fixing enabled goes to fixing", func(t *testing.T) {
		id := "F999-dta2"
		ws := setupPhaseWorkspace(t, id)
		s := base
		s.FeatureID = id
		pc := protocol.ProfileConfig{Phases: []protocol.PhaseSpec{{Phase: string(protocol.PhaseFixing)}}}
		cont, err := deepTransitionAccepting(ws, id, &s, pc)
		if err != nil || !cont {
			t.Fatalf("got (cont=%v, err=%v), want (true, nil)", cont, err)
		}
		if s.Phase != protocol.PhaseFixing || s.Role != protocol.RoleFixer {
			t.Fatalf("phase/role = (%s,%s), want (fixing, fixer)", s.Phase, s.Role)
		}
	})
}

func TestWriteDeepReviewFailReport(t *testing.T) {
	id := "F999-wdfr"
	ws := setupPhaseWorkspace(t, id)
	if err := os.MkdirAll(ws.RoundDir(id, 1), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDeepReviewFailReport(ws, id, 1, "self-heal-exhausted", "3 iterations")

	data, err := os.ReadFile(filepath.Join(ws.RoundDir(id, 1), protocol.DeepReviewReport))
	if err != nil {
		t.Fatalf("report not written: %v", err)
	}
	content := string(data)
	if !contains(content, "self-heal-exhausted") || !contains(content, "3 iterations") {
		t.Fatalf("report missing reason/detail: %s", content)
	}
	// 產出必須是可被 ParseReviewVerdict 判為 FAIL（含 critical）的格式，才能驅動後續 amending。
	if ReviewPassedAtPath(filepath.Join(ws.RoundDir(id, 1), protocol.DeepReviewReport)) {
		t.Fatal("FAIL report should not parse as passed")
	}
}

func TestWriteDeepEscalation(t *testing.T) {
	id := "F999-wde"
	ws := setupPhaseWorkspace(t, id)
	if err := os.MkdirAll(ws.RoundDir(id, 1), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDeepEscalation(ws, id, 1, "blocker", "cannot proceed")

	esc := ReadEscalation(ws, id, 1)
	if !esc.Needed || esc.Reason != "blocker" || esc.Detail != "cannot proceed" {
		t.Fatalf("escalation = %+v, want needed/blocker/cannot proceed", esc)
	}
}

func TestNewEnrichRunner_DisabledReturnsNil(t *testing.T) {
	id := "F999-ner"
	ws := setupPhaseWorkspace(t, id)
	r := newBareRunner(ws, id) // Cfg.EnrichDiscoveredFeatures 預設 false
	if got := r.newEnrichRunner("claude", 1); got != nil {
		t.Fatalf("enrich disabled should return nil runner, got %v", got)
	}
}

// TestResolveRunnerAndModel_Success 驗證 coding phase 能從 cfg 解析出 runner，不觸發 error path。
func TestResolveRunnerAndModel_Success(t *testing.T) {
	id := "F165-rrm"
	ws, cfg, feature := setupDeepWS(t, id)
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Ops: fakeConvergeOps{}}}
	s, _ := ws.ReadState(id)
	pc := resolvePC(t, cfg, feature)

	phaseRunner, _, err := r.resolveRunnerAndModel(protocol.PhaseCoding, protocol.RoleCoder, pc, &s)
	if err != nil {
		t.Fatalf("resolveRunnerAndModel error: %v", err)
	}
	if phaseRunner != "claude" {
		t.Fatalf("phaseRunner = %q, want claude", phaseRunner)
	}
}

// TestResolvePrompt_FallbackGenerates 驗證 pending 為 nil 時 resolvePrompt 直接產生 prompt
// 且非空（template 缺失時走 inline fallback）。
func TestResolvePrompt_FallbackGenerates(t *testing.T) {
	id := "F165-rp"
	ws, cfg, feature := setupDeepWS(t, id)
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Ops: fakeConvergeOps{}}}
	s, _ := ws.ReadState(id)

	var pending *prompt.Prefetch
	got := r.resolvePrompt(&pending, protocol.RoleCoder, &s, "claude")
	if got == "" {
		t.Fatal("resolvePrompt returned empty string")
	}
	if pending != nil {
		t.Fatal("pending should be reset to nil after resolvePrompt")
	}
}

// TestGenerateAcceptanceSummary_WritesFile 驗證有 verify.json / review-report / deep-review-report
// 可彙整時，generateAcceptanceSummary 寫出 acceptance-summary.md。
func TestGenerateAcceptanceSummary_WritesFile(t *testing.T) {
	id := "F165-gas"
	ws, cfg, feature := setupDeepWS(t, id)
	// deep-review-report 存在讓 summary 有內容可彙整。
	writePhaseFile(t, filepath.Join(ws.RoundDir(id, 1), protocol.DeepReviewReport), cleanPassReport)
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Ops: fakeConvergeOps{}}}

	r.generateAcceptanceSummary(1)
	if _, err := os.Stat(filepath.Join(ws.RoundDir(id, 1), protocol.AcceptanceSummaryFile)); err != nil {
		t.Fatalf("acceptance-summary.md not written: %v", err)
	}
}

// TestSyncWorktree_NoopSameRoot 驗證 RunnerWs 與 Ws 同 root 時 sync 為 no-op（不 panic、不寫檔）。
func TestSyncWorktree_NoopSameRoot(t *testing.T) {
	id := "F165-sync"
	ws := setupPhaseWorkspace(t, id)
	r := newBareRunner(ws, id)
	r.syncToWorktree(id, 1)
	r.syncFromWorktree(id, 1)
}

// TestRunHealthCheck_NotTestingPhase 驗證非 testing phase 直接回 (false,nil) 不做健檢。
func TestRunHealthCheck_NotTestingPhase(t *testing.T) {
	id := "F165-hc"
	ws := setupPhaseWorkspace(t, id)
	r := newBareRunner(ws, id)
	s := protocol.State{FeatureID: id, Phase: protocol.PhaseCoding, Round: 1}
	stop, err := r.runHealthCheck(context.Background(), &s)
	if err != nil || stop {
		t.Fatalf("non-testing phase: got (stop=%v, err=%v), want (false, nil)", stop, err)
	}
}

// TestDeepGuardCheck_Fail 驗證 guard 未通過時 deepGuardCheck 落地 needs-attention 並回 (false,nil)。
// workspace 缺 required design 檔（task-brief/criteria 未寫），guard.Check 對 feature 檢查失敗。
func TestDeepGuardCheck_Fail(t *testing.T) {
	id := "F999-dgc"
	ws := setupPhaseWorkspace(t, id)
	s := protocol.State{FeatureID: id, Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: 1, Active: true}
	if err := ws.WriteState(id, s); err != nil {
		t.Fatal(err)
	}
	ok, err := deepGuardCheck(ws, id, &s, fakeConvergeOps{}, protocol.RoleDeepReviewer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("guard should fail for workspace missing required files")
	}
	if s.Phase != protocol.PhaseNeedsAttention {
		t.Fatalf("phase = %s, want needs-attention after guard fail", s.Phase)
	}
}
