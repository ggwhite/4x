package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestLoadProfiles_BuiltinUnit(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - unit\nverify_commands:\n  - \"echo ok\"\n")

	cfg := protocol.Config{}
	profiles := loadProfiles(ws, featureID, cfg)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Name != "unit" {
		t.Errorf("expected name 'unit', got %q", profiles[0].Name)
	}
	if profiles[0].Content == "" {
		t.Error("expected non-empty content for unit profile")
	}
}

func TestLoadProfiles_NoProfiles_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "verify_commands:\n  - \"echo ok\"\n")

	profiles := loadProfiles(ws, featureID, protocol.Config{})
	if profiles != nil {
		t.Errorf("expected nil, got %v", profiles)
	}
}

func TestLoadProfiles_SettingsOverride(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - unit\nverify_commands:\n  - \"echo ok\"\n")

	cfg := protocol.Config{
		TestProfiles: map[string]protocol.TestProfileOverride{
			"unit": {Content: "custom unit instructions"},
		},
	}
	profiles := loadProfiles(ws, featureID, cfg)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Content != "custom unit instructions" {
		t.Errorf("expected override content, got %q", profiles[0].Content)
	}
}

func TestLoadProfiles_SettingsInclude(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - custom\nverify_commands:\n  - \"echo ok\"\n")

	includePath := filepath.Join(root, "my-profile.md")
	writeTestFileHelper(t, includePath, "custom profile content from file")

	cfg := protocol.Config{
		TestProfiles: map[string]protocol.TestProfileOverride{
			"custom": {Include: "my-profile.md"},
		},
	}
	profiles := loadProfiles(ws, featureID, cfg)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Content != "custom profile content from file" {
		t.Errorf("expected file content, got %q", profiles[0].Content)
	}
}

func TestLoadProfiles_UnknownProfile_ReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - nonexistent\nverify_commands:\n  - \"echo ok\"\n")

	profiles := loadProfiles(ws, featureID, protocol.Config{})
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles for unknown name, got %d", len(profiles))
	}
}

func TestLoadProfiles_MultipleProfiles(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	writeTestFileHelper(t, stratPath, "profiles:\n  - unit\n  - web\nverify_commands:\n  - \"echo ok\"\n")

	profiles := loadProfiles(ws, featureID, protocol.Config{})
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "unit" || profiles[1].Name != "web" {
		t.Errorf("expected [unit, web], got [%s, %s]", profiles[0].Name, profiles[1].Name)
	}
}

func TestLoadProfiles_NoStrategyFile_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	protocol.Init(root, protocol.Config{})
	ws := &protocol.Workspace{Root: root}
	featureID := "test-feat"
	ws.InitFeatureDir(featureID)

	profiles := loadProfiles(ws, featureID, protocol.Config{})
	if profiles != nil {
		t.Errorf("expected nil when no test-strategy.yaml, got %v", profiles)
	}
}

func writeTestFileHelper(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
