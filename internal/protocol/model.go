package protocol

import (
	"fmt"
	"sort"
	"strings"
)

// TierStrong 是廠牌無關的「強推理」canonical tier，語意為判斷密集 / deep review 場景，
// 是原 Claude 中心化命名 "opus" 的中性替代。舊別名 "opus" 仍透過 tierAliases 雙向相容。
const TierStrong = "strong"

// TierFast 是廠牌無關的「快速 / 量大」canonical tier，語意為例行、量大的 coding / review
// 場景，是原 Claude 中心化命名 "sonnet" 的中性替代。舊別名 "sonnet" 仍透過 tierAliases 雙向相容。
const TierFast = "fast"

// defaultTier 是 role / runner 均未指定 model 時的 fallback tier（例行場景）。
const defaultTier = TierFast

// DefaultDeepTier 是 deep_model 未設定時的 fallback tier（判斷密集場景）。
// full profile 包含 deep-reviewing，即使未明確設定 deep_model，
// 只要 runner 能解析此 tier 就會自動啟用 deep review。
const DefaultDeepTier = TierStrong

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

	return ResolveTierModel(cfg, runnerName, tier)
}

// ResolveMiniCoderModel 解析 deep-reviewing / reviewing 自癒循環中 mini-coder 使用的 model name。
// mini-coder 是 coding 性質的子 role，預設沿用 coder 已解析出的 model（fallbackModel），確保未特別
// 設定時行為不變；若使用者在 roles.mini-coder.model 明確設定 tier，則以該設定優先，讓 mini-coder 可
// 獨立於 coder 做 per-role model 路由。fallbackModel 由 caller 以 coder 的 ResolvePhaseModel 結果傳入。
func ResolveMiniCoderModel(cfg Config, runnerName, fallbackModel string) (string, error) {
	if rc, ok := cfg.Roles[string(RoleMiniCoder)]; ok && rc.Model != "" {
		return ResolveTierModel(cfg, runnerName, rc.Model)
	}
	return fallbackModel, nil
}

// tierAliases 回傳指定 tier 的向後相容別名清單，供 ResolveTierModel 在原名查無對應時依序 fallback。
// canonical 與舊 Claude 中心化命名雙向對應：strong↔opus、fast↔sonnet；其餘 tier 無別名，回傳 nil。
func tierAliases(tier string) []string {
	switch tier {
	case TierStrong:
		return []string{"opus"}
	case "opus":
		return []string{TierStrong}
	case TierFast:
		return []string{"sonnet"}
	case "sonnet":
		return []string{TierFast}
	default:
		return nil
	}
}

// ResolveTierModel 把抽象 tier 解析為指定 runner 認識的實際 model name。
// 查表順序：先以 tier 原名試 runners[name].tiers[tier] > model_tiers[tier][runner]，
// 再依序對 tierAliases(tier) 回傳的別名（strong↔opus、fast↔sonnet）重試同兩層。
// 原名的 exact match 一律優先於別名，確保既有 settings.json 與 --phase-override 呼叫方式無行為回歸；
// 查無任何名字才回 error（沿用「不 pass through tier name」語意）。
// 供 ResolveModel、ResolvePhaseModel 共用，也供 caller 對 DefaultDeepTier 做 fallback 解析。
func ResolveTierModel(cfg Config, runnerName, tier string) (string, error) {
	runnerCfg := cfg.Runners[runnerName]
	for _, name := range append([]string{tier}, tierAliases(tier)...) {
		if model, ok := runnerCfg.Tiers[name]; ok {
			return model, nil
		}
		if tierMap, ok := cfg.ModelTiers[name]; ok {
			if model, ok := tierMap[runnerName]; ok {
				return model, nil
			}
		}
	}
	return "", fmt.Errorf("runner %q has no model for tier %q", runnerName, tier)
}

// ResolveDeepModel 解析 role 的 deep_model tier，回傳 runner 認識的 model name。
// 若 role 未設 deep_model，回傳空字串與 nil error（表示不需要 deep model）。
// caller（如 runDeepReviewPhase）可自行用 ResolveTierModel + DefaultDeepTier 做 fallback。
func ResolveDeepModel(cfg Config, runnerName string, role Role) (string, error) {
	rc, ok := cfg.Roles[string(role)]
	if !ok || rc.DeepModel == "" {
		return "", nil
	}
	return ResolveTierModel(cfg, runnerName, rc.DeepModel)
}

// DefaultAngleMapping 回傳預設的 diff 路徑到 deep review angle 的映射表。
// key 為路徑前綴；以 "*" 開頭的 key 為後綴匹配（如 "*_test.go"）。
func DefaultAngleMapping() map[string][]int {
	return map[string][]int{
		"internal/state/":    {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		"internal/protocol/": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		"internal/guard/":    {1, 2, 3, 5, 6, 7, 8, 9, 10, 11},
		"internal/server/":   {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		"internal/":          {1, 2, 3, 5, 6, 7, 8, 9, 10, 11},
		"cmd/":               {1, 2, 3, 5, 6, 7, 8, 9, 10, 11},
		"templates/":         {1, 2, 8, 10},
		"plugins/":           {1, 2, 8, 10},
		"docs/":              {8, 10},
		"dashboard/":         {1, 2, 3, 5, 6, 7, 8, 10},
		"*_test.go":          {1, 5, 6, 8},
	}
}

// ResolveAngleMapping 從 config 讀取 deep-reviewer 的 angle mapping；
// 未設定時回傳 DefaultAngleMapping。
func ResolveAngleMapping(cfg Config) map[string][]int {
	if rc, ok := cfg.Roles[string(RoleDeepReviewer)]; ok && len(rc.AngleMapping) > 0 {
		return rc.AngleMapping
	}
	return DefaultAngleMapping()
}

// SelectDeepReviewAngles 根據變更檔案路徑和 mapping 表選出應執行的 deep review angle。
// 每個檔案匹配最長前綴（或後綴規則），取該規則的 angle 清單；
// 所有檔案的 angle 取聯集並升冪排序。無任何檔案匹配時回傳全部 1..DeepReviewAngleCount。
func SelectDeepReviewAngles(mapping map[string][]int, files []ChangedFile) ([]int, []AngleMatch) {
	seen := make(map[int]bool)
	var matches []AngleMatch

	for _, f := range files {
		rule, angles := matchAngleRule(mapping, f.Path)
		if rule == "" {
			continue
		}
		matches = append(matches, AngleMatch{File: f.Path, Rule: rule, Angles: angles})
		for _, a := range angles {
			seen[a] = true
		}
	}

	if len(seen) == 0 {
		all := make([]int, DeepReviewAngleCount)
		for i := range all {
			all[i] = i + 1
		}
		return all, nil
	}

	selected := make([]int, 0, len(seen))
	for a := range seen {
		selected = append(selected, a)
	}
	sort.Ints(selected)
	return selected, matches
}

// matchAngleRule 對單一檔案路徑找出最匹配的 mapping 規則。
// 前綴規則取最長匹配；後綴規則（key 以 "*" 開頭）取最長匹配；前綴優先於後綴。
func matchAngleRule(mapping map[string][]int, path string) (string, []int) {
	bestPrefix := ""
	var bestPrefixAngles []int
	bestSuffix := ""
	var bestSuffixAngles []int

	for pattern, angles := range mapping {
		if strings.HasPrefix(pattern, "*") {
			suffix := pattern[1:]
			if strings.HasSuffix(path, suffix) && len(suffix) > len(bestSuffix) {
				bestSuffix = suffix
				bestSuffixAngles = angles
			}
		} else {
			if strings.HasPrefix(path, pattern) && len(pattern) > len(bestPrefix) {
				bestPrefix = pattern
				bestPrefixAngles = angles
			}
		}
	}

	if bestPrefix != "" {
		return bestPrefix, bestPrefixAngles
	}
	if bestSuffix != "" {
		return "*" + bestSuffix, bestSuffixAngles
	}
	return "", nil
}

// AllAngles 回傳完整的 1..DeepReviewAngleCount angle 清單。
func AllAngles() []int {
	all := make([]int, DeepReviewAngleCount)
	for i := range all {
		all[i] = i + 1
	}
	return all
}

// GroupSelectedAngles 把指定的 angle 清單切分給 parallelReviewers 個 sub-reviewer，
// 回傳每個 sub-reviewer 負責的 angle 編號清單。
// parallelReviewers <= 1 或 angles 為空時回傳 nil（走 fallback 單 agent）。
func GroupSelectedAngles(parallelReviewers, anglesPerReviewer int, angles []int) [][]int {
	if parallelReviewers <= 1 || len(angles) == 0 {
		return nil
	}
	size := anglesPerReviewer
	if size <= 0 {
		size = (len(angles) + parallelReviewers - 1) / parallelReviewers
	}
	if size <= 0 {
		size = 1
	}
	var groups [][]int
	for i := 0; i < len(angles); i += size {
		end := i + size
		if end > len(angles) {
			end = len(angles)
		}
		group := make([]int, end-i)
		copy(group, angles[i:end])
		groups = append(groups, group)
	}
	return groups
}
