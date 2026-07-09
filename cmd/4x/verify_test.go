package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func setupVerifyWorkspace(t *testing.T, cfg protocol.Config, featureID string) string {
	t.Helper()
	dir := t.TempDir()
	if err := protocol.Init(dir, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: dir}
	if err := ws.SaveFeature(feature.Feature{
		ID:     featureID,
		Name:   featureID,
		Status: feature.StatusInProgress,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState(featureID, protocol.State{
		FeatureID: featureID,
		Phase:     protocol.PhaseTesting,
		Role:      protocol.RoleTester,
		Round:     1,
		Active:    false,
		Runner:    "mock",
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestVerifyFallback_NoTestStrategy(t *testing.T) {
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
			Test:  []string{"echo test-ok"},
			Lint:  []string{"echo lint-ok"},
		},
		Default: "claude",
	}
	featureID := "F001-fallback"
	dir := setupVerifyWorkspace(t, cfg, featureID)

	out, err := run4x(dir, "verify", featureID, "--json")
	if err != nil {
		t.Fatalf("verify failed: %v\n%s", err, out)
	}

	ws := &protocol.Workspace{Root: dir}
	verifyPath := filepath.Join(ws.RoundDir(featureID, 1), protocol.VerifyFile)
	data, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatalf("verify.json not created: %v", err)
	}

	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("parse verify.json: %v", err)
	}
	if !ev.Passed {
		t.Error("expected verify to pass")
	}
	if len(ev.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(ev.Commands))
	}
	for _, cmd := range ev.Commands {
		if cmd.Group != "fallback" {
			t.Errorf("command %q: group = %q, want fallback", cmd.Command, cmd.Group)
		}
	}
}

func TestVerifyFallback_ACResults(t *testing.T) {
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
			Test:  []string{"echo test-ok"},
		},
		Default: "claude",
	}
	featureID := "F001-ac-results"
	dir := setupVerifyWorkspace(t, cfg, featureID)

	out, err := run4x(dir, "verify", featureID, "--json")
	if err != nil {
		t.Fatalf("verify failed: %v\n%s", err, out)
	}

	ws := &protocol.Workspace{Root: dir}
	verifyPath := filepath.Join(ws.RoundDir(featureID, 1), protocol.VerifyFile)
	data, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatalf("verify.json not created: %v", err)
	}

	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("parse verify.json: %v", err)
	}
	if len(ev.ACResults) != 2 {
		t.Fatalf("expected 2 ac_results, got %d", len(ev.ACResults))
	}
	for _, ac := range ev.ACResults {
		if ac.ID[:7] != "verify:" {
			t.Errorf("ac_results ID %q does not start with 'verify:'", ac.ID)
		}
		if !ac.Passed {
			t.Errorf("ac_results %q should be passed", ac.ID)
		}
		if len(ac.Evidence) != 1 {
			t.Errorf("ac_results %q: expected 1 evidence, got %d", ac.ID, len(ac.Evidence))
		}
	}
}

func TestVerifyFallback_WithTestStrategy(t *testing.T) {
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
			Test:  []string{"echo test-ok"},
			Lint:  []string{"echo lint-ok"},
		},
		Default: "claude",
	}
	featureID := "F001-with-strategy"
	dir := setupVerifyWorkspace(t, cfg, featureID)

	ws := &protocol.Workspace{Root: dir}
	tsPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	tsContent := []byte("verify_commands:\n  - echo strategy-cmd\n")
	if err := os.WriteFile(tsPath, tsContent, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run4x(dir, "verify", featureID, "--json")
	if err != nil {
		t.Fatalf("verify failed: %v\n%s", err, out)
	}

	verifyPath := filepath.Join(ws.RoundDir(featureID, 1), protocol.VerifyFile)
	data, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatalf("verify.json not created: %v", err)
	}

	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("parse verify.json: %v", err)
	}
	if len(ev.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(ev.Commands))
	}
	if ev.Commands[0].Command != "echo strategy-cmd" {
		t.Errorf("expected 'echo strategy-cmd', got %q", ev.Commands[0].Command)
	}
	if ev.Commands[0].Group != "default" {
		t.Errorf("group = %q, want default (not fallback)", ev.Commands[0].Group)
	}
	if len(ev.ACResults) != 0 {
		t.Errorf("expected no ac_results on normal path, got %d", len(ev.ACResults))
	}
}

func TestVerifyCmd_Allowlist_EndToEnd(t *testing.T) {
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:                   "test",
			VerifyCommandAllowlist: []string{"make"},
		},
		Default: "claude",
	}
	featureID := "F162-allowlist-e2e"
	dir := setupVerifyWorkspace(t, cfg, featureID)
	ws := &protocol.Workspace{Root: dir}

	sentinel := filepath.Join(dir, "sentinel")
	tsPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	tsContent := "verify_commands:\n  - touch " + sentinel + "\n"
	if err := os.WriteFile(tsPath, []byte(tsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run4x(dir, "verify", featureID, "--json")
	if err == nil {
		t.Fatalf("verify should fail when command is blocked, got success\n%s", out)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("sentinel should not exist after blocked command, stat err=%v", err)
	}

	verifyPath := filepath.Join(ws.RoundDir(featureID, 1), protocol.VerifyFile)
	data, rerr := os.ReadFile(verifyPath)
	if rerr != nil {
		t.Fatalf("verify.json not created: %v", rerr)
	}
	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("parse verify.json: %v", err)
	}
	if ev.Passed {
		t.Fatal("verify.json passed should be false for blocked command")
	}
	if len(ev.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(ev.Commands))
	}
	got := ev.Commands[0]
	if got.ExitCode != 126 {
		t.Errorf("blocked ExitCode = %d, want 126", got.ExitCode)
	}
	if got.Error != "blocked" {
		t.Errorf("blocked Error = %q, want blocked", got.Error)
	}
	if got.Skipped {
		t.Error("blocked command should be a failure, not skipped")
	}
}

func TestVerifyFallback_ParseError(t *testing.T) {
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo ok"},
		},
		Default: "claude",
	}
	featureID := "F001-parse-error"
	dir := setupVerifyWorkspace(t, cfg, featureID)

	ws := &protocol.Workspace{Root: dir}
	tsPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	if err := os.WriteFile(tsPath, []byte("{{invalid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := run4x(dir, "verify", featureID, "--json")
	if err == nil {
		t.Error("expected error for invalid YAML test-strategy")
	}
}
