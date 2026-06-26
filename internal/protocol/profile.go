package protocol

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/feature"
)

// profileSelectablePhases 是 profile 可選的工作 phase 白名單，對應 state.PhaseToRole 有 role 的
// phase（不含 init/amending/pending-review/done/blocked/needs-attention/abandoned）。
var profileSelectablePhases = map[Phase]bool{
	PhaseDesigning:       true,
	PhaseDesignReviewing: true,
	PhaseCoding:          true,
	PhaseReviewing:       true,
	PhaseDeepReviewing:   true,
	PhaseTesting:         true,
	PhaseAccepting:       true,
}

// roleToPhaseMap 是 role → phase 的對應（與 state.PhaseToRole 的工作 phase 反向一致）。
// 放在 protocol package（而非 state）避免循環依賴：state 依賴 protocol。
var roleToPhaseMap = map[Role]Phase{
	RoleDesigner:       PhaseDesigning,
	RoleDesignReviewer: PhaseDesignReviewing,
	RoleCoder:          PhaseCoding,
	RoleReviewer:       PhaseReviewing,
	RoleDeepReviewer:   PhaseDeepReviewing,
	RoleTester:         PhaseTesting,
	RoleAcceptor:       PhaseAccepting,
}

// phaseToRoleMap 是 phase → role 的對應，供 doctor 等需要從 phase 反推 role 解析 model 時使用。
var phaseToRoleMap = map[Phase]Role{
	PhaseDesigning:       RoleDesigner,
	PhaseDesignReviewing: RoleDesignReviewer,
	PhaseCoding:          RoleCoder,
	PhaseReviewing:       RoleReviewer,
	PhaseDeepReviewing:   RoleDeepReviewer,
	PhaseTesting:         RoleTester,
	PhaseAccepting:       RoleAcceptor,
}

// IsSelectablePhase 回報某 phase 是否屬於 profile 可選的工作 phase 白名單。
func IsSelectablePhase(phase Phase) bool {
	return profileSelectablePhases[phase]
}

// SelectablePhases 回傳 profile 可選 phase 的 canonical pipeline 順序清單，
// 供 UI 列舉與 doctor 驗證使用。
func SelectablePhases() []Phase {
	return []Phase{
		PhaseDesigning, PhaseDesignReviewing, PhaseCoding, PhaseReviewing,
		PhaseTesting, PhaseDeepReviewing, PhaseAccepting,
	}
}

// roleToPhase 把 role 對應到其工作 phase；無對應（如 mini-coder 等子 role）時回空字串。
func roleToPhase(role Role) Phase {
	return roleToPhaseMap[role]
}

// phaseRole 把工作 phase 對應回其 role；無對應時回空字串。
func phaseRole(phase Phase) Role {
	return phaseToRoleMap[phase]
}

// PhaseRole 回傳工作 phase 對應的 role；非 profile 工作 phase 回空字串。
func PhaseRole(phase Phase) Role {
	return phaseRole(phase)
}

// normalize 把舊格式 Roles[]string / CoderModel 轉成新的 Phases 結構（向後相容）。
// 僅在 Phases 為空且 Roles 非空時轉換：每個 role 映射成對應 phase 的 PhaseSpec，
// coder 那筆若有 CoderModel 則設為其 Model。已是新格式（Phases 非空）時不動作（冪等）。
func (pc *ProfileConfig) normalize() {
	if len(pc.Phases) > 0 || len(pc.Roles) == 0 {
		return
	}
	for _, r := range pc.Roles {
		phase := roleToPhase(Role(r))
		if phase == "" {
			continue
		}
		spec := PhaseSpec{Phase: string(phase)}
		if Role(r) == RoleCoder && pc.CoderModel != "" {
			spec.Model = pc.CoderModel
		}
		pc.Phases = append(pc.Phases, spec)
	}
}

// HasPhase 回報此 profile 是否包含指定 phase。
func (pc ProfileConfig) HasPhase(phase Phase) bool {
	for _, ps := range pc.Phases {
		if ps.Phase == string(phase) {
			return true
		}
	}
	return false
}

// DefaultProfiles 回傳內建 pipeline profile，作為 auto-select 與 fallback 用。
//   - full：完整 7 phase，適合高風險功能（金流、結算、核心協議）
//   - lite：designing → coding → testing，有設計有驗證但省略所有 review 層，token 約 full 的 40%
//   - normal：coding → reviewing → testing → accepting，已知要做什麼時跳過設計階段
//   - quick：coding → reviewing，最精簡的 code + review
func DefaultProfiles() map[string]ProfileConfig {
	return map[string]ProfileConfig{
		"full": {Phases: []PhaseSpec{
			{Phase: string(PhaseDesigning)},
			{Phase: string(PhaseDesignReviewing)},
			{Phase: string(PhaseCoding)},
			{Phase: string(PhaseReviewing)},
			{Phase: string(PhaseDeepReviewing)},
			{Phase: string(PhaseTesting)},
			{Phase: string(PhaseAccepting)},
		}},
		"lite": {Phases: []PhaseSpec{
			{Phase: string(PhaseDesigning)},
			{Phase: string(PhaseCoding)},
			{Phase: string(PhaseTesting)},
		}},
		"normal": {Phases: []PhaseSpec{
			{Phase: string(PhaseCoding)},
			{Phase: string(PhaseReviewing)},
			{Phase: string(PhaseTesting)},
			{Phase: string(PhaseAccepting)},
		}},
		"quick": {Phases: []PhaseSpec{
			{Phase: string(PhaseCoding)},
			{Phase: string(PhaseReviewing)},
		}},
	}
}

// ResolveProfile 決定本次 run 使用的 profile 名稱與設定。
//
// 解析優先序：
//   - override 非空（--profile / resume 沿用）：先查 cfg.Profiles，再退 DefaultProfiles；都找不到回 error。
//   - override 為空且 feature YAML profile 欄位非空：用 f.Profile（查 cfg.Profiles 再退
//     DefaultProfiles，找不到回 error），供 batch per-feature 套用不同 profile。
//   - override 為空且 cfg.DefaultProfile 非空：用 cfg.DefaultProfile（查 cfg.Profiles 再退
//     DefaultProfiles，找不到印 warning 退 full）。
//   - override 為空且 cfg.DefaultProfile 為空：維持既有 fallback——cfg.Profiles 為空回 full；
//     非空時依 feature.Priority auto-select（nil/0/1→full、2→normal、>=3→quick）。
//
// 解析出的 profile 若不含 coding phase 視為設定錯誤，回 error（coding 為唯一必要 phase）。
func ResolveProfile(cfg Config, f feature.Feature, override string) (string, ProfileConfig, error) {
	defaults := DefaultProfiles()

	lookup := func(name string) (ProfileConfig, bool) {
		if pc, ok := cfg.Profiles[name]; ok {
			return pc, true
		}
		if pc, ok := defaults[name]; ok {
			return pc, true
		}
		return ProfileConfig{}, false
	}

	var name string
	var pc ProfileConfig

	switch {
	case override != "":
		got, ok := lookup(override)
		if !ok {
			return "", ProfileConfig{}, fmt.Errorf("unknown profile %q", override)
		}
		name, pc = override, got

	case f.Profile != "":
		got, ok := lookup(f.Profile)
		if !ok {
			return "", ProfileConfig{}, fmt.Errorf("unknown profile %q in feature %s", f.Profile, f.ID)
		}
		name, pc = f.Profile, got

	case cfg.DefaultProfile != "":
		got, ok := lookup(cfg.DefaultProfile)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: default_profile %q not found, falling back to full\n", cfg.DefaultProfile)
			name, pc = "full", defaults["full"]
		} else {
			name, pc = cfg.DefaultProfile, got
		}

	case len(cfg.Profiles) == 0:
		name, pc = "full", defaults["full"]

	default:
		name = autoSelectProfile(f.Priority)
		got, ok := lookup(name)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: profile %q not found, falling back to full\n", name)
			name, pc = "full", defaults["full"]
		} else {
			pc = got
		}
	}

	// 防禦性 normalize：涵蓋以 Go literal 直接建構（未經 JSON UnmarshalJSON）的 ProfileConfig。
	pc.normalize()

	if !pc.EnablesPhase(PhaseCoding) {
		return "", ProfileConfig{}, fmt.Errorf("profile %q is missing required phase %q", name, PhaseCoding)
	}

	return name, pc, nil
}

// autoSelectProfile 依 feature priority 對應到內建 profile 名稱。
// priority 越大代表越不重要，pipeline 越精簡。
func autoSelectProfile(priority *int) string {
	if priority == nil {
		return "full"
	}
	switch p := *priority; {
	case p <= 1:
		return "full"
	case p == 2:
		return "normal"
	default:
		return "quick"
	}
}

// phaseSpec 回傳 profile 中對應某 phase 的 PhaseSpec 與是否存在。
func (pc ProfileConfig) phaseSpec(phase Phase) (PhaseSpec, bool) {
	for _, ps := range pc.Phases {
		if Phase(ps.Phase) == phase {
			return ps, true
		}
	}
	return PhaseSpec{}, false
}

// EnablesPhase 判斷某 phase 是否啟用於 profile 的 Phases 列表中。
func (pc ProfileConfig) EnablesPhase(phase Phase) bool {
	_, ok := pc.phaseSpec(phase)
	return ok
}

// EnablesRole 判斷某 role 是否啟用：把 role 映射到工作 phase，再檢查該 phase 是否在 Phases 中。
// 無對應 phase 的 role（如 deep-reviewing 內的子 role）一律回 false。
func (pc ProfileConfig) EnablesRole(role Role) bool {
	phase := roleToPhase(role)
	if phase == "" {
		return false
	}
	return pc.EnablesPhase(phase)
}
