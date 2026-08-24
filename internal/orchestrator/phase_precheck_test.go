package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

// setupDesigningPrecheck 準備一個剛跑完 designing 的 feature：必備 artifact 齊全，
// test-strategy.yaml 內容由 strategy 決定。
func setupDesigningPrecheck(t *testing.T, featureID, strategy string) *protocol.Workspace {
	t.Helper()
	ws := setupPhaseWorkspace(t, featureID)
	featureDir := ws.FeatureDir(featureID)
	writePhaseFile(t, filepath.Join(featureDir, protocol.TaskBrief), "# Brief")
	writePhaseFile(t, filepath.Join(featureDir, protocol.Criteria), "# Criteria")
	writePhaseFile(t, filepath.Join(featureDir, protocol.TestStratFile), strategy)
	return ws
}

// badStrategy 的 verify 命令用 pipe 吞掉 go test 的 exit code，必被 precheck 攔下。
const badStrategy = "verify_commands:\n  - \"go test ./... 2>&1 | grep -q PASS\"\n"

// TestNextPhaseDesigningPrecheckPass 合法 test-strategy.yaml 不影響既有轉場。（AC-12a）
func TestNextPhaseDesigningPrecheckPass(t *testing.T) {
	ws := setupDesigningPrecheck(t, "F188-ok", "verify_commands:\n  - make test\n")

	next, role, reason := NextPhaseAfter(ws, "F188-ok", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})
	if next != protocol.PhaseDesignReviewing || role != protocol.RoleDesignReviewer || reason != "" {
		t.Fatalf("got (%s, %s, %q), want (%s, %s, \"\")", next, role, reason, protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer)
	}
}

// TestNextPhaseDesigningPrecheckRetry precheck 失敗且尚有重試額度 → 退回 designing 並寫 guard-feedback。（AC-12b）
func TestNextPhaseDesigningPrecheckRetry(t *testing.T) {
	ws := setupDesigningPrecheck(t, "F188-retry", badStrategy)

	next, role, reason := NextPhaseAfter(ws, "F188-retry", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})
	if next != protocol.PhaseDesigning || role != protocol.RoleDesigner || reason != "" {
		t.Fatalf("got (%s, %s, %q), want (%s, %s, \"\")", next, role, reason, protocol.PhaseDesigning, protocol.RoleDesigner)
	}

	data, err := os.ReadFile(filepath.Join(ws.RoundDir("F188-retry", 1), protocol.GuardFeedback))
	if err != nil {
		t.Fatalf("guard-feedback.json must exist: %v", err)
	}
	var fb struct {
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(data, &fb); err != nil {
		t.Fatalf("guard-feedback.json unmarshal: %v", err)
	}
	if len(fb.Errors) == 0 {
		t.Fatal("guard-feedback.json errors must be non-empty")
	}

	// 回傳 tuple 必須真的被狀態機接受。只比對 tuple 不足以證明重試路徑存在——
	// designing 少了 self-edge 時，orchestrator 會在 state.Transition 就 abort。
	after, terr := state.Transition(protocol.State{Phase: protocol.PhaseDesigning, Role: protocol.RoleDesigner, Round: 1}, next, role)
	if terr != nil {
		t.Fatalf("state.Transition(designing → %s) = %v, want nil", next, terr)
	}
	if after.Phase != protocol.PhaseDesigning || after.Role != protocol.RoleDesigner || after.Round != 1 {
		t.Fatalf("after transition: (%s, %s, round %d), want (designing, designer, round 1)", after.Phase, after.Role, after.Round)
	}
	if !IsDesigningGuardRetry(protocol.PhaseDesigning, next) {
		t.Error("IsDesigningGuardRetry = false; orchestrator would count the retry as a designer escalation")
	}
}

// TestNextPhaseDesigningPrecheckExhausted 重試額度用盡（已有 guard-feedback，或 GuardRetries 達上限）
// → needs-attention 並把 finding 訊息串進 stop reason。（AC-12c、AC-12d）
func TestNextPhaseDesigningPrecheckExhausted(t *testing.T) {
	t.Run("guard-feedback-exists", func(t *testing.T) {
		ws := setupDesigningPrecheck(t, "F188-exh-a", badStrategy)
		writePhaseFile(t, filepath.Join(ws.RoundDir("F188-exh-a", 1), protocol.GuardFeedback), `{"errors":["old"]}`)

		next, _, reason := NextPhaseAfter(ws, "F188-exh-a", protocol.State{Phase: protocol.PhaseDesigning, Round: 1})
		if next != protocol.PhaseNeedsAttention {
			t.Fatalf("next = %s, want %s", next, protocol.PhaseNeedsAttention)
		}
		if reason == "" {
			t.Fatal("stop reason must carry the precheck findings")
		}
	})

	t.Run("global-retry-cap", func(t *testing.T) {
		ws := setupDesigningPrecheck(t, "F188-exh-b", badStrategy)
		s := protocol.State{Phase: protocol.PhaseDesigning, Round: 1, GuardRetries: state.MaxGuardRetries}

		next, _, reason := NextPhaseAfter(ws, "F188-exh-b", s)
		if next != protocol.PhaseNeedsAttention {
			t.Fatalf("next = %s, want %s", next, protocol.PhaseNeedsAttention)
		}
		if reason == "" {
			t.Fatal("stop reason must carry the precheck findings")
		}
	})
}

// TestIsDesigningGuardRetry 固定閘門重試與 designer escalation 的區分：只有 designing → designing
// 算 guard retry，coder / tester / design-reviewer 打回 designing 一律不算。
func TestIsDesigningGuardRetry(t *testing.T) {
	tests := []struct {
		from, to protocol.Phase
		want     bool
	}{
		{protocol.PhaseDesigning, protocol.PhaseDesigning, true},
		{protocol.PhaseDesignReviewing, protocol.PhaseDesigning, false},
		{protocol.PhaseCoding, protocol.PhaseDesigning, false},
		{protocol.PhaseTesting, protocol.PhaseDesigning, false},
		{protocol.PhaseDesigning, protocol.PhaseDesignReviewing, false},
	}
	for _, tt := range tests {
		if got := IsDesigningGuardRetry(tt.from, tt.to); got != tt.want {
			t.Errorf("IsDesigningGuardRetry(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

// TestNextPhaseDesigningPrecheckClearsGuardFeedback 閘門重試後又通過時，必須清掉自己寫的
// guard-feedback.json。designing 與 testing 共用同一個 round 級檔案，殘留會污染同一輪的 Tester
// prompt，並讓 tester 的 guard retry 因 guardFeedbackExists 為 true 而失效。
func TestNextPhaseDesigningPrecheckClearsGuardFeedback(t *testing.T) {
	ws := setupDesigningPrecheck(t, "F188-clear", badStrategy)

	if next, _, _ := NextPhaseAfter(ws, "F188-clear", protocol.State{Phase: protocol.PhaseDesigning, Round: 1}); next != protocol.PhaseDesigning {
		t.Fatalf("first pass: next = %s, want %s", next, protocol.PhaseDesigning)
	}
	if !guardFeedbackExists(ws, "F188-clear", 1) {
		t.Fatal("first pass must write guard-feedback.json")
	}

	writePhaseFile(t, filepath.Join(ws.FeatureDir("F188-clear"), protocol.TestStratFile), "verify_commands:\n  - make test\n")
	next, _, _ := NextPhaseAfter(ws, "F188-clear", protocol.State{Phase: protocol.PhaseDesigning, Round: 1, GuardRetries: 1})
	if next != protocol.PhaseDesignReviewing {
		t.Fatalf("second pass: next = %s, want %s", next, protocol.PhaseDesignReviewing)
	}
	if guardFeedbackExists(ws, "F188-clear", 1) {
		t.Error("guard-feedback.json must be removed once the designing gate passes")
	}
}
