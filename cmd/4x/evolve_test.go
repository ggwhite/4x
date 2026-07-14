package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/evolution"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// fakeGateRunner 模擬 gate role：讀 .4x/gate-input.json，對每個 candidate 寫出 accept verdict
// 到 .4x/gate-verdicts.json，不打真 LLM。
type fakeGateRunner struct {
	dot        string
	valueScore float64
	whyNotHack string
	called     *bool
}

func (f *fakeGateRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	if f.called != nil {
		*f.called = true
	}
	pool, err := protocol.LoadCandidates(filepath.Join(f.dot, protocol.GateInputFile))
	if err != nil {
		return nil, err
	}
	type verdict struct {
		Title      string  `json:"title"`
		Verdict    string  `json:"verdict"`
		ValueScore float64 `json:"value_score"`
		WhyNotHack string  `json:"why_not_hack"`
	}
	out := struct {
		Verdicts []verdict `json:"verdicts"`
	}{}
	for _, c := range pool.Candidates {
		out.Verdicts = append(out.Verdicts, verdict{
			Title: c.Title, Verdict: "accept", ValueScore: f.valueScore, WhyNotHack: f.whyNotHack,
		})
	}
	data, _ := json.Marshal(out)
	if err := os.WriteFile(filepath.Join(f.dot, protocol.GateVerdictsFile), data, 0o644); err != nil {
		return nil, err
	}
	tmp, _ := os.CreateTemp("", "gate-*.log")
	tmp.Close()
	return &runner.Result{ExitCode: 0, LogFile: tmp.Name()}, nil
}

// setupEvolveWorkspace 建一個含 1 筆可被 mine 的 escalation 的 workspace。
func setupEvolveWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "evolve-test"}}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	seed := feat.Feature{ID: "F001-seed", Name: "F001: Seed", Status: feat.StatusInProgress}
	if err := ws.SaveFeature(seed); err != nil {
		t.Fatal(err)
	}
	dir := ws.RoundDir(seed.ID, 1)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	esc := protocol.Escalation{Needed: true, Reason: "blocker", Detail: "db pool exhausted under sustained load"}
	data, _ := json.Marshal(esc)
	if err := os.WriteFile(filepath.Join(dir, protocol.EscalationFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// os.Pipe() 預設緩衝區僅 64KB；fn() 若輸出超過此上限（如 dryRunLoop 印出多個角色的完整
	// prompt），同步寫入會在 fn() 內部塞滿緩衝區並卡住等待 reader，但 reader 原本要等 fn()
	// 返回才啟動，形成死結直到測試逾時。改為並行讀取以邊寫邊清緩衝區，避免此類死結。
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

func dotFile(ws *protocol.Workspace, name string) string {
	return filepath.Join(ws.DotDir(), name)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// AC-1：四個 flag 與命令說明存在。
func TestEvolveCmd_Flags(t *testing.T) {
	cmd := newEvolveCmd()
	if cmd.Use != "evolve" {
		t.Errorf("Use = %q, want evolve", cmd.Use)
	}
	for _, name := range []string{"auto-run", "dry-run", "min-occurrences", "force"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

// AC-2：dry-run 不寫任何 .4x 檔、不 spawn runner，stdout 印摘要。
func TestEvolve_DryRun_NoMutation(t *testing.T) {
	ws := setupEvolveWorkspace(t)
	opts := evolveOpts{dryRun: true, minOccurrences: 3}

	out := captureStdout(t, func() {
		if err := runEvolve(context.Background(), ws, protocol.Config{}, opts, evolveDeps{}); err != nil {
			t.Fatalf("dry-run: %v", err)
		}
	})

	for _, name := range []string{
		protocol.CandidatesFile, protocol.GateInputFile, protocol.AcceptedCandidatesFile,
		protocol.EvolveReportFile, protocol.EvolveStateFile,
	} {
		if fileExists(dotFile(ws, name)) {
			t.Errorf("dry-run must not write %s", name)
		}
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("stdout missing dry-run summary: %q", out)
	}
}

// AC-3/AC-4：pipeline 串接，gate verdicts → accepted → enqueue not-started feature。
func TestEvolve_Pipeline_EnqueuesNotStarted(t *testing.T) {
	ws := setupEvolveWorkspace(t)
	deps := evolveDeps{
		gateRunner:   &fakeGateRunner{dot: ws.DotDir(), valueScore: 0.9, whyNotHack: "genuinely valuable, recurring blocker"},
		enrichRunner: &mockEnrichRunner{logContent: validEnrichLogForTest},
	}

	_ = captureStdout(t, func() {
		if err := runEvolve(context.Background(), ws, protocol.Config{}, evolveOpts{minOccurrences: 3}, deps); err != nil {
			t.Fatalf("evolve: %v", err)
		}
	})

	if !fileExists(dotFile(ws, protocol.GateVerdictsFile)) {
		t.Error("gate-verdicts.json not produced")
	}
	if !fileExists(dotFile(ws, protocol.AcceptedCandidatesFile)) {
		t.Error("accepted-candidates.json not produced")
	}
	created := discoveredOther(t, ws, "F001-seed")
	if created == nil {
		t.Fatal("expected an enqueued feature")
	}
	if created.Status != feat.StatusNotStarted {
		t.Errorf("status = %q, want not-started", created.Status)
	}
	if len(created.Subtasks) < 2 {
		t.Errorf("enriched feature should have subtasks, got %d", len(created.Subtasks))
	}
}

// AC-5：enrich discarded 時仍以 bare feature 排入，report 標 enriched=false。
func TestEvolve_EnrichDiscarded_BareFallback(t *testing.T) {
	ws := setupEvolveWorkspace(t)
	deps := evolveDeps{
		gateRunner:   &fakeGateRunner{dot: ws.DotDir(), valueScore: 0.9, whyNotHack: "valuable"},
		enrichRunner: &mockEnrichRunner{logContent: "[ENRICHMENT-RESULT]\n{\"subtasks\":[],\"priority\":0}\n[/ENRICHMENT-RESULT]"},
	}

	_ = captureStdout(t, func() {
		if err := runEvolve(context.Background(), ws, protocol.Config{}, evolveOpts{minOccurrences: 3}, deps); err != nil {
			t.Fatalf("evolve: %v", err)
		}
	})

	created := discoveredOther(t, ws, "F001-seed")
	if created == nil {
		t.Fatal("expected feature even when enrich discarded (bare fallback)")
	}
	if created.Status != feat.StatusNotStarted {
		t.Errorf("status = %q, want not-started", created.Status)
	}
	report, _ := os.ReadFile(dotFile(ws, protocol.EvolveReportFile))
	if !strings.Contains(string(report), "enriched=false") {
		t.Errorf("report should mark enriched=false:\n%s", report)
	}
}

// AC-7：不給 --auto-run 時，runFeature 不被呼叫，feature 留在 not-started。
func TestEvolve_NoAutoRun_DoesNotInvokeRunLoop(t *testing.T) {
	ws := setupEvolveWorkspace(t)
	ranFeature := false
	deps := evolveDeps{
		gateRunner: &fakeGateRunner{dot: ws.DotDir(), valueScore: 0.9, whyNotHack: "valuable"},
		runFeature: func(context.Context, string) (protocol.State, error) {
			ranFeature = true
			return protocol.State{}, nil
		},
	}

	_ = captureStdout(t, func() {
		if err := runEvolve(context.Background(), ws, protocol.Config{}, evolveOpts{minOccurrences: 3}, deps); err != nil {
			t.Fatalf("evolve: %v", err)
		}
	})

	if ranFeature {
		t.Error("runFeature must not be called without --auto-run")
	}
	created := discoveredOther(t, ws, "F001-seed")
	if created == nil || created.Status != feat.StatusNotStarted {
		t.Errorf("feature should be not-started, got %+v", created)
	}
}

// AC-6：--auto-run 觸及 protected path 時 report 標 SelfModBlocked，且不自動完成。
func TestEvolve_AutoRun_SelfModBlocked(t *testing.T) {
	ws := setupEvolveWorkspace(t)
	deps := evolveDeps{
		gateRunner: &fakeGateRunner{dot: ws.DotDir(), valueScore: 0.9, whyNotHack: "valuable"},
		runFeature: func(_ context.Context, id string) (protocol.State, error) {
			// 模擬一個改到受保護路徑且未核准的 feature。
			return protocol.State{
				FeatureID:       id,
				Phase:           protocol.PhasePendingReview,
				SelfModTouched:  true,
				SelfModPaths:    []string{"internal/state/machine.go"},
				SelfModApproved: false,
			}, nil
		},
	}

	_ = captureStdout(t, func() {
		if err := runEvolve(context.Background(), ws, protocol.Config{}, evolveOpts{minOccurrences: 3, autoRun: true}, deps); err != nil {
			t.Fatalf("evolve: %v", err)
		}
	})

	report, _ := os.ReadFile(dotFile(ws, protocol.EvolveReportFile))
	if !strings.Contains(string(report), "self-mod blocked=true") {
		t.Errorf("report should mark self-mod blocked:\n%s", report)
	}
	// evolve 不 merge/done，feature 自然不會是 done。
	created := discoveredOther(t, ws, "F001-seed")
	if created != nil && created.Status == feat.StatusDone {
		t.Error("self-mod blocked feature must not be auto-completed to done")
	}
}

// AC-8：accepted 為空時 ConsecutiveNoAccept 遞增。
func TestEvolve_NoAccept_CounterIncrements(t *testing.T) {
	ws := setupEvolveWorkspace(t)
	// gate 全 reject（value_score 低於 floor）。
	deps := evolveDeps{
		gateRunner: &fakeGateRunner{dot: ws.DotDir(), valueScore: 0.1, whyNotHack: "weak"},
	}

	for round := 1; round <= 2; round++ {
		_ = captureStdout(t, func() {
			if err := runEvolve(context.Background(), ws, protocol.Config{}, evolveOpts{minOccurrences: 3}, deps); err != nil {
				t.Fatalf("evolve round %d: %v", round, err)
			}
		})
		st, err := evolution.LoadEvolveState(dotFile(ws, protocol.EvolveStateFile))
		if err != nil {
			t.Fatal(err)
		}
		if st.ConsecutiveNoAccept != round {
			t.Errorf("after round %d ConsecutiveNoAccept = %d, want %d", round, st.ConsecutiveNoAccept, round)
		}
	}
}

// AC-9：達 halt 門檻且未 --force → 早退標 Halted、不 mine；--force → 照常跑。
func TestEvolve_Halt_AndForce(t *testing.T) {
	ws := setupEvolveWorkspace(t)
	// 預置 state 使 counter 達門檻（預設 max_idle_rounds=3）。
	pre := evolution.EvolveState{Version: 1, Round: 5, ConsecutiveNoAccept: 3}
	if err := pre.Save(dotFile(ws, protocol.EvolveStateFile)); err != nil {
		t.Fatal(err)
	}

	// 未給 force：halt。
	called := false
	deps := evolveDeps{gateRunner: &fakeGateRunner{dot: ws.DotDir(), valueScore: 0.9, whyNotHack: "v", called: &called}}
	_ = captureStdout(t, func() {
		if err := runEvolve(context.Background(), ws, protocol.Config{}, evolveOpts{minOccurrences: 3}, deps); err != nil {
			t.Fatalf("halt run: %v", err)
		}
	})
	if called {
		t.Error("halted run must not spawn gate runner")
	}
	report, _ := os.ReadFile(dotFile(ws, protocol.EvolveReportFile))
	if !strings.Contains(string(report), "## Halted") || !strings.Contains(string(report), "true") {
		t.Errorf("report should mark Halted true:\n%s", report)
	}
	if fileExists(dotFile(ws, protocol.CandidatesFile)) {
		t.Error("halted run must not mine (no candidates.json)")
	}

	// 給 force：照常跑，gate runner 被呼叫。
	called = false
	_ = captureStdout(t, func() {
		if err := runEvolve(context.Background(), ws, protocol.Config{}, evolveOpts{minOccurrences: 3, force: true}, deps); err != nil {
			t.Fatalf("force run: %v", err)
		}
	})
	if !called {
		t.Error("--force run should spawn gate runner")
	}
}
