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
		{protocol.PhaseDesigning, protocol.PhaseDesignReviewing},
		{protocol.PhaseDesignReviewing, protocol.PhaseCoding},
		{protocol.PhaseDesignReviewing, protocol.PhaseDesigning},
		{protocol.PhaseCoding, protocol.PhaseReviewing},
		{protocol.PhaseReviewing, protocol.PhaseTesting},
		{protocol.PhaseReviewing, protocol.PhaseAmending},
		{protocol.PhaseAmending, protocol.PhaseReviewing},
		{protocol.PhaseTesting, protocol.PhaseDeepReviewing},
		{protocol.PhaseTesting, protocol.PhaseAmending},
		{protocol.PhaseDeepReviewing, protocol.PhaseAccepting},
		{protocol.PhaseDeepReviewing, protocol.PhaseAmending},
		{protocol.PhaseAccepting, protocol.PhasePendingReview},
		{protocol.PhasePendingReview, protocol.PhaseDone},
		{protocol.PhasePendingReview, protocol.PhaseBlocked},
		{protocol.PhasePendingReview, protocol.PhaseNeedsAttention},
		{protocol.PhaseInit, protocol.PhaseDone},
		{protocol.PhaseDesigning, protocol.PhaseDone},
		{protocol.PhaseCoding, protocol.PhaseDone},
		{protocol.PhaseReviewing, protocol.PhaseDone},
		{protocol.PhaseTesting, protocol.PhaseDone},
		{protocol.PhaseAmending, protocol.PhaseDone},
		{protocol.PhaseAccepting, protocol.PhaseDone},
		{protocol.PhaseBlocked, protocol.PhaseDone},
		{protocol.PhaseNeedsAttention, protocol.PhaseDone},
		{protocol.PhaseBlocked, protocol.PhaseDesigning},
		{protocol.PhaseBlocked, protocol.PhaseCoding},
		{protocol.PhaseBlocked, protocol.PhaseTesting},
		{protocol.PhaseNeedsAttention, protocol.PhaseDesigning},
		{protocol.PhaseNeedsAttention, protocol.PhaseCoding},
		{protocol.PhaseNeedsAttention, protocol.PhaseReviewing},
		{protocol.PhaseNeedsAttention, protocol.PhaseTesting},
		{protocol.PhaseNeedsAttention, protocol.PhaseDeepReviewing},
		{protocol.PhaseNeedsAttention, protocol.PhaseAmending},
		{protocol.PhaseNeedsAttention, protocol.PhaseAccepting},
		{protocol.PhaseBlocked, protocol.PhaseReviewing},
		{protocol.PhaseBlocked, protocol.PhaseDeepReviewing},
		{protocol.PhaseBlocked, protocol.PhaseAmending},
		{protocol.PhaseBlocked, protocol.PhaseAccepting},
		{protocol.PhaseInit, protocol.PhaseBlocked},
		{protocol.PhaseDesigning, protocol.PhaseBlocked},
		{protocol.PhaseCoding, protocol.PhaseBlocked},
		{protocol.PhaseReviewing, protocol.PhaseBlocked},
		{protocol.PhaseTesting, protocol.PhaseBlocked},
		{protocol.PhaseAmending, protocol.PhaseBlocked},
		{protocol.PhaseAccepting, protocol.PhaseBlocked},
		{protocol.PhaseCoding, protocol.PhaseNeedsAttention},
		{protocol.PhaseReviewing, protocol.PhaseNeedsAttention},
		{protocol.PhaseTesting, protocol.PhaseNeedsAttention},
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
		{protocol.PhaseInit, protocol.PhaseCoding},
		{protocol.PhaseDesigning, protocol.PhaseCoding},
		{protocol.PhaseDesigning, protocol.PhaseTesting},
		{protocol.PhaseCoding, protocol.PhaseAccepting},
		{protocol.PhaseCoding, protocol.PhaseAmending},
		{protocol.PhaseReviewing, protocol.PhaseAccepting},
		{protocol.PhaseReviewing, protocol.PhaseCoding},
		{protocol.PhaseTesting, protocol.PhaseAccepting},
		{protocol.PhaseTesting, protocol.PhaseCoding},
		{protocol.PhaseAmending, protocol.PhaseTesting},
		{protocol.PhaseAmending, protocol.PhaseCoding},
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
	_, err := Transition(s, protocol.PhaseTesting, protocol.RoleTester)
	if err == nil {
		t.Error("expected error for invalid transition, got nil")
	}
}

func TestTransition_DoneSetsActivefalse(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseInit, Active: true}
	got, err := Transition(s, protocol.PhaseDone, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Phase != protocol.PhaseDone {
		t.Errorf("Phase = %s, want done", got.Phase)
	}
	if got.Active {
		t.Error("Active = true, want false after transition to done")
	}
}

// W2（AC-3）：transition 到 abandoned 後 Active 必須為 false。
func TestTransition_AbandonedSetsActiveFalse(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseCoding, Round: 2, Active: true}
	got, err := Transition(s, protocol.PhaseAbandoned, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Phase != protocol.PhaseAbandoned {
		t.Errorf("Phase = %s, want abandoned", got.Phase)
	}
	if got.Active {
		t.Error("Active = true, want false after transition to abandoned")
	}
}

func TestTransition_FirstCodingRound(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseDesignReviewing, Round: 0}
	got, err := Transition(s, protocol.PhaseCoding, protocol.RoleCoder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 1 {
		t.Errorf("Round = %d, want 1", got.Round)
	}
}

func TestTransition_AmendingIncrementsRound(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseReviewing, Round: 1}
	got, err := Transition(s, protocol.PhaseAmending, protocol.RoleCoder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 2 {
		t.Errorf("Round = %d, want 2", got.Round)
	}
}

func TestTransition_TestingToAmendingIncrementsRound(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseTesting, Round: 1}
	got, err := Transition(s, protocol.PhaseAmending, protocol.RoleCoder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 2 {
		t.Errorf("Round = %d, want 2", got.Round)
	}
}

func TestTransition_AmendingToReviewingNoIncrement(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseAmending, Round: 2}
	got, err := Transition(s, protocol.PhaseReviewing, protocol.RoleReviewer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 2 {
		t.Errorf("Round = %d, want 2", got.Round)
	}
}

func TestTransition_RoundNotIncrementedForNonCoding(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseCoding, Round: 1}
	got, err := Transition(s, protocol.PhaseReviewing, protocol.RoleReviewer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 1 {
		t.Errorf("Round = %d, want 1", got.Round)
	}
}

func TestTransition_BlockedToCodingKeepsRound(t *testing.T) {
	s := protocol.State{Phase: protocol.PhaseBlocked, Round: 2}
	got, err := Transition(s, protocol.PhaseCoding, protocol.RoleCoder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Round != 2 {
		t.Errorf("Round = %d, want 2", got.Round)
	}
}

func TestPhaseToRole(t *testing.T) {
	tests := []struct {
		phase protocol.Phase
		want  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer},
		{protocol.PhaseAccepting, protocol.RoleAcceptor},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseAmending, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseInit, ""},
		{protocol.PhasePendingReview, ""},
		{protocol.PhaseDone, ""},
		{protocol.PhaseBlocked, ""},
		{protocol.PhaseNeedsAttention, ""},
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

// TestCanTransition_TerminalCannotTransitionOut 驗證 done／abandoned 為不可逆終態，
// 任何轉出（含 universal target）都不合法。
func TestCanTransition_TerminalCannotTransitionOut(t *testing.T) {
	tests := []struct {
		from protocol.Phase
		to   protocol.Phase
	}{
		{protocol.PhaseDone, protocol.PhaseBlocked},
		{protocol.PhaseDone, protocol.PhaseNeedsAttention},
		{protocol.PhaseDone, protocol.PhaseDone},
		{protocol.PhaseDone, protocol.PhaseAbandoned},
		{protocol.PhaseAbandoned, protocol.PhaseBlocked},
		{protocol.PhaseAbandoned, protocol.PhaseNeedsAttention},
		{protocol.PhaseAbandoned, protocol.PhaseDone},
		{protocol.PhaseAbandoned, protocol.PhaseAbandoned},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"→"+string(tt.to), func(t *testing.T) {
			if CanTransition(tt.from, tt.to) {
				t.Errorf("CanTransition(%s, %s) = true, want false (terminal is irreversible)", tt.from, tt.to)
			}
		})
	}
}

// TestShouldStop_MaxRoundsZeroSkipsCheck 驗證 MaxRounds<=0 代表不設上限，
// run 一開始（Round=0）不應立即觸發 max rounds 停止。
func TestShouldStop_MaxRoundsZeroSkipsCheck(t *testing.T) {
	for _, max := range []int{0, -1} {
		s := protocol.State{Round: 0, MaxRounds: max}
		if stop, reason := ShouldStop(s); stop {
			t.Errorf("ShouldStop(MaxRounds=%d) = true (%q), want false", max, reason)
		}
		// 即使 round 已推進，無上限時仍不因 max rounds 停止。
		s = protocol.State{Round: 99, MaxRounds: max}
		if stop, _ := ShouldStop(s); stop {
			t.Errorf("ShouldStop(Round=99, MaxRounds=%d) = true, want false", max)
		}
	}
	// 無上限時 ConsecutiveNoProgress 條件仍應生效。
	s := protocol.State{Round: 5, MaxRounds: 0, ConsecutiveNoProgress: 3}
	if stop, _ := ShouldStop(s); !stop {
		t.Error("ShouldStop(MaxRounds=0, NoProgress=3) = false, want true")
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

// TestTransitions_ReviewingOutEdges_Unchanged 守住 F144 Design Ruling 1：reviewing 同輪收斂
// 以 mini-coder 子 role 於同一 phase 內完成，不新增任何 state machine 轉換。故 reviewing 的
// 出邊集合必須維持 {testing, amending}（長度 2），不得因 F144 多出其他出邊。
func TestTransitions_ReviewingOutEdges_Unchanged(t *testing.T) {
	edges := transitions[protocol.PhaseReviewing]
	if len(edges) != 2 {
		t.Fatalf("transitions[reviewing] out-edge count = %d, want 2 (%v)", len(edges), edges)
	}
	want := map[protocol.Phase]bool{
		protocol.PhaseTesting:  true,
		protocol.PhaseAmending: true,
	}
	for _, e := range edges {
		if !want[e] {
			t.Errorf("unexpected reviewing out-edge %q; F144 must not add new reviewing transitions", e)
		}
		delete(want, e)
	}
	for missing := range want {
		t.Errorf("missing expected reviewing out-edge %q", missing)
	}
	if !CanTransition(protocol.PhaseReviewing, protocol.PhaseTesting) {
		t.Error("CanTransition(reviewing, testing) = false, want true")
	}
	if !CanTransition(protocol.PhaseReviewing, protocol.PhaseAmending) {
		t.Error("CanTransition(reviewing, amending) = false, want true")
	}
}
