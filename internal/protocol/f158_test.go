package protocol

import (
	"encoding/json"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

// f158Config 提供三個 runner（claude/codex/gemini）與 default_runner=claude，
// 供 F158 per-role runner 路由的解析優先序測試共用。
func f158Config() Config {
	return Config{
		Default: "claude",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{"opus": "opus", "sonnet": "sonnet"}},
			"codex":  {Command: "codex", Tiers: map[string]string{"opus": "gpt-5.5", "sonnet": "gpt-5.5"}},
			"gemini": {Command: "gemini", Tiers: map[string]string{"opus": "gemini-pro", "sonnet": "gemini-flash"}},
		},
	}
}

// AC-1：RoleConfig 具 Runner 欄位（json tag runner,omitempty），能從 settings.json 真實解析。
func TestF158_RoleConfigRunnerParse(t *testing.T) {
	raw := []byte(`{"default_runner":"claude","runners":{"claude":{"command":"claude"}},"roles":{"deep-reviewer":{"runner":"codex"}}}`)
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := cfg.Roles["deep-reviewer"].Runner; got != "codex" {
		t.Fatalf("roles.deep-reviewer.runner: got %q, want codex", got)
	}
}

// AC-2：ResolvePhaseRunner 在既有優先序中插入 roles 層，位於 profile PhaseSpec 之下、cfg.Default 之上。
func TestF158_ResolvePhaseRunner_RolesPriority(t *testing.T) {
	tests := []struct {
		name    string
		roles   map[string]RoleConfig
		pc      ProfileConfig
		feature feature.Feature
		want    string
	}{
		{
			name:  "roles only",
			roles: map[string]RoleConfig{"coder": {Runner: "codex"}},
			pc:    ProfileConfig{},
			want:  "codex",
		},
		{
			name:  "profile PhaseSpec beats roles",
			roles: map[string]RoleConfig{"coder": {Runner: "codex"}},
			pc:    ProfileConfig{Phases: []PhaseSpec{{Phase: string(PhaseCoding), Runner: "gemini"}}},
			want:  "gemini",
		},
		{
			name:    "feature override beats roles",
			roles:   map[string]RoleConfig{"coder": {Runner: "codex"}},
			pc:      ProfileConfig{},
			feature: feature.Feature{PhaseOverrides: map[string]feature.PhaseOverride{"coding": {Runner: "gemini"}}},
			want:    "gemini",
		},
		{
			name:  "roles beats default",
			roles: map[string]RoleConfig{"coder": {Runner: "codex"}},
			pc:    ProfileConfig{},
			want:  "codex", // cfg.Default 為 claude，roles 勝
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := f158Config()
			cfg.Roles = tt.roles
			got, err := ResolvePhaseRunner(cfg, tt.feature, tt.pc, PhaseCoding, "")
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// AC-3：roles.{role}.runner 指向未知 runner 且無更高層覆寫時，fail fast 回 error，name 為空。
func TestF158_ResolvePhaseRunner_UnknownRunnerFailsFast(t *testing.T) {
	cfg := f158Config()
	cfg.Roles = map[string]RoleConfig{"coder": {Runner: "ghost"}}
	got, err := ResolvePhaseRunner(cfg, feature.Feature{}, ProfileConfig{}, PhaseCoding, "")
	if err == nil {
		t.Fatal("expected error for unknown roles runner")
	}
	if got != "" {
		t.Errorf("expected empty name on error, got %q", got)
	}
}

// AC-4：未設定任何 roles.{role}.runner 覆寫時，每個 selectable phase 的解析結果與現狀一致＝cfg.Default。
func TestF158_ResolvePhaseRunner_NoOverrideUnchanged(t *testing.T) {
	cfg := f158Config()
	for _, phase := range SelectablePhases() {
		got, err := ResolvePhaseRunner(cfg, feature.Feature{}, ProfileConfig{}, phase, "")
		if err != nil {
			t.Fatalf("phase %s: %v", phase, err)
		}
		if got != cfg.Default {
			t.Errorf("phase %s: got %q, want %q (default)", phase, got, cfg.Default)
		}
	}
}

// AC-5：mergeRole 正確合併 Runner——project 層非空覆寫 user 層；project 未設時保留 user 值。
func TestF158_MergeRole_Runner(t *testing.T) {
	t.Run("project overrides user", func(t *testing.T) {
		got := mergeRole(RoleConfig{Runner: "claude"}, RoleConfig{Runner: "codex"})
		if got.Runner != "codex" {
			t.Errorf("got %q, want codex (project wins)", got.Runner)
		}
	})
	t.Run("user retained when project unset", func(t *testing.T) {
		got := mergeRole(RoleConfig{Runner: "codex"}, RoleConfig{})
		if got.Runner != "codex" {
			t.Errorf("got %q, want codex (user retained)", got.Runner)
		}
	})
}

// AC-6：端到端消費驗證——經 ResolvePipeline（run loop 與 dashboard preview 共用的解析路徑），
// roles.deep-reviewer.runner=codex 確實流到 deep-reviewing phase，其餘 phase 仍為 cfg.Default。
func TestF158_ResolvePipeline_RolesRunner(t *testing.T) {
	cfg := pipelineConfig()
	cfg.Roles["deep-reviewer"] = RoleConfig{Runner: "codex"}

	resolved, err := ResolvePipeline(cfg, feature.Feature{}, "full", "", nil)
	if err != nil {
		t.Fatalf("ResolvePipeline: %v", err)
	}

	var sawDeep, sawCoding bool
	for _, rp := range resolved {
		switch rp.Phase {
		case string(PhaseDeepReviewing):
			sawDeep = true
			if rp.Runner != "codex" {
				t.Errorf("deep-reviewing runner: got %q, want codex", rp.Runner)
			}
		case string(PhaseCoding):
			sawCoding = true
			if rp.Runner != cfg.Default {
				t.Errorf("coding runner: got %q, want %q (default)", rp.Runner, cfg.Default)
			}
		}
	}
	if !sawDeep {
		t.Fatal("full profile 應含 deep-reviewing phase")
	}
	if !sawCoding {
		t.Fatal("full profile 應含 coding phase")
	}
}
