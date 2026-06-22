package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// setupMineWorkspace 建立一個含 escalation 與 stuck feature 的最小 fixture，回傳 root。
func setupMineWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "x"}, Default: "claude"}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	if err := ws.SaveFeature(feature.Feature{ID: "F001-a", Name: "Feature A", Status: feature.StatusInProgress}); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("F001-a"); err != nil {
		t.Fatal(err)
	}
	// 一筆 escalation。
	roundDir := ws.RoundDir("F001-a", 1)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	esc, _ := json.Marshal(protocol.Escalation{Needed: true, Reason: "blocker", Detail: "external dep down"})
	if err := os.WriteFile(filepath.Join(roundDir, protocol.EscalationFile), esc, 0o644); err != nil {
		t.Fatal(err)
	}
	// 一個 stuck feature。
	if err := ws.SaveFeature(feature.Feature{ID: "F002-b", Name: "Feature B", Status: feature.StatusInProgress}); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("F002-b"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F002-b", protocol.State{FeatureID: "F002-b", Phase: protocol.PhaseNeedsAttention, StopReason: "guard-failed"}); err != nil {
		t.Fatal(err)
	}
	return root
}

func runMine(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newMineCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("mine execute: %v\noutput:\n%s", err, buf.String())
	}
	return buf.String()
}

func TestMineCommand_WritesCandidates(t *testing.T) {
	root := setupMineWorkspace(t)
	t.Chdir(root)

	runMine(t)

	path := filepath.Join(root, protocol.DirName, protocol.CandidatesFile)
	pool, err := protocol.LoadCandidates(path)
	if err != nil {
		t.Fatalf("LoadCandidates: %v", err)
	}
	if pool.Version != 1 {
		t.Errorf("version = %d, want 1", pool.Version)
	}
	if pool.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should be set by mine command")
	}
	// 預期 1 筆 escalation + 1 筆 stuck。
	if len(pool.Candidates) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(pool.Candidates), pool.Candidates)
	}
	sources := map[protocol.CandidateSource]int{}
	for _, c := range pool.Candidates {
		sources[c.Source]++
	}
	if sources[protocol.SourceEscalation] != 1 || sources[protocol.SourceStuck] != 1 {
		t.Errorf("unexpected source distribution: %+v", sources)
	}
}

// TestMineCommand_RerunIsIdempotent 確認對未變動的歷史重跑 mine 時 candidate pool 維持穩定，
// 不會因為偶數次執行把既有 candidate 覆寫成空（deep-review CRITICAL）。
func TestMineCommand_RerunIsIdempotent(t *testing.T) {
	root := setupMineWorkspace(t)
	t.Chdir(root)

	path := filepath.Join(root, protocol.DirName, protocol.CandidatesFile)

	runMine(t)
	first, err := protocol.LoadCandidates(path)
	if err != nil {
		t.Fatalf("LoadCandidates after first run: %v", err)
	}
	if len(first.Candidates) != 2 {
		t.Fatalf("first run got %d candidates, want 2", len(first.Candidates))
	}

	// 第二、三次對相同歷史重跑：pool 必須與第一次相同，不可被清空或振盪。
	for i := 2; i <= 3; i++ {
		runMine(t)
		pool, err := protocol.LoadCandidates(path)
		if err != nil {
			t.Fatalf("LoadCandidates after run %d: %v", i, err)
		}
		if len(pool.Candidates) != len(first.Candidates) {
			t.Fatalf("run %d got %d candidates, want %d (pool not idempotent): %+v",
				i, len(pool.Candidates), len(first.Candidates), pool.Candidates)
		}
	}
}

func TestMineCommand_DryRunDoesNotWrite(t *testing.T) {
	root := setupMineWorkspace(t)
	t.Chdir(root)

	out := runMine(t, "--dry-run")

	path := filepath.Join(root, protocol.DirName, protocol.CandidatesFile)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("dry-run should not write candidates.json, but it exists (err=%v)", err)
	}
	if !bytes.Contains([]byte(out), []byte("Dry run")) {
		t.Errorf("dry-run output missing notice: %s", out)
	}
}

func TestMineCommand_CustomOutputAndMinOccurrences(t *testing.T) {
	root := setupMineWorkspace(t)
	t.Chdir(root)

	custom := filepath.Join(root, "out.json")
	runMine(t, "--output", custom, "--min-occurrences", "5")

	if _, err := os.Stat(custom); err != nil {
		t.Fatalf("custom output not written: %v", err)
	}
}

func TestMineCommand_FlagsRegistered(t *testing.T) {
	cmd := newMineCmd()
	for _, name := range []string{"min-occurrences", "output", "dry-run"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered", name)
		}
	}
	if got := cmd.Flags().Lookup("min-occurrences").DefValue; got != "3" {
		t.Errorf("min-occurrences default = %q, want 3", got)
	}
}
