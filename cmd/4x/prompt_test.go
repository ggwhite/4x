package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/prompt"
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
	profiles := prompt.LoadProfiles(ws, featureID, cfg)
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

func TestLoadRoleTemplate_Gate(t *testing.T) {
	tmpl, err := prompt.LoadRoleTemplate("", protocol.RoleGate)
	if err != nil {
		t.Fatalf("load gate template: %v", err)
	}
	if tmpl == nil {
		t.Fatal("expected non-nil gate template")
	}
}

func TestLoadRoleTemplate_DesignReviewer(t *testing.T) {
	tmpl, err := prompt.LoadRoleTemplate("", protocol.RoleDesignReviewer)
	if err != nil {
		t.Fatalf("load design-reviewer template: %v", err)
	}
	var b strings.Builder
	err = tmpl.Execute(&b, prompt.Data{
		Feature: feature.Feature{ID: "F091-design-review-phase", Name: "Design Review Phase"},
		DotDir:  "/tmp/project/.4x",
		Round:   0,
	})
	if err != nil {
		t.Fatalf("execute template: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "/tmp/project/.4x/run/F091-design-review-phase/design-review-report.md") {
		t.Fatalf("template should contain mandatory feature-level design review path, got:\n%s", out)
	}
}

func TestLoadRoleTemplate_ProjectOverride(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	dotDir := ws.DotDir()

	tmplDir := filepath.Join(dotDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customContent := `CUSTOM DESIGNER PROMPT for {{.Feature.ID}}`
	writeTestFileHelper(t, filepath.Join(tmplDir, "designer.md.tmpl"), customContent)

	tmpl, err := prompt.LoadRoleTemplate(dotDir, protocol.RoleDesigner)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}

	var b strings.Builder
	err = tmpl.Execute(&b, prompt.Data{
		Feature: feature.Feature{ID: "F089-test", Name: "Test"},
		DotDir:  dotDir,
	})
	if err != nil {
		t.Fatalf("execute template: %v", err)
	}
	if !strings.Contains(b.String(), "CUSTOM DESIGNER PROMPT for F089-test") {
		t.Errorf("expected project override content, got:\n%s", b.String())
	}
}

func TestLoadRoleTemplate_FallbackBuiltin(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	dotDir := ws.DotDir()

	tmpl, err := prompt.LoadRoleTemplate(dotDir, protocol.RoleDesigner)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}

	var b strings.Builder
	err = tmpl.Execute(&b, prompt.Data{
		Feature: feature.Feature{ID: "F089-test", Name: "Test"},
		DotDir:  dotDir,
	})
	if err != nil {
		t.Fatalf("execute template: %v", err)
	}
	if !strings.Contains(b.String(), "You are the Designer for feature") {
		t.Errorf("expected builtin template content, got:\n%s", b.String())
	}
}

func TestPromptData_DesignerWithLearnings(t *testing.T) {
	tmpl, err := prompt.LoadRoleTemplate("", protocol.RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}

	data := prompt.Data{
		Feature: feature.Feature{ID: "F089-test", Name: "Test Feature"},
		DotDir:  "/tmp/.4x",
		Learnings: []learning.Entry{
			{ID: "L001", Category: learning.CategoryCodeQuality, Content: "always wrap errors"},
			{ID: "L002", Category: learning.CategoryDesign, Content: "split large features"},
		},
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "Past Learnings") {
		t.Error("expected Past Learnings section")
	}
	if !strings.Contains(out, "[code-quality]") || !strings.Contains(out, "always wrap errors") {
		t.Error("expected L001 content in prompt")
	}
}

func TestPromptData_DesignerWithoutLearnings(t *testing.T) {
	tmpl, err := prompt.LoadRoleTemplate("", protocol.RoleDesigner)
	if err != nil {
		t.Fatal(err)
	}

	data := prompt.Data{
		Feature: feature.Feature{ID: "F089-test", Name: "Test"},
		DotDir:  "/tmp/.4x",
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "Past Learnings") {
		t.Error("should not contain Past Learnings when empty")
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

	profiles := prompt.LoadProfiles(ws, featureID, protocol.Config{})
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
	profiles := prompt.LoadProfiles(ws, featureID, cfg)
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
	profiles := prompt.LoadProfiles(ws, featureID, cfg)
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

	profiles := prompt.LoadProfiles(ws, featureID, protocol.Config{})
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

	profiles := prompt.LoadProfiles(ws, featureID, protocol.Config{})
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

	profiles := prompt.LoadProfiles(ws, featureID, protocol.Config{})
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

func TestCondensePlan(t *testing.T) {
	plan := `# Feature Plan

**Goal:** Build something great.

## Global Constraints

- No LLM in CLI
- Follow gofmt

## File Structure

| File | Action |
|---|---|
| foo.go | Create |

### Task 1: Setup config

**Files:**
- Create: internal/config.go

**Interfaces:**
- Produces: Config struct

- [ ] Step 1: Create the config struct
- [ ] Step 2: Add validation
- [ ] Step 3: Write tests

### Task 2: Implement handler

**Files:**
- Modify: cmd/server.go

- [ ] Step 1: Add route
- [ ] Step 2: Parse request

## Self-Review

Check everything.
`
	got := prompt.CondensePlan(plan)

	if !strings.Contains(got, "## Global Constraints") {
		t.Error("should keep Global Constraints")
	}
	if !strings.Contains(got, "## File Structure") {
		t.Error("should keep File Structure")
	}
	if !strings.Contains(got, "### Task 1: Setup config") {
		t.Error("should keep Task 1 header")
	}
	if !strings.Contains(got, "### Task 2: Implement handler") {
		t.Error("should keep Task 2 header")
	}
	if !strings.Contains(got, "Produces: Config struct") {
		t.Error("should keep Interfaces content")
	}
	if strings.Contains(got, "- [ ] Step 1") {
		t.Error("should strip step checkboxes")
	}
	if strings.Contains(got, "- [ ] Step 2") {
		t.Error("should strip step checkboxes")
	}
	if !strings.Contains(got, "## Self-Review") {
		t.Error("should keep non-task sections after tasks")
	}
	if len(got) >= len(plan) {
		t.Errorf("condensed (%d) should be shorter than original (%d)", len(got), len(plan))
	}
}

func TestCondensePlan_NoTasks(t *testing.T) {
	plan := "# Plan\n\nJust a simple plan with no tasks.\n"
	got := prompt.CondensePlan(plan)
	if got != plan {
		t.Errorf("plan without tasks should be unchanged\ngot:  %q\nwant: %q", got, plan)
	}
}

func TestCompactBlankLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no blanks", "a\nb\nc", "a\nb\nc"},
		{"single blank preserved", "a\n\nb", "a\n\nb"},
		{"triple blank compressed", "a\n\n\n\nb", "a\n\nb"},
		{"mixed", "# H1\n\n\n\ntext\n\n\n\n## H2\n\nlist", "# H1\n\ntext\n\n## H2\n\nlist"},
		{"trailing blanks", "a\n\n\n", "a\n"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prompt.CompactBlankLines(tt.in)
			if got != tt.want {
				t.Errorf("compactBlankLines(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}
