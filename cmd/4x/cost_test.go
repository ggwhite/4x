package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

func TestParseStreamFileName(t *testing.T) {
	cases := []struct {
		name      string
		wantRound int
		wantRole  string
		wantOK    bool
	}{
		{"round-1-coder.stream.jsonl", 1, "coder", true},
		{"round-0-designer.stream.jsonl", 0, "designer", true},
		{"round-2-deep-reviewer-3.stream.jsonl", 2, "deep-reviewer", true},
		{"round-2-deep-fix-1.stream.jsonl", 2, "deep-fix", true},
		{"round-2-design-reviewer.stream.jsonl", 2, "design-reviewer", true},
		{"round-1-coder.log", 0, "", false},
		{"garbage.stream.jsonl", 0, "", false},
	}
	for _, c := range cases {
		round, role, ok := parseStreamFileName(c.name)
		if ok != c.wantOK || round != c.wantRound || role != c.wantRole {
			t.Errorf("parseStreamFileName(%q) = (%d, %q, %v), want (%d, %q, %v)",
				c.name, round, role, ok, c.wantRound, c.wantRole, c.wantOK)
		}
	}
}

func costTestWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}); err != nil {
		t.Fatal(err)
	}
	return &protocol.Workspace{Root: root}
}

func writeStreamLog(t *testing.T, ws *protocol.Workspace, featureID, fileName, content string) {
	t.Helper()
	dir := runner.LogDir(ws, featureID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resultLine(cost float64) string {
	return `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}` + "\n" +
		`{"type":"result","subtype":"success","total_cost_usd":` +
		formatFloat(cost) + `,"usage":{"input_tokens":10,"output_tokens":20}}` + "\n"
}

func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func TestCollectCost_StreamPrimary(t *testing.T) {
	ws := costTestWorkspace(t)
	writeStreamLog(t, ws, "F001-a", "round-1-coder.stream.jsonl", resultLine(2.5))
	writeStreamLog(t, ws, "F001-a", "round-1-reviewer.stream.jsonl", resultLine(1.0))
	writeStreamLog(t, ws, "F001-a", "round-2-coder.stream.jsonl", resultLine(0.5))

	data, err := collectCost(ws, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(data.Entries))
	}
	total, calls := grandTotal(data.Entries)
	if calls != 3 || total != 4.0 {
		t.Errorf("total=%v calls=%d, want total=4.0 calls=3", total, calls)
	}
	if data.Skipped != 0 {
		t.Errorf("skipped=%d, want 0", data.Skipped)
	}
}

func TestCollectCost_SkippedMissingCost(t *testing.T) {
	ws := costTestWorkspace(t)
	// 有成本的一筆
	writeStreamLog(t, ws, "F001-a", "round-1-coder.stream.jsonl", resultLine(2.5))
	// 缺 total_cost_usd 的舊 run（只有 assistant，無 result cost）→ 應計入 skipped
	writeStreamLog(t, ws, "F001-a", "round-1-tester.stream.jsonl",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`+"\n"+
			`{"type":"result","subtype":"success","duration_ms":100}`+"\n")

	data, err := collectCost(ws, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(data.Entries))
	}
	if data.Skipped != 1 {
		t.Errorf("skipped=%d, want 1", data.Skipped)
	}
}

func TestCollectCost_EventsFallback(t *testing.T) {
	ws := costTestWorkspace(t)
	// 沒有任何 stream.jsonl，只有 events.jsonl 的 run-end
	dir := ws.FeatureDir("F002-old")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	events := `{"ts":"2026-01-01T00:00:00Z","type":"run-end","role":"coder","round":1,"cost_usd":3.0}` + "\n" +
		`{"ts":"2026-01-01T00:01:00Z","type":"run-end","role":"reviewer","round":1,"cost_usd":1.0}` + "\n" +
		`{"ts":"2026-01-01T00:02:00Z","type":"phase-start","role":"tester","round":1}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, protocol.EventsFile), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := collectCost(ws, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Entries) != 2 {
		t.Fatalf("got %d entries, want 2 (run-end only)", len(data.Entries))
	}
	total, _ := grandTotal(data.Entries)
	if total != 4.0 {
		t.Errorf("total=%v, want 4.0", total)
	}
}

func TestCollectCost_StreamOverridesEvents(t *testing.T) {
	ws := costTestWorkspace(t)
	// 同一 feature 同時有 stream 與 events；stream 為主，不應與 events 重複計算。
	writeStreamLog(t, ws, "F003-both", "round-1-coder.stream.jsonl", resultLine(2.0))
	dir := ws.FeatureDir("F003-both")
	events := `{"ts":"2026-01-01T00:00:00Z","type":"run-end","role":"coder","round":1,"cost_usd":9.9}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, protocol.EventsFile), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := collectCost(ws, "")
	if err != nil {
		t.Fatal(err)
	}
	total, calls := grandTotal(data.Entries)
	if calls != 1 || total != 2.0 {
		t.Errorf("total=%v calls=%d, want total=2.0 calls=1 (stream wins, no double count)", total, calls)
	}
}

func TestCost_JSON_E2E(t *testing.T) {
	dir, ws := initWorkspace(t)
	writeStreamLog(t, ws, "F004-e2e", "round-1-coder.stream.jsonl", resultLine(4.0))
	writeStreamLog(t, ws, "F004-e2e", "round-2-coder.stream.jsonl", resultLine(1.0))

	// by-role 預設視圖
	out, err := run4x(dir, "cost", "--json")
	if err != nil {
		t.Fatalf("cost --json failed: %v\n%s", err, out)
	}
	var byRole costJSON
	if err := json.Unmarshal([]byte(out), &byRole); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if byRole.View != "by-role" || byRole.Calls != 2 || byRole.TotalUSD != 5.0 {
		t.Errorf("by-role: got view=%s calls=%d total=%v, want by-role/2/5.0", byRole.View, byRole.Calls, byRole.TotalUSD)
	}

	// by-round 視圖含 retry 佔比
	out, err = run4x(dir, "cost", "--by-round", "--json")
	if err != nil {
		t.Fatalf("cost --by-round --json failed: %v\n%s", err, out)
	}
	var byRound costJSON
	if err := json.Unmarshal([]byte(out), &byRound); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if byRound.View != "by-round" || byRound.RetryUSD != 1.0 {
		t.Errorf("by-round: got view=%s retryUsd=%v, want by-round/1.0", byRound.View, byRound.RetryUSD)
	}
	if byRound.RetryPct < 19.9 || byRound.RetryPct > 20.1 {
		t.Errorf("by-round: retryPct=%v, want ~20.0", byRound.RetryPct)
	}

	// by-feature 明細
	out, err = run4x(dir, "cost", "--feature", "F004-e2e", "--json")
	if err != nil {
		t.Fatalf("cost --feature --json failed: %v\n%s", err, out)
	}
	var byFeature costJSON
	if err := json.Unmarshal([]byte(out), &byFeature); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if byFeature.View != "by-feature" || byFeature.Feature != "F004-e2e" || len(byFeature.Rows) != 2 {
		t.Errorf("by-feature: got view=%s feature=%s rows=%d, want by-feature/F004-e2e/2",
			byFeature.View, byFeature.Feature, len(byFeature.Rows))
	}
}
