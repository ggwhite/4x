package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// readRunEndEvents 讀回 events.jsonl 內所有 run-end 事件，供接線測試斷言 codex 欄位。
func readRunEndEvents(t *testing.T, ws *protocol.Workspace, featureID string) []protocol.Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	var out []protocol.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e protocol.Event
		if json.Unmarshal([]byte(line), &e) == nil && e.Type == "run-end" {
			out = append(out, e)
		}
	}
	return out
}

// assertCodexRunEnd 斷言存在一筆 role 為 want 且 codex!=nil、tokens_used>0 的 run-end。
func assertCodexRunEnd(t *testing.T, ws *protocol.Workspace, featureID string, want protocol.Role) {
	t.Helper()
	for _, e := range readRunEndEvents(t, ws, featureID) {
		if e.Role == want && e.Codex != nil {
			if e.TokensUsed <= 0 {
				t.Errorf("%s run-end codex 存在但 tokens_used=%d，want >0", want, e.TokensUsed)
			}
			return
		}
	}
	t.Errorf("找不到 role=%s 且含 codex 的 run-end event", want)
}

// assertRunEndNoCodex 斷言存在 role 為 want 的 run-end，但其 codex 為 nil（rollout 缺失的 skip 案例）。
func assertRunEndNoCodex(t *testing.T, ws *protocol.Workspace, featureID string, want protocol.Role) {
	t.Helper()
	found := false
	for _, e := range readRunEndEvents(t, ws, featureID) {
		if e.Role == want {
			found = true
			if e.Codex != nil {
				t.Errorf("%s run-end 應無 codex（rollout 缺失），got %+v", want, e.Codex)
			}
		}
	}
	if !found {
		t.Errorf("找不到 role=%s 的 run-end event（rollout 缺失時仍須寫出）", want)
	}
}

// codexRunnerConfig 回傳可被 ResolvePhaseRunner/ResolveModel 解析的 codex runner 設定。
func codexRunnerConfig() protocol.RunnerConfig {
	return protocol.RunnerConfig{Tiers: map[string]string{"sonnet": "gpt-5.5", "opus": "gpt-5.5"}}
}

// TestRunReviewTestParallelCodexWiring 驗證 AC-9：RunReviewTestParallel 的 reviewer/tester
// run-end 接上 codex 用量；rollout 缺失時仍寫出 run-end（無 codex）、不回 error。
func TestRunReviewTestParallelCodexWiring(t *testing.T) {
	t.Run("codex usage recorded", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CODEX_HOME", home)
		writeCodexRollout(t, home, codexTestSessionID,
			codexTokenCountJSON(1, 60, 1783625200, 1784030252, 15804))

		ws, cfg, feature := setupParallelWS(t, t.TempDir(), "F168-par")
		cfg.Runners["codex"] = codexRunnerConfig()
		script := &parallelScript{ws: ws, featureID: feature.ID, round: 1,
			reviewerReports: []string{cleanPassReport}, codexSessionID: codexTestSessionID}
		r := newParallelRunner(ws, ws, cfg, feature, fakeConvergeOps{}, script)
		r.ManualRunner = "codex"

		s, _ := ws.ReadState(feature.ID)
		if _, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature)); err != nil {
			t.Fatalf("RunReviewTestParallel: %v", err)
		}
		assertCodexRunEnd(t, ws, feature.ID, protocol.RoleReviewer)
		assertCodexRunEnd(t, ws, feature.ID, protocol.RoleTester)
	})

	t.Run("rollout missing skips codex", func(t *testing.T) {
		home := t.TempDir() // 空的 CODEX_HOME，無任何 rollout
		t.Setenv("CODEX_HOME", home)

		ws, cfg, feature := setupParallelWS(t, t.TempDir(), "F168-par-skip")
		cfg.Runners["codex"] = codexRunnerConfig()
		script := &parallelScript{ws: ws, featureID: feature.ID, round: 1,
			reviewerReports: []string{cleanPassReport}, codexSessionID: codexTestSessionID}
		r := newParallelRunner(ws, ws, cfg, feature, fakeConvergeOps{}, script)
		r.ManualRunner = "codex"

		s, _ := ws.ReadState(feature.ID)
		if _, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature)); err != nil {
			t.Fatalf("RunReviewTestParallel: %v", err)
		}
		assertRunEndNoCodex(t, ws, feature.ID, protocol.RoleReviewer)
	})
}

// codexDeepParallelScript 是平行 deep review 的 codex-aware fake runner：sub-reviewer 寫 partial、
// synthesizer 寫 deep-review-report，所有 log 皆前置 thread.started 事件。
type codexDeepParallelScript struct {
	ws        *protocol.Workspace
	featureID string
	round     int
	sessionID string
}

func (ds *codexDeepParallelScript) newRunner(_, logPath, _ string) runner.Runner {
	return funcRunner(func(_ context.Context) (*runner.Result, error) {
		content := "exit-0\n"
		if ds.sessionID != "" {
			content = `{"type":"thread.started","thread_id":"` + ds.sessionID + `"}` + "\n" + content
		}
		_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
		_ = os.WriteFile(logPath, []byte(content), 0o644)
		rd := ds.ws.RoundDir(ds.featureID, ds.round)
		_ = os.MkdirAll(rd, 0o755)
		base := filepath.Base(logPath)
		switch {
		case strings.Contains(base, "synthesizer"):
			_ = os.WriteFile(filepath.Join(rd, protocol.DeepReviewReport), []byte(cleanPassReport), 0o644)
		case strings.Contains(base, "deep-reviewer"):
			idx := trailingLogIndex(base)
			_ = os.WriteFile(filepath.Join(rd, prompt.DeepReviewPartialName(idx)),
				[]byte("# Partial\n## Statistics\n- none\n"), 0o644)
		}
		return &runner.Result{ExitCode: 0, LogFile: logPath}, nil
	})
}

// trailingLogIndex 解析 round-<r>-deep-reviewer-<idx>.log 尾端的 idx。
func trailingLogIndex(base string) int {
	name := strings.TrimSuffix(base, ".log")
	parts := strings.Split(name, "-")
	if len(parts) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}

// TestDeepReviewParallelCodexWiring 驗證 AC-10（clean-pass 分支）：runDeepReviewParallel 的
// sub-reviewer run-end 與 runSynthesizer 的 synthesizer run-end 皆接上 codex 用量；
// rollout 缺失時仍寫出 run-end、不回 error。
func TestDeepReviewParallelCodexWiring(t *testing.T) {
	groups := [][]int{{1}, {2}} // len>1 → 走平行 + synthesizer 路徑

	t.Run("codex usage recorded", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CODEX_HOME", home)
		writeCodexRollout(t, home, codexTestSessionID,
			codexTokenCountJSON(1, 60, 1783625200, 1784030252, 15804))

		ws, cfg, feature := setupDeepWS(t, "F168-deeppar")
		script := &codexDeepParallelScript{ws: ws, featureID: feature.ID, round: 1, sessionID: codexTestSessionID}
		r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg,
			Ops: fakeConvergeOps{}, NewRunner: script.newRunner}}

		s, _ := ws.ReadState(feature.ID)
		if _, err := r.runDeepReviewParallel(context.Background(), &s, "codex", "gpt-5.5", groups, 1); err != nil {
			t.Fatalf("runDeepReviewParallel: %v", err)
		}
		assertCodexRunEnd(t, ws, feature.ID, protocol.RoleDeepReviewer)
		assertCodexRunEnd(t, ws, feature.ID, protocol.RoleSynthesizer)
	})

	t.Run("rollout missing skips codex", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CODEX_HOME", home)

		ws, cfg, feature := setupDeepWS(t, "F168-deeppar-skip")
		script := &codexDeepParallelScript{ws: ws, featureID: feature.ID, round: 1, sessionID: codexTestSessionID}
		r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg,
			Ops: fakeConvergeOps{}, NewRunner: script.newRunner}}

		s, _ := ws.ReadState(feature.ID)
		if _, err := r.runDeepReviewParallel(context.Background(), &s, "codex", "gpt-5.5", groups, 1); err != nil {
			t.Fatalf("runDeepReviewParallel: %v", err)
		}
		assertRunEndNoCodex(t, ws, feature.ID, protocol.RoleSynthesizer)
	})
}

// TestDeepReviewSubRoleCodexWiring 驗證 AC-10（fail-self-heal / 單 agent 分支）：runSubRole
// （runDeepSubRole 委派）的 run-end 接上 codex 用量。以 deepScript 驅動單 agent deep-review。
func TestDeepReviewSubRoleCodexWiring(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	writeCodexRollout(t, home, codexTestSessionID,
		codexTokenCountJSON(1, 60, 1783625200, 1784030252, 15804))

	ws, cfg, feature := setupDeepWS(t, "F168-deepsub")
	feature.Profile = "lite"
	cfg.Profiles = map[string]protocol.ProfileConfig{
		"lite": {Phases: []protocol.PhaseSpec{
			{Phase: string(protocol.PhaseCoding)},
			{Phase: string(protocol.PhaseReviewing)},
			{Phase: string(protocol.PhaseTesting)},
			{Phase: string(protocol.PhaseDeepReviewing)},
			{Phase: string(protocol.PhaseAccepting)},
		}},
	}
	cfg.Runners["codex"] = codexRunnerConfig()
	if err := ws.SaveFeature(feature); err != nil {
		t.Fatal(err)
	}

	script := &deepScript{ws: ws, featureID: feature.ID, round: 1, report: cleanPassReport, codexSessionID: codexTestSessionID}
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg,
		Ops: fakeConvergeOps{}, NewRunner: script.newRunner, ManualRunner: "codex"}}

	s, _ := ws.ReadState(feature.ID)
	if _, err := r.runDeepReviewPhase(context.Background(), &s); err != nil {
		t.Fatalf("runDeepReviewPhase: %v", err)
	}
	assertCodexRunEnd(t, ws, feature.ID, protocol.RoleDeepReviewer)
}
