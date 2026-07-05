package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/plugins"
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

// TestSyncPluginsDetectsMissingLearningsContextImport 涵蓋 F130 的核心情境：
// plugin 檔案本身與其根目錄 import 都已是最新（穩態），但 learnings-context import 缺失。
// comparePlugins() 必須偵測到這個缺口，syncPlugins() 才會觸發 installPlugins() 補上。
func TestSyncPluginsDetectsMissingLearningsContextImport(t *testing.T) {
	root := t.TempDir()
	dotDir := filepath.Join(root, ".4x")
	pluginDir := filepath.Join(dotDir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}

	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude"},
		},
	}

	embedData, err := plugins.FS.ReadFile("claude-code/CLAUDE.md")
	if err != nil {
		t.Fatalf("read embedded CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "CLAUDE.md"), embedData, 0o644); err != nil {
		t.Fatalf("write plugin CLAUDE.md: %v", err)
	}

	rootClaude := filepath.Join(root, "CLAUDE.md")
	rootContent := "@.4x/plugins/CLAUDE.md\n\n# Project\n"
	if err := os.WriteFile(rootClaude, []byte(rootContent), 0o644); err != nil {
		t.Fatalf("write root CLAUDE.md: %v", err)
	}

	report := comparePlugins(root, cfg)

	statusByPath := make(map[string]string)
	for _, r := range report {
		statusByPath[r.path] = r.status
	}

	if got := statusByPath[filepath.Join(".4x", "plugins", "CLAUDE.md")]; got != statusCurrent {
		t.Errorf("plugin CLAUDE.md content should be %q, got %q", statusCurrent, got)
	}
	if got := statusByPath["CLAUDE.md"]; got != statusCurrent {
		t.Errorf("root CLAUDE.md plugin import should be %q, got %q", statusCurrent, got)
	}
	if got, ok := statusByPath["CLAUDE.md (learnings-context)"]; !ok {
		t.Fatal("comparePlugins should report missing learnings-context import")
	} else if got != statusUpdated {
		t.Errorf("learnings-context import should be %q, got %q", statusUpdated, got)
	}

	syncPlugins(root, cfg)

	data, err := os.ReadFile(rootClaude)
	if err != nil {
		t.Fatalf("read root CLAUDE.md after sync: %v", err)
	}
	if !strings.Contains(string(data), "@.4x/"+protocol.LearningsContextFile) {
		t.Error("syncPlugins should append learnings-context import to CLAUDE.md once triggered")
	}
}
