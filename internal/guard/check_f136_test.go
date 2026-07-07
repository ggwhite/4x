package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// writeDocsGateConfig 寫入含 docs_check 的 settings.json。
func writeDocsGateConfig(t *testing.T, ws *protocol.Workspace, docsCheck []string) {
	t.Helper()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:      "test",
			DocsCheck: docsCheck,
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))
}

func setupDocsGateFeature(t *testing.T, ws *protocol.Workspace, featureID string, phase protocol.Phase) {
	t.Helper()
	writeState(t, ws, featureID, protocol.State{Phase: phase, Round: 1})
	dir := ws.FeatureDir(featureID)
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	if err := os.MkdirAll(ws.RoundDir(featureID, 1), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestCheckDocsGate_OptInSkip 未設定 docs_check 時完全跳過，不寫 artifact、不記 warn。
func TestCheckDocsGate_OptInSkip(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-dgskip")
	writeDocsGateConfig(t, ws, nil)
	setupDocsGateFeature(t, ws, "F999-dgskip", protocol.PhaseCoding)

	r := Check(ws, "F999-dgskip", nil)
	dgPath := filepath.Join(ws.RoundDir("F999-dgskip", 1), protocol.DocsGateFile)
	if _, err := os.Stat(dgPath); err == nil {
		t.Fatal("docs-gate.json should not be written when docs_check is unset")
	}
	for _, w := range r.Warns {
		if strings.Contains(w, "docs-gate") {
			t.Fatalf("expected no docs-gate warn when opt-out, got: %q", w)
		}
	}
}

// TestCheckDocsGate_SkipsNonCodingPhase 非 coding/amending phase 不執行 docs-gate。
func TestCheckDocsGate_SkipsNonCodingPhase(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-dgphase")
	writeDocsGateConfig(t, ws, []string{"echo docs-ok"})
	setupDocsGateFeature(t, ws, "F999-dgphase", protocol.PhaseReviewing)

	Check(ws, "F999-dgphase", nil)
	dgPath := filepath.Join(ws.RoundDir("F999-dgphase", 1), protocol.DocsGateFile)
	if _, err := os.Stat(dgPath); err == nil {
		t.Fatal("docs-gate.json should not exist for reviewing phase")
	}
}

// TestCheckDocsGate_CodingSuccess docs_check 全通過時寫出 passed=true 的 artifact。
func TestCheckDocsGate_CodingSuccess(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-dgpass")
	writeDocsGateConfig(t, ws, []string{"echo docs-ok", "echo i18n-ok"})
	setupDocsGateFeature(t, ws, "F999-dgpass", protocol.PhaseCoding)

	r := Check(ws, "F999-dgpass", nil)
	if !r.Pass {
		t.Fatalf("expected pass, got errors: %v", r.Errors)
	}
	dgPath := filepath.Join(ws.RoundDir("F999-dgpass", 1), protocol.DocsGateFile)
	data, err := os.ReadFile(dgPath)
	if err != nil {
		t.Fatalf("docs-gate.json not written: %v", err)
	}
	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("invalid docs-gate.json: %v", err)
	}
	if !ev.Passed {
		t.Fatal("expected docs-gate passed=true")
	}
	if ev.Role != protocol.RoleCoder {
		t.Fatalf("expected role coder, got %q", ev.Role)
	}
}

// TestCheckDocsGate_NonBlockingOnFail docs_check 失敗時：非阻塞（r.Pass 維持 true）、
// 只記 WARN、artifact 記 passed=false。這是 F136 的核心約束——不阻塞 coder 主要工作流。
func TestCheckDocsGate_NonBlockingOnFail(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-dgfail")
	// 第一個指令失敗、第二個成功，驗證兩者獨立執行（第二個不被 skip）。
	writeDocsGateConfig(t, ws, []string{"false", "echo i18n-ok"})
	setupDocsGateFeature(t, ws, "F999-dgfail", protocol.PhaseCoding)

	r := Check(ws, "F999-dgfail", nil)
	if !r.Pass {
		t.Fatalf("docs-gate must be non-blocking: expected r.Pass=true, got errors: %v", r.Errors)
	}
	found := false
	for _, w := range r.Warns {
		if strings.Contains(w, "docs-gate") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a docs-gate WARN on failure, got warns: %v", r.Warns)
	}

	dgPath := filepath.Join(ws.RoundDir("F999-dgfail", 1), protocol.DocsGateFile)
	data, err := os.ReadFile(dgPath)
	if err != nil {
		t.Fatalf("docs-gate.json not written: %v", err)
	}
	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("invalid docs-gate.json: %v", err)
	}
	if ev.Passed {
		t.Fatal("expected docs-gate passed=false")
	}
	// 每個指令各自成 group，第二個指令即使前一個失敗也應執行、不被 skip。
	for _, cmd := range ev.Commands {
		if cmd.Command == "echo i18n-ok" && cmd.Skipped {
			t.Fatal("independent docs command should not be skipped when another fails")
		}
	}
}

// TestCheckDocsGate_AmendingPhase amending phase 也執行 docs-gate。
func TestCheckDocsGate_AmendingPhase(t *testing.T) {
	ws := setupGuardWorkspace(t, "F999-dgamend")
	writeDocsGateConfig(t, ws, []string{"echo docs-ok"})
	setupDocsGateFeature(t, ws, "F999-dgamend", protocol.PhaseAmending)

	r := Check(ws, "F999-dgamend", nil)
	if !r.Pass {
		t.Fatalf("expected pass for amending phase, got errors: %v", r.Errors)
	}
	dgPath := filepath.Join(ws.RoundDir("F999-dgamend", 1), protocol.DocsGateFile)
	if _, err := os.Stat(dgPath); err != nil {
		t.Fatal("docs-gate.json should be written for amending phase")
	}
}
