package state

import (
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
)

// 合法的 phase 轉換表（對齊 docs/design.md §4）
// blocked 和 needs-attention 是 universal target，由 CanTransition 特殊處理
var transitions = map[protocol.Phase][]protocol.Phase{
	protocol.PhaseInit:           {protocol.PhaseDesigning},
	protocol.PhaseDesigning:      {protocol.PhaseCoding},
	protocol.PhaseCoding:         {protocol.PhaseReviewing, protocol.PhaseDesigning},
	protocol.PhaseReviewing:      {protocol.PhaseTesting, protocol.PhaseAmending},
	protocol.PhaseAmending:       {protocol.PhaseReviewing, protocol.PhaseDesigning},
	protocol.PhaseTesting:        {protocol.PhaseDeepReviewing, protocol.PhaseAmending, protocol.PhaseDesigning},
	protocol.PhaseDeepReviewing:  {protocol.PhaseAccepting, protocol.PhaseAmending},
	protocol.PhaseAccepting:      {protocol.PhasePendingReview},
	protocol.PhasePendingReview:  {protocol.PhaseDone},
	protocol.PhaseBlocked:        {protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseTesting},
	protocol.PhaseNeedsAttention: {protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseTesting},
}

// CanTransition 檢查從 from 到 to 是否合法
func CanTransition(from, to protocol.Phase) bool {
	if to == protocol.PhaseBlocked || to == protocol.PhaseNeedsAttention || to == protocol.PhaseDone || to == protocol.PhaseAbandoned {
		return true
	}
	allowed, ok := transitions[from]
	if !ok {
		return false
	}
	for _, p := range allowed {
		if p == to {
			return true
		}
	}
	return false
}

// Transition 執行 state 轉換，回傳錯誤或更新後的 state
func Transition(s protocol.State, to protocol.Phase, role protocol.Role) (protocol.State, error) {
	if !CanTransition(s.Phase, to) {
		return s, fmt.Errorf("invalid transition: %s → %s", s.Phase, to)
	}
	s.Phase = to
	s.Role = role
	switch {
	case to == protocol.PhaseDone:
		s.Active = false
	case to == protocol.PhaseCoding && s.Round == 0:
		s.Round = 1
	case to == protocol.PhaseAmending:
		s.Round++
	}
	return s, nil
}

// PhaseToRole 回傳某 phase 預設對應的 role
func PhaseToRole(p protocol.Phase) protocol.Role {
	switch p {
	case protocol.PhaseDesigning:
		return protocol.RoleDesigner
	case protocol.PhaseAccepting:
		return protocol.RoleAcceptor
	case protocol.PhaseCoding, protocol.PhaseAmending:
		return protocol.RoleCoder
	case protocol.PhaseReviewing:
		return protocol.RoleReviewer
	case protocol.PhaseDeepReviewing:
		return protocol.RoleDeepReviewer
	case protocol.PhaseTesting:
		return protocol.RoleTester
	default:
		return ""
	}
}

// ShouldStop 檢查是否觸發停止條件
func ShouldStop(s protocol.State) (bool, string) {
	if s.Round >= s.MaxRounds {
		return true, fmt.Sprintf("reached max rounds (%d)", s.MaxRounds)
	}
	if s.ConsecutiveNoProgress >= 3 {
		return true, "3 consecutive rounds with no progress"
	}
	return false, ""
}
