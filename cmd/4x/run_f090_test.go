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

func TestProfileOptions(t *testing.T) {
	cfg := protocol.Config{Profiles: protocol.DefaultProfiles()}
	opts := profileOptions(cfg)
	if len(opts) < 3 {
		t.Fatalf("expected at least 3 options, got %d", len(opts))
	}
	if opts[0] != "full" || opts[1] != "normal" || opts[2] != "quick" {
		t.Errorf("unexpected order: %v", opts)
	}
}

func TestProfileOptions_CustomAppended(t *testing.T) {
	cfg := protocol.Config{Profiles: map[string]protocol.ProfileConfig{
		"my-custom": {Phases: []protocol.PhaseSpec{{Phase: "coding"}}},
	}}
	opts := profileOptions(cfg)
	found := false
	for _, o := range opts {
		if o == "my-custom" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom profile not in options: %v", opts)
	}
}

func TestResolveProfile_FeatureYAML(t *testing.T) {
	cfg := protocol.Config{
		Profiles:       protocol.DefaultProfiles(),
		DefaultProfile: "full",
	}
	f := feature.Feature{ID: "feat-x", Profile: "quick"}
	name, _, err := protocol.ResolveProfile(cfg, f, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "quick" {
		t.Errorf("expected quick from feature YAML, got %q", name)
	}
}

func TestResolveProfile_FlagOverridesFeatureYAML(t *testing.T) {
	cfg := protocol.Config{
		Profiles:       protocol.DefaultProfiles(),
		DefaultProfile: "full",
	}
	f := feature.Feature{ID: "feat-x", Profile: "quick"}
	name, _, err := protocol.ResolveProfile(cfg, f, "normal")
	if err != nil {
		t.Fatal(err)
	}
	if name != "normal" {
		t.Errorf("expected normal from flag, got %q", name)
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

	if err := runLoop(context.Background(), ws, ws, f, cfg, s, nil, factory, "never", "", nil); err != nil {
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

// TestRunLoop_ManualRunnerModelResolves 驗證 --runner 手動指定 runner 時，
// model 仍能正確解析。回歸測試：先前 run loop 誤把 manualRunner（runner 名稱）
// 當作 ResolvePhaseModel 的 manual model tier，導致以 --runner 啟動時 model 解析必然失敗。
func TestRunLoop_ManualRunnerModelResolves(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "manualrunner"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "echo"},
			"codex":  {Command: "echo"},
		},
		// 僅定義 sonnet/opus tier；若 manualRunner（"codex"）被誤當 tier 查詢必報錯。
		ModelTiers: map[string]map[string]string{
			"sonnet": {"claude": "claude-sonnet", "codex": "gpt"},
			"opus":   {"claude": "claude-opus", "codex": "gpt"},
		},
		Profiles: map[string]protocol.ProfileConfig{
			"custom": {Phases: []protocol.PhaseSpec{
				{Phase: string(protocol.PhaseCoding)},
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
	if err := ws.InitFeatureDir("feat-mr"); err != nil {
		t.Fatal(err)
	}
	f := feature.Feature{ID: "feat-mr", Name: "Manual runner", Status: "not-started"}
	ws.SaveFeature(f)

	s := protocol.State{
		FeatureID: "feat-mr", Phase: protocol.PhaseInit,
		MaxRounds: 5, Active: true, Runner: "codex", Profile: "custom",
	}
	ws.WriteState("feat-mr", s)

	mock := &mockRunner{ws: ws, featureID: "feat-mr", outcomes: []mockOutcome{
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

	// manualRunner="codex"（模擬 --runner codex）：每個 phase 都應用 codex 且 model 解析成功。
	if err := runLoop(context.Background(), ws, ws, f, cfg, s, nil, factory, "never", "codex", nil); err != nil {
		t.Fatalf("runLoop error: %v", err)
	}

	for i, ph := range mock.phases {
		if gotRunners[i] != "codex" {
			t.Errorf("phase %s used runner %q, want codex", ph, gotRunners[i])
		}
	}
}
