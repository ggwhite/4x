package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
)

const codexTestSessionID = "019f4755-bafb-7de3-9aa3-fa2a2ece5d1c"

// codexTokenCountJSON 產生一筆 rate_limits 非 null 的 token_count rollout 事件 JSON。
func codexTokenCountJSON(primaryPct, secondaryPct float64, primaryReset, secondaryReset int64, tokens int) string {
	return fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d}},"rate_limits":{"primary":{"used_percent":%g,"window_minutes":300,"resets_at":%d},"secondary":{"used_percent":%g,"window_minutes":10080,"resets_at":%d}}}}`,
		tokens, primaryPct, primaryReset, secondaryPct, secondaryReset)
}

// writeCodexRollout 在 codexHome/sessions/2026/07/09/ 寫入 rollout fixture，回傳其路徑。
func writeCodexRollout(t *testing.T, codexHome, sessionID string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(codexHome, "sessions", "2026", "07", "09")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	path := filepath.Join(dir, "rollout-2026-07-09T22-43-48-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	return path
}

// writeCodexRoundLog 寫一個含 thread.started 的 codex round log（模擬 codex --json 的 JSONL 輸出，
// 開頭刻意含非 JSON 行以驗證略過）。
func writeCodexRoundLog(t *testing.T, logPath, sessionID string) {
	t.Helper()
	content := "Reading prompt from stdin...\n" +
		`{"type":"thread.started","thread_id":"` + sessionID + `"}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":1}}` + "\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write round log: %v", err)
	}
}

// TestParseCodexUsage_HappyPath 驗證 AC-2：從 thread.started 定位 rollout 並正確擷取
// 4 個百分比/resets 欄位與 tokens。須用真實 temp 檔案。
func TestParseCodexUsage_HappyPath(t *testing.T) {
	home := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "round-1-coder.log")
	writeCodexRoundLog(t, logPath, codexTestSessionID)
	writeCodexRollout(t, home, codexTestSessionID,
		codexTokenCountJSON(1.0, 60.0, 1783625200, 1784030252, 15804))

	usage, tokens := ParseCodexUsage(logPath, home)
	if usage == nil {
		t.Fatal("usage = nil, want non-nil")
	}
	if usage.PrimaryPercent != 1.0 || usage.SecondaryPercent != 60.0 {
		t.Errorf("percent = (%v, %v), want (1, 60)", usage.PrimaryPercent, usage.SecondaryPercent)
	}
	if usage.PrimaryResetsAt != 1783625200 || usage.SecondaryResetsAt != 1784030252 {
		t.Errorf("resets = (%d, %d), want (1783625200, 1784030252)", usage.PrimaryResetsAt, usage.SecondaryResetsAt)
	}
	if tokens != 15804 {
		t.Errorf("tokens = %d, want 15804", tokens)
	}
}

// TestParseCodexUsage_LastNonNullRateLimits 驗證 AC-4：多筆 token_count（非null=10% → null →
// 非null=42%）時只取最後一筆非 null 者。
func TestParseCodexUsage_LastNonNullRateLimits(t *testing.T) {
	home := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "round-1-coder.log")
	writeCodexRoundLog(t, logPath, codexTestSessionID)
	writeCodexRollout(t, home, codexTestSessionID,
		codexTokenCountJSON(10, 10, 1, 2, 100),
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":200}},"rate_limits":null}}`,
		codexTokenCountJSON(42, 84, 3, 4, 300),
	)

	usage, tokens := ParseCodexUsage(logPath, home)
	if usage == nil {
		t.Fatal("usage = nil, want non-nil")
	}
	if usage.PrimaryPercent != 42 {
		t.Errorf("PrimaryPercent = %v, want 42 (最後一筆非 null)", usage.PrimaryPercent)
	}
	if tokens != 300 {
		t.Errorf("tokens = %d, want 300 (取同一筆事件的 total_tokens)", tokens)
	}
}

// TestParseCodexUsage_BadInputs 驗證 AC-3：各種壞輸入一律回 (nil, 0)、不 panic。
func TestParseCodexUsage_BadInputs(t *testing.T) {
	t.Run("no thread.started", func(t *testing.T) {
		home := t.TempDir()
		logPath := filepath.Join(t.TempDir(), "round-1-coder.log")
		if err := os.WriteFile(logPath, []byte("Reading prompt from stdin...\n{\"type\":\"turn.completed\"}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeCodexRollout(t, home, codexTestSessionID, codexTokenCountJSON(1, 60, 1, 2, 15804))
		if u, n := ParseCodexUsage(logPath, home); u != nil || n != 0 {
			t.Errorf("got (%v, %d), want (nil, 0)", u, n)
		}
	})

	t.Run("rollout glob no match", func(t *testing.T) {
		home := t.TempDir()
		logPath := filepath.Join(t.TempDir(), "round-1-coder.log")
		writeCodexRoundLog(t, logPath, codexTestSessionID)
		// home 內沒有任何 rollout 檔。
		if u, n := ParseCodexUsage(logPath, home); u != nil || n != 0 {
			t.Errorf("got (%v, %d), want (nil, 0)", u, n)
		}
	})

	t.Run("all rate_limits null", func(t *testing.T) {
		home := t.TempDir()
		logPath := filepath.Join(t.TempDir(), "round-1-coder.log")
		writeCodexRoundLog(t, logPath, codexTestSessionID)
		writeCodexRollout(t, home, codexTestSessionID,
			`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":100}},"rate_limits":null}}`,
			`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":200}},"rate_limits":null}}`,
		)
		if u, n := ParseCodexUsage(logPath, home); u != nil || n != 0 {
			t.Errorf("got (%v, %d), want (nil, 0)", u, n)
		}
	})

	t.Run("bad JSON rollout", func(t *testing.T) {
		home := t.TempDir()
		logPath := filepath.Join(t.TempDir(), "round-1-coder.log")
		writeCodexRoundLog(t, logPath, codexTestSessionID)
		writeCodexRollout(t, home, codexTestSessionID, "{not json at all", "}}}garbage")
		if u, n := ParseCodexUsage(logPath, home); u != nil || n != 0 {
			t.Errorf("got (%v, %d), want (nil, 0)", u, n)
		}
	})
}

// TestResolveCodexHome 驗證 AC-5：env 非空原樣回傳；env 為空回退 <userHome>/.codex。
func TestResolveCodexHome(t *testing.T) {
	if got := ResolveCodexHome("/custom/codex"); got != "/custom/codex" {
		t.Errorf("ResolveCodexHome(/custom/codex) = %q, want /custom/codex", got)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("UserHomeDir unavailable: %v", err)
	}
	if got := ResolveCodexHome(""); got != filepath.Join(home, ".codex") {
		t.Errorf("ResolveCodexHome(\"\") = %q, want %q", got, filepath.Join(home, ".codex"))
	}
}

// TestRunEndMetrics_RolloutPrecedence 驗證 AC-6：codex precedence 為「rollout 優先」。
// 消費端驗證——追到 runEndMetrics 真正決定寫入 event 的 tokens 回傳值。
func TestRunEndMetrics_RolloutPrecedence(t *testing.T) {
	home := t.TempDir()
	logDir := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	// round log 內的 turn.completed 讓 ParseRunStatsFromLog 解析出非 0 的 codex token（=500）。
	logContent := `{"type":"thread.started","thread_id":"` + codexTestSessionID + `"}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":400,"output_tokens":80,"reasoning_output_tokens":20}}` + "\n"

	// 分支 A：rollout 可用（total_tokens=15804）→ 即使 log 解析出 500，也以 rollout 為準。
	logA := filepath.Join(logDir, "round-1-coder.log")
	if err := os.WriteFile(logA, []byte(logContent), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCodexRollout(t, home, codexTestSessionID, codexTokenCountJSON(1, 60, 1, 2, 15804))
	// 先確認 log 本身解析出的是 500（非 0），才能證明後續 15804 是 rollout「覆蓋」而非「補位」。
	if got := ParseRunStatsFromLog(logA).Tokens; got != 500 {
		t.Fatalf("ParseRunStatsFromLog log tokens = %d, want 500 (前提)", got)
	}
	rA := &Runner{Config: Config{Feature: feat.Feature{ID: "F178"}}}
	if tokens, _, codex := rA.runEndMetrics("codex", logA); codex == nil || tokens != 15804 {
		t.Errorf("rollout 優先分支 tokens = %d (codex nil? %v), want 15804", tokens, codex == nil)
	}

	// 分支 B：rollout 不可用（session id 對不到 rollout，cu==nil）→ 保留 log 解析的 codex 值（500）。
	logB := filepath.Join(logDir, "round-1-tester.log")
	noRolloutContent := `{"type":"thread.started","thread_id":"no-such-session"}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":400,"output_tokens":80,"reasoning_output_tokens":20}}` + "\n"
	if err := os.WriteFile(logB, []byte(noRolloutContent), 0o644); err != nil {
		t.Fatal(err)
	}
	rB := &Runner{Config: Config{Feature: feat.Feature{ID: "F178"}}}
	if tokens, _, codex := rB.runEndMetrics("codex", logB); codex != nil || tokens != 500 {
		t.Errorf("rollout 不可用分支 tokens = %d (codex nil? %v), want 500 且 codex==nil", tokens, codex == nil)
	}
}

// TestRunEndMetricsCodex 驗證 AC-8：runEndMetrics 對 codex 成功/失敗與 claude 三案的行為。
func TestRunEndMetricsCodex(t *testing.T) {
	home := t.TempDir()
	logDir := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	// codex 成功：rollout 可用（total_tokens=15804）→ rollout 優先，覆蓋 log 解析值。
	okLog := filepath.Join(logDir, "round-1-coder.log")
	writeCodexRoundLog(t, okLog, codexTestSessionID)
	writeCodexRollout(t, home, codexTestSessionID, codexTokenCountJSON(1, 60, 1, 2, 15804))
	r := &Runner{Config: Config{Feature: feat.Feature{ID: "F168"}}}
	tokens, _, codex := r.runEndMetrics("codex", okLog)
	if codex == nil {
		t.Fatal("codex = nil, want non-nil on success")
	}
	if tokens != 15804 {
		t.Errorf("tokens = %d, want 15804 (rollout 優先)", tokens)
	}
	if r.totalTokens != 15804 {
		t.Errorf("r.totalTokens = %d, want 15804 (累加)", r.totalTokens)
	}

	// codex 失敗（thread_id 對不到 rollout，cu==nil）：保留 ParseRunStatsFromLog 從 round log
	// 的 turn.completed 解析出的 codex fallback 值（writeCodexRoundLog 寫 input_tokens:1 → 1）。
	failLog := filepath.Join(logDir, "round-1-tester.log")
	writeCodexRoundLog(t, failLog, "no-such-session-id")
	rFail := &Runner{Config: Config{Feature: feat.Feature{ID: "F168"}}}
	fTokens, fCost, fCodex := rFail.runEndMetrics("codex", failLog)
	if fCodex != nil || fTokens != 1 || fCost != 0 {
		t.Errorf("codex fail = (%d, %v, %v), want (1, 0, nil) — rollout 不可用時保留 log fallback", fTokens, fCost, fCodex)
	}

	// claude 路徑不受影響：codex 恆 nil，成本照常從 [result] 行解析。
	claudeLog := filepath.Join(logDir, "round-1-reviewer.log")
	if err := os.WriteFile(claudeLog, []byte("[result] success (10.0s, $2.5000)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rClaude := &Runner{Config: Config{Feature: feat.Feature{ID: "F168"}}}
	_, cCost, cCodex := rClaude.runEndMetrics("claude", claudeLog)
	if cCodex != nil {
		t.Error("claude codex != nil, want nil")
	}
	if cCost != 2.5 {
		t.Errorf("claude cost = %v, want 2.5", cCost)
	}
}
