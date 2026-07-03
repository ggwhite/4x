package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ggwhite/4x/internal/orchestrator"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// costRoleMockRunner 跟 roleMockRunner 一樣讓 reviewing phase 的 reviewer/tester 皆
// PASS，額外在各自的 log 檔留下可解析的 cost。用來驗證 RunReviewTestParallel 是否把
// 平行角色的成本累加進 Runner.totalCostUSD（F129 殘留 bug 的回歸測試：修正前這條
// 路徑只把 cost 寫進 events.jsonl，從未累加進行程內的 totalCostUSD，導致 CLI 結尾
// 印出的「$X total」低估實際花費）。
type costRoleMockRunner struct {
	ws        *protocol.Workspace
	featureID string
	role      string
	logPath   string
	mu        *sync.Mutex
}

func (m *costRoleMockRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, _ := m.ws.ReadState(m.featureID)
	round := s.Round
	roundDir := m.ws.RoundDir(m.featureID, round)
	os.MkdirAll(roundDir, 0o755)
	featureDir := m.ws.FeatureDir(m.featureID)

	switch m.role {
	case "designer":
		os.WriteFile(filepath.Join(featureDir, protocol.TaskBrief), []byte("# Brief"), 0o644)
		os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte("# Criteria"), 0o644)
	case "design-reviewer":
		os.WriteFile(filepath.Join(featureDir, protocol.DesignReviewReport), []byte("## Verdict\nPASS\n"), 0o644)
	case "coder":
		os.WriteFile(filepath.Join(roundDir, protocol.CoderReport), []byte("# Coder Report"), 0o644)
		bgData, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: round, Role: protocol.RoleCoder})
		os.WriteFile(filepath.Join(roundDir, protocol.BuildGateFile), bgData, 0o644)
	case "reviewer":
		os.WriteFile(filepath.Join(roundDir, protocol.ReviewReport), []byte("## Verdict\nPASS\n"), 0o644)
		os.MkdirAll(filepath.Dir(m.logPath), 0o755)
		os.WriteFile(m.logPath, []byte("[result] success (1.0s, $1.5000)\n"), 0o644)
	case "tester":
		ve := protocol.VerifyEvidence{Passed: true, Round: round,
			ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS (0.5s)"}}}}
		data, _ := json.Marshal(ve)
		os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), data, 0o644)
		os.WriteFile(filepath.Join(roundDir, protocol.TestReport), []byte("# Test"), 0o644)
		os.WriteFile(filepath.Join(featureDir, protocol.FinalReport), []byte("# Final\n\n## Status\nready-for-review\n"), 0o644)
		os.MkdirAll(filepath.Dir(m.logPath), 0o755)
		os.WriteFile(m.logPath, []byte("[result] success (1.0s, $2.5000)\n"), 0o644)
	}
	return &runner.Result{ExitCode: 0}, nil
}

// TestRunReviewTestParallel_AccumulatesCost 驗證平行 review/test（F129 發現的殘留
// bug）的 reviewer/tester 成本會累加進 orchestrator.Result.TotalCostUSD，而不是只
// 寫進 events.jsonl 卻遺失在行程內的總價統計裡（修正前這個值恆為 0）。
func TestRunReviewTestParallel_AccumulatesCost(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-cost")
	feature, _ := ws.LoadFeature("feat-cost")
	cfg, _ := ws.ReadConfig()
	cfg.ParallelReviewTest = true

	s := protocol.State{
		FeatureID: "feat-cost", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "mock", Profile: "full",
	}
	ws.WriteState("feat-cost", s)

	var mu sync.Mutex
	factory := func(_, logPath, _ string) runner.Runner {
		return &costRoleMockRunner{ws: ws, featureID: "feat-cost", role: roleFromLogPath(logPath), logPath: logPath, mu: &mu}
	}

	r := orchestrator.NewRunner(orchestrator.Config{
		Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, NewRunner: factory,
		CommitStrategy: "never",
	})
	result, err := r.RunLoop(context.Background(), s)
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}

	const want = 1.5 + 2.5 // reviewer $1.5 + tester $2.5，一輪就通過
	if result.TotalCostUSD != want {
		t.Errorf("TotalCostUSD = %.4f, want %.4f — parallel reviewer/tester cost must accumulate into Runner.totalCostUSD, not just events.jsonl", result.TotalCostUSD, want)
	}
}
