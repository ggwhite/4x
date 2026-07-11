package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestParseRunStatsFromLog(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name     string
		content  string
		wantTok  int
		wantCost float64
	}{
		{"claude format", "some output\ntokens used\n73,204\n", 73204, 0},
		{"large number", "output\ntokens used\n1,234,567\n", 1234567, 0},
		{"no commas", "output\ntokens used\n5000\n", 5000, 0},
		{"no token info", "just some log output\n", 0, 0},
		{"empty file", "", 0, 0},
		{"tokens used without number", "tokens used\n", 0, 0},
		{"tokens used with trailing text", "tokens used\n90,648\nsome trailing output\n", 90648, 0},
		{"stream-json result", "[tool_use] Bash: go test\n[result] success (325.5s, $2.2826)\n", 0, 2.2826},
		{"stream-json cost only", "[result] success (10.2s, $0.1500)\n", 0, 0.15},
		{"stream-json no cost", "[result] success (10.2s, $0.0000)\n", 0, 0},
		{"both formats", "tokens used\n50000\n[result] success (100.0s, $1.5000)\n", 50000, 1.5},
		// codex round log（無 claude token/[result] 行）：整檔掃描累加各 turn.completed 三欄 usage。
		{
			"codex single turn",
			`{"type":"thread.started","thread_id":"x"}` + "\n" +
				`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2,"reasoning_output_tokens":1}}` + "\n",
			13, 0,
		},
		{
			"codex multi turn",
			`{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":20,"reasoning_output_tokens":5}}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}` + "\n" +
				`{"type":"turn.completed","usage":{"input_tokens":1000,"output_tokens":200,"reasoning_output_tokens":50}}` + "\n",
			1375, 0,
		},
		// codex 壞行不失敗、只累加有效 turn（AC-11 壞輸入案例）。
		{
			"codex with bad line",
			`{bad json` + "\n" +
				`{"type":"turn.completed","usage":{"input_tokens":7,"output_tokens":3,"reasoning_output_tokens":0}}` + "\n",
			10, 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(dir, tt.name+".log")
			if err := os.WriteFile(p, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got := ParseRunStatsFromLog(p)
			if got.Tokens != tt.wantTok {
				t.Errorf("Tokens = %d, want %d", got.Tokens, tt.wantTok)
			}
			if got.CostUSD != tt.wantCost {
				t.Errorf("CostUSD = %f, want %f", got.CostUSD, tt.wantCost)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		got := ParseRunStatsFromLog(filepath.Join(dir, "nonexistent.log"))
		if got.Tokens != 0 || got.CostUSD != 0 {
			t.Errorf("ParseRunStatsFromLog(missing) = %+v, want zero", got)
		}
	})
}

// TestNextRoleIteration 驗證同一 round 內某 role 重複執行時（例如
// design-reviewing FAIL 打回 designing，round 本身不會遞增），每次呼叫都拿到
// 遞增的迭代號；不同 round 或不同 role 各自獨立計數。
func TestNextRoleIteration(t *testing.T) {
	counts := map[string]int{}

	if got := nextRoleIteration(counts, 0, protocol.RoleDesigner); got != 1 {
		t.Errorf("designer 第 1 次 = %d, want 1", got)
	}
	if got := nextRoleIteration(counts, 0, protocol.RoleDesignReviewer); got != 1 {
		t.Errorf("design-reviewer 第 1 次 = %d, want 1", got)
	}
	if got := nextRoleIteration(counts, 0, protocol.RoleDesigner); got != 2 {
		t.Errorf("designer 第 2 次 = %d, want 2", got)
	}
	if got := nextRoleIteration(counts, 0, protocol.RoleDesignReviewer); got != 2 {
		t.Errorf("design-reviewer 第 2 次 = %d, want 2", got)
	}
	if got := nextRoleIteration(counts, 1, protocol.RoleDesigner); got != 1 {
		t.Errorf("round 換了應重新計數 = %d, want 1", got)
	}
}

// TestArchiveDesignArtifact_Designer 驗證 designer 剛寫入的 task-brief.md／
// acceptance-criteria.md 會被複製一份到 design-rounds/round-<round>-<iteration>/，
// 讓 design-reviewing FAIL 打回 designing 之前的版本不會因為下一輪覆寫而消失。
func TestArchiveDesignArtifact_Designer(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-1")
	writePhaseFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TaskBrief), "brief v1")
	writePhaseFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.Criteria), "criteria v1")

	archiveDesignArtifact(ws, "feat-1", 0, 1, protocol.RoleDesigner)

	dir := filepath.Join(ws.FeatureDir("feat-1"), protocol.DesignRoundsDir, "round-0-1")
	if got, err := os.ReadFile(filepath.Join(dir, protocol.TaskBrief)); err != nil || string(got) != "brief v1" {
		t.Fatalf("task-brief archive = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, protocol.Criteria)); err != nil || string(got) != "criteria v1" {
		t.Fatalf("criteria archive = %q, err=%v", got, err)
	}
}

// TestArchiveDesignArtifact_DesignReviewer 驗證 design-reviewer 的
// design-review-report.md 也會被歸檔到同一個 round-<round>-<iteration>/ 目錄。
func TestArchiveDesignArtifact_DesignReviewer(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-1")
	writePhaseFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.DesignReviewReport), "FAIL: needs work")

	archiveDesignArtifact(ws, "feat-1", 0, 1, protocol.RoleDesignReviewer)

	dir := filepath.Join(ws.FeatureDir("feat-1"), protocol.DesignRoundsDir, "round-0-1")
	if got, err := os.ReadFile(filepath.Join(dir, protocol.DesignReviewReport)); err != nil || string(got) != "FAIL: needs work" {
		t.Fatalf("report archive = %q, err=%v", got, err)
	}
}

// TestArchiveDesignArtifact_OtherRoleNoop 驗證非 designer/design-reviewer 的 role
// 不會觸發歸檔（coding phase 已經有自己的 rounds/round-N/ 機制，不需要重複）。
func TestArchiveDesignArtifact_OtherRoleNoop(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-1")

	archiveDesignArtifact(ws, "feat-1", 1, 1, protocol.RoleCoder)

	if _, err := os.Stat(filepath.Join(ws.FeatureDir("feat-1"), protocol.DesignRoundsDir)); !os.IsNotExist(err) {
		t.Fatalf("expected no design-rounds dir for coder role, stat err = %v", err)
	}
}

// TestNewRunner_SeedsCostFromEvents 驗證 NewRunner 建構時以 TotalCost seed
// totalCostUSD：有 events.jsonl 歷史 run-end 時 seed 值等於其總和，無歷史時為 0。
func TestNewRunner_SeedsCostFromEvents(t *testing.T) {
	t.Run("with history", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, "feat-seed")
		if err := ws.AppendEvent("feat-seed", protocol.Event{Type: "run-end", CostUSD: 1.5}); err != nil {
			t.Fatal(err)
		}
		if err := ws.AppendEvent("feat-seed", protocol.Event{Type: "run-end", CostUSD: 2.25}); err != nil {
			t.Fatal(err)
		}

		r := NewRunner(Config{Ws: ws, Feature: feature.Feature{ID: "feat-seed"}})
		if want := 3.75; r.totalCostUSD != want {
			t.Errorf("totalCostUSD = %v, want %v", r.totalCostUSD, want)
		}
	})

	t.Run("no history", func(t *testing.T) {
		ws := setupPhaseWorkspace(t, "feat-new")

		r := NewRunner(Config{Ws: ws, Feature: feature.Feature{ID: "feat-new"}})
		if r.totalCostUSD != 0 {
			t.Errorf("totalCostUSD = %v, want 0", r.totalCostUSD)
		}
	})
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{73204, "73,204"},
		{1234567, "1,234,567"},
	}
	for _, tt := range tests {
		got := FormatTokens(tt.n)
		if got != tt.want {
			t.Errorf("FormatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
