package protocol

// AllPhases 回傳所有可持久化於 state.json 的 Phase 值（enum SoT）。
// 依 internal/state/machine.go 的 CanTransition 裁定：fixing 是合法 to-phase
// （deep-reviewing→fixing、fixing→{accepting,amending}），abandoned/done 為終態，
// blocked/needs-attention 為 universal target，皆可寫入 phase，故 15 個全含。
func AllPhases() []Phase {
	return []Phase{
		PhaseInit,
		PhaseDesigning,
		PhaseDesignReviewing,
		PhaseCoding,
		PhaseReviewing,
		PhaseDeepReviewing,
		PhaseFixing,
		PhaseTesting,
		PhaseAmending,
		PhaseAccepting,
		PhasePendingReview,
		PhaseDone,
		PhaseAbandoned,
		PhaseBlocked,
		PhaseNeedsAttention,
	}
}

// AllRoles 回傳所有可出現在 state.json.role 的 Role 值（enum SoT）。
// 即 internal/state/machine.go 的 PhaseToRole() 會指派（非空）的 8 個 pipeline 角色。
// 排除 sub-role（mini-coder/re-verifier/synthesizer，全程維持 deep-reviewing，僅用於
// event/log 辨識，不佔 state.role）與非 pipeline 角色（gate/consolidator/round-summarizer）。
func AllRoles() []Role {
	return []Role{
		RoleDesigner,
		RoleDesignReviewer,
		RoleCoder,
		RoleReviewer,
		RoleDeepReviewer,
		RoleTester,
		RoleAcceptor,
		RoleFixer,
	}
}

// ConfigurableRoles 回傳可在 settings.json roles 區塊設定 per-role model 的 Role 集合（白名單 SoT）。
// 即全部 pipeline 角色（AllRoles，含 fixer）再加上 deep-reviewing / reviewing 自癒循環的
// mini-coder 子 role——mini-coder 可透過 roles.mini-coder.model 做獨立 model 路由，故納入白名單。
// 其餘子 role（re-verifier/synthesizer）與非 pipeline 角色（gate/consolidator/round-summarizer）
// 目前不支援 per-role model 設定，故不列入。
func ConfigurableRoles() []Role {
	return append(AllRoles(), RoleMiniCoder)
}

// AllSubPhases 回傳全部 4 個 SubPhase 常量（enum SoT）。
func AllSubPhases() []SubPhase {
	return []SubPhase{
		SubPhaseReviewing,
		SubPhaseSynthesizing,
		SubPhaseFixing,
		SubPhaseReverifying,
	}
}

// AllEventTypes 回傳 production 實際發出的 event type 集合（enum SoT）。
// 以全庫 grep production 程式碼（Type: "..." composite literal 與
// pipeOutput(...,type) helper）為準，不以現有 schema enum 為準；schema 曾列出的
// phase-end/step/verify/scope-check/blocked/heartbeat/subtask-done 在 production
// 無任何 emitter，故不列入。
func AllEventTypes() []string {
	return []string{
		"conditional-pass-residual",
		"escalation",
		"feature-discovered",
		"force-done",
		"guard-fail",
		"health-check-failed",
		"hook",
		"phase-skipped",
		"phase-start",
		"run-end",
		"run-error",
		"run-output",
		"run-start",
		"self-mod-approved",
		"self-mod-detected",
		"transition",
	}
}

// AllNotifyLevels 回傳 Event.Notify 的合法值（enum SoT）。
func AllNotifyLevels() []string {
	return []string{
		NotifySuccess,
		NotifyError,
		NotifyWarning,
	}
}
