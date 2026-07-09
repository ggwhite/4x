package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// failLookPath 模擬 command 一律不在 PATH。
func failLookPath(string) (string, error) { return "", errors.New("not found") }

// AC-7：checkRoles 對 roles.{role}.runner 三態驗證，且驗證與 cfg.Default 是否存在解耦（DR-9）。
func TestF158_CheckRoles_RunnerOverride(t *testing.T) {
	runners := map[string]protocol.RunnerConfig{
		"claude": {Command: "claude", Tiers: map[string]string{"opus": "claude-opus", "sonnet": "claude-sonnet"}},
		"codex":  {Command: "codex", Tiers: map[string]string{"opus": "gpt-5.5", "sonnet": "gpt-5.5"}},
	}

	tests := []struct {
		name     string
		cfg      protocol.Config
		lookPath func(string) (string, error)
		wantSev  Severity
	}{
		{
			name: "unknown runner fails",
			cfg: protocol.Config{
				Default: "claude", Runners: runners,
				Roles: map[string]protocol.RoleConfig{"deep-reviewer": {Runner: "ghost"}},
			},
			lookPath: okLookPath,
			wantSev:  SeverityFail,
		},
		{
			name: "exists but not in PATH warns",
			cfg: protocol.Config{
				Default: "claude", Runners: runners,
				Roles: map[string]protocol.RoleConfig{"deep-reviewer": {Runner: "codex"}},
			},
			lookPath: failLookPath,
			wantSev:  SeverityWarn,
		},
		{
			name: "valid and in PATH passes",
			cfg: protocol.Config{
				Default: "claude", Runners: runners,
				Roles: map[string]protocol.RoleConfig{"deep-reviewer": {Runner: "codex"}},
			},
			lookPath: okLookPath,
			wantSev:  SeverityPass,
		},
		{
			name: "no default runner still validates override (DR-9)",
			cfg: protocol.Config{
				Default: "", Runners: runners,
				Roles: map[string]protocol.RoleConfig{"deep-reviewer": {Runner: "missing"}},
			},
			lookPath: okLookPath,
			wantSev:  SeverityFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := checkRoles(tt.cfg, tt.lookPath)
			c := findCheck(checks, sectionRoles, "deep-reviewer runner")
			if c == nil {
				t.Fatalf("找不到 deep-reviewer runner override 的 check，得 %+v", checks)
			}
			if c.Severity != tt.wantSev {
				t.Fatalf("severity: got %s, want %s（detail=%q）", c.Severity, tt.wantSev, c.Detail)
			}
		})
	}
}

// AC-9（post-merge 缺陷 #2）：checkRoles 對有 roles.{role}.runner 覆寫的 role，應對「覆寫後的
// runner」解析 model tier，而不是永遠對 cfg.Default 解析。若覆寫到的 runner 缺少該 tier，
// 應 WARN；今日的 bug 是永遠用 cfg.Default（有該 tier）解析，因而誤報 PASS。
func TestF158_CheckRoles_ModelResolvedAgainstOverrideRunner(t *testing.T) {
	cfg := protocol.Config{
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{"opus": "claude-opus", "sonnet": "claude-sonnet"}},
			// codex 只有 sonnet tier，沒有 opus。
			"codex": {Command: "codex", Tiers: map[string]string{"sonnet": "gpt-5.5"}},
		},
		Roles: map[string]protocol.RoleConfig{
			"reviewer": {Model: "opus", Runner: "codex"},
		},
	}
	checks := checkRoles(cfg, okLookPath)
	c := findCheck(checks, sectionRoles, "reviewer")
	if c == nil {
		t.Fatalf("找不到 reviewer 的 model check，得 %+v", checks)
	}
	if c.Severity != SeverityWarn {
		t.Fatalf("reviewer 覆寫到缺少 opus tier 的 codex，應 WARN（而非沿用 default runner claude 誤報 PASS），得 %+v", c)
	}
}

// AC-10（post-merge 缺陷 #3）：checkRoles 對 roles.deep-reviewer.runner 覆寫，deep model tier
// 的可解析性應對「覆寫後的 deep runner」驗證，而不是永遠對 cfg.Default 驗證。若覆寫到的 runner
// 無 opus tier 且 reviewer.deep_model 未設定（fallback 到 DefaultDeepTier），
// runtime 會靜默跳過 deep-reviewing；doctor 應 WARN，而非誤報 PASS。
func TestF158_CheckRoles_DeepModelResolvedAgainstOverrideRunner(t *testing.T) {
	cfg := protocol.Config{
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{"opus": "claude-opus", "sonnet": "claude-sonnet"}},
			"codex":  {Command: "codex", Tiers: map[string]string{"sonnet": "gpt-5.5"}},
		},
		Roles: map[string]protocol.RoleConfig{
			"reviewer":      {Model: "sonnet"}, // 未設 deep_model → fallback DefaultDeepTier("opus")
			"deep-reviewer": {Runner: "codex"},
		},
	}
	checks := checkRoles(cfg, okLookPath)
	c := findCheck(checks, sectionRoles, "deep-reviewer")
	if c == nil {
		t.Fatalf("找不到 deep-reviewer 的 deep model check，得 %+v", checks)
	}
	if c.Severity != SeverityWarn {
		t.Fatalf("deep-reviewer 覆寫到缺少 opus tier 的 codex，應 WARN（runtime 會靜默跳過 deep-reviewing），得 %+v", c)
	}
}

// AC-11（post-merge 缺陷 #4）：cfg.Roles 的 key 若不是 canonical role 名稱（如 typo "reviewr"），
// 即使其 runner 值合法且在 PATH，也不會被 ResolvePhaseRunner 讀到（PhaseRole 只讀 canonical
// role），此覆寫等同 dead config；checkRoles 應對這種 key 報 FAIL，而非放行成 PASS。
func TestF158_CheckRoles_UnknownRoleKeyFails(t *testing.T) {
	cfg := protocol.Config{
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude"},
		},
		Roles: map[string]protocol.RoleConfig{
			"reviewr": {Runner: "claude"}, // typo of "reviewer"
		},
	}
	checks := checkRoles(cfg, okLookPath)
	c := findCheck(checks, sectionRoles, "reviewr runner")
	if c == nil {
		t.Fatalf("找不到 reviewr runner 的 check，得 %+v", checks)
	}
	if c.Severity != SeverityFail {
		t.Fatalf("非 canonical role key 的 runner 覆寫應 FAIL（不會生效），得 %+v", c)
	}
}

// writeFeatureWithOverride 寫一個含 phase_overrides.{phase}.runner 的 feature YAML。
func writeFeatureWithOverride(t *testing.T, root, id, phase, runner string) {
	t.Helper()
	body := "id: " + id + "\nname: " + id + "\nstatus: in-progress\n" +
		"phase_overrides:\n  " + phase + ":\n    runner: " + runner + "\n"
	path := filepath.Join(root, ".4x", "features", id+".yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// AC-8：checkFeatureYAML 對 feature YAML 的 phase_overrides.{phase}.runner 做三態驗證（DR-8）。
func TestF158_CheckFeatureYAML_RunnerOverride(t *testing.T) {
	runners := map[string]protocol.RunnerConfig{
		"claude": {Command: "claude"},
		"codex":  {Command: "codex"},
	}

	t.Run("unknown runner fails", func(t *testing.T) {
		root := setupWorkspace(t)
		writeFeatureWithOverride(t, root, "F900", "deep-reviewing", "ghost")
		ws := &protocol.Workspace{Root: root}
		cfg := protocol.Config{Runners: runners}
		checks := checkFeatureYAML(ws, cfg, okLookPath)
		c := findCheck(checks, sectionWorkspace, "F900.yaml")
		if c == nil || c.Severity != SeverityFail {
			t.Fatalf("未知 runner 應 FAIL，得 %+v", c)
		}
	})

	t.Run("exists but not in PATH warns", func(t *testing.T) {
		root := setupWorkspace(t)
		writeFeatureWithOverride(t, root, "F901", "deep-reviewing", "codex")
		ws := &protocol.Workspace{Root: root}
		cfg := protocol.Config{Runners: runners}
		checks := checkFeatureYAML(ws, cfg, failLookPath)
		c := findCheck(checks, sectionWorkspace, "F901.yaml")
		if c == nil || c.Severity != SeverityWarn {
			t.Fatalf("runner 不在 PATH 應 WARN，得 %+v", c)
		}
	})

	t.Run("valid and in PATH no issue", func(t *testing.T) {
		root := setupWorkspace(t)
		writeFeatureWithOverride(t, root, "F902", "deep-reviewing", "codex")
		ws := &protocol.Workspace{Root: root}
		cfg := protocol.Config{Runners: runners}
		checks := checkFeatureYAML(ws, cfg, okLookPath)
		if hasSeverity(checks, sectionWorkspace, SeverityFail) || hasSeverity(checks, sectionWorkspace, SeverityWarn) {
			t.Fatalf("合法 runner 覆寫不應產生 Fail/Warn，得 %+v", checks)
		}
		if c := findCheck(checks, sectionWorkspace, "feature yaml"); c == nil || c.Severity != SeverityPass {
			t.Fatalf("全乾淨應回單一 summary PASS，得 %+v", c)
		}
	})
}

// AC-12（post-merge 缺陷 #5）：phase_overrides 的 phase key 若不是 canonical phase（如 typo
// "reviewng"），ResolvePhaseRunner/ResolvePhaseModel 永遠不會用這個 key 去查
// f.PhaseOverrides（執行時比對的是真正的 phase 常量），此覆寫等同 dead config；
// checkFeatureYAML 應報 FAIL，而非放行成 PASS。
func TestF158_CheckFeatureYAML_UnknownPhaseKeyFails(t *testing.T) {
	root := setupWorkspace(t)
	writeFeatureWithOverride(t, root, "F903", "reviewng", "codex") // typo of "reviewing"
	ws := &protocol.Workspace{Root: root}
	cfg := protocol.Config{Runners: map[string]protocol.RunnerConfig{"codex": {Command: "codex"}}}
	checks := checkFeatureYAML(ws, cfg, okLookPath)
	c := findCheck(checks, sectionWorkspace, "F903.yaml")
	if c == nil {
		t.Fatalf("找不到 F903.yaml 的 check，得 %+v", checks)
	}
	if c.Severity != SeverityFail {
		t.Fatalf("非 canonical phase key 的覆寫應 FAIL（不會生效），得 %+v", c)
	}
}
