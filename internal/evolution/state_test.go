package evolution

import (
	"path/filepath"
	"testing"
)

func TestLoadEvolveState_Missing(t *testing.T) {
	got, err := LoadEvolveState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if got.Round != 0 || got.ConsecutiveNoAccept != 0 {
		t.Errorf("missing file should be zero value, got %+v", got)
	}
}

func TestEvolveState_SaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), protocolEvolveStateFile)
	want := EvolveState{Version: 1, Round: 4, ConsecutiveNoAccept: 2}
	if err := want.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadEvolveState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Round != 4 || got.ConsecutiveNoAccept != 2 || got.Version != 1 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

// TestEvolveState_ShouldHalt 涵蓋 AC-8/AC-10：counter 遞增語意與 maxIdle 停用/啟用門檻。
func TestEvolveState_ShouldHalt(t *testing.T) {
	cases := []struct {
		name    string
		counter int
		maxIdle int
		want    bool
	}{
		{"未達門檻", 2, 3, false},
		{"剛好達門檻", 3, 3, true},
		{"超過門檻", 4, 3, true},
		{"maxIdle 0 停用", 100, 0, false},
		{"maxIdle 負數停用", 100, -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := EvolveState{ConsecutiveNoAccept: tc.counter}
			if got := s.ShouldHalt(tc.maxIdle); got != tc.want {
				t.Errorf("ShouldHalt(%d) with counter %d = %v, want %v", tc.maxIdle, tc.counter, got, tc.want)
			}
		})
	}
}

const protocolEvolveStateFile = "evolve-state.json"
