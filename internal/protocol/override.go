package protocol

import (
	"fmt"

	"github.com/ggwhite/4x/internal/feature"
)

// ResolvePhaseRunner 依統一覆寫優先序解析某 phase 實際使用的 runner 名稱。
//
// 優先序（高→低）：
//   - manual：4x run 手動帶入（--runner flag），空字串代表未手動指定。
//   - f.PhaseOverrides[phase].Runner：feature YAML 對此 phase 的覆寫。
//   - profile 對應 PhaseSpec.Runner：profile 對此 phase 的覆寫。
//   - cfg.Roles[PhaseRole(phase)].Runner：settings.json 對此 role 的全程覆寫（跨 model 對抗審查）。
//   - cfg.Default：default_runner。
//
// 解析結果必須存在於 cfg.Runners，否則回 error。
func ResolvePhaseRunner(cfg Config, f feature.Feature, pc ProfileConfig, phase Phase, manual string) (string, error) {
	name := manual
	if name == "" {
		if ov, ok := f.PhaseOverrides[string(phase)]; ok && ov.Runner != "" {
			name = ov.Runner
		}
	}
	if name == "" {
		if spec, ok := pc.phaseSpec(phase); ok && spec.Runner != "" {
			name = spec.Runner
		}
	}
	if name == "" {
		role := PhaseRole(phase)
		if rc, ok := cfg.Roles[string(role)]; ok && rc.Runner != "" {
			name = rc.Runner
		}
	}
	if name == "" {
		name = cfg.Default
	}
	if name == "" {
		return "", fmt.Errorf("no runner resolved for phase %q (no --runner, feature override, profile, or default_runner)", phase)
	}
	if _, ok := cfg.Runners[name]; !ok {
		return "", fmt.Errorf("runner %q for phase %q not found in runners", name, phase)
	}
	return name, nil
}

// ResolvePhaseModel 依統一覆寫優先序解析某 phase 實際使用的 model name。
//
// tier 優先序（高→低）：
//   - manual：4x run 手動帶入的 model tier，空字串代表未手動指定。
//   - f.PhaseOverrides[phase].Model：feature YAML 對此 phase 的覆寫。
//   - profile 對應 PhaseSpec.Model：profile 對此 phase 的覆寫。
//   - 以上皆空 → fallback 回 ResolveModel（roles[role].Model → runner.Model → defaultTier，
//     即 canonical TierFast "fast"）。
//
// 取得 tier 後一律用 runner 的 tiers / model_tiers 解析為實際 model name；
// 解析不到回 error（沿用 ResolveModel 的錯誤語意）。
func ResolvePhaseModel(cfg Config, f feature.Feature, pc ProfileConfig, phase Phase, role Role, runnerName, manual string) (string, error) {
	tier := manual
	if tier == "" {
		if ov, ok := f.PhaseOverrides[string(phase)]; ok && ov.Model != "" {
			tier = ov.Model
		}
	}
	if tier == "" {
		if spec, ok := pc.phaseSpec(phase); ok && spec.Model != "" {
			tier = spec.Model
		}
	}
	if tier != "" {
		return ResolveTierModel(cfg, runnerName, tier)
	}
	return ResolveModel(cfg, runnerName, role)
}
