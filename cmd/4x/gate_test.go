package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func setupGateWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "x"}, Default: "claude"}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGateCmd_Pre(t *testing.T) {
	root := setupGateWorkspace(t)
	dot := filepath.Join(root, protocol.DirName)
	pool := protocol.CandidatePool{Version: 1, Candidates: []protocol.Candidate{
		{Title: "Fresh idea", Description: "novel", Source: protocol.SourceStuck},
	}}
	if err := pool.Save(filepath.Join(dot, protocol.CandidatesFile)); err != nil {
		t.Fatal(err)
	}
	if err := runGate(root, gateOpts{pre: true}); err != nil {
		t.Fatalf("runGate pre: %v", err)
	}
	got, err := protocol.LoadCandidates(filepath.Join(dot, protocol.GateInputFile))
	if err != nil {
		t.Fatalf("load gate-input: %v", err)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Title != "Fresh idea" {
		t.Errorf("gate-input = %+v, want 1 fresh idea", got.Candidates)
	}
}

func TestGateCmd_Post(t *testing.T) {
	root := setupGateWorkspace(t)
	dot := filepath.Join(root, protocol.DirName)
	pool := protocol.CandidatePool{Version: 1, Candidates: []protocol.Candidate{
		{Title: "Fresh idea", Description: "novel", Source: protocol.SourceStuck},
	}}
	if err := pool.Save(filepath.Join(dot, protocol.GateInputFile)); err != nil {
		t.Fatal(err)
	}
	verdicts := `{"verdicts":[{"title":"Fresh idea","verdict":"accept","value_score":0.8,"why_not_hack":"real"}]}`
	if err := os.WriteFile(filepath.Join(dot, protocol.GateVerdictsFile), []byte(verdicts), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGate(root, gateOpts{post: true}); err != nil {
		t.Fatalf("runGate post: %v", err)
	}
	acc, err := protocol.LoadCandidates(filepath.Join(dot, protocol.AcceptedCandidatesFile))
	if err != nil {
		t.Fatalf("load accepted: %v", err)
	}
	if len(acc.Candidates) != 1 || acc.Candidates[0].ValueScore != 0.8 || acc.Candidates[0].WhyNotHack != "real" {
		t.Errorf("accepted = %+v", acc.Candidates)
	}
}

func TestGateCmd_FlagsMutuallyExclusive(t *testing.T) {
	root := setupGateWorkspace(t)
	if err := runGate(root, gateOpts{}); err == nil {
		t.Error("expected error when neither --pre nor --post given")
	}
	if err := runGate(root, gateOpts{pre: true, post: true}); err == nil {
		t.Error("expected error when both --pre and --post given")
	}
}
