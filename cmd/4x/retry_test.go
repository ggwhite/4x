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

	newState, from, err := retryTransition(ws, "feat-retry-na", protocol.PhaseAccepting)
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
}

func TestRetry_FromBlocked(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-bl")
	writeRetryState(t, ws, "feat-retry-bl", protocol.PhaseBlocked)

	newState, from, err := retryTransition(ws, "feat-retry-bl", protocol.PhaseAccepting)
	if err != nil {
		t.Fatalf("retryTransition error: %v", err)
	}
	if from != protocol.PhaseBlocked {
		t.Errorf("from = %s, want blocked", from)
	}
	if newState.Phase != protocol.PhaseAccepting {
		t.Errorf("phase = %s, want accepting", newState.Phase)
	}
}

func TestRetry_CustomTargetPhase(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-amend")
	writeRetryState(t, ws, "feat-retry-amend", protocol.PhaseNeedsAttention)

	newState, _, err := retryTransition(ws, "feat-retry-amend", protocol.PhaseAmending)
	if err != nil {
		t.Fatalf("retryTransition error: %v", err)
	}
	if newState.Phase != protocol.PhaseAmending {
		t.Errorf("phase = %s, want amending", newState.Phase)
	}
}

func TestRetry_InvalidSourcePhase(t *testing.T) {
	ws := setupLoopWorkspace(t, "feat-retry-inv")
	writeRetryState(t, ws, "feat-retry-inv", protocol.PhaseCoding)

	_, _, err := retryTransition(ws, "feat-retry-inv", protocol.PhaseAccepting)
	if err == nil {
		t.Error("expected error for non-terminal source phase, got nil")
	}
}
