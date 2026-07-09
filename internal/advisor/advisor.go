// Package advisor 實作 F166 profile 建議的 deterministic heuristic 純函式（無 LLM）。
// 依 feature 的結構性訊號（subtask 數、repo 數、description 長度、priority、是否含 refactor
// 關鍵字）計算一個 profile 建議與逐項理由。純建議、不強制；權重與門檻由 protocol.Config
// 的 ProfileAdvisor 區段可調。本套件 import protocol 與 feature，不得反向被 protocol import。
package advisor

import (
	"fmt"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// 預設值——ProfileAdvisorConfig 對應欄位為 nil/空時套用。
const (
	defaultSubtaskPoints   = 2
	defaultRepoPoints      = 3
	defaultDescBucketRunes = 300
	defaultPriorityWeight  = 2
	defaultRefactorPoints  = -4
	defaultHeavyMinScore   = 8
	defaultMediumMinScore  = 3

	defaultHeavyProfile  = "full"
	defaultMediumProfile = "normal"
	defaultLightProfile  = "quick"
)

// DefaultRefactorKeywords 回傳內建 refactor 關鍵字清單（全小寫，供比對用）。
// 命中任一即視為 refactor 類 feature，傾向建議更精簡的 profile。
func DefaultRefactorKeywords() []string {
	return []string{
		"refactor",
		"重構",
		"rename",
		"改名",
		"拆分",
		"搬遷",
		"下沉",
		"god object",
		"extract",
		"move",
	}
}

// ResolvedConfig 為填妥預設值後的 profile advisor 設定，供 Recommend 直接取用。
type ResolvedConfig struct {
	// Enabled 為 false 時 Recommend 一律回 ok=false，不產生任何建議。
	Enabled bool
	// SubtaskPoints 為每個 subtask 加的分數（可為 0）。
	SubtaskPoints int
	// RepoPoints 為每多一個 repo（超過第一個）加的分數（可為 0）。
	RepoPoints int
	// DescBucketRunes 為 description 每滿 N rune 加 1 分的桶大小；<=0 時 desc 訊號視為 0（不除以 0）。
	DescBucketRunes int
	// PriorityWeight 為 priority 分數權重（可為 0）。
	PriorityWeight int
	// RefactorPoints 為命中 refactor 關鍵字時加的分數（預設負值，可為 0）。
	RefactorPoints int
	// RefactorKeywords 為判定 refactor 類 feature 的關鍵字清單（全小寫）。
	RefactorKeywords []string
	// HeavyMinScore 為建議 HeavyProfile 的最低分數門檻。
	HeavyMinScore int
	// MediumMinScore 為建議 MediumProfile 的最低分數門檻（否則 LightProfile）。
	MediumMinScore int
	// HeavyProfile / MediumProfile / LightProfile 為三個 tier 對應的 profile 名稱。
	HeavyProfile  string
	MediumProfile string
	LightProfile  string
}

// ResolveConfig 把 cfg.ProfileAdvisor 的 nil/空值欄位補上預設值後回傳一個全部填妥的結構。
// cfg.ProfileAdvisor 為 nil 時等同全部套預設。各 *int 欄位一律「nil→預設、非 nil→照值（含明確 0）」（DR-4）。
func ResolveConfig(cfg protocol.Config) ResolvedConfig {
	c := protocol.ProfileAdvisorConfig{}
	if cfg.ProfileAdvisor != nil {
		c = *cfg.ProfileAdvisor
	}

	rc := ResolvedConfig{
		Enabled:          true,
		SubtaskPoints:    defaultSubtaskPoints,
		RepoPoints:       defaultRepoPoints,
		DescBucketRunes:  defaultDescBucketRunes,
		PriorityWeight:   defaultPriorityWeight,
		RefactorPoints:   defaultRefactorPoints,
		RefactorKeywords: DefaultRefactorKeywords(),
		HeavyMinScore:    defaultHeavyMinScore,
		MediumMinScore:   defaultMediumMinScore,
		HeavyProfile:     defaultHeavyProfile,
		MediumProfile:    defaultMediumProfile,
		LightProfile:     defaultLightProfile,
	}

	// Enabled 用 sentinel pointer：nil→啟用，非 nil→照值。
	if c.Enabled != nil {
		rc.Enabled = *c.Enabled
	}
	// 各 *int 一律 nil→預設、非 nil→照值（含明確 0）。
	if c.SubtaskPoints != nil {
		rc.SubtaskPoints = *c.SubtaskPoints
	}
	if c.RepoPoints != nil {
		rc.RepoPoints = *c.RepoPoints
	}
	if c.DescBucketRunes != nil {
		rc.DescBucketRunes = *c.DescBucketRunes
	}
	if c.PriorityWeight != nil {
		rc.PriorityWeight = *c.PriorityWeight
	}
	if c.RefactorPoints != nil {
		rc.RefactorPoints = *c.RefactorPoints
	}
	if len(c.RefactorKeywords) > 0 {
		rc.RefactorKeywords = c.RefactorKeywords
	}
	if c.HeavyMinScore != nil {
		rc.HeavyMinScore = *c.HeavyMinScore
	}
	if c.MediumMinScore != nil {
		rc.MediumMinScore = *c.MediumMinScore
	}
	if c.HeavyProfile != "" {
		rc.HeavyProfile = c.HeavyProfile
	}
	if c.MediumProfile != "" {
		rc.MediumProfile = c.MediumProfile
	}
	if c.LightProfile != "" {
		rc.LightProfile = c.LightProfile
	}

	return rc
}

// Signals 為從 feature 萃取出的 deterministic 結構性訊號，供計分使用。
type Signals struct {
	// SubtaskCount 為 subtask 數量。
	SubtaskCount int
	// RepoCount 為 repo 數量（DR-1：以 len(f.Repos) 作為 repo 廣度訊號）。
	RepoCount int
	// DescRunes 為 description 的 rune 數（CJK-safe，非 byte 數）。
	DescRunes int
	// Priority 直接帶入 f.Priority（nil 表示未設定）。
	Priority *int
	// RefactorKeyword 為 name+description 小寫後命中的第一個 refactor 關鍵字；無命中為空。
	RefactorKeyword string
}

// Extract 從 feature 依 ResolvedConfig 萃取出計分所需的 deterministic 訊號。
func Extract(f feature.Feature, rc ResolvedConfig) Signals {
	sig := Signals{
		SubtaskCount: len(f.Subtasks),
		RepoCount:    len(f.Repos),
		DescRunes:    len([]rune(f.Description)),
		Priority:     f.Priority,
	}

	haystack := strings.ToLower(f.Name + " " + f.Description)
	for _, kw := range rc.RefactorKeywords {
		if kw == "" {
			continue
		}
		if strings.Contains(haystack, strings.ToLower(kw)) {
			sig.RefactorKeyword = kw
			break
		}
	}

	return sig
}

// Recommendation 為 profile 建議結果：建議的 profile 名稱、總分、以及逐項人類可讀理由。
type Recommendation struct {
	// Profile 為建議的 profile 名稱。
	Profile string
	// Score 為 heuristic 計算出的總分。
	Score int
	// Reasons 為逐項貢獻的繁中理由（含結尾的總分→建議行）。
	Reasons []string
}

// Recommend 依 cfg 的 heuristic 設定對 feature 計算 profile 建議。
// 回傳 (rec, true) 表示有建議可印；(Recommendation{}, false) 表示不應印任何建議
// （advisor 停用、或建議的 profile 名稱不存在於 cfg.Profiles ∪ DefaultProfiles，DR-5）。
func Recommend(cfg protocol.Config, f feature.Feature) (Recommendation, bool) {
	rc := ResolveConfig(cfg)
	if !rc.Enabled {
		return Recommendation{}, false
	}

	sig := Extract(f, rc)

	var score int
	var reasons []string

	// subtask 訊號
	if sub := rc.SubtaskPoints * sig.SubtaskCount; sub != 0 {
		score += sub
		reasons = append(reasons, fmt.Sprintf("%d 個 subtask（%+d）", sig.SubtaskCount, sub))
	}

	// repo 訊號：只計超過第一個的部分
	if extraRepos := sig.RepoCount - 1; extraRepos > 0 {
		if rep := rc.RepoPoints * extraRepos; rep != 0 {
			score += rep
			reasons = append(reasons, fmt.Sprintf("跨 %d 個 repo（%+d）", sig.RepoCount, rep))
		}
	}

	// desc 訊號：DescBucketRunes <= 0 時視為 0（避免除以 0）
	if rc.DescBucketRunes > 0 {
		if d := sig.DescRunes / rc.DescBucketRunes; d != 0 {
			score += d
			reasons = append(reasons, fmt.Sprintf("description 約 %d 字（%+d）", sig.DescRunes, d))
		}
	}

	// priority 訊號：nil 視為 p=0（比照 autoSelectProfile nil→full 語意，最重要）
	p := 0
	if sig.Priority != nil {
		p = clamp(*sig.Priority, 0, 3)
	}
	if pr := rc.PriorityWeight * (2 - p); pr != 0 {
		score += pr
		if sig.Priority != nil {
			reasons = append(reasons, fmt.Sprintf("priority %d（%+d）", *sig.Priority, pr))
		} else {
			reasons = append(reasons, fmt.Sprintf("未設 priority（%+d）", pr))
		}
	}

	// refactor 訊號
	if sig.RefactorKeyword != "" && rc.RefactorPoints != 0 {
		score += rc.RefactorPoints
		reasons = append(reasons, fmt.Sprintf("命中 refactor 關鍵字「%s」（%+d）", sig.RefactorKeyword, rc.RefactorPoints))
	}

	// 分數→profile 映射
	var profile string
	switch {
	case score >= rc.HeavyMinScore:
		profile = rc.HeavyProfile
	case score >= rc.MediumMinScore:
		profile = rc.MediumProfile
	default:
		profile = rc.LightProfile
	}

	// DR-5：建議的 profile 名稱必須存在於 cfg.Profiles ∪ DefaultProfiles，否則不印任何建議。
	if !profileExists(cfg, profile) {
		return Recommendation{}, false
	}

	reasons = append(reasons, fmt.Sprintf("總分 %d → 建議 profile: %s", score, profile))

	return Recommendation{Profile: profile, Score: score, Reasons: reasons}, true
}

// Render 把 Recommendation 格式化成供 CLI 印出的多行字串（兩個注入點共用同一 formatter）。
func Render(featureID string, rec Recommendation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "💡 profile 建議：%s（未指定 --profile；此為建議，不強制）\n", rec.Profile)
	b.WriteString("   理由：\n")
	for _, r := range rec.Reasons {
		fmt.Fprintf(&b, "     - %s\n", r)
	}
	fmt.Fprintf(&b, "   採用：4x run %s --profile %s\n", featureID, rec.Profile)
	return b.String()
}

// profileExists 回報 name 是否存在於 cfg.Profiles ∪ protocol.DefaultProfiles()。
func profileExists(cfg protocol.Config, name string) bool {
	if _, ok := cfg.Profiles[name]; ok {
		return true
	}
	if _, ok := protocol.DefaultProfiles()[name]; ok {
		return true
	}
	return false
}

// clamp 把 v 限制在 [lo, hi] 區間內。
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
