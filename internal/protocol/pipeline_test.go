package protocol

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

// pipelineConfig 提供具 profiles 區段與兩 runner 的 Config，供 ResolvePipeline 測試共用。
func pipelineConfig() Config {
	return Config{
		Default: "claude",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{"opus": "opus", "sonnet": "sonnet"}},
			"codex":  {Command: "codex", Tiers: map[string]string{"opus": "gpt-5.5", "sonnet": "gpt-5.5-mini"}},
			"gemini": {Command: "gemini", Tiers: map[string]string{"opus": "gemini-pro", "sonnet": "gemini-flash"}},
		},
		Roles: map[string]RoleConfig{
			"coder": {Model: "sonnet"},
			// deep_model 掛在 reviewer role；deep_reviewer.model 故意設不同值，
			// 用來驗證 deep-reviewing 走的是 reviewer.deep_model 而非 deep_reviewer.model。
			"reviewer":      {DeepModel: "opus"},
			"deep_reviewer": {Model: "sonnet"},
		},
		Profiles: map[string]ProfileConfig{
			"full":   DefaultProfiles()["full"],
			"normal": DefaultProfiles()["normal"],
			"quick":  DefaultProfiles()["quick"],
		},
	}
}

func TestResolvePipeline_PhaseCounts(t *testing.T) {
	cfg := pipelineConfig()
	tests := []struct {
		profile string
		want    int
	}{
		{"full", 8},
		{"normal", 4},
		{"quick", 2},
	}
	for _, tt := range tests {
		got, err := ResolvePipeline(cfg, feature.Feature{}, tt.profile, "", nil)
		if err != nil {
			t.Fatalf("%s: %v", tt.profile, err)
		}
		if len(got) != tt.want {
			t.Errorf("profile %s: got %d phases, want %d", tt.profile, len(got), tt.want)
		}
	}
}

func TestResolvePipeline_CanonicalOrderAndContent(t *testing.T) {
	cfg := pipelineConfig()
	got, err := ResolvePipeline(cfg, feature.Feature{}, "quick", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d phases, want 2", len(got))
	}
	// quick = coding → reviewing（canonical 順序）。
	if got[0].Phase != string(PhaseCoding) || got[1].Phase != string(PhaseReviewing) {
		t.Errorf("order wrong: %s, %s", got[0].Phase, got[1].Phase)
	}
	if got[0].Role != string(RoleCoder) {
		t.Errorf("coding role: got %q, want coder", got[0].Role)
	}
	if got[0].Runner != "claude" {
		t.Errorf("coding runner: got %q, want claude (default)", got[0].Runner)
	}
	// coder model 來自 roles.coder.model=sonnet，透過 claude tiers 解析成 sonnet。
	if got[0].Model != "sonnet" {
		t.Errorf("coding model: got %q, want sonnet", got[0].Model)
	}
}

func TestResolvePipeline_OverridePrecedence(t *testing.T) {
	cfg := pipelineConfig()
	// feature YAML 對 reviewing 設 runner=codex；本次 run 臨時覆寫成 gemini 應勝出。
	f := feature.Feature{
		PhaseOverrides: map[string]feature.PhaseOverride{
			"reviewing": {Runner: "codex"},
		},
	}
	overrides := map[Phase]PhaseSpec{
		PhaseReviewing: {Phase: string(PhaseReviewing), Runner: "gemini", Model: "opus"},
	}
	got, err := ResolvePipeline(cfg, f, "quick", "", overrides)
	if err != nil {
		t.Fatal(err)
	}
	var reviewing *ResolvedPhase
	for i := range got {
		if got[i].Phase == string(PhaseReviewing) {
			reviewing = &got[i]
		}
	}
	if reviewing == nil {
		t.Fatal("reviewing phase missing")
	}
	if reviewing.Runner != "gemini" {
		t.Errorf("reviewing runner: got %q, want gemini (temp override beats feature YAML)", reviewing.Runner)
	}
	// opus tier 對 gemini runner 解析成 gemini-pro。
	if reviewing.Model != "gemini-pro" {
		t.Errorf("reviewing model: got %q, want gemini-pro", reviewing.Model)
	}
	// coding 未覆寫 → 沿用既有優先序（default claude）。
	if got[0].Phase == string(PhaseCoding) && got[0].Runner != "claude" {
		t.Errorf("coding runner: got %q, want claude (unaffected)", got[0].Runner)
	}
}

func TestResolvePipeline_ManualRunnerVsOverride(t *testing.T) {
	cfg := pipelineConfig()
	// 全域 manualRunner=codex，但 coding 有 per-phase 臨時覆寫 gemini → gemini 勝。
	overrides := map[Phase]PhaseSpec{
		PhaseCoding: {Phase: string(PhaseCoding), Runner: "gemini"},
	}
	got, err := ResolvePipeline(cfg, feature.Feature{}, "quick", "codex", overrides)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Phase != string(PhaseCoding) || got[0].Runner != "gemini" {
		t.Errorf("coding runner: got %q, want gemini (phase override beats --runner)", got[0].Runner)
	}
	// reviewing 無 per-phase 覆寫 → 沿用全域 manualRunner=codex。
	if got[1].Phase != string(PhaseReviewing) || got[1].Runner != "codex" {
		t.Errorf("reviewing runner: got %q, want codex (global --runner)", got[1].Runner)
	}
}

func TestResolvePipeline_UnknownProfileErrors(t *testing.T) {
	cfg := pipelineConfig()
	if _, err := ResolvePipeline(cfg, feature.Feature{}, "ghost", "", nil); err == nil {
		t.Error("expected error for unknown profile")
	}
}

// TestResolvePipeline_DeepReviewModelConsistency 確認 preview 的 deep-reviewing model
// 與實際 run loop（runDeepReviewPhase）採用的 ResolveDeepModel 解析結果完全一致：
// 都讀 reviewer.deep_model（opus），而非 deep_reviewer.model（sonnet）。
func TestResolvePipeline_DeepReviewModelConsistency(t *testing.T) {
	cfg := pipelineConfig()
	// full profile 含 deep-reviewing phase。
	got, err := ResolvePipeline(cfg, feature.Feature{}, "full", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var deep *ResolvedPhase
	for i := range got {
		if got[i].Phase == string(PhaseDeepReviewing) {
			deep = &got[i]
		}
	}
	if deep == nil {
		t.Fatal("deep-reviewing phase missing from full pipeline")
	}

	// 與 run loop 同一路徑重算：ResolveDeepModel(cfg, runner, RoleReviewer)。
	want, err := ResolveDeepModel(cfg, deep.Runner, RoleReviewer)
	if err != nil {
		t.Fatalf("ResolveDeepModel: %v", err)
	}
	if deep.Model != want {
		t.Errorf("deep-reviewing model: preview=%q, run loop=%q (must match)", deep.Model, want)
	}
	// 具體值：reviewer.deep_model=opus → claude tiers 解析成 opus，而非 deep_reviewer.model=sonnet。
	if deep.Model != "opus" {
		t.Errorf("deep-reviewing model: got %q, want opus (from reviewer.deep_model, not deep_reviewer.model)", deep.Model)
	}
}

func TestEffectiveManual(t *testing.T) {
	overrides := map[Phase]PhaseSpec{
		PhaseReviewing: {Phase: string(PhaseReviewing), Runner: "gemini"},
		PhaseTesting:   {Phase: string(PhaseTesting), Model: "opus"},
	}
	// 有 runner 覆寫 → runnerManual=gemini，無 model 覆寫 → modelManual 空。
	if rm, mm := EffectiveManual(overrides, PhaseReviewing, "codex"); rm != "gemini" || mm != "" {
		t.Errorf("reviewing: got (%q,%q), want (gemini,)", rm, mm)
	}
	// 只覆寫 model → runnerManual 沿用 manualRunner，modelManual=opus。
	if rm, mm := EffectiveManual(overrides, PhaseTesting, "codex"); rm != "codex" || mm != "opus" {
		t.Errorf("testing: got (%q,%q), want (codex,opus)", rm, mm)
	}
	// 無覆寫 → runnerManual 沿用 manualRunner，modelManual 空。
	if rm, mm := EffectiveManual(overrides, PhaseCoding, "codex"); rm != "codex" || mm != "" {
		t.Errorf("coding: got (%q,%q), want (codex,)", rm, mm)
	}
}
