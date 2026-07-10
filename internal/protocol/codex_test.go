package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexPresetArgsHasJSON 驗證 AC-1：codex preset 的 Args 為 ["exec","--json"]，
// OutputFormat 維持空字串（不走 stream-json processor），Stdin/Quiet 維持 true。
func TestCodexPresetArgsHasJSON(t *testing.T) {
	codex, ok := SupportedRunnerMap()["codex"]
	if !ok {
		t.Fatal("codex preset not found in SupportedRunnerMap")
	}
	wantArgs := []string{"exec", "--json"}
	if len(codex.Args) != len(wantArgs) {
		t.Fatalf("codex Args = %v, want %v", codex.Args, wantArgs)
	}
	for i, a := range wantArgs {
		if codex.Args[i] != a {
			t.Fatalf("codex Args = %v, want %v", codex.Args, wantArgs)
		}
	}
	if codex.OutputFormat != "" {
		t.Errorf("codex OutputFormat = %q, want empty (codex JSONL 不相容 claude stream-json processor)", codex.OutputFormat)
	}
	if !BoolVal(codex.Stdin) {
		t.Error("codex Stdin = false, want true")
	}
	if !BoolVal(codex.Quiet) {
		t.Error("codex Quiet = false, want true")
	}
}

// TestCodexEventSerialization 驗證 AC-6：codex run-end event 序列化到 events.jsonl 後，
// 該行含 codex 物件（primary_pct 為 0 時仍存在、不被 omit）與非零 tokens_used；
// claude 既有 run-end（Codex==nil）序列化時不出現 codex key。
func TestCodexEventSerialization(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("F168-codex"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	codexEvt := Event{
		Type: "run-end", Phase: PhaseCoding, Role: RoleCoder, Round: 1,
		TokensUsed: 15804,
		Codex:      &CodexUsage{PrimaryPercent: 0, SecondaryPercent: 60},
	}
	claudeEvt := Event{
		Type: "run-end", Phase: PhaseCoding, Role: RoleReviewer, Round: 1,
		TokensUsed: 100, CostUSD: 1.23,
	}
	if err := ws.AppendEvent("F168-codex", codexEvt); err != nil {
		t.Fatalf("AppendEvent codex: %v", err)
	}
	if err := ws.AppendEvent("F168-codex", claudeEvt); err != nil {
		t.Fatalf("AppendEvent claude: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(ws.FeatureDir("F168-codex"), EventsFile))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("events.jsonl 有 %d 行，want 2", len(lines))
	}

	// codex run-end 行：含 codex 物件、primary_pct:0（不被 omit）、secondary_pct:60、tokens_used:15804。
	codexLine := lines[0]
	for _, want := range []string{`"codex"`, `"primary_pct":0`, `"secondary_pct":60`, `"tokens_used":15804`} {
		if !strings.Contains(codexLine, want) {
			t.Errorf("codex run-end 行缺少 %s：\n%s", want, codexLine)
		}
	}

	// claude run-end 行：不含 codex key。
	claudeLine := lines[1]
	if strings.Contains(claudeLine, `"codex"`) {
		t.Errorf("claude run-end 行不該含 codex key：\n%s", claudeLine)
	}
}
