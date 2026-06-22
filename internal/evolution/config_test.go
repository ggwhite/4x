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

func intPtr(n int) *int { return &n }

// TestResolveEvolution_MaxIdleRounds 驗證 nil/0/負數/正數四種輸入的 resolve 結果（AC-10、L013）。
func TestResolveEvolution_MaxIdleRounds(t *testing.T) {
	cases := []struct {
		name string
		in   *int
		want int
	}{
		{"nil 套預設 3", nil, 3},
		{"明確 0 停用", intPtr(0), 0},
		{"負數停用", intPtr(-1), -1},
		{"正數照值", intPtr(5), 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := protocol.Config{Evolution: &protocol.EvolutionConfig{MaxIdleRounds: tc.in}}
			got := ResolveEvolution(cfg).MaxIdleRounds
			if got != tc.want {
				t.Errorf("MaxIdleRounds = %d, want %d", got, tc.want)
			}
		})
	}
}
