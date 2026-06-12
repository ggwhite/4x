package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestDeployPluginsCreatesSharedDir(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude"},
		},
	}

	deployPlugins(root, cfg)

	sharedCreator := filepath.Join(dotDir, "plugins", "shared", "CREATOR.md")
	if _, err := os.Stat(sharedCreator); err != nil {
		t.Errorf("shared/CREATOR.md not deployed: %v", err)
	}
}

func TestDeployPluginsSharedDeployedWithoutRunners(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{},
	}

	deployPlugins(root, cfg)

	sharedCreator := filepath.Join(dotDir, "plugins", "shared", "CREATOR.md")
	if _, err := os.Stat(sharedCreator); err != nil {
		t.Errorf("shared/CREATOR.md should deploy even without runners: %v", err)
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
