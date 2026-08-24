package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
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

	// testerErr 非 nil 時 tester runner 直接回傳該錯誤（模擬 runner 失敗路徑）。
	testerErr error
	// miniAction 非 nil 時在 mini-coder 被呼叫時執行（模擬收斂期間寫 escalation 等副作用）。
	miniAction func()
	// codexSessionID 非空時，runner log 前置一行 thread.started 事件（模擬 codex --json 輸出），
	// 讓 runEndMetrics 能解析出 session id 並定位 rollout fixture（F168 codex 接線測試用）。
	codexSessionID string

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
		logContent := "exit-0\n"
		if ps.codexSessionID != "" {
			logContent = `{"type":"thread.started","thread_id":"` + ps.codexSessionID + `"}` + "\n" + logContent
			_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
		}
		_ = os.WriteFile(logPath, []byte(logContent), 0o644)
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
			if ps.miniAction != nil {
				ps.miniAction()
			}
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
			if ps.testerErr != nil {
				return nil, ps.testerErr
			}
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
	writeFile(t, filepath.Join(fd, protocol.TaskBrief), "# Task Brief\n## Premise Challenge\n- verified\n")
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

// TestRunReviewTestParallel_TesterLogNoCollisionOnRerun 驗證：同一個 Runner 在同一 round 內
// 第二次呼叫 RunReviewTestParallel（模擬 review convergence 修完後、sequential testing phase
// 又重跑 tester 的情境）時，tester log 檔名要透過 r.roleRoundIter 取得 -2 後綴，不與第一次
// 的 round-1-tester.log 同名互相覆寫（實際案例：ws-227 review-fix 收斂後重跑 tester，
// dashboard 側邊清單看不出新一輪 testing 已經開始）。
func TestRunReviewTestParallel_TesterLogNoCollisionOnRerun(t *testing.T) {
	root := t.TempDir()
	ws, cfg, feature := setupParallelWS(t, root, "F177-tester-rerun")
	script := &parallelScript{ws: ws, featureID: feature.ID, round: 1, reviewerReports: []string{cleanPassReport, cleanPassReport}}
	r := newParallelRunner(ws, ws, cfg, feature, fakeConvergeOps{}, script)
	// parallelScript.newRunner 只在 codexSessionID 非空時才 MkdirAll logPath 的目錄
	// （模擬 codex 產出時才需要）；此測試斷言 log 檔案本身是否存在，故需自行預先建立。
	if err := os.MkdirAll(runner.LogDir(ws, feature.ID), 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}

	s, _ := ws.ReadState(feature.ID)
	if _, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature)); err != nil {
		t.Fatalf("RunReviewTestParallel (1st): %v", err)
	}
	firstLog := filepath.Join(runner.LogDir(ws, feature.ID), runner.LogFileName(1, string(protocol.RoleTester)))
	if _, err := os.Stat(firstLog); err != nil {
		t.Fatalf("first tester log missing: %v", err)
	}

	// 重置 phase 回 reviewing，模擬同一 round 內再跑一次平行 review+test。
	s.Phase = protocol.PhaseReviewing
	s.Role = protocol.RoleReviewer
	if err := ws.WriteState(feature.ID, s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	if _, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature)); err != nil {
		t.Fatalf("RunReviewTestParallel (2nd): %v", err)
	}

	secondLog := filepath.Join(runner.LogDir(ws, feature.ID), runner.IterationLogFileName(1, string(protocol.RoleTester), 2))
	if _, err := os.Stat(secondLog); err != nil {
		t.Fatalf("second tester log %s missing, want distinct -2 suffixed file: %v", secondLog, err)
	}
	firstContent, _ := os.ReadFile(firstLog)
	if len(firstContent) == 0 {
		t.Error("first tester log was overwritten/emptied by second run, want preserved")
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

// TestRunReviewTestParallel_WorktreeMarkerSyncFailure 驗證 worktree 模式下，若
// SyncFeatureToWorktree 無法把 parallelReview:true 真的複製進 worktree 的 state.json
// （模擬：worktree 的 feature 目錄路徑被一個同名檔案佔用，MkdirAll 必然失敗），
// RunReviewTestParallel 回傳 hard error 並把 feature 轉 needs-attention，而不是靜默放
// reviewer/tester 在缺少並行豁免依據的情況下跑下去（F151 gap 的反面案例：若驗證漏接，
// 這裡會誤判成功繼續往下跑，本測試會抓到）。
func TestRunReviewTestParallel_WorktreeMarkerSyncFailure(t *testing.T) {
	mainRoot := t.TempDir()
	wtRoot := t.TempDir()
	main, cfg, feature := setupParallelWS(t, mainRoot, "F151-syncfail")
	if err := protocol.Init(wtRoot, cfg); err != nil {
		t.Fatalf("Init worktree: %v", err)
	}
	wt := &protocol.Workspace{Root: wtRoot}

	// 讓 worktree 側的 feature 目錄路徑被一個同名檔案佔用，SyncFeatureToWorktree 的
	// os.MkdirAll(dstDir, ...) 必然失敗（best-effort，只 slog.Warn 不中止），state.json
	// 永遠複製不進去。
	featureDirPath := wt.FeatureDir(feature.ID)
	if err := os.MkdirAll(filepath.Dir(featureDirPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(featureDirPath, []byte("blocking file"), 0o644); err != nil {
		t.Fatal(err)
	}

	script := &parallelScript{ws: wt, featureID: feature.ID, round: 1, reviewerReports: []string{cleanPassReport}}
	r := newParallelRunner(main, wt, cfg, feature, fakeConvergeOps{}, script)

	s, _ := main.ReadState(feature.ID)
	cont, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature))
	if err == nil {
		t.Fatal("RunReviewTestParallel returned nil error, want hard error when parallel marker never synced to worktree")
	}
	if cont {
		t.Error("cont = true, want false on marker sync failure")
	}
	if script.reviewerCalls > 0 || script.testerCalls > 0 {
		t.Error("reviewer/tester were invoked despite parallel marker never reaching the worktree")
	}
	// StopState 只設 Active=false + StopReason（不動 Phase），與同檔其他 resolveErr 型
	// hard-error 路徑（如 model resolution 失敗）一致。
	got, _ := main.ReadState(feature.ID)
	if got.Active {
		t.Error("main state.json active = true, want false after hard error")
	}
	if got.StopReason != "sync-error" {
		t.Errorf("main state.json stopReason = %q, want sync-error", got.StopReason)
	}
}

// TestRunReviewTestParallel_ConditionalConvergence 驗證 AC-7 + F151 review Finding 1：reviewer
// 首次 CONDITIONAL PASS 時 RunReviewTestParallel 呼叫 runReviewConvergence，觸發 mini-coder ≥1 次
// 並重跑 reviewer ≥1 次。收斂套用過程式碼變更，本輪 tester 平行產出的 verify.json 已 stale——
// 必須清掉並只轉入 testing 讓 tester 重跑，不可沿用 stale verify.json 雙跳 deep-reviewing。
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
		t.Fatal("cont = false, want true (converged clean PASS should hand back to main loop)")
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
	if s.Phase != protocol.PhaseTesting {
		t.Errorf("phase = %q, want testing (convergence applied code changes; tester must re-run)", s.Phase)
	}
	if s.Role != protocol.RoleTester {
		t.Errorf("role = %q, want tester", s.Role)
	}
	if _, err := os.Stat(filepath.Join(ws.RoundDir(feature.ID, 1), protocol.VerifyFile)); !os.IsNotExist(err) {
		t.Errorf("stale verify.json still present after convergence (stat err = %v), want removed", err)
	}
}

// TestRunReviewTestParallel_RunnerError_ResetsParallelFlag 驗證 F151 review Finding 5 失敗路徑：
// tester runner 失敗時 RunReviewTestParallel 回傳 error，但 main state.json 的 parallelReview
// 仍已重設為 false，不殘留到後續 resume。
func TestRunReviewTestParallel_RunnerError_ResetsParallelFlag(t *testing.T) {
	root := t.TempDir()
	ws, cfg, feature := setupParallelWS(t, root, "F151-err")
	script := &parallelScript{ws: ws, featureID: feature.ID, round: 1,
		reviewerReports: []string{cleanPassReport}, testerErr: errors.New("tester runner boom")}
	r := newParallelRunner(ws, ws, cfg, feature, fakeConvergeOps{}, script)

	s, _ := ws.ReadState(feature.ID)
	cont, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature))
	if err == nil {
		t.Fatal("err = nil, want tester runner error")
	}
	if cont {
		t.Error("cont = true, want false on runner error")
	}
	if s.ParallelReview {
		t.Error("in-memory state ParallelReview = true after runner error, want false")
	}
	if got, _ := ws.ReadState(feature.ID); got.ParallelReview {
		t.Error("main state.json parallelReview = true after runner error, want false")
	}
}

// TestRunReviewTestParallel_ConvergenceEscalation 驗證 F151 review Finding 2：收斂期間
// mini-coder 寫入的 escalation.json 不可被丟棄——收斂完成後須重讀並依既有語意路由
// （非 designer reason → needs-attention）。
func TestRunReviewTestParallel_ConvergenceEscalation(t *testing.T) {
	root := t.TempDir()
	ws, cfg, feature := setupParallelWS(t, root, "F151-esc")
	script := &parallelScript{ws: ws, featureID: feature.ID, round: 1, reviewerReports: []string{condPassReport, cleanPassReport}}
	script.miniAction = func() {
		writeFile(t, filepath.Join(ws.RoundDir(feature.ID, 1), protocol.EscalationFile),
			`{"needed": true, "reason": "blocker", "detail": "cannot fix warning without out-of-scope change"}`)
	}
	r := newParallelRunner(ws, ws, cfg, feature, fakeConvergeOps{}, script)

	s, _ := ws.ReadState(feature.ID)
	cont, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("RunReviewTestParallel: %v", err)
	}
	if cont {
		t.Error("cont = true, want false (escalation written during convergence must stop the loop)")
	}
	if s.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("phase = %q, want needs-attention (post-convergence escalation must be honored)", s.Phase)
	}
	if s.StopMessage != "blocker" {
		t.Errorf("stop message = %q, want %q", s.StopMessage, "blocker")
	}
}

// TestParallelReviewRouted_GateComplement 驗證 F151 review Finding 3：routePhase 的 parallel
// 路由條件與 RunLoop 的 serial 收斂 gate 共用 parallelReviewRouted、彼此互補——
// parallel_review_test=true 但 profile 缺 tester（如內建 quick）時不路由 parallel 路徑，
// serial gate（!parallelReviewRouted）因而為 true，CONDITIONAL PASS 收斂不會兩頭落空。
func TestParallelReviewRouted_GateComplement(t *testing.T) {
	root := t.TempDir()
	ws, cfg, feature := setupParallelWS(t, root, "F151-gate")
	r := newParallelRunner(ws, ws, cfg, feature, fakeConvergeOps{}, &parallelScript{ws: ws, featureID: feature.ID, round: 1})

	_, quickPC, err := protocol.ResolveProfile(cfg, feature, "quick")
	if err != nil {
		t.Fatalf("ResolveProfile(quick): %v", err)
	}
	if r.parallelReviewRouted(quickPC) {
		t.Error("parallelReviewRouted(quick) = true, want false (no tester in profile → serial convergence must run)")
	}
	s, _ := ws.ReadState(feature.ID)
	routed, _, err := r.routePhase(context.Background(), &s, quickPC)
	if err != nil {
		t.Fatalf("routePhase: %v", err)
	}
	if routed {
		t.Error("routePhase routed reviewing with quick profile, want serial path")
	}

	_, fullPC, err := protocol.ResolveProfile(cfg, feature, "full")
	if err != nil {
		t.Fatalf("ResolveProfile(full): %v", err)
	}
	if !r.parallelReviewRouted(fullPC) {
		t.Error("parallelReviewRouted(full) = false, want true (reviewer+tester enabled, parallel_review_test on)")
	}

	cfgOff := cfg
	cfgOff.ParallelReviewTest = false
	rOff := newParallelRunner(ws, ws, cfgOff, feature, fakeConvergeOps{}, &parallelScript{ws: ws, featureID: feature.ID, round: 1})
	if rOff.parallelReviewRouted(fullPC) {
		t.Error("parallelReviewRouted = true with parallel_review_test=false, want false")
	}
}

// TestRecoverState_ClearsParallelReview 驗證 F151 review Finding 4：process 在 parallel 段落
// 內硬死、state.json 殘留 parallelReview:true 時，RecoverState 一律清除（不論是否觸發
// phase recovery），避免 stale 訊號被後續每次 WriteState 一路帶著。
func TestRecoverState_ClearsParallelReview(t *testing.T) {
	root := t.TempDir()
	ws, cfg, feature := setupParallelWS(t, root, "F151-recover")

	// 不觸發 phase recovery 的 early-return 路徑（designing / round 0）。
	s := protocol.State{FeatureID: feature.ID, Phase: protocol.PhaseDesigning, Role: protocol.RoleDesigner, Round: 0, Active: true, ParallelReview: true}
	got, err := RecoverState(ws, feature.ID, s, cfg, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("RecoverState: %v", err)
	}
	if got.ParallelReview {
		t.Error("ParallelReview = true after RecoverState (early-return path), want false")
	}

	// 觸發 phase recovery 的路徑（reviewing / round 1，模擬並行段落內硬死）。
	s = protocol.State{FeatureID: feature.ID, Phase: protocol.PhaseReviewing, Role: protocol.RoleReviewer, Round: 1, Active: true, ParallelReview: true}
	got, err = RecoverState(ws, feature.ID, s, cfg, resolvePC(t, cfg, feature))
	if err != nil {
		t.Fatalf("RecoverState (recovery path): %v", err)
	}
	if got.ParallelReview {
		t.Error("ParallelReview = true after RecoverState (recovery path), want false")
	}
}
