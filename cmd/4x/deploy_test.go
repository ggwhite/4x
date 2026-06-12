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

	creatorSkill := filepath.Join(dotDir, "plugins", "CREATOR-SKILL.md")
	if _, err := os.Stat(creatorSkill); err != nil {
		t.Errorf("CREATOR-SKILL.md not deployed: %v", err)
	}
}

func TestDeployPluginsSharedDeployedForAllRunners(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	os.MkdirAll(dotDir, 0o755)

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{
			"gemini": {Command: "gemini"},
		},
	}

	deployPlugins(root, cfg)

	sharedCreator := filepath.Join(dotDir, "plugins", "shared", "CREATOR.md")
	if _, err := os.Stat(sharedCreator); err != nil {
		t.Errorf("shared/CREATOR.md should deploy for any runner: %v", err)
	}
}
