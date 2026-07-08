// Package evolution 實作 F097 evolve 價值閘門的 deterministic veto 純函式（無 LLM）。
// candidate 型別一律重用 internal/protocol，本套件不另造平行型別。
package evolution

import "github.com/ggwhite/4x/internal/protocol"

// 預設值——EvolutionConfig 對應欄位為零值時套用。
const (
	defaultValueFloor       = 0.6
	defaultMaxAcceptPerRun  = 3
	defaultMaxBacklogUndone = 15
	defaultDedupThreshold   = 0.6
	defaultMaxIdleRounds    = 3

	defaultCandidateMaxIdleDays = 30
)

// ResolvedEvolution 為填妥預設值後的 evolution 設定，供 veto 邏輯直接取用。
type ResolvedEvolution struct {
	ValueFloor       float64
	MaxAcceptPerRun  int
	MaxBacklogUndone int
	GateRunner       string
	GateModel        string
	DedupThreshold   float64
	// MaxIdleRounds 為 anti-spin 早退門檻：<= 0 表示停用（永遠跑），正數才啟用。
	// 來源 EvolutionConfig.MaxIdleRounds 為 nil 時套預設 3，非 nil 照值（含明確的 0/負數）。
	MaxIdleRounds int
	// CandidateMaxIdleDays 為 candidate 老化門檻：來源 EvolutionConfig.CandidateMaxIdleDays 為 nil 時
	// 套預設 30，非 nil 照值（含明確的 0=停用老化）。
	CandidateMaxIdleDays int
}

// ResolveEvolution 把 cfg.Evolution 的零值數值欄位補上預設值後回傳。
// cfg.Evolution 為 nil 時等同全部套預設；GateRunner/GateModel 不補預設（空字串保留）。
func ResolveEvolution(cfg protocol.Config) ResolvedEvolution {
	e := protocol.EvolutionConfig{}
	if cfg.Evolution != nil {
		e = *cfg.Evolution
	}
	r := ResolvedEvolution{
		ValueFloor:       e.ValueFloor,
		MaxAcceptPerRun:  e.MaxAcceptPerRun,
		MaxBacklogUndone: e.MaxBacklogUndone,
		GateRunner:       e.GateRunner,
		GateModel:        e.GateModel,
		DedupThreshold:   e.DedupThreshold,
	}
	if r.ValueFloor == 0 {
		r.ValueFloor = defaultValueFloor
	}
	if r.MaxAcceptPerRun == 0 {
		r.MaxAcceptPerRun = defaultMaxAcceptPerRun
	}
	if r.MaxBacklogUndone == 0 {
		r.MaxBacklogUndone = defaultMaxBacklogUndone
	}
	if r.DedupThreshold == 0 {
		r.DedupThreshold = defaultDedupThreshold
	}
	// MaxIdleRounds 用 sentinel pointer 區分「未設」與「設為 0」（L013）：
	// nil → 預設 3；非 nil → 照值（含明確的 0/負數，代表停用 halt）。
	if e.MaxIdleRounds == nil {
		r.MaxIdleRounds = defaultMaxIdleRounds
	} else {
		r.MaxIdleRounds = *e.MaxIdleRounds
	}
	// CandidateMaxIdleDays 同樣用 sentinel pointer：nil → 預設 30；非 nil → 照值（含明確的 0=停用）。
	if e.CandidateMaxIdleDays == nil {
		r.CandidateMaxIdleDays = defaultCandidateMaxIdleDays
	} else {
		r.CandidateMaxIdleDays = *e.CandidateMaxIdleDays
	}
	return r
}
