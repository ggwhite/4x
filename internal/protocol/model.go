package protocol

import "fmt"

const defaultTier = "sonnet"

// defaultMaxFixRounds 是 deep-reviewing phase 內自癒循環的預設最大迭代次數。
const defaultMaxFixRounds = 2

// ResolveMaxFixRounds 解析 deep-reviewing phase 自癒循環的最大修正輪數。
// 讀取 cfg.Roles[role].MaxFixRounds（role 一般為 RoleDeepReviewer）；
// 未設定或 <= 0 時回傳預設 defaultMaxFixRounds（2）。
func ResolveMaxFixRounds(cfg Config, role Role) int {
	if rc, ok := cfg.Roles[string(role)]; ok && rc.MaxFixRounds > 0 {
		return rc.MaxFixRounds
	}
	return defaultMaxFixRounds
}

// ResolveParallelReviewers 解析平行 deep review 要 spawn 的 sub-reviewer 數量。
// 讀取 cfg.Roles[role].ParallelReviewers（role 一般為 RoleDeepReviewer）；
// 未設定或 <= 0 時回傳 1，代表 fallback 單 agent 模式（不分檔、不跑 synthesizer）。
func ResolveParallelReviewers(cfg Config, role Role) int {
	if rc, ok := cfg.Roles[string(role)]; ok && rc.ParallelReviewers > 0 {
		return rc.ParallelReviewers
	}
	return 1
}

// ResolveAnglesPerReviewer 解析每個 sub-reviewer 負責的 review angle 數量。
// 讀取 cfg.Roles[role].AnglesPerReviewer（role 一般為 RoleDeepReviewer）；
// 未設定或 <= 0 時回傳 0，由 GroupReviewAngles 改用 ceil(總 angle/N) 平均分配。
func ResolveAnglesPerReviewer(cfg Config, role Role) int {
	if rc, ok := cfg.Roles[string(role)]; ok && rc.AnglesPerReviewer > 0 {
		return rc.AnglesPerReviewer
	}
	return 0
}

// GroupReviewAngles 把 1..totalAngles 的 review angle 切分給 parallelReviewers 個 sub-reviewer，
// 回傳每個 sub-reviewer 負責的 angle 編號清單（1-based、連續、不重複、完整覆蓋）。
//
// 切分規則：anglesPerReviewer > 0 時以它為每組固定大小依序切（最後一組可能較少）；
// anglesPerReviewer <= 0 時用 ceil(totalAngles/parallelReviewers) 平均分配。
// parallelReviewers <= 1 或 totalAngles <= 0 時回傳 nil（由 caller 走 fallback 單 agent）。
// 實際產生的非空組數可能少於 parallelReviewers（angle 不夠分時）。
func GroupReviewAngles(parallelReviewers, anglesPerReviewer, totalAngles int) [][]int {
	if parallelReviewers <= 1 || totalAngles <= 0 {
		return nil
	}
	size := anglesPerReviewer
	if size <= 0 {
		size = (totalAngles + parallelReviewers - 1) / parallelReviewers
	}
	if size <= 0 {
		size = 1
	}
	var groups [][]int
	for start := 1; start <= totalAngles; start += size {
		end := start + size
		if end > totalAngles+1 {
			end = totalAngles + 1
		}
		group := make([]int, 0, end-start)
		for a := start; a < end; a++ {
			group = append(group, a)
		}
		groups = append(groups, group)
	}
	return groups
}

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

	return resolveTierModel(cfg, runnerName, tier)
}

// resolveTierModel 把抽象 tier 解析為指定 runner 認識的實際 model name。
// 優先序：runners[name].tiers[tier] > model_tiers[tier][runner] > error。
// 抽出供 ResolveModel 與 ResolvePhaseModel 共用，確保 tier→model 的解析語意一致。
func resolveTierModel(cfg Config, runnerName, tier string) (string, error) {
	runnerCfg := cfg.Runners[runnerName]
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
