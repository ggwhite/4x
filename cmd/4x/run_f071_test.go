package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// TestPrefetchablePhase 驗證只對「主迴圈頂端會走同步 generatePrompt」的 phase 預生成。
func TestPrefetchablePhase(t *testing.T) {
	serial := protocol.Config{}
	parallel := protocol.Config{ParallelReviewTest: true}

	cases := []struct {
		phase protocol.Phase
		cfg   protocol.Config
		want  bool
	}{
		{protocol.PhaseCoding, serial, true},
		{protocol.PhaseAmending, serial, true},
		{protocol.PhaseTesting, serial, true},
		{protocol.PhaseAccepting, serial, true},
		{protocol.PhaseReviewing, serial, true},
		{protocol.PhaseReviewing, parallel, false}, // 平行模式由 runReviewTestParallel 接管
		{protocol.PhaseDeepReviewing, serial, false},
		{protocol.PhaseDesigning, serial, false},
		{protocol.PhaseDone, serial, false},
		{protocol.PhasePendingReview, serial, false},
		{protocol.PhaseBlocked, serial, false},
		{protocol.PhaseNeedsAttention, serial, false},
	}
	for _, c := range cases {
		if got := prefetchablePhase(c.phase, c.cfg); got != c.want {
			t.Errorf("prefetchablePhase(%s, parallel=%v) = %v, want %v", c.phase, c.cfg.ParallelReviewTest, got, c.want)
		}
	}
}

// TestRunLoop_PrefetchConsistency 驗證：跑完整 happy path 後，runner 每一個 phase 收到的
// prompt 都與「以該 phase 的 role+round 同步呼叫 generatePrompt」的結果完全一致。
// 若 prefetch 的 role+round keying 有誤（例如服務到上一輪或別的 role 的 prompt），此斷言會失敗。
func TestRunLoop_PrefetchConsistency(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-prefetch")
	feature, _ := ws.LoadFeature("feat-prefetch")
	cfg, _ := ws.ReadConfig()

	s := protocol.State{
		FeatureID: "feat-prefetch", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	ws.WriteState("feat-prefetch", s)

	mock := &mockRunner{ws: ws, featureID: "feat-prefetch", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	if err := runLoop(context.Background(), ws, ws, feature, cfg, s, nil, func(string, string, string) runner.Runner { return mock }, "never", "", nil); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	if len(mock.prompts) != len(mock.phases) {
		t.Fatalf("captured %d prompts but %d phases", len(mock.prompts), len(mock.phases))
	}

	// 重建每一輪的 round 編號：designing 在 round 1，coding 起一律 round 1（happy path 單輪）。
	for i, phase := range mock.phases {
		role := mock.roles[i]
		round := 1
		if phase == protocol.PhaseDesigning || phase == protocol.PhaseDesignReviewing {
			round = 0
		}
		want, err := generatePrompt(ws, ws, feature, cfg, role, round, 0, "")
		if err != nil {
			t.Fatalf("generatePrompt(%s) failed: %v", role, err)
		}
		if mock.prompts[i] != want {
			t.Errorf("phase[%d]=%s role=%s prompt mismatch\n got: %.80q\nwant: %.80q",
				i, phase, role, mock.prompts[i], want)
		}
	}
}

// commitRecordingOps 記錄 Commit 呼叫；Commit 內刻意 sleep，
// 若 runLoop 沒有在 return 前 Wait 背景 commit，counter 會在斷言時尚未到位。
type commitRecordingOps struct {
	mockOps
	mu      sync.Mutex
	commits []string
	delay   time.Duration
}

func (o *commitRecordingOps) Commit(_, _, msg string) error {
	if o.delay > 0 {
		time.Sleep(o.delay)
	}
	o.mu.Lock()
	o.commits = append(o.commits, msg)
	o.mu.Unlock()
	return nil
}

func (o *commitRecordingOps) snapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.commits...)
}

// TestRunLoop_AsyncCommitCompletesBeforeReturn 驗證 P3：worktree 模式 per-round commit
// 改為背景執行後，runLoop 仍透過 defer commitWG.Wait() 保證所有背景 commit 在 return 前完成。
func TestRunLoop_AsyncCommitCompletesBeforeReturn(t *testing.T) {
	main := setupLoopWorkspace(t, "feat-async-commit")
	feature, _ := main.LoadFeature("feat-async-commit")
	cfg, _ := main.ReadConfig()

	// worktree 模式：runnerWs 為獨立 root，state 透過 syncFeatureToWorktree 同步過去。
	runnerWs := &protocol.Workspace{Root: t.TempDir()}

	s := protocol.State{
		FeatureID: "feat-async-commit", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	main.WriteState("feat-async-commit", s)

	// runner 在 worktree 端讀 state、寫 artifact。
	mock := &mockRunner{ws: runnerWs, featureID: "feat-async-commit", outcomes: []mockOutcome{
		{}, {}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}
	ops := &commitRecordingOps{delay: 20 * time.Millisecond}

	if err := runLoop(context.Background(), main, runnerWs, feature, cfg, s, ops, func(string, string, string) runner.Runner { return mock }, "per-round", "", nil); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := main.ReadState("feat-async-commit")
	if final.Phase != protocol.PhasePendingReview {
		t.Fatalf("phase = %s, want pending-review", final.Phase)
	}

	// happy path 只有 1 次 coding，故恰好 1 次 per-round commit；
	// 能在 runLoop return 後立刻讀到，證明 background commit 已被 Wait 完成。
	commits := ops.snapshot()
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1: %v", len(commits), commits)
	}
	if commits[0] != "wip(feat-async-commit): round 1" {
		t.Errorf("commit msg = %q, want round 1 wip", commits[0])
	}
}

// TestRunLoop_GuardFailNoCommit 驗證 P3 行為變更：guard 失敗（→ needs-attention）那輪
// 不再自動 wip-commit（commit 移到 guard 通過之後才啟動）。
func TestRunLoop_GuardFailNoCommit(t *testing.T) {
	main := setupLoopWorkspace(t, "feat-guard-nocommit")
	f := feature.Feature{ID: "feat-guard-nocommit", Name: "Guard NoCommit", Status: "not-started", Repos: []string{"allowed-repo"}}
	main.SaveFeature(f)
	feature, _ := main.LoadFeature("feat-guard-nocommit")
	cfg, _ := main.ReadConfig()

	runnerWs := &protocol.Workspace{Root: t.TempDir()}

	s := protocol.State{
		FeatureID: "feat-guard-nocommit", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock",
	}
	main.WriteState("feat-guard-nocommit", s)

	mock := &mockRunner{ws: runnerWs, featureID: "feat-guard-nocommit", outcomes: []mockOutcome{
		{}, {}, // designer, coder
	}}
	// DetectChangedRepos 回報 out-of-scope → coding 後 guard 失敗。
	ops := &commitRecordingOps{}
	ops.mockOps.changedRepos = []string{"out-of-scope-repo"}

	if err := runLoop(context.Background(), main, runnerWs, feature, cfg, s, ops, func(string, string, string) runner.Runner { return mock }, "per-round", "", nil); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	final, _ := main.ReadState("feat-guard-nocommit")
	if final.Phase != protocol.PhaseNeedsAttention {
		t.Fatalf("phase = %s, want needs-attention", final.Phase)
	}
	if commits := ops.snapshot(); len(commits) != 0 {
		t.Errorf("guard-fail round should not auto-commit, got: %v", commits)
	}
}
