package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// parallelScript 是 RunReviewTestParallel 的 scripted fake runner。依 log 檔名分辨子 role，
// reviewer 寫 review-report.md（依序取 reviewerReports）、tester 寫 verify.json + test-report.md
// + final-report.md、mini-coder（review-fix）僅記數。全部 runner 皆讀 readWs（worktree 模式下為
// worktree）的 state.json 並記錄當下 parallelReview 值，供 AC-2 斷言訊號傳播。以 mutex 保護
// 所有共享欄位——reviewer 與 tester 在 RunReviewTestParallel 內並行執行（-race 安全）。
type parallelScript struct {
	mu        sync.Mutex
	ws        *protocol.Workspace // runner 讀寫的 workspace（RunnerWs；worktree 模式下為 worktree）
	featureID string
	round     int

	reviewerReports []string
	reviewerIdx     int
	reviewerCalls   int
	testerCalls     int
	miniCalls       int

	reviewerSawParallel bool
	testerSawParallel   bool
}

// passingVerify 產生一份 passed=true、單一 exit-0 command、ac_results 全 pass 的 verify.json 內容。
func passingVerify(round int) []byte {
	ve := protocol.VerifyEvidence{
		Passed: true,
		Round:  round,
		Role:   protocol.RoleTester,
		Commands: []protocol.VerifyCommand{
			{Command: "go test ./...", ExitCode: 0},
		},
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"$ go test ./... → ok"}},
		},
	}
	data, _ := json.Marshal(ve)
	return data
}

func (ps *parallelScript) newRunner(_, logPath, _ string) runner.Runner {
	return funcRunner(func(_ context.Context) (*runner.Result, error) {
		_ = os.WriteFile(logPath, []byte("exit-0\n"), 0o644)
		base := filepath.Base(logPath)

		ps.mu.Lock()
		defer ps.mu.Unlock()

		// 讀 runner 實際使用的 workspace（worktree 模式為 worktree）的 state.json，記錄 parallel 訊號。
		sawParallel := false
		if st, err := ps.ws.ReadState(ps.featureID); err == nil {
			sawParallel = st.ParallelReview
		}

		roundDir := ps.ws.RoundDir(ps.featureID, ps.round)
		switch {
		case strings.Contains(base, "review-fix"):
			ps.miniCalls++
		case strings.Contains(base, "reviewer"):
			ps.reviewerCalls++
			ps.reviewerSawParallel = sawParallel
			content := cleanPassReport
			if ps.reviewerIdx < len(ps.reviewerReports) {
				content = ps.reviewerReports[ps.reviewerIdx]
			} else if len(ps.reviewerReports) > 0 {
				content = ps.reviewerReports[len(ps.reviewerReports)-1]
			}
			ps.reviewerIdx++
			_ = os.MkdirAll(roundDir, 0o755)
			_ = os.WriteFile(filepath.Join(roundDir, protocol.ReviewReport), []byte(content), 0o644)
		case strings.Contains(base, "tester"):
			ps.testerCalls++
			ps.testerSawParallel = sawParallel
			_ = os.MkdirAll(roundDir, 0o755)
			_ = os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), passingVerify(ps.round), 0o644)
			_ = os.WriteFile(filepath.Join(roundDir, protocol.TestReport), []byte("# Test Report\n## Verdict\nPASS\n"), 0o644)
			_ = os.WriteFile(filepath.Join(ps.ws.FeatureDir(ps.featureID), protocol.FinalReport), []byte("# Final Report\nPASS\n"), 0o644)
		}
		return &runner.Result{ExitCode: 0, LogFile: logPath}, nil
	})
}

// setupParallelWS 建立一個 reviewing/reviewer 起點的 workspace，seed task-brief + criteria + state，
// 但不預寫 review-report（由 scripted reviewer runner 產出）。
func setupParallelWS(t *testing.T, root, featureID string) (*protocol.Workspace, protocol.Config, feat.Feature) {
	t.Helper()
	cfg := protocol.Config{
		Project:            protocol.ProjectConfig{Name: "f151"},
		Default:            "claude",
		ParallelReviewTest: true,
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Tiers: map[string]string{"sonnet": "claude-sonnet", "opus": "claude-opus"}},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws := &protocol.Workspace{Root: root}
	feature := feat.Feature{ID: featureID, Name: featureID + " test", Status: feat.StatusInProgress}
	if err := ws.SaveFeature(feature); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	fd := ws.FeatureDir(featureID)
	if err := os.MkdirAll(fd, 0o755); err != nil {
		t.Fatalf("mkdir feature dir: %v", err)
	}
	st := protocol.State{FeatureID: featureID, Phase: protocol.PhaseReviewing, Role: protocol.RoleReviewer, Round: 1, Active: true}
	if err := ws.WriteState(featureID, st); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	writeFile(t, filepath.Join(fd, protocol.TaskBrief), "# Task Brief\n")
	writeFile(t, filepath.Join(fd, protocol.Criteria), "# Acceptance Criteria\n")
	return ws, cfg, feature
}

func newParallelRunner(main, runnerWs *protocol.Workspace, cfg protocol.Config, feature feat.Feature, ops gitops.Ops, script *parallelScript) *Runner {
	return &Runner{Config: Config{
		Ws:        main,
		RunnerWs:  runnerWs,
		Feature:   feature,
		Cfg:       cfg,
		Ops:       ops,
		NewRunner: script.newRunner,
	}}
}

// TestRunReviewTestParallel_HappyPath 驗證 AC-6 + AC-3（非 worktree）：reviewer PASS + tester
// passing verify.json → 回 (true,nil)、推進 deep-reviewing、round 不變、verify.json 存在，
// 且回寫的 main state.json parallelReview 已重置為 false。
func TestRunReviewTestParallel_HappyPath(t *testing.T) {
	root := t.TempDir()
	ws, cfg, feature := setupParallelWS(t, root, "F151-happy")
	script := &parallelScript{ws: ws, featureID: feature.ID, round: 1, reviewerReports: []string{cleanPassReport}}
	r := newParallelRunner(ws, ws, cfg, feature, fakeConvergeOps{}, script)

	s, _ := ws.ReadState(feature.ID)
	roundBefore := s.Round
	cont, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("RunReviewTestParallel: %v", err)
	}
	if !cont {
		t.Fatal("cont = false, want true (happy path should advance)")
	}
	if s.Phase != protocol.PhaseDeepReviewing {
		t.Errorf("phase = %q, want deep-reviewing", s.Phase)
	}
	if s.Round != roundBefore {
		t.Errorf("round changed %d → %d, want unchanged", roundBefore, s.Round)
	}
	if s.ParallelReview {
		t.Error("in-memory state ParallelReview = true, want false after parallel section")
	}
	// AC-3：回寫的 main state.json 亦已重置。
	if got, _ := ws.ReadState(feature.ID); got.ParallelReview {
		t.Error("main state.json parallelReview = true after completion, want false (must not leak to later phases)")
	}
	if _, err := os.Stat(filepath.Join(ws.RoundDir(feature.ID, 1), protocol.VerifyFile)); err != nil {
		t.Errorf("verify.json missing: %v", err)
	}
}

// TestRunReviewTestParallel_WorktreeSignalPropagation 驗證 AC-2 + AC-3：worktree 模式
// （RunnerWs != Ws）下，訊號在 SyncFeatureToWorktree 之前寫入 main 並同步到 worktree；reviewer
// 與 tester 兩個 fake runner 從 worktree state.json 讀到的 parallelReview 皆為 true；完成後 main
// state.json 重置為 false。
func TestRunReviewTestParallel_WorktreeSignalPropagation(t *testing.T) {
	mainRoot := t.TempDir()
	wtRoot := t.TempDir()
	main, cfg, feature := setupParallelWS(t, mainRoot, "F151-wt")
	if err := protocol.Init(wtRoot, cfg); err != nil {
		t.Fatalf("Init worktree: %v", err)
	}
	wt := &protocol.Workspace{Root: wtRoot}

	// runner 讀寫 worktree（RunnerWs）——與真實 worktree 模式一致。
	script := &parallelScript{ws: wt, featureID: feature.ID, round: 1, reviewerReports: []string{cleanPassReport}}
	r := newParallelRunner(main, wt, cfg, feature, fakeConvergeOps{}, script)

	s, _ := main.ReadState(feature.ID)
	cont, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("RunReviewTestParallel: %v", err)
	}
	if !cont {
		t.Fatal("cont = false, want true")
	}
	if !script.reviewerSawParallel {
		t.Error("reviewer read parallelReview=false from worktree state.json, want true (signal not propagated to worktree)")
	}
	if !script.testerSawParallel {
		t.Error("tester read parallelReview=false from worktree state.json, want true (signal not propagated to worktree)")
	}
	// AC-3：完成後 main state.json 重置為 false（worktree 殘留 true 不回抄覆蓋）。
	if got, _ := main.ReadState(feature.ID); got.ParallelReview {
		t.Error("main state.json parallelReview = true after completion, want false")
	}
	if s.Phase != protocol.PhaseDeepReviewing {
		t.Errorf("phase = %q, want deep-reviewing", s.Phase)
	}
}

// TestRunReviewTestParallel_ConditionalConvergence 驗證 AC-7：reviewer 首次 CONDITIONAL PASS
// 時 RunReviewTestParallel 呼叫 runReviewConvergence，觸發 mini-coder ≥1 次並重跑 reviewer ≥1 次；
// 收斂為乾淨 PASS 後推進 deep-reviewing、round 不變、全程 phase 維持 reviewing 直到收斂結束。
func TestRunReviewTestParallel_ConditionalConvergence(t *testing.T) {
	root := t.TempDir()
	ws, cfg, feature := setupParallelWS(t, root, "F151-cond")
	// 首次 reviewer（並行）寫 CONDITIONAL PASS；收斂重跑 reviewer 寫乾淨 PASS。
	script := &parallelScript{ws: ws, featureID: feature.ID, round: 1, reviewerReports: []string{condPassReport, cleanPassReport}}
	r := newParallelRunner(ws, ws, cfg, feature, fakeConvergeOps{}, script)

	s, _ := ws.ReadState(feature.ID)
	roundBefore := s.Round
	cont, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("RunReviewTestParallel: %v", err)
	}
	if !cont {
		t.Fatal("cont = false, want true (converged clean PASS should advance)")
	}
	if script.miniCalls < 1 {
		t.Errorf("mini-coder called %d times, want >=1 (conditional-pass convergence)", script.miniCalls)
	}
	// reviewer 至少被呼叫 2 次：1 次並行首評 + ≥1 次收斂重跑。
	if script.reviewerCalls < 2 {
		t.Errorf("reviewer called %d times, want >=2 (initial + convergence re-run)", script.reviewerCalls)
	}
	if s.Round != roundBefore {
		t.Errorf("round changed %d → %d, want unchanged", roundBefore, s.Round)
	}
	if s.Phase != protocol.PhaseDeepReviewing {
		t.Errorf("phase = %q, want deep-reviewing after convergence", s.Phase)
	}
}
