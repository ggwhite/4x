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
)

// ResolvedEvolution 為填妥預設值後的 evolution 設定，供 veto 邏輯直接取用。
type ResolvedEvolution struct {
	ValueFloor       float64
	MaxAcceptPerRun  int
	MaxBacklogUndone int
	GateRunner       string
	GateModel        string
	DedupThreshold   float64
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
	return r
}
