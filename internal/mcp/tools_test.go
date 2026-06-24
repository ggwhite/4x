package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// cmdKey 從 handler 組出的 args 推導出指令路徑作為 mockExec 的 key。
// 對已知的多層 subcommand（batch/config/learn）取前兩段（如 "batch next"、"config get"），
// 其餘取首段（如 "status"、"check"），確保既有單段 key 測試與新增 subcommand tool 皆可分別 mock。
func cmdKey(args []string) string {
	if len(args) == 0 {
		return ""
	}
	first := args[0]
	multi := map[string]bool{"batch": true, "config": true, "learn": true}
	if multi[first] && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
		return first + " " + args[1]
	}
	return first
}

func mockExec(responses map[string]string) ExecFunc {
	return func(ctx context.Context, args ...string) (json.RawMessage, error) {
		key := cmdKey(args)
		if resp, ok := responses[key]; ok {
			return json.RawMessage(resp), nil
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	}
}

// captureExec 回傳一個 ExecFunc 與一個指標，後者在被呼叫後保存收到的 args，
// 供測試斷言 handler 組出的旗標／參數正確。
func captureExec(resp string) (ExecFunc, *[]string) {
	got := &[]string{}
	fn := func(ctx context.Context, args ...string) (json.RawMessage, error) {
		*got = args
		return json.RawMessage(resp), nil
	}
	return fn, got
}

// argsContain 檢查 args 是否含某個值（旗標或位置參數）。
func argsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestStatusTool_NoArgs(t *testing.T) {
	exec := mockExec(map[string]string{
		"status": `{"features":[{"id":"F001","name":"test","status":"not-started"}]}`,
	})
	h := &Handlers{Exec: exec}

	input := StatusInput{}
	result, err := h.Status(context.Background(), input)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestStatusTool_WithFeatureID(t *testing.T) {
	exec := mockExec(map[string]string{
		"status": `{"feature":{"id":"F001","name":"test"},"state":null}`,
	})
	h := &Handlers{Exec: exec}

	input := StatusInput{FeatureID: "F001"}
	result, err := h.Status(context.Background(), input)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestNewTool(t *testing.T) {
	exec := mockExec(map[string]string{
		"new": `{"featureId":"F002-hello","name":"hello","path":".4x/features/F002-hello.yaml"}`,
	})
	h := &Handlers{Exec: exec}

	input := NewInput{Name: "hello"}
	result, err := h.New(context.Background(), input)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestCheckTool(t *testing.T) {
	exec := mockExec(map[string]string{
		"check": `{"pass":true,"errors":[],"warnings":[]}`,
	})
	h := &Handlers{Exec: exec}

	input := CheckInput{FeatureID: "F001"}
	result, err := h.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestStopTool(t *testing.T) {
	dir := t.TempDir()
	dotDir := filepath.Join(dir, ".4x")
	featuresDir := filepath.Join(dotDir, "features")
	featureDir := filepath.Join(dotDir, "run", "F001-test")
	os.MkdirAll(featuresDir, 0o755)
	os.MkdirAll(featureDir, 0o755)

	state := `{"featureId":"F001-test","phase":"coding","role":"coder","round":1,"maxRounds":5,"active":true,"runner":"claude","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}`
	os.WriteFile(filepath.Join(featureDir, "state.json"), []byte(state), 0o644)
	os.WriteFile(filepath.Join(featuresDir, "F001-test.yaml"), []byte("id: F001-test\nname: test\nstatus: in-progress\n"), 0o644)

	h := &Handlers{WorkspaceRoot: dir}
	input := StopInput{FeatureID: "F001-test"}
	result, err := h.Stop(context.Background(), input)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	var parsed struct {
		FeatureID string `json:"featureId"`
		Stopped   bool   `json:"stopped"`
	}
	json.Unmarshal(result, &parsed)
	if !parsed.Stopped {
		t.Error("stopped should be true")
	}
	if parsed.FeatureID != "F001-test" {
		t.Errorf("featureId = %q, want F001-test", parsed.FeatureID)
	}

	// F082：Stop 改為寫 stop signal 檔，不直接改寫 state.json，避免與 run loop 競寫。
	// 確認 state.json 維持原樣（仍 active），由 run loop 後續消費 signal 才收斂。
	data, _ := os.ReadFile(filepath.Join(featureDir, "state.json"))
	var s protocol.State
	json.Unmarshal(data, &s)
	if !s.Active {
		t.Error("state.json should be left untouched by Stop (run loop is the sole writer)")
	}

	// 確認 stop signal 檔被建立。
	ws := &protocol.Workspace{Root: dir}
	if !ws.StopRequested("F001-test") {
		t.Error("stop signal file should exist after Stop")
	}
}

// assertJSON 斷言 handler 回傳非 nil 且為合法 JSON。
func assertJSON(t *testing.T, res json.RawMessage, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if !json.Valid(res) {
		t.Fatalf("result is not valid JSON: %s", string(res))
	}
}

// --- mockExec keying：subcommand 可分別 mock，既有單段 key 不互撞 ---

func TestCmdKey(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"status", "--json"}, "status"},
		{[]string{"status", "F001", "--json"}, "status"},
		{[]string{"check", "F001", "--json"}, "check"},
		{[]string{"batch", "next", "--json"}, "batch next"},
		{[]string{"batch", "stop", "--json"}, "batch stop"},
		{[]string{"config", "get", "locale", "--json"}, "config get"},
		{[]string{"learn", "list", "--json"}, "learn list"},
	}
	for _, c := range cases {
		if got := cmdKey(c.args); got != c.want {
			t.Errorf("cmdKey(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestMockExec_SubcommandsDoNotCollide(t *testing.T) {
	exec := mockExec(map[string]string{
		"batch next": `{"featureId":"F001"}`,
		"batch stop": `{"stopped":true}`,
	})
	h := &Handlers{Exec: exec}

	next, err := h.BatchNext(context.Background(), struct{}{})
	assertJSON(t, next, err)
	stop, err := h.BatchStop(context.Background(), struct{}{})
	assertJSON(t, stop, err)
}

// --- mcp-core ---

func TestDoneTool(t *testing.T) {
	exec, got := captureExec(`{"featureId":"F001","phase":"done","merged":true,"conflict":false}`)
	h := &Handlers{Exec: exec}

	res, err := h.Done(context.Background(), DoneInput{FeatureID: "F001", ApproveSelfMod: true})
	assertJSON(t, res, err)
	if !argsContain(*got, "--approve-self-mod") {
		t.Errorf("expected --approve-self-mod in args, got %v", *got)
	}

	_, _ = h.Done(context.Background(), DoneInput{FeatureID: "F001"})
	if argsContain(*got, "--approve-self-mod") {
		t.Errorf("did not expect --approve-self-mod when ApproveSelfMod=false, got %v", *got)
	}
}

func TestVerifyTool(t *testing.T) {
	exec, got := captureExec(`{"passed":true}`)
	h := &Handlers{Exec: exec}

	res, err := h.Verify(context.Background(), VerifyInput{FeatureID: "F001", Round: 3})
	assertJSON(t, res, err)
	if !argsContain(*got, "--round") || !argsContain(*got, "3") {
		t.Errorf("expected --round 3 in args, got %v", *got)
	}

	_, _ = h.Verify(context.Background(), VerifyInput{FeatureID: "F001"})
	if argsContain(*got, "--round") {
		t.Errorf("did not expect --round when Round=0, got %v", *got)
	}
}

func TestApproveTool(t *testing.T) {
	exec, got := captureExec(`{"featureId":"F001","status":"not-started"}`)
	h := &Handlers{Exec: exec}
	res, err := h.Approve(context.Background(), ApproveInput{FeatureID: "F001"})
	assertJSON(t, res, err)
	if !argsContain(*got, "approve") || !argsContain(*got, "F001") {
		t.Errorf("unexpected args: %v", *got)
	}
}

func TestRejectTool(t *testing.T) {
	exec, got := captureExec(`{"featureId":"F001","status":"abandoned"}`)
	h := &Handlers{Exec: exec}
	res, err := h.Reject(context.Background(), RejectInput{FeatureID: "F001"})
	assertJSON(t, res, err)
	if !argsContain(*got, "reject") {
		t.Errorf("unexpected args: %v", *got)
	}
}

func TestMineTool(t *testing.T) {
	exec, got := captureExec(`{"candidates":2,"output":".4x/candidates.json","dryRun":true}`)
	h := &Handlers{Exec: exec}

	res, err := h.Mine(context.Background(), MineInput{MinOccurrences: 3, Output: "out.json", DryRun: true})
	assertJSON(t, res, err)
	if !argsContain(*got, "--min-occurrences") || !argsContain(*got, "3") {
		t.Errorf("expected --min-occurrences 3, got %v", *got)
	}
	if !argsContain(*got, "--output") || !argsContain(*got, "out.json") {
		t.Errorf("expected --output out.json, got %v", *got)
	}
	if !argsContain(*got, "--dry-run") {
		t.Errorf("expected --dry-run, got %v", *got)
	}

	_, _ = h.Mine(context.Background(), MineInput{})
	if argsContain(*got, "--dry-run") || argsContain(*got, "--min-occurrences") || argsContain(*got, "--output") {
		t.Errorf("did not expect optional flags for empty input, got %v", *got)
	}
}

func TestSubtaskTool(t *testing.T) {
	exec, got := captureExec(`{"featureId":"F001","subtaskId":"st1","status":"done"}`)
	h := &Handlers{Exec: exec}
	res, err := h.Subtask(context.Background(), SubtaskInput{FeatureID: "F001", SubtaskID: "st1", Status: "done"})
	assertJSON(t, res, err)
	if !argsContain(*got, "--status") || !argsContain(*got, "done") || !argsContain(*got, "st1") {
		t.Errorf("unexpected args: %v", *got)
	}
}

// --- mcp-mgmt ---

func TestBatchNextTool(t *testing.T) {
	exec, got := captureExec(`{"featureId":"F002"}`)
	h := &Handlers{Exec: exec}
	res, err := h.BatchNext(context.Background(), struct{}{})
	assertJSON(t, res, err)
	if cmdKey(*got) != "batch next" {
		t.Errorf("expected 'batch next' command, got %v", *got)
	}
}

func TestBatchStopTool(t *testing.T) {
	exec, got := captureExec(`{"stopped":true}`)
	h := &Handlers{Exec: exec}
	res, err := h.BatchStop(context.Background(), struct{}{})
	assertJSON(t, res, err)
	if cmdKey(*got) != "batch stop" {
		t.Errorf("expected 'batch stop' command, got %v", *got)
	}
}

func TestCleanTool(t *testing.T) {
	exec, got := captureExec(`{"cleaned":["F001"],"freedBytes":100,"dryRun":false,"candidates":[]}`)
	h := &Handlers{Exec: exec}

	res, err := h.Clean(context.Background(), CleanInput{FeatureID: "F001", Force: true})
	assertJSON(t, res, err)
	if !argsContain(*got, "F001") || !argsContain(*got, "--force") {
		t.Errorf("expected feature id and --force, got %v", *got)
	}

	_, _ = h.Clean(context.Background(), CleanInput{DryRun: true})
	if !argsContain(*got, "--dry-run") {
		t.Errorf("expected --dry-run, got %v", *got)
	}
	if argsContain(*got, "--force") {
		t.Errorf("did not expect --force, got %v", *got)
	}
}

func TestDoctorTool(t *testing.T) {
	exec := mockExec(map[string]string{"doctor": `{"ok":true}`})
	h := &Handlers{Exec: exec}
	res, err := h.Doctor(context.Background(), struct{}{})
	assertJSON(t, res, err)
}

func TestLearnListTool(t *testing.T) {
	exec, got := captureExec(`{"entries":[],"active":0,"total":0}`)
	h := &Handlers{Exec: exec}

	res, err := h.LearnList(context.Background(), LearnListInput{Category: "design"})
	assertJSON(t, res, err)
	if !argsContain(*got, "--category") || !argsContain(*got, "design") {
		t.Errorf("expected --category design, got %v", *got)
	}

	_, _ = h.LearnList(context.Background(), LearnListInput{})
	if argsContain(*got, "--category") {
		t.Errorf("did not expect --category for empty input, got %v", *got)
	}
}

func TestLearnPruneTool(t *testing.T) {
	exec, got := captureExec(`{"removed":0,"dryRun":true,"staleIds":[]}`)
	h := &Handlers{Exec: exec}

	res, err := h.LearnPrune(context.Background(), LearnPruneInput{DryRun: true})
	assertJSON(t, res, err)
	if !argsContain(*got, "--dry-run") {
		t.Errorf("expected --dry-run, got %v", *got)
	}

	_, _ = h.LearnPrune(context.Background(), LearnPruneInput{})
	if argsContain(*got, "--dry-run") {
		t.Errorf("did not expect --dry-run, got %v", *got)
	}
}

func TestLearnPromoteTool(t *testing.T) {
	exec, got := captureExec(`{"id":"L001","promoted":true}`)
	h := &Handlers{Exec: exec}
	res, err := h.LearnPromote(context.Background(), LearnPromoteInput{ID: "L001"})
	assertJSON(t, res, err)
	if cmdKey(*got) != "learn promote" || !argsContain(*got, "L001") {
		t.Errorf("unexpected args: %v", *got)
	}
}

func TestLearnRemoveTool(t *testing.T) {
	exec, got := captureExec(`{"id":"L001","removed":true}`)
	h := &Handlers{Exec: exec}
	res, err := h.LearnRemove(context.Background(), LearnRemoveInput{ID: "L001"})
	assertJSON(t, res, err)
	if cmdKey(*got) != "learn remove" || !argsContain(*got, "L001") {
		t.Errorf("unexpected args: %v", *got)
	}
}

func TestEvolveTool(t *testing.T) {
	exec, got := captureExec(`{"dryRun":true,"scanned":0,"new":0,"wouldEnqueue":0}`)
	h := &Handlers{Exec: exec}

	res, err := h.Evolve(context.Background(), EvolveInput{
		DryRun: true, MinOccurrences: 2, Runner: "claude", AutoRun: true, Force: true, MaxRounds: 3, Timeout: 60,
	})
	assertJSON(t, res, err)
	for _, want := range []string{"--dry-run", "--min-occurrences", "2", "--runner", "claude", "--auto-run", "--force", "--max-rounds", "3", "--timeout", "60"} {
		if !argsContain(*got, want) {
			t.Errorf("expected %q in args, got %v", want, *got)
		}
	}

	_, _ = h.Evolve(context.Background(), EvolveInput{})
	for _, unwant := range []string{"--dry-run", "--auto-run", "--force", "--runner", "--min-occurrences"} {
		if argsContain(*got, unwant) {
			t.Errorf("did not expect %q for empty input, got %v", unwant, *got)
		}
	}
}

// --- mcp-ops ---

func TestConfigGetTool(t *testing.T) {
	exec, got := captureExec(`{"key":"locale","value":"en"}`)
	h := &Handlers{Exec: exec}
	res, err := h.ConfigGet(context.Background(), ConfigGetInput{Key: "locale"})
	assertJSON(t, res, err)
	if cmdKey(*got) != "config get" || !argsContain(*got, "locale") {
		t.Errorf("unexpected args: %v", *got)
	}
}

func TestConfigSetTool(t *testing.T) {
	exec, got := captureExec(`{"key":"locale","value":"zh-TW","path":"/x"}`)
	h := &Handlers{Exec: exec}
	res, err := h.ConfigSet(context.Background(), ConfigSetInput{Key: "locale", Value: "zh-TW"})
	assertJSON(t, res, err)
	if cmdKey(*got) != "config set" || !argsContain(*got, "zh-TW") {
		t.Errorf("unexpected args: %v", *got)
	}
}

func TestConfigListTool(t *testing.T) {
	exec := mockExec(map[string]string{"config list": `{"locale":"en"}`})
	h := &Handlers{Exec: exec}
	res, err := h.ConfigList(context.Background(), struct{}{})
	assertJSON(t, res, err)
}

func TestGateTool_PrePost(t *testing.T) {
	execPre, gotPre := captureExec(`{"phase":"pre","kept":1,"dropped":0}`)
	hPre := &Handlers{Exec: execPre}
	res, err := hPre.Gate(context.Background(), GateInput{Phase: "pre"})
	assertJSON(t, res, err)
	if !argsContain(*gotPre, "--pre") {
		t.Errorf("expected --pre, got %v", *gotPre)
	}

	execPost, gotPost := captureExec(`{"phase":"post","accepted":1,"rejected":[]}`)
	hPost := &Handlers{Exec: execPost}
	res, err = hPost.Gate(context.Background(), GateInput{Phase: "post"})
	assertJSON(t, res, err)
	if !argsContain(*gotPost, "--post") {
		t.Errorf("expected --post, got %v", *gotPost)
	}
}

func TestGateTool_InvalidPhase(t *testing.T) {
	called := false
	exec := func(ctx context.Context, args ...string) (json.RawMessage, error) {
		called = true
		return json.RawMessage(`{}`), nil
	}
	h := &Handlers{Exec: exec}
	_, err := h.Gate(context.Background(), GateInput{Phase: "bogus"})
	if err == nil {
		t.Fatal("expected error for invalid phase")
	}
	if called {
		t.Error("Exec should not be called for invalid phase")
	}
}

func TestMergeTool(t *testing.T) {
	exec, got := captureExec(`{"featureId":"F001","merged":true,"conflict":false}`)
	h := &Handlers{Exec: exec}

	res, err := h.Merge(context.Background(), MergeInput{FeatureID: "F001", ApproveSelfMod: true})
	assertJSON(t, res, err)
	if !argsContain(*got, "--approve-self-mod") {
		t.Errorf("expected --approve-self-mod, got %v", *got)
	}

	_, _ = h.Merge(context.Background(), MergeInput{FeatureID: "F001"})
	if argsContain(*got, "--approve-self-mod") {
		t.Errorf("did not expect --approve-self-mod, got %v", *got)
	}
}
