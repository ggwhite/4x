package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestDumpTemplates_CreatesFiles(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	dotDir := filepath.Join(root, protocol.DirName)

	if err := dumpTemplates(dotDir, false); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dotDir, "templates", "designer.md.tmpl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected designer.md.tmpl to exist: %v", err)
	}
	if !strings.Contains(string(data), "You are the Designer") {
		t.Error("expected designer template content")
	}

	localePath := filepath.Join(dotDir, "templates", "locale.tmpl")
	if _, err := os.Stat(localePath); err != nil {
		t.Fatalf("expected locale.tmpl to exist: %v", err)
	}
}

func TestDumpTemplates_SkipsExisting(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	dotDir := filepath.Join(root, protocol.DirName)

	tmplDir := filepath.Join(dotDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "designer.md.tmpl"), []byte("MY CUSTOM"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := dumpTemplates(dotDir, false); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(tmplDir, "designer.md.tmpl"))
	if string(data) != "MY CUSTOM" {
		t.Errorf("expected existing file to be preserved, got %q", string(data))
	}
}

func TestDumpTemplates_ForceOverwrites(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	dotDir := filepath.Join(root, protocol.DirName)

	tmplDir := filepath.Join(dotDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "designer.md.tmpl"), []byte("MY CUSTOM"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := dumpTemplates(dotDir, true); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(tmplDir, "designer.md.tmpl"))
	if string(data) == "MY CUSTOM" {
		t.Error("expected force to overwrite existing file")
	}
	if !strings.Contains(string(data), "You are the Designer") {
		t.Error("expected builtin content after force overwrite")
	}
}

func TestInitRunnerAwareRoleDefaults(t *testing.T) {
	for _, runner := range []string{"codex", "claude"} {
		t.Run(runner, func(t *testing.T) {
			tmpHome := t.TempDir()
			t.Setenv("HOME", tmpHome)
			// 刻意寫入「只有 default_runner」的最小 user config（不帶 Locale/Runners），
			// 驗證 ensureUserConfig 會保留它、不覆寫成內建 claude 預設，
			// init 才能真正依此 default_runner 產生 canonical role 預設。
			if err := protocol.WriteUserConfig(protocol.UserConfig{DefaultRunner: runner}); err != nil {
				t.Fatal(err)
			}

			tmpCwd := t.TempDir()
			t.Chdir(tmpCwd)

			cmd := newInitCmd()
			if err := cmd.RunE(cmd, nil); err != nil {
				t.Fatalf("init RunE: %v", err)
			}

			// ensureUserConfig 必須保留最小設定，不得把 default_runner 改寫掉。
			if uc, err := protocol.ReadUserConfig(); err != nil {
				t.Fatalf("read user config: %v", err)
			} else if uc.DefaultRunner != runner {
				t.Fatalf("default_runner overwritten: got %q, want %q", uc.DefaultRunner, runner)
			}

			dotDir := filepath.Join(tmpCwd, protocol.DirName)
			cfg, err := protocol.ReadConfig(dotDir)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}

			checks := []struct {
				role, model, deep string
			}{
				{"designer", "strong", ""},
				{"coder", "fast", ""},
				{"reviewer", "fast", "strong"},
				{"tester", "fast", ""},
			}
			for _, c := range checks {
				rc := cfg.Roles[c.role]
				if rc.Model != c.model {
					t.Errorf("%s: %s.model = %q, want %q", runner, c.role, rc.Model, c.model)
				}
				if rc.DeepModel != c.deep {
					t.Errorf("%s: %s.deep_model = %q, want %q", runner, c.role, rc.DeepModel, c.deep)
				}
			}

			raw, err := os.ReadFile(filepath.Join(dotDir, protocol.ConfigFile))
			if err != nil {
				t.Fatal(err)
			}
			for _, legacy := range []string{"opus", "sonnet"} {
				if strings.Contains(string(raw), legacy) {
					t.Errorf("%s: settings.json must not contain literal %q:\n%s", runner, legacy, raw)
				}
			}
		})
	}
}
