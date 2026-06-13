package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestInstallPluginsCreatesSharedDir(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude"},
		},
	}

	installPlugins(root, cfg)

	sharedCreator := filepath.Join(dotDir, "plugins", "shared", "CREATOR.md")
	if _, err := os.Stat(sharedCreator); err != nil {
		t.Errorf("shared/CREATOR.md not installed: %v", err)
	}
}

func TestInstallPluginsSharedInstalledWithoutRunners(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{},
	}

	installPlugins(root, cfg)

	sharedCreator := filepath.Join(dotDir, "plugins", "shared", "CREATOR.md")
	if _, err := os.Stat(sharedCreator); err != nil {
		t.Errorf("shared/CREATOR.md should install even without runners: %v", err)
	}
}

func TestComparePluginsIncludesSharedFiles(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{},
	}

	report := comparePlugins(root, cfg)

	found := false
	for _, r := range report {
		if r.path == filepath.Join(".4x", "plugins", "shared", "CREATOR.md") {
			found = true
			if r.status != statusCreated {
				t.Errorf("shared/CREATOR.md should be 'created', got %q", r.status)
			}
		}
	}
	if !found {
		t.Error("comparePlugins should include shared/CREATOR.md in report")
	}
}
