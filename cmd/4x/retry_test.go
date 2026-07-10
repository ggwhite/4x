package main

import (
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

func writeRetryState(t *testing.T, ws *protocol.Workspace, featureID string, phase protocol.Phase) {
	t.Helper()
	s := protocol.State{
		FeatureID: featureID,
		Phase:     phase,
		Round:     1,
		MaxRounds: 5,
		Active:    true,
		Runner:    "mock",
		CreatedAt: time.Now(),
	}
	if err := ws.WriteState(featureID, s); err != nil {
		t.Fatalf("writeRetryState: %v", err)
	}
}

func TestRetry_FromNeedsAttention(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-na")
	writeRetryState(t, ws, "feat-retry-na", protocol.PhaseNeedsAttention)

	newState, from, autodetected, err := retryTransition(ws, "feat-retry-na", protocol.PhaseAccepting)
	if err != nil {
		t.Fatalf("retryTransition error: %v", err)
	}
	if from != protocol.PhaseNeedsAttention {
		t.Errorf("from = %s, want needs-attention", from)
	}
	if newState.Phase != protocol.PhaseAccepting {
		t.Errorf("phase = %s, want accepting", newState.Phase)
	}
	if !newState.Active {
		t.Error("Active should be true after retry")
	}
	if autodetected {
		t.Error("autodetected should be false when --to is explicitly given")
	}
}

func TestRetry_FromBlocked(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-bl")
	writeRetryState(t, ws, "feat-retry-bl", protocol.PhaseBlocked)

	newState, from, autodetected, err := retryTransition(ws, "feat-retry-bl", protocol.PhaseAccepting)
	if err != nil {
		t.Fatalf("retryTransition error: %v", err)
	}
	if from != protocol.PhaseBlocked {
		t.Errorf("from = %s, want blocked", from)
	}
	if newState.Phase != protocol.PhaseAccepting {
		t.Errorf("phase = %s, want accepting", newState.Phase)
	}
	if autodetected {
		t.Error("autodetected should be false when --to is explicitly given")
	}
}

func TestRetry_SetsManualPhase(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-manual")
	writeRetryState(t, ws, "feat-retry-manual", protocol.PhaseNeedsAttention)

	newState, _, _, err := retryTransition(ws, "feat-retry-manual", protocol.PhaseDeepReviewing)
	if err != nil {
		t.Fatalf("retryTransition error: %v", err)
	}
	if !newState.ManualPhase {
		t.Error("ManualPhase should be true after retry (manual phase intervention)")
	}
	// 讀回持久化的 state 也應帶旗標，供 child run 的 RecoverState 尊重。
	got, err := ws.ReadState("feat-retry-manual")
	if err != nil {
		t.Fatalf("ReadState error: %v", err)
	}
	if !got.ManualPhase {
		t.Error("persisted ManualPhase should be true after retry")
	}
}

func TestRetry_CustomTargetPhase(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-amend")
	writeRetryState(t, ws, "feat-retry-amend", protocol.PhaseNeedsAttention)

	newState, _, autodetected, err := retryTransition(ws, "feat-retry-amend", protocol.PhaseAmending)
	if err != nil {
		t.Fatalf("retryTransition error: %v", err)
	}
	if newState.Phase != protocol.PhaseAmending {
		t.Errorf("phase = %s, want amending", newState.Phase)
	}
	if autodetected {
		t.Error("autodetected should be false when --to is explicitly given")
	}
}

func TestRetry_InvalidSourcePhase(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-inv")
	writeRetryState(t, ws, "feat-retry-inv", protocol.PhaseCoding)

	_, _, _, err := retryTransition(ws, "feat-retry-inv", protocol.PhaseAccepting)
	if err == nil {
		t.Error("expected error for non-terminal source phase, got nil")
	}
}

// writeRetryStateWithRole 寫入指定 phase/role/round 的 state，供自動偵測測試使用。
func writeRetryStateWithRole(t *testing.T, ws *protocol.Workspace, featureID string, phase protocol.Phase, role protocol.Role, round int) {
	t.Helper()
	s := protocol.State{
		FeatureID: featureID,
		Phase:     phase,
		Role:      role,
		Round:     round,
		MaxRounds: 5,
		Active:    true,
		Runner:    "mock",
		CreatedAt: time.Now(),
	}
	if err := ws.WriteState(featureID, s); err != nil {
		t.Fatalf("writeRetryStateWithRole: %v", err)
	}
}

// TestRetry_AutodetectFromRole 驗證 AC-4/AC-7：未帶 --to 時，於臨界區內依 cur.Role /
// cur.Round 自動偵測 target 並轉換，autodetected==true，且 newState.Role 等於原 cur.Role。
func TestRetry_AutodetectFromRole(t *testing.T) {
	tests := []struct {
		name      string
		role      protocol.Role
		round     int
		wantPhase protocol.Phase
		wantRole  protocol.Role
	}{
		{"designer→designing", protocol.RoleDesigner, 0, protocol.PhaseDesigning, protocol.RoleDesigner},
		{"coder-round1→coding", protocol.RoleCoder, 1, protocol.PhaseCoding, protocol.RoleCoder},
		{"coder-round3→amending", protocol.RoleCoder, 3, protocol.PhaseAmending, protocol.RoleCoder},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := "feat-autodetect-" + tt.name
			ws := setupLoopWorkspace(t, id)
			writeRetryStateWithRole(t, ws, id, protocol.PhaseNeedsAttention, tt.role, tt.round)

			newState, from, autodetected, err := retryTransition(ws, id, "")
			if err != nil {
				t.Fatalf("retryTransition error: %v", err)
			}
			if from != protocol.PhaseNeedsAttention {
				t.Errorf("from = %s, want needs-attention", from)
			}
			if !autodetected {
				t.Error("autodetected = false, want true")
			}
			if newState.Phase != tt.wantPhase {
				t.Errorf("phase = %s, want %s", newState.Phase, tt.wantPhase)
			}
			// round-trip 性質：偵測轉換後 newState.Role 應等於原 cur.Role。
			if newState.Role != tt.wantRole {
				t.Errorf("role = %s, want %s", newState.Role, tt.wantRole)
			}
		})
	}
}

// TestRetry_AutodetectFallbackAccepting 驗證 AC-5/AC-7：role 為空（RoleToPhase 回 ""）時，
// 未帶 --to 應 fallback accepting，且 autodetected==false（不觸發自動偵測訊息）。
func TestRetry_AutodetectFallbackAccepting(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-autodetect-fallback")
	writeRetryStateWithRole(t, ws, "feat-autodetect-fallback", protocol.PhaseNeedsAttention, "", 1)

	newState, _, autodetected, err := retryTransition(ws, "feat-autodetect-fallback", "")
	if err != nil {
		t.Fatalf("retryTransition error: %v", err)
	}
	if newState.Phase != protocol.PhaseAccepting {
		t.Errorf("phase = %s, want accepting (fallback)", newState.Phase)
	}
	if autodetected {
		t.Error("autodetected = true, want false on fallback accepting")
	}
}

// TestRetry_ExplicitOverridesAutodetect 驗證 AC-6/AC-7：來源 role 可自動偵測（designer→designing），
// 但明確帶 --to accepting 時優先權最高，仍轉 accepting 且 autodetected==false。
func TestRetry_ExplicitOverridesAutodetect(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-explicit-override")
	writeRetryStateWithRole(t, ws, "feat-explicit-override", protocol.PhaseNeedsAttention, protocol.RoleDesigner, 0)

	newState, _, autodetected, err := retryTransition(ws, "feat-explicit-override", protocol.PhaseAccepting)
	if err != nil {
		t.Fatalf("retryTransition error: %v", err)
	}
	if newState.Phase != protocol.PhaseAccepting {
		t.Errorf("phase = %s, want accepting (explicit --to overrides autodetect)", newState.Phase)
	}
	if autodetected {
		t.Error("autodetected = true, want false when --to is explicitly given")
	}
}
