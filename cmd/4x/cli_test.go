package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

var binaryPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "4x-cli-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "4x")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func run4x(dir string, args ...string) (string, error) {
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestInit_CreatesWorkspace(t *testing.T) {
	dir := t.TempDir()
	out, err := run4x(dir, "init")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(dir, ".4x", "settings.json")); err != nil {
		t.Error(".4x/settings.json not created")
	}
	if _, err := os.Stat(filepath.Join(dir, ".4x", "features")); err != nil {
		t.Error(".4x/features/ not created")
	}
}

func TestInit_DefaultClaudeUsesStreamJSON(t *testing.T) {
	dir := t.TempDir()
	out, err := run4x(dir, "init")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".4x", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var cfg protocol.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}

	claude := cfg.Runners["claude"]
	if claude.OutputFormat != "stream-json" {
		t.Errorf("claude.output_format = %q, want stream-json", claude.OutputFormat)
	}
	if claude.Tty {
		t.Error("claude.tty should be false for stream-json runner")
	}

	args := strings.Join(claude.Args, " ")
	for _, want := range []string{"--output-format", "stream-json", "--verbose"} {
		if !strings.Contains(args, want) {
			t.Errorf("claude args missing %q: %v", want, claude.Args)
		}
	}
}

func TestInit_DetectsGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.26\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("build:\n\tgo build\n"), 0o644)

	out, err := run4x(dir, "init")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".4x", "settings.json"))
	cfg := struct {
		Project struct {
			Language string   `json:"language"`
			Build    []string `json:"build"`
		} `json:"project"`
	}{}
	json.Unmarshal(data, &cfg)

	if cfg.Project.Language != "go" {
		t.Errorf("language = %q, want go", cfg.Project.Language)
	}
	if len(cfg.Project.Build) == 0 || cfg.Project.Build[0] != "make build" {
		t.Errorf("build = %v, want [make build]", cfg.Project.Build)
	}
}

func TestInit_DetectsNode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte(""), 0o644)

	out, err := run4x(dir, "init")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".4x", "settings.json"))
	cfg := struct {
		Project struct {
			Language string `json:"language"`
		} `json:"project"`
	}{}
	json.Unmarshal(data, &cfg)

	if cfg.Project.Language != "typescript" {
		t.Errorf("language = %q, want typescript", cfg.Project.Language)
	}
}

func TestInit_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")

	_, err := run4x(dir, "init")
	if err == nil {
		t.Error("expected error on duplicate init")
	}
}

func TestNew_CreatesFeatureYAML(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")

	out, err := run4x(dir, "new", "My test feature")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	path := filepath.Join(dir, ".4x", "features", "F001-my-test-feature.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("feature YAML not created at %s", path)
	}
}

func TestNew_FeatureContent(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Content check feature")

	data, err := os.ReadFile(filepath.Join(dir, ".4x", "features", "F001-content-check-feature.yaml"))
	if err != nil {
		t.Fatalf("read feature: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "id: F001-content-check-feature") {
		t.Error("missing id field")
	}
	if !strings.Contains(content, "name: 'F001: Content check feature'") {
		t.Error("missing name field")
	}
	if !strings.Contains(content, "status: not-started") {
		t.Error("missing status field")
	}
}

func TestStatus_ShowsBacklogDriftWarning(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Backlog drift")
	writeCLIFile(t, filepath.Join(dir, protocol.BacklogFile), `{"version":1,"features":[{"id":"F001-backlog-drift","name":"Backlog drift","status":"done"}]}`)

	out, err := run4x(dir, "status")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `WARN: feature_list.json mismatch for feature "F001-backlog-drift" field "status": canonical "not-started", mirror "done"`) {
		t.Fatalf("output = %q, want backlog drift warning", out)
	}
	if !strings.Contains(out, "F001-backlog-drift") || !strings.Contains(out, "not-started") {
		t.Fatalf("output = %q, want status from feature YAML", out)
	}
}

func TestStatusPending_ShowsBacklogDriftWarning(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Pending drift")
	writeCLIFile(t, filepath.Join(dir, protocol.BacklogFile), `{"version":1,"features":[{"id":"F001-pending-drift","name":"Pending drift","status":"done"}]}`)

	out, err := run4x(dir, "status", "--pending")
	if err != nil {
		t.Fatalf("status --pending failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `WARN: feature_list.json mismatch for feature "F001-pending-drift" field "status": canonical "not-started", mirror "done"`) {
		t.Fatalf("output = %q, want backlog drift warning", out)
	}
}

func TestCheckJSON_IncludesBacklogDriftWarning(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Check drift")
	featureID := "F001-check-drift"
	checkDriftDir := filepath.Join(dir, protocol.DirName, featureID)
	if err := os.MkdirAll(checkDriftDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(checkDriftDir, protocol.StateFile), `{"featureId":"F001-check-drift","phase":"init","role":"","round":0,"maxRounds":5,"active":false,"runner":"mock"}`)
	writeCLIFile(t, filepath.Join(dir, protocol.BacklogFile), `{"version":1,"features":[{"id":"F001-check-drift","name":"Check drift","description":"Check drift","status":"done"}]}`)

	out, err := run4x(dir, "check", featureID, "--json")
	if err != nil {
		t.Fatalf("check --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"warnings"`) {
		t.Fatalf("output = %q, want warnings key", out)
	}
	if !strings.Contains(out, `feature_list.json mismatch for feature \"F001-check-drift\" field \"status\": canonical \"not-started\", mirror \"done\"`) {
		t.Fatalf("output = %q, want JSON backlog drift warning", out)
	}
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestUpgrade_NoWorkspace(t *testing.T) {
	dir := t.TempDir()
	out, err := run4x(dir, "upgrade")
	if err == nil {
		t.Fatal("expected error when no workspace exists")
	}
	if !strings.Contains(out, "找不到 .4x/") {
		t.Fatalf("output = %q, want workspace not found message", out)
	}
}

func TestUpgrade_DeploysPlugins(t *testing.T) {
	dir := t.TempDir()
	if _, err := run4x(dir, "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	pluginFile := filepath.Join(dir, ".4x", "plugins", "CLAUDE.md")
	if err := os.Remove(pluginFile); err != nil {
		t.Fatalf("remove plugin: %v", err)
	}

	out, err := run4x(dir, "upgrade")
	if err != nil {
		t.Fatalf("upgrade failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(pluginFile); err != nil {
		t.Error("plugin file not restored after upgrade")
	}
}

func TestUpgrade_PreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	if _, err := run4x(dir, "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	claudeFile := filepath.Join(dir, "CLAUDE.md")
	data, err := os.ReadFile(claudeFile)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}

	userContent := "\n## My Custom Section\nCustom content here\n"
	if err := os.WriteFile(claudeFile, append(data, []byte(userContent)...), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	out, err := run4x(dir, "upgrade")
	if err != nil {
		t.Fatalf("upgrade failed: %v\n%s", err, out)
	}

	after, _ := os.ReadFile(claudeFile)
	content := string(after)
	if !strings.Contains(content, "@.4x/plugins/CLAUDE.md") {
		t.Error("@import line missing after upgrade")
	}
	if !strings.Contains(content, "My Custom Section") {
		t.Error("user content was removed by upgrade")
	}
}

func TestUpgrade_UpdatesStaleFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := run4x(dir, "init"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	pluginFile := filepath.Join(dir, ".4x", "plugins", "CLAUDE.md")
	if err := os.WriteFile(pluginFile, []byte("stale content"), 0o644); err != nil {
		t.Fatalf("write stale plugin: %v", err)
	}

	out, err := run4x(dir, "upgrade")
	if err != nil {
		t.Fatalf("upgrade failed: %v\n%s", err, out)
	}

	data, _ := os.ReadFile(pluginFile)
	if string(data) == "stale content" {
		t.Error("plugin file not updated after upgrade")
	}
	if len(data) == 0 {
		t.Error("plugin file is empty after upgrade")
	}
}

func TestTransition_TestingToAcceptingRequiresTesterArtifacts(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Manual gate")

	featureID := "F001-manual-gate"
	featureDir := filepath.Join(dir, protocol.DirName, featureID)
	roundDir := filepath.Join(featureDir, protocol.RoundsDir, "round-1")
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}

	state := `{"featureId":"F001-manual-gate","phase":"testing","role":"tester","round":1,"maxRounds":5,"active":true,"runner":"mock"}`
	os.WriteFile(filepath.Join(featureDir, protocol.StateFile), []byte(state), 0o644)
	os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), []byte(`{"passed":true,"round":1}`), 0o644)
	os.WriteFile(filepath.Join(roundDir, protocol.TestReport), []byte("# Test"), 0o644)
	os.WriteFile(filepath.Join(featureDir, protocol.FinalReport), []byte("# Final"), 0o644)

	out, err := run4x(dir, "transition", featureID, "--to", string(protocol.PhaseAccepting))
	if err == nil {
		t.Fatal("transition should fail when commit-plan.md is missing")
	}
	if !strings.Contains(out, protocol.CommitPlan) {
		t.Fatalf("output = %q, want missing commit-plan detail", out)
	}
}

func TestStatus_JSON_ListAll(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "JSON test feature")

	out, err := run4x(dir, "status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v\n%s", err, out)
	}

	var result struct {
		Features []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"features"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(result.Features) != 1 {
		t.Fatalf("got %d features, want 1", len(result.Features))
	}
	if result.Features[0].Status != "not-started" {
		t.Errorf("status = %q, want not-started", result.Features[0].Status)
	}
}

func TestStatus_JSON_SingleFeature(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Detail test")

	out, err := run4x(dir, "status", "F001", "--json")
	if err != nil {
		t.Fatalf("status <id> --json failed: %v\n%s", err, out)
	}

	var result struct {
		Feature struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"feature"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !strings.HasPrefix(result.Feature.ID, "F001-") {
		t.Errorf("id = %q, want F001-* prefix", result.Feature.ID)
	}
}

func TestNew_JSON(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")

	out, err := run4x(dir, "new", "--json", "JSON new test")
	if err != nil {
		t.Fatalf("new --json failed: %v\n%s", err, out)
	}

	var result struct {
		FeatureID string `json:"featureId"`
		Name      string `json:"name"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !strings.HasPrefix(result.FeatureID, "F") {
		t.Errorf("featureId = %q, want F-prefix", result.FeatureID)
	}
	if result.Path == "" {
		t.Error("path is empty")
	}
}

func TestTransition_JSON(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Trans JSON")

	featureID := "F001-trans-json"
	featureDir := filepath.Join(dir, protocol.DirName, featureID)
	os.MkdirAll(featureDir, 0o755)
	os.WriteFile(filepath.Join(featureDir, protocol.StateFile),
		[]byte(`{"featureId":"F001-trans-json","phase":"init","role":"","round":0,"maxRounds":5,"active":true,"runner":"mock","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}`), 0o644)

	out, err := run4x(dir, "transition", featureID, "--to", "designing", "--json")
	if err != nil {
		t.Fatalf("transition --json failed: %v\n%s", err, out)
	}

	var result struct {
		FeatureID string `json:"featureId"`
		From      string `json:"from"`
		To        string `json:"to"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result.From != "init" || result.To != "designing" {
		t.Errorf("got from=%q to=%q, want init→designing", result.From, result.To)
	}
}

func TestTransition_JSON_Error(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Trans err")

	featureID := "F001-trans-err"
	featureDir := filepath.Join(dir, protocol.DirName, featureID)
	os.MkdirAll(featureDir, 0o755)
	os.WriteFile(filepath.Join(featureDir, protocol.StateFile),
		[]byte(`{"featureId":"F001-trans-err","phase":"init","role":"","round":0,"maxRounds":5,"active":true,"runner":"mock","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}`), 0o644)

	out, err := run4x(dir, "transition", featureID, "--to", "testing", "--json")
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}

	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON on error: %v\n%s", err, out)
	}
	if result.Error == "" {
		t.Error("error field is empty")
	}
}
