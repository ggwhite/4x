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
	protocol.PhaseBlocked:        {protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing, protocol.PhaseTesting, protocol.PhaseDeepReviewing, protocol.PhaseAmending, protocol.PhaseAccepting},
	protocol.PhaseNeedsAttention: {protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing, protocol.PhaseTesting, protocol.PhaseDeepReviewing, protocol.PhaseAmending, protocol.PhaseAccepting},
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
	case to == protocol.PhaseAbandoned:
		s.Active = false
	case to == protocol.PhaseCoding && s.Round == 0:
		s.Round = 1
	case to == protocol.PhaseAmending:
		s.Round++
	}
	return s, nil
}

// isRecoverablePhase 回傳 p 是否為 crash recovery 可校正到的工作 phase。
// 對齊 blocked / needs-attention 作為 universal source 能轉往的目標集合（見 transitions 表），
// 排除 init / pending-review / done / abandoned 等不該被 artifacts 推斷覆蓋的 phase。
func isRecoverablePhase(p protocol.Phase) bool {
	switch p {
	case protocol.PhaseDesigning, protocol.PhaseCoding, protocol.PhaseReviewing,
		protocol.PhaseTesting, protocol.PhaseDeepReviewing, protocol.PhaseAmending,
		protocol.PhaseAccepting:
		return true
	default:
		return false
	}
}

// RecoverTo 在 crash recovery 場景下，把 state 校正到依實際 artifacts 推斷出的 phase。
// 與 Transition 不同，它不受一般 transitions 轉換表限制——crash 後 state.json 的 phase
// 與磁碟 artifacts 可能不一致（例：state.json 停在 coding，artifacts 卻已跑到 deep-review FAIL），
// 一般轉換表（如 coding → amending 非法）會擋下這種校正。RecoverTo 改以「目標 phase 是否為
// 可恢復的工作 phase」為合法性判準，並套用與 Transition 完全相同的 round 副作用
// （首次 coding → Round=1、amending → Round++），確保 recovery 後的語意與正常流程一致。
//
// 用法：僅供 run resume 校正 crash 後 phase 用，不可取代正常流程的 Transition。
func RecoverTo(s protocol.State, to protocol.Phase, role protocol.Role) (protocol.State, error) {
	if !isRecoverablePhase(to) {
		return s, fmt.Errorf("invalid recovery target: %s", to)
	}
	s.Phase = to
	s.Role = role
	switch {
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
