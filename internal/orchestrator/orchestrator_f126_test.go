package orchestrator

import (
	"errors"
	"testing"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestDeferRunCleanup_MarksNeedsAttention 驗證 AC-7（Issue 4 Part A）：
// DeferRunCleanup 在 Active==true 觸發兜底時，除補 StopReason=process-exit（原本為空才補）
// 外，也把 Phase 設為 needs-attention，讓 `4x retry` 能接手，不再停在原本的工作 phase。
func TestDeferRunCleanup_MarksNeedsAttention(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-defer-cleanup")
	s := protocol.State{
		FeatureID: "feat-defer-cleanup",
		Phase:     protocol.PhaseFixing,
		Round:     2,
		Active:    true,
		Role:      protocol.RoleFixer,
		CreatedAt: time.Now(),
	}
	if err := ws.WriteState("feat-defer-cleanup", s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	DeferRunCleanup(ws, "feat-defer-cleanup")

	got, err := ws.ReadState("feat-defer-cleanup")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.Active {
		t.Error("Active should be false after DeferRunCleanup fallback")
	}
	if got.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("Phase = %s, want needs-attention", got.Phase)
	}
	if got.StopReason != "process-exit" {
		t.Errorf("StopReason = %q, want process-exit", got.StopReason)
	}
}

// TestDeferRunCleanup_DoesNotOverridePhaseWhenAlreadyStopped 驗證 DeferRunCleanup 只在
// 「原本 StopReason 為空」的兜底情境覆寫 Phase；若 loop 已正常寫入終態（StopReason 非空、
// Active 已 false），DeferRunCleanup 完全不動它——維持既有不變式。
func TestDeferRunCleanup_DoesNotOverridePhaseWhenAlreadyStopped(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-defer-clean2")
	s := protocol.State{
		FeatureID:  "feat-defer-clean2",
		Phase:      protocol.PhaseBlocked,
		Round:      1,
		Active:     false,
		StopReason: "soft-fail",
		CreatedAt:  time.Now(),
	}
	if err := ws.WriteState("feat-defer-clean2", s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	DeferRunCleanup(ws, "feat-defer-clean2")

	got, err := ws.ReadState("feat-defer-clean2")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.Phase != protocol.PhaseBlocked {
		t.Errorf("Phase = %s, want blocked (unchanged)", got.Phase)
	}
	if got.StopReason != "soft-fail" {
		t.Errorf("StopReason = %q, want soft-fail (unchanged)", got.StopReason)
	}
}

// TestAbortTransition_PersistsNeedsAttention 驗證 AC-9（Issue 4 Part B）根因修復：
// abortTransition（RunLoop 在角色 exit-0 完成後、state.Transition 本身失敗時呼叫——
// pre-transition hook 失敗已由 ExecutePhaseHooks 自行處理，不會呼叫本函式）必須把
// Active 持久化為 false 並轉去 needs-attention，不能讓 state.json 停在轉換前的舊 phase
// （Active 仍 true）。這確保 cmd/4x/run.go 的 defer DeferRunCleanup 兜底不會再把
// 「角色其實已完成、只是收尾失敗」的狀況誤標為 process-exit/interrupted。
func TestAbortTransition_PersistsNeedsAttention(t *testing.T) {
	ws := setupPhaseWorkspace(t, "feat-abort-transition")
	s := protocol.State{
		FeatureID: "feat-abort-transition",
		Phase:     protocol.PhaseFixing,
		Round:     3,
		Active:    true,
		Role:      protocol.RoleFixer,
		CreatedAt: time.Now(),
	}
	if err := ws.WriteState("feat-abort-transition", s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	r := NewRunner(Config{Ws: ws, Feature: feat.Feature{ID: "feat-abort-transition"}})
	r.abortTransition("feat-abort-transition", &s, protocol.PhaseFixing, protocol.RoleFixer, "mock", errors.New("state transition failed"))

	got, err := ws.ReadState("feat-abort-transition")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.Active {
		t.Error("Active should be false after abortTransition")
	}
	if got.Phase != protocol.PhaseNeedsAttention {
		t.Errorf("Phase = %s, want needs-attention", got.Phase)
	}
	if got.StopReason != "transition-error" {
		t.Errorf("StopReason = %q, want transition-error", got.StopReason)
	}
}
