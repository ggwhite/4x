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
