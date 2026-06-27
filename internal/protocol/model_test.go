package protocol

import "testing"

func TestResolveModel_RoleTier(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus", "gemini": "gemini-2.5-pro"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini"},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	got, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gemini-2.5-pro" {
		t.Errorf("got %q, want %q", got, "gemini-2.5-pro")
	}
}

func TestResolveModel_FallbackRunnerDefault(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Model: "opus"},
		},
		Roles: map[string]RoleConfig{
			"coder": {},
		},
	}
	got, err := ResolveModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "opus" {
		t.Errorf("got %q, want %q", got, "opus")
	}
}

func TestResolveModel_FallbackSonnet(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"sonnet": {"claude": "sonnet"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{},
	}
	got, err := ResolveModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "sonnet" {
		t.Errorf("got %q, want %q", got, "sonnet")
	}
}

func TestResolveModel_RunnerTiersOverride(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"gemini": "gemini-2.5-pro"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini", Tiers: map[string]string{"opus": "gemini-2.5-pro-preview"}},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	got, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gemini-2.5-pro-preview" {
		t.Errorf("got %q, want %q", got, "gemini-2.5-pro-preview")
	}
}

func TestResolveModel_NotFound(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"gemini": {Command: "gemini"},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	_, err := ResolveModel(cfg, "gemini", RoleDesigner)
	if err == nil {
		t.Error("expected error when tier mapping not found")
	}
}

func TestResolveDeepModel_Found(t *testing.T) {
	cfg := Config{
		ModelTiers: map[string]map[string]string{
			"opus": {"claude": "opus"},
		},
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{
			"reviewer": {Model: "sonnet", DeepModel: "opus"},
		},
	}
	got, err := ResolveDeepModel(cfg, "claude", RoleReviewer)
	if err != nil {
		t.Fatal(err)
	}
	if got != "opus" {
		t.Errorf("got %q, want %q", got, "opus")
	}
}

func TestResolveDeepModel_NoDeepModel(t *testing.T) {
	cfg := Config{
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]RoleConfig{
			"coder": {Model: "sonnet"},
		},
	}
	got, err := ResolveDeepModel(cfg, "claude", RoleCoder)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestDeepModelFallbackToDefaultTier(t *testing.T) {
	cfg := Config{
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{"opus": "opus"}},
		},
		Roles: map[string]RoleConfig{
			"reviewer": {Model: "sonnet"},
		},
	}
	// ResolveDeepModel 回傳空字串（reviewer 未設 deep_model）
	got, err := ResolveDeepModel(cfg, "claude", RoleReviewer)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("ResolveDeepModel got %q, want empty", got)
	}
	// caller 用 ResolveTierModel + DefaultDeepTier 做 fallback
	fallback, err := ResolveTierModel(cfg, "claude", DefaultDeepTier)
	if err != nil {
		t.Fatal(err)
	}
	if fallback != "opus" {
		t.Errorf("fallback got %q, want %q", fallback, "opus")
	}
}

func TestResolveMaxFixRounds_Default(t *testing.T) {
	cfg := Config{}
	if got := ResolveMaxFixRounds(cfg, RoleDeepReviewer); got != defaultMaxFixRounds {
		t.Errorf("got %d, want default %d", got, defaultMaxFixRounds)
	}
}

func TestResolveMaxFixRounds_Configured(t *testing.T) {
	cfg := Config{
		Roles: map[string]RoleConfig{
			"deep-reviewer": {MaxFixRounds: 5},
		},
	}
	if got := ResolveMaxFixRounds(cfg, RoleDeepReviewer); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestResolveMaxFixRounds_NonPositiveFallsBackToDefault(t *testing.T) {
	cfg := Config{
		Roles: map[string]RoleConfig{
			"deep-reviewer": {MaxFixRounds: 0},
		},
	}
	if got := ResolveMaxFixRounds(cfg, RoleDeepReviewer); got != defaultMaxFixRounds {
		t.Errorf("got %d, want default %d", got, defaultMaxFixRounds)
	}
}

func TestResolveParallelReviewers(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want int
	}{
		{"unset falls back to 1", Config{}, 1},
		{"configured 3", Config{Roles: map[string]RoleConfig{"deep-reviewer": {ParallelReviewers: 3}}}, 3},
		{"non-positive falls back to 1", Config{Roles: map[string]RoleConfig{"deep-reviewer": {ParallelReviewers: 0}}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveParallelReviewers(tt.cfg, RoleDeepReviewer); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResolveAnglesPerReviewer(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want int
	}{
		{"unset returns 0 (auto split)", Config{}, 0},
		{"configured 4", Config{Roles: map[string]RoleConfig{"deep-reviewer": {AnglesPerReviewer: 4}}}, 4},
		{"non-positive returns 0", Config{Roles: map[string]RoleConfig{"deep-reviewer": {AnglesPerReviewer: -1}}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveAnglesPerReviewer(tt.cfg, RoleDeepReviewer); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGroupReviewAngles(t *testing.T) {
	// flatten 把分組攤平成單一序列，方便驗證「完整覆蓋且不重複」。
	flatten := func(groups [][]int) []int {
		var out []int
		for _, g := range groups {
			out = append(out, g...)
		}
		return out
	}
	assertFullCoverage := func(t *testing.T, groups [][]int, total int) {
		t.Helper()
		seen := make(map[int]bool)
		for _, a := range flatten(groups) {
			if a < 1 || a > total {
				t.Fatalf("angle %d out of range 1..%d", a, total)
			}
			if seen[a] {
				t.Fatalf("angle %d appears more than once", a)
			}
			seen[a] = true
		}
		if len(seen) != total {
			t.Fatalf("coverage %d angles, want %d (groups=%v)", len(seen), total, groups)
		}
	}

	t.Run("fallback when parallelReviewers <= 1", func(t *testing.T) {
		if got := GroupReviewAngles(1, 0, 11); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("default 3 reviewers auto split of 11", func(t *testing.T) {
		got := GroupReviewAngles(3, 0, 11)
		want := [][]int{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11}}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if len(got[i]) != len(want[i]) {
				t.Fatalf("group %d = %v, want %v", i, got[i], want[i])
			}
			for j := range want[i] {
				if got[i][j] != want[i][j] {
					t.Fatalf("group %d = %v, want %v", i, got[i], want[i])
				}
			}
		}
		assertFullCoverage(t, got, 11)
	})

	t.Run("explicit anglesPerReviewer", func(t *testing.T) {
		got := GroupReviewAngles(4, 3, 11)
		// size=3 依序切：[1-3][4-6][7-9][10-11]
		if len(got) != 4 {
			t.Fatalf("got %d groups, want 4: %v", len(got), got)
		}
		assertFullCoverage(t, got, 11)
	})

	t.Run("more reviewers than angles still covers all", func(t *testing.T) {
		got := GroupReviewAngles(20, 0, 11)
		assertFullCoverage(t, got, 11)
	})
}

func TestSelectDeepReviewAngles(t *testing.T) {
	mapping := map[string][]int{
		"internal/state/": {1, 2, 3, 4},
		"internal/":       {1, 2, 3},
		"docs/":           {8, 10},
		"*_test.go":       {1, 5, 6},
	}

	t.Run("prefix match uses longest prefix", func(t *testing.T) {
		files := []ChangedFile{{Path: "internal/state/machine.go", Lines: 10}}
		angles, matches := SelectDeepReviewAngles(mapping, files)
		if len(angles) != 4 || angles[0] != 1 || angles[3] != 4 {
			t.Errorf("got %v, want [1 2 3 4]", angles)
		}
		if len(matches) != 1 || matches[0].Rule != "internal/state/" {
			t.Errorf("got matches %v, want rule=internal/state/", matches)
		}
	})

	t.Run("shorter prefix fallback", func(t *testing.T) {
		files := []ChangedFile{{Path: "internal/guard/check.go", Lines: 5}}
		angles, _ := SelectDeepReviewAngles(mapping, files)
		if len(angles) != 3 || angles[0] != 1 || angles[2] != 3 {
			t.Errorf("got %v, want [1 2 3]", angles)
		}
	})

	t.Run("suffix match", func(t *testing.T) {
		files := []ChangedFile{{Path: "pkg/foo_test.go", Lines: 20}}
		angles, matches := SelectDeepReviewAngles(mapping, files)
		if len(angles) != 3 || angles[0] != 1 || angles[1] != 5 || angles[2] != 6 {
			t.Errorf("got %v, want [1 5 6]", angles)
		}
		if len(matches) != 1 || matches[0].Rule != "*_test.go" {
			t.Errorf("got matches %v, want rule=*_test.go", matches)
		}
	})

	t.Run("prefix takes precedence over suffix", func(t *testing.T) {
		files := []ChangedFile{{Path: "internal/guard/check_test.go", Lines: 10}}
		angles, matches := SelectDeepReviewAngles(mapping, files)
		if len(angles) != 3 || angles[0] != 1 || angles[2] != 3 {
			t.Errorf("got %v, want [1 2 3] (prefix internal/ should win)", angles)
		}
		if len(matches) != 1 || matches[0].Rule != "internal/" {
			t.Errorf("got matches %v, want rule=internal/", matches)
		}
	})

	t.Run("union across multiple files", func(t *testing.T) {
		files := []ChangedFile{
			{Path: "docs/guide/cli.md", Lines: 5},
			{Path: "internal/state/machine.go", Lines: 10},
		}
		angles, _ := SelectDeepReviewAngles(mapping, files)
		expected := map[int]bool{1: true, 2: true, 3: true, 4: true, 8: true, 10: true}
		if len(angles) != len(expected) {
			t.Errorf("got %v, want union with %d angles", angles, len(expected))
		}
		for _, a := range angles {
			if !expected[a] {
				t.Errorf("unexpected angle %d in %v", a, angles)
			}
		}
	})

	t.Run("no match returns all angles", func(t *testing.T) {
		files := []ChangedFile{{Path: "unknown/path.go", Lines: 10}}
		angles, matches := SelectDeepReviewAngles(mapping, files)
		if len(angles) != DeepReviewAngleCount {
			t.Errorf("got %d angles, want %d (all)", len(angles), DeepReviewAngleCount)
		}
		if matches != nil {
			t.Errorf("got matches %v, want nil", matches)
		}
	})

	t.Run("empty files returns all angles", func(t *testing.T) {
		angles, _ := SelectDeepReviewAngles(mapping, nil)
		if len(angles) != DeepReviewAngleCount {
			t.Errorf("got %d angles, want %d", len(angles), DeepReviewAngleCount)
		}
	})
}

func TestGroupSelectedAngles(t *testing.T) {
	t.Run("nil when parallelReviewers <= 1", func(t *testing.T) {
		if got := GroupSelectedAngles(1, 0, []int{1, 2, 3}); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("nil when angles empty", func(t *testing.T) {
		if got := GroupSelectedAngles(3, 0, nil); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("splits selected angles evenly", func(t *testing.T) {
		got := GroupSelectedAngles(3, 0, []int{1, 3, 5, 7, 8, 10})
		want := [][]int{{1, 3}, {5, 7}, {8, 10}}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if len(got[i]) != len(want[i]) {
				t.Fatalf("group %d = %v, want %v", i, got[i], want[i])
			}
			for j := range want[i] {
				if got[i][j] != want[i][j] {
					t.Fatalf("group %d = %v, want %v", i, got[i], want[i])
				}
			}
		}
	})

	t.Run("last group can be smaller", func(t *testing.T) {
		got := GroupSelectedAngles(3, 0, []int{1, 2, 5, 8, 10})
		if len(got) != 3 {
			t.Fatalf("got %d groups, want 3: %v", len(got), got)
		}
		total := 0
		for _, g := range got {
			total += len(g)
		}
		if total != 5 {
			t.Fatalf("total angles %d, want 5", total)
		}
	})

	t.Run("explicit anglesPerReviewer", func(t *testing.T) {
		got := GroupSelectedAngles(2, 2, []int{1, 3, 5, 8, 10})
		if len(got) != 3 {
			t.Fatalf("got %d groups, want 3: %v", len(got), got)
		}
		if len(got[0]) != 2 || len(got[1]) != 2 || len(got[2]) != 1 {
			t.Fatalf("group sizes wrong: %v", got)
		}
	})
}

func TestDefaultAngleMapping(t *testing.T) {
	m := DefaultAngleMapping()
	if len(m) == 0 {
		t.Fatal("default mapping should not be empty")
	}
	for key, angles := range m {
		if len(angles) == 0 {
			t.Errorf("mapping %q has empty angles", key)
		}
		for _, a := range angles {
			if a < 1 || a > DeepReviewAngleCount {
				t.Errorf("mapping %q has angle %d out of range", key, a)
			}
		}
	}
}

func TestResolveAngleMapping(t *testing.T) {
	t.Run("uses config when set", func(t *testing.T) {
		cfg := Config{
			Roles: map[string]RoleConfig{
				"deep-reviewer": {AngleMapping: map[string][]int{"custom/": {1, 2}}},
			},
		}
		m := ResolveAngleMapping(cfg)
		if _, ok := m["custom/"]; !ok {
			t.Error("should use config mapping")
		}
	})

	t.Run("falls back to default", func(t *testing.T) {
		cfg := Config{}
		m := ResolveAngleMapping(cfg)
		if _, ok := m["internal/state/"]; !ok {
			t.Error("should fall back to default mapping")
		}
	})
}

func TestAllAngles(t *testing.T) {
	all := AllAngles()
	if len(all) != DeepReviewAngleCount {
		t.Fatalf("got %d, want %d", len(all), DeepReviewAngleCount)
	}
	for i, a := range all {
		if a != i+1 {
			t.Errorf("angle[%d] = %d, want %d", i, a, i+1)
		}
	}
}
