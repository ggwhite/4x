package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

func TestProfileOptions_OrderAndUnion(t *testing.T) {
	cfg := protocol.Config{Profiles: map[string]protocol.ProfileConfig{
		"zeta":   {Phases: []protocol.PhaseSpec{{Phase: string(protocol.PhaseCoding)}}},
		"alpha":  {Phases: []protocol.PhaseSpec{{Phase: string(protocol.PhaseCoding)}}},
		"normal": {Phases: []protocol.PhaseSpec{{Phase: string(protocol.PhaseCoding)}}}, // 覆寫內建名
	}}
	got := profileOptions(cfg)
	// 內建 full/normal/quick 在前（canonical 序），自訂 alpha/zeta 字母序在後。
	want := []string{"full", "normal", "quick", "alpha", "zeta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSelectProfileInteractive(t *testing.T) {
	cfg := protocol.Config{Profiles: protocol.DefaultProfiles(), DefaultProfile: "normal"}
	f := feature.Feature{ID: "feat-x"}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"enter uses default", "\n", "normal", false},
		{"explicit number", "3\n", "quick", false},
		{"first option", "1\n", "full", false},
		{"eof uses default", "", "normal", false},
		{"invalid number", "9\n", "", true},
		{"non-numeric", "abc\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			got, err := selectProfileInteractive(strings.NewReader(tt.input), &out, cfg, f)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRunLoop_PerPhaseRunner 驗證 run loop 依 profile 的 per-phase 覆寫，
// 不同 phase 使用不同 runner（coding→codex，其餘→default claude）。
func TestRunLoop_PerPhaseRunner(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "perphase"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "echo"},
			"codex":  {Command: "echo"},
		},
		ModelTiers: map[string]map[string]string{
			"sonnet": {"claude": "claude-sonnet", "codex": "gpt"},
			"opus":   {"claude": "claude-opus", "codex": "gpt"},
		},
		Profiles: map[string]protocol.ProfileConfig{
			"custom": {Phases: []protocol.PhaseSpec{
				{Phase: string(protocol.PhaseCoding), Runner: "codex"},
				{Phase: string(protocol.PhaseReviewing)},
				{Phase: string(protocol.PhaseTesting)},
				{Phase: string(protocol.PhaseAccepting)},
			}},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir("feat-pp"); err != nil {
		t.Fatal(err)
	}
	f := feature.Feature{ID: "feat-pp", Name: "Per-phase", Status: "not-started"}
	ws.SaveFeature(f)

	s := protocol.State{
		FeatureID: "feat-pp", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "claude", Profile: "custom",
	}
	ws.WriteState("feat-pp", s)

	mock := &mockRunner{ws: ws, featureID: "feat-pp", outcomes: []mockOutcome{
		{}, {reviewVerdict: "PASS"}, {testPassed: true}, {},
	}}

	var mu sync.Mutex
	var gotRunners []string
	factory := func(rn, _, _ string) runner.Runner {
		mu.Lock()
		gotRunners = append(gotRunners, rn)
		mu.Unlock()
		return mock
	}

	if err := runLoop(context.Background(), ws, ws, f, cfg, s, nil, factory, "never", ""); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	// mock.phases 與 gotRunners 依序對應（sequential pipeline，無平行）。
	if len(mock.phases) != len(gotRunners) {
		t.Fatalf("phase/runner length mismatch: %d vs %d", len(mock.phases), len(gotRunners))
	}
	for i, ph := range mock.phases {
		want := "claude"
		if ph == protocol.PhaseCoding {
			want = "codex"
		}
		if gotRunners[i] != want {
			t.Errorf("phase %s used runner %q, want %q", ph, gotRunners[i], want)
		}
	}
	// 至少要跑過 coding 才有意義。
	sawCoding := false
	for _, ph := range mock.phases {
		if ph == protocol.PhaseCoding {
			sawCoding = true
		}
		if ph == protocol.PhaseDesigning {
			t.Error("designer should be skipped (not in custom profile)")
		}
	}
	if !sawCoding {
		t.Error("expected coding phase to run")
	}
}
