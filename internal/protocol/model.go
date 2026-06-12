package protocol

import "fmt"

const defaultTier = "sonnet"

// ResolveModel 根據抽象 tier 解析出指定 runner 認識的 model name。
// 優先序：runners[name].tiers[tier] > model_tiers[tier][runner] > error。
// 若 tier 在兩處都找不到對應，回傳 error 而非 pass through tier name。
func ResolveModel(cfg Config, runnerName string, role Role) (string, error) {
	runnerCfg := cfg.Runners[runnerName]

	tier := ""
	if rc, ok := cfg.Roles[string(role)]; ok {
		tier = rc.Model
	}
	if tier == "" {
		tier = runnerCfg.Model
	}
	if tier == "" {
		tier = defaultTier
	}

	if model, ok := runnerCfg.Tiers[tier]; ok {
		return model, nil
	}
	if tierMap, ok := cfg.ModelTiers[tier]; ok {
		if model, ok := tierMap[runnerName]; ok {
			return model, nil
		}
	}

	return "", fmt.Errorf("runner %q has no model for tier %q", runnerName, tier)
}

// ResolveDeepModel 解析 role 的 deep_model tier，回傳 runner 認識的 model name。
// 若 role 未設 deep_model，回傳空字串與 nil error（表示不需要 deep model）。
func ResolveDeepModel(cfg Config, runnerName string, role Role) (string, error) {
	rc, ok := cfg.Roles[string(role)]
	if !ok || rc.DeepModel == "" {
		return "", nil
	}

	tier := rc.DeepModel
	runnerCfg := cfg.Runners[runnerName]

	if model, ok := runnerCfg.Tiers[tier]; ok {
		return model, nil
	}
	if tierMap, ok := cfg.ModelTiers[tier]; ok {
		if model, ok := tierMap[runnerName]; ok {
			return model, nil
		}
	}

	return "", fmt.Errorf("runner %q has no model for deep tier %q", runnerName, tier)
}
