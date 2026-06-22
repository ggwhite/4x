package evolution

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestResolveEvolution_Defaults(t *testing.T) {
	got := ResolveEvolution(protocol.Config{})
	if got.ValueFloor != 0.6 {
		t.Errorf("ValueFloor = %v, want 0.6", got.ValueFloor)
	}
	if got.MaxAcceptPerRun != 3 {
		t.Errorf("MaxAcceptPerRun = %v, want 3", got.MaxAcceptPerRun)
	}
	if got.MaxBacklogUndone != 15 {
		t.Errorf("MaxBacklogUndone = %v, want 15", got.MaxBacklogUndone)
	}
	if got.DedupThreshold != 0.6 {
		t.Errorf("DedupThreshold = %v, want 0.6", got.DedupThreshold)
	}
}

func TestResolveEvolution_Overrides(t *testing.T) {
	cfg := protocol.Config{Evolution: &protocol.EvolutionConfig{
		ValueFloor: 0.8, MaxAcceptPerRun: 1, MaxBacklogUndone: 5,
		GateRunner: "gemini", GateModel: "flash", DedupThreshold: 0.5,
	}}
	got := ResolveEvolution(cfg)
	if got.ValueFloor != 0.8 || got.MaxAcceptPerRun != 1 || got.MaxBacklogUndone != 5 || got.DedupThreshold != 0.5 {
		t.Errorf("numeric overrides not applied: %+v", got)
	}
	if got.GateRunner != "gemini" || got.GateModel != "flash" {
		t.Errorf("runner/model overrides not applied: %+v", got)
	}
}
