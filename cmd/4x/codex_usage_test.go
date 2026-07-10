package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/orchestrator"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// codexTestFeature 建立一個可被 LoadFeature 讀回的最小 feature。
func codexTestFeature(id string) feature.Feature {
	return feature.Feature{ID: id, Name: id + " test", Status: feature.StatusInProgress}
}

const codexTestSID = "019f4755-bafb-7de3-9aa3-fa2a2ece5d1c"

// codexTokenCountLine 產生一筆 rate_limits 非 null 的 token_count rollout 事件 JSON。
func codexTokenCountLine(primaryPct, secondaryPct float64, tokens int) string {
	return fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d}},"rate_limits":{"primary":{"used_percent":%g,"window_minutes":300,"resets_at":1783625200},"secondary":{"used_percent":%g,"window_minutes":10080,"resets_at":1784030252}}}}`,
		tokens, primaryPct, secondaryPct)
}

// writeCodexRolloutFixture 在 codexHome 寫入一個 rollout fixture。
func writeCodexRolloutFixture(t *testing.T, codexHome, sessionID string, tokens int) {
	t.Helper()
	dir := filepath.Join(codexHome, "sessions", "2026", "07", "09")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-07-09T22-43-48-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(codexTokenCountLine(1, 60, tokens)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// codexRunEndEvent 回傳一行序列化好的 codex run-end event JSON（供 status/cost 顯示測試）。
func codexRunEndLine(round int, primaryPct, secondaryPct float64, tokens int) string {
	e := protocol.Event{
		Timestamp: "2026-07-09T00:00:00Z", Type: "run-end", Role: protocol.RoleCoder, Round: round,
		Runner: "codex", TokensUsed: tokens,
		Codex: &protocol.CodexUsage{PrimaryPercent: primaryPct, SecondaryPercent: secondaryPct},
	}
	b, _ := json.Marshal(e)
	return string(b)
}

// writeEventsFile 寫入 feature 的 events.jsonl。
func writeEventsFile(t *testing.T, ws *protocol.Workspace, featureID string, lines ...string) {
	t.Helper()
	dir := ws.FeatureDir(featureID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, protocol.EventsFile), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- AC-13：4x status 顯示 codex 用量 ---

func TestStatusFeatureDetailCodexUsage(t *testing.T) {
	ws := costTestWorkspace(t)
	if err := ws.SaveFeature(codexTestFeature("F168-st")); err != nil {
		t.Fatal(err)
	}
	writeEventsFile(t, ws, "F168-st", codexRunEndLine(3, 1, 60, 15804))

	out := captureStdout(t, func() {
		if err := showFeatureDetail(ws, "F168-st"); err != nil {
			t.Fatalf("showFeatureDetail: %v", err)
		}
	})
	if !strings.Contains(out, "Codex usage") {
		t.Errorf("輸出缺少 Codex usage 區塊：\n%s", out)
	}
	if !strings.Contains(out, "round 3: 5h 1% / 1wk 60% (15,804 tokens)") {
		t.Errorf("輸出缺少預期的 codex 明細行：\n%s", out)
	}
}

func TestStatusFeatureDetailCodexUsageAbsent(t *testing.T) {
	ws := costTestWorkspace(t)
	if err := ws.SaveFeature(codexTestFeature("F168-nost")); err != nil {
		t.Fatal(err)
	}
	// 只有 claude run-end（無 codex）。
	writeEventsFile(t, ws, "F168-nost",
		`{"ts":"2026-07-09T00:00:00Z","type":"run-end","role":"coder","round":1,"cost_usd":1.5}`)

	out := captureStdout(t, func() {
		if err := showFeatureDetail(ws, "F168-nost"); err != nil {
			t.Fatalf("showFeatureDetail: %v", err)
		}
	})
	if strings.Contains(out, "Codex usage") {
		t.Errorf("無 codex 用量的 feature 不該印 Codex usage：\n%s", out)
	}
}

// --- AC-14：4x cost --feature 顯示 codex 用量（含混合場景） ---

func TestCostByFeatureCodexUsageMixed(t *testing.T) {
	ws := costTestWorkspace(t)
	// claude stream（USD 2.0）＋ codex run-end（cost_usd 0，額度用量）混合。
	writeStreamLog(t, ws, "F168-mix", "round-1-coder.stream.jsonl", resultLine(2.0))
	writeEventsFile(t, ws, "F168-mix",
		codexRunEndLine(3, 1, 60, 15804)) // 該行 cost_usd 為 0（不進 USD）

	data, err := collectCost(ws, "F168-mix")
	if err != nil {
		t.Fatal(err)
	}
	data.CodexRounds = latestCodexUsageByRound(ws, "F168-mix")

	// USD 統計不受 codex 影響：stream-first，只算 claude 的 2.0。
	total, calls := grandTotal(data.Entries)
	if total != 2.0 || calls != 1 {
		t.Errorf("USD total=%v calls=%d, want 2.0/1（codex cost_usd=0 不進統計）", total, calls)
	}
	// codex 額度用量獨立讀 events，不被 stream-first 遮蔽。
	if len(data.CodexRounds) != 1 || data.CodexRounds[0].Round != 3 || data.CodexRounds[0].Tokens != 15804 {
		t.Fatalf("CodexRounds = %+v, want 1 筆 round=3 tokens=15804", data.CodexRounds)
	}

	// 文字輸出含 codex 區塊 + USD 表。
	textOut := captureStdout(t, func() {
		if err := renderByFeature(data, "F168-mix", false); err != nil {
			t.Fatalf("renderByFeature text: %v", err)
		}
	})
	if !strings.Contains(textOut, "Codex usage") ||
		!strings.Contains(textOut, "round 3: 5h 1% / 1wk 60% (15,804 tokens)") {
		t.Errorf("文字輸出缺 codex 區塊：\n%s", textOut)
	}
	if !strings.Contains(textOut, "2.0000") {
		t.Errorf("文字輸出遺失 USD 表：\n%s", textOut)
	}

	// JSON 輸出含 codex_rounds 欄位。
	jsonOut := captureStdout(t, func() {
		if err := renderByFeature(data, "F168-mix", true); err != nil {
			t.Fatalf("renderByFeature json: %v", err)
		}
	})
	var parsed costJSON
	if err := json.Unmarshal([]byte(jsonOut), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, jsonOut)
	}
	if len(parsed.CodexRounds) != 1 || parsed.CodexRounds[0].Round != 3 {
		t.Errorf("JSON codex_rounds = %+v, want 1 筆 round=3", parsed.CodexRounds)
	}
	if parsed.TotalUSD != 2.0 {
		t.Errorf("JSON totalUsd = %v, want 2.0", parsed.TotalUSD)
	}
}

func TestCostByFeatureCodexUsageAbsent(t *testing.T) {
	ws := costTestWorkspace(t)
	writeStreamLog(t, ws, "F168-pure", "round-1-coder.stream.jsonl", resultLine(2.0))

	data, err := collectCost(ws, "F168-pure")
	if err != nil {
		t.Fatal(err)
	}
	data.CodexRounds = latestCodexUsageByRound(ws, "F168-pure")

	jsonOut := captureStdout(t, func() {
		if err := renderByFeature(data, "F168-pure", true); err != nil {
			t.Fatalf("renderByFeature json: %v", err)
		}
	})
	if strings.Contains(jsonOut, "codex_rounds") {
		t.Errorf("純 claude feature 的 JSON 不該含 codex_rounds：\n%s", jsonOut)
	}
	textOut := captureStdout(t, func() {
		if err := renderByFeature(data, "F168-pure", false); err != nil {
			t.Fatalf("renderByFeature text: %v", err)
		}
	})
	if strings.Contains(textOut, "Codex usage") {
		t.Errorf("純 claude feature 的文字輸出不該含 Codex usage：\n%s", textOut)
	}
}

// --- AC-11：主 run loop 順序 run-end 接上 codex 用量 ---

// codexLoopMock 是驅動 RunLoop 的 role-dispatch fake runner：對每個 role 的 log 皆寫入
// thread.started 事件（模擬 codex --json），並寫出推進所需的最小 artifact。
type codexLoopMock struct {
	ws        *protocol.Workspace
	featureID string
	role      string
	logPath   string
	sessionID string
	mu        *sync.Mutex
}

func (m *codexLoopMock) Run(_ context.Context, _ string) (*runner.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, _ := m.ws.ReadState(m.featureID)
	round := s.Round
	roundDir := m.ws.RoundDir(m.featureID, round)
	_ = os.MkdirAll(roundDir, 0o755)
	featureDir := m.ws.FeatureDir(m.featureID)

	if m.logPath != "" && m.sessionID != "" {
		_ = os.MkdirAll(filepath.Dir(m.logPath), 0o755)
		_ = os.WriteFile(m.logPath, []byte(`{"type":"thread.started","thread_id":"`+m.sessionID+`"}`+"\nexit-0\n"), 0o644)
	}

	switch m.role {
	case "designer":
		_ = os.WriteFile(filepath.Join(featureDir, protocol.TaskBrief), []byte("# Brief\n## Premise Challenge\n- verified\n"), 0o644)
		_ = os.WriteFile(filepath.Join(featureDir, protocol.Criteria), []byte("# Criteria"), 0o644)
	case "design-reviewer":
		_ = os.WriteFile(filepath.Join(featureDir, protocol.DesignReviewReport), []byte("## Verdict\nPASS\n"), 0o644)
	case "coder":
		_ = os.WriteFile(filepath.Join(roundDir, protocol.CoderReport), []byte("# Coder Report"), 0o644)
		bg, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: round, Role: protocol.RoleCoder})
		_ = os.WriteFile(filepath.Join(roundDir, protocol.BuildGateFile), bg, 0o644)
	case "reviewer":
		_ = os.WriteFile(filepath.Join(roundDir, protocol.ReviewReport), []byte("## Verdict\nPASS\n"), 0o644)
	case "tester":
		ve := protocol.VerifyEvidence{Passed: true, Round: round,
			ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS"}}}}
		data, _ := json.Marshal(ve)
		_ = os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), data, 0o644)
		_ = os.WriteFile(filepath.Join(roundDir, protocol.TestReport), []byte("# Test"), 0o644)
		_ = os.WriteFile(filepath.Join(featureDir, protocol.FinalReport), []byte("# Final\n\n## Status\nready-for-review\n"), 0o644)
	}
	return &runner.Result{ExitCode: 0}, nil
}

func runCodexMainLoop(t *testing.T, featureID string, withRollout bool) *protocol.Workspace {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if withRollout {
		writeCodexRolloutFixture(t, home, codexTestSID, 15804)
	}

	ws := setupLoopWorkspace(t, featureID)
	feature, _ := ws.LoadFeature(featureID)
	cfg, _ := ws.ReadConfig()
	cfg.Default = "codex"
	cfg.Runners["codex"] = protocol.RunnerConfig{Tiers: map[string]string{"sonnet": "gpt-5.5", "opus": "gpt-5.5"}}

	s := protocol.State{FeatureID: featureID, Phase: protocol.PhaseInit, MaxRounds: 5, Active: true, Runner: "codex"}
	if err := ws.WriteState(featureID, s); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	factory := func(_, logPath, _ string) runner.Runner {
		return &codexLoopMock{ws: ws, featureID: featureID, role: roleFromLogPath(logPath),
			logPath: logPath, sessionID: codexTestSID, mu: &mu}
	}
	r := orchestrator.NewRunner(orchestrator.Config{
		Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, NewRunner: factory, CommitStrategy: "never",
	})
	if _, err := r.RunLoop(context.Background(), s); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	return ws
}

func codexCoderRunEnd(t *testing.T, ws *protocol.Workspace, featureID string) (found bool, hasCodex bool, tokens int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e protocol.Event
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Type == "run-end" && e.Role == protocol.RoleCoder {
			found = true
			if e.Codex != nil {
				hasCodex = true
				tokens = e.TokensUsed
			}
		}
	}
	return found, hasCodex, tokens
}

func TestRunLoopMainLoopCodexWiring(t *testing.T) {
	t.Run("codex usage recorded", func(t *testing.T) {
		ws := runCodexMainLoop(t, "feat-codex-main", true)
		found, hasCodex, tokens := codexCoderRunEnd(t, ws, "feat-codex-main")
		if !found {
			t.Fatal("找不到 coder run-end event")
		}
		if !hasCodex {
			t.Fatal("coder run-end 缺 codex 物件")
		}
		if tokens != 15804 {
			t.Errorf("coder run-end tokens = %d, want 15804", tokens)
		}
	})

	t.Run("rollout missing skips codex", func(t *testing.T) {
		ws := runCodexMainLoop(t, "feat-codex-skip", false)
		found, hasCodex, _ := codexCoderRunEnd(t, ws, "feat-codex-skip")
		if !found {
			t.Fatal("rollout 缺失時 coder run-end 仍須寫出")
		}
		if hasCodex {
			t.Error("rollout 缺失時 coder run-end 不該含 codex")
		}
	})
}
