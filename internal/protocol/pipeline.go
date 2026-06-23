package protocol

import "github.com/ggwhite/4x/internal/feature"

// ResolvedPhase 描述 pipeline 中某個啟用 phase 解析後的最終結果：
// 合併所有覆寫層級（per-phase 臨時覆寫 > --runner > feature YAML > profile > roles > default）
// 後，該 phase 實際會用哪個 role / runner / model。供 dashboard pipeline 預覽顯示，
// 也作為「preview 與實際 run loop 解析一致」的共用真相源。
type ResolvedPhase struct {
	// Phase 是 canonical pipeline 中的 phase 名稱（designing/coding/...）。
	Phase string `json:"phase"`
	// Role 是該 phase 對應的 role（designer/coder/...）。
	Role string `json:"role"`
	// Runner 是解析後實際使用的 runner 名稱。
	Runner string `json:"runner"`
	// Model 是解析後實際使用的 model name（非 tier，已透過 runner tiers 解析）。
	Model string `json:"model"`
}

// EffectiveManual 計算某 phase 的「有效手動覆寫」：把本次 run 的 per-phase 臨時覆寫
// （runOverrides，覆寫優先序最高層）疊在全域 manualRunner（--runner）之上。
//
//   - runnerManual：該 phase 有覆寫且 Runner 非空時取覆寫值，否則沿用 manualRunner。
//   - modelManual：該 phase 有覆寫且 Model 非空時取覆寫值，否則為空（沿用下層解析）。
//
// 計算結果作為 ResolvePhaseRunner / ResolvePhaseModel 的 manual 參數傳入，
// 不改動 F090 既有的解析語意——只是在最高層多疊一層臨時覆寫。
func EffectiveManual(runOverrides map[Phase]PhaseSpec, phase Phase, manualRunner string) (runnerManual, modelManual string) {
	runnerManual = manualRunner
	if spec, ok := runOverrides[phase]; ok {
		if spec.Runner != "" {
			runnerManual = spec.Runner
		}
		if spec.Model != "" {
			modelManual = spec.Model
		}
	}
	return runnerManual, modelManual
}

// ResolvePipeline 解析給定 profile + 臨時覆寫下的完整 pipeline，回傳依 canonical 順序、
// 僅含 profile 啟用 phase 的 []ResolvedPhase。dashboard preview 與實際 run loop 共用此解析
// 路徑（透過 EffectiveManual 與 ResolvePhaseRunner / ResolvePhaseModel），確保預覽結果與
// 實際執行採用的 runner/model 完全一致。
//
// profileName 為空時走 ResolveProfile 既有 fallback（feature YAML / default_profile /
// priority auto-select）。任一 phase 的 runner/model 解析失敗即回 error。
func ResolvePipeline(cfg Config, f feature.Feature, profileName string, manualRunner string, runOverrides map[Phase]PhaseSpec) ([]ResolvedPhase, error) {
	_, pc, err := ResolveProfile(cfg, f, profileName)
	if err != nil {
		return nil, err
	}

	var resolved []ResolvedPhase
	for _, phase := range SelectablePhases() {
		if !pc.EnablesPhase(phase) {
			continue
		}
		role := PhaseRole(phase)
		runnerManual, modelManual := EffectiveManual(runOverrides, phase, manualRunner)

		runnerName, err := ResolvePhaseRunner(cfg, f, pc, phase, runnerManual)
		if err != nil {
			return nil, err
		}

		var model string
		if phase == PhaseDeepReviewing {
			// deep-reviewing 的 model 由 reviewer role 的 deep_model 解析（與 cmd/4x runDeepReviewPhase
			// 完全一致），不走 regular model tier；per-phase model 覆寫對此 phase 不適用（run loop 也忽略），
			// 故傳 runnerName 而非套用 modelManual，確保 preview 與實際執行的 deep model 結果相同。
			model, err = ResolveDeepModel(cfg, runnerName, RoleReviewer)
			if err == nil && model == "" {
				model, _ = ResolveTierModel(cfg, runnerName, DefaultDeepTier)
			}
		} else {
			model, err = ResolvePhaseModel(cfg, f, pc, phase, role, runnerName, modelManual)
		}
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, ResolvedPhase{
			Phase:  string(phase),
			Role:   string(role),
			Runner: runnerName,
			Model:  model,
		})
	}
	return resolved, nil
}
