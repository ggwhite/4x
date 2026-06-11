package state

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestCanTransition_Valid(t *testing.T) {
	tests := []struct {
		from protocol.Phase
		to   protocol.Phase
	}{
		{protocol.PhaseInit, protocol.PhaseDesigning},
		{protocol.PhaseDesigning, protocol.PhaseCoding},
		{protocol.PhaseDesigning, protocol.PhaseBlocked},
		{protocol.PhaseCoding, protocol.PhaseReviewing},
		{protocol.PhaseCoding, protocol.PhaseAmending},
		{protocol.PhaseCoding, protocol.PhaseBlocked},
		{protocol.PhaseReviewing, protocol.PhaseCoding},
		{protocol.PhaseReviewing, protocol.PhaseTesting},
		{protocol.PhaseReviewing, protocol.PhaseBlocked},
		{protocol.PhaseTesting, protocol.PhaseCoding},
		{protocol.PhaseTesting, protocol.PhaseAmending},
		{protocol.PhaseTesting, protocol.PhaseAccepting},
		{protocol.PhaseTesting, protocol.PhaseBlocked},
		{protocol.PhaseAmending, protocol.PhaseCoding},
		{protocol.PhaseAmending, protocol.PhaseBlocked},
		{protocol.PhaseAccepting, protocol.PhaseDone},
		{protocol.PhaseAccepting, protocol.PhaseBlocked},
		{protocol.PhaseBlocked, protocol.PhaseDesigning},
		{protocol.PhaseBlocked, protocol.PhaseCoding},
		{protocol.PhaseBlocked, protocol.PhaseTesting},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"→"+string(tt.to), func(t *testing.T) {
			if !CanTransition(tt.from, tt.to) {
				t.Errorf("CanTransition(%s, %s) = false, want true", tt.from, tt.to)
			}
		})
	}
}

func TestCanTransition_Invalid(t *testing.T) {
	tests := []struct {
		from protocol.Phase
		to   protocol.Phase
	}{
		{protocol.PhaseInit, protocol.PhaseTesting},
		{protocol.PhaseInit, protocol.PhaseDone},
		{protocol.PhaseInit, protocol.PhaseCoding},
		{protocol.PhaseDesigning, protocol.PhaseTesting},
		{protocol.PhaseDesigning, protocol.PhaseDone},
		{protocol.PhaseCoding, protocol.PhaseDone},
		{protocol.PhaseCoding, protocol.PhaseAccepting},
		{protocol.PhaseReviewing, protocol.PhaseDone},
		{protocol.PhaseReviewing, protocol.PhaseAccepting},
		{protocol.PhaseTesting, protocol.PhaseDesigning},
		{protocol.PhaseAmending, protocol.PhaseTesting},
		{protocol.PhaseAmending, protocol.PhaseDone},
		{protocol.PhaseAccepting, protocol.PhaseCoding},
		{protocol.PhaseDone, protocol.PhaseInit},
		{protocol.PhaseDone, protocol.PhaseCoding},
		{"nonexistent", protocol.PhaseCoding},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"→"+string(tt.to), func(t *testing.T) {
			if CanTransition(tt.from, tt.to) {
				t.Errorf("CanTransition(%s, %s) = true, want false", tt.from, tt.to)
			}
		})
	}
}

func TestTransition_UpdatesPhaseAndRole(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseInit}
	got, err := Transition(s, protocol.PhaseDesigning, protocol.RoleDesigner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Phase != protocol.PhaseDesigning {
		t.Errorf("Phase = %s, want designing", got.Phase)
	}
	if got.Role != protocol.RoleDesigner {
		t.Errorf("Role = %s, want designer", got.Role)
	}
}

func TestTransition_InvalidReturnsError(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseInit}
	_, err := Transition(s, protocol.PhaseDone, protocol.RoleDesigner)
	if err == nil {
		t.Error("expected error for invalid transition, got nil")
	}
}

func TestTransition_RoundIncrement(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseDesigning, Round: 0}
	got, err := Transition(s, protocol.PhaseCoding, protocol.RoleCoder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 1 {
		t.Errorf("Round = %d, want 1 (first coding round)", got.Round)
	}
}

func TestTransition_AmendingToCodingShouldNotIncrementRound(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseAmending, Round: 2}
	got, err := Transition(s, protocol.PhaseCoding, protocol.RoleCoder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 2 {
		t.Errorf("Round = %d, want 2 (amending→coding should not increment)", got.Round)
	}
}

func TestTransition_RoundNotIncrementedForNonCoding(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseCoding, Round: 1}
	got, err := Transition(s, protocol.PhaseReviewing, protocol.RoleReviewer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 1 {
		t.Errorf("Round = %d, want 1 (reviewing should not increment)", got.Round)
	}
}

func TestPhaseToRole(t *testing.T) {
	tests := []struct {
		phase protocol.Phase
		want  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseAmending, protocol.RoleDesigner},
		{protocol.PhaseAccepting, protocol.RoleDesigner},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseInit, ""},
		{protocol.PhaseDone, ""},
		{protocol.PhaseBlocked, ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			got := PhaseToRole(tt.phase)
			if got != tt.want {
				t.Errorf("PhaseToRole(%s) = %s, want %s", tt.phase, got, tt.want)
			}
		})
	}
}

func TestShouldStop_MaxRounds(t *testing.T) {
	s := protocol.State{Round: 5, MaxRounds: 5}
	stop, reason := ShouldStop(s)
	if !stop {
		t.Error("ShouldStop = false, want true (max rounds)")
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestShouldStop_NoProgress(t *testing.T) {
	s := protocol.State{Round: 2, MaxRounds: 10, ConsecutiveNoProgress: 3}
	stop, reason := ShouldStop(s)
	if !stop {
		t.Error("ShouldStop = false, want true (no progress)")
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestShouldStop_Continue(t *testing.T) {
	s := protocol.State{Round: 2, MaxRounds: 10, ConsecutiveNoProgress: 1}
	stop, _ := ShouldStop(s)
	if stop {
		t.Error("ShouldStop = true, want false (should continue)")
	}
}
