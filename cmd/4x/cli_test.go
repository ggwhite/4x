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

	path := filepath.Join(dir, ".4x", "features", "my-test-feature.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("feature YAML not created at %s", path)
	}
}

func TestNew_FeatureContent(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Content check feature")

	data, err := os.ReadFile(filepath.Join(dir, ".4x", "features", "content-check-feature.yaml"))
	if err != nil {
		t.Fatalf("read feature: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "id: content-check-feature") {
		t.Error("missing id field")
	}
	if !strings.Contains(content, "name: Content check feature") {
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
	writeCLIFile(t, filepath.Join(dir, protocol.BacklogFile), `{"version":1,"features":[{"id":"backlog-drift","name":"Backlog drift","status":"done"}]}`)

	out, err := run4x(dir, "status")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `WARN: feature_list.json mismatch for feature "backlog-drift" field "status": canonical "not-started", mirror "done"`) {
		t.Fatalf("output = %q, want backlog drift warning", out)
	}
	if !strings.Contains(out, "backlog-drift") || !strings.Contains(out, "not-started") {
		t.Fatalf("output = %q, want status from feature YAML", out)
	}
}

func TestStatusPending_ShowsBacklogDriftWarning(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Pending drift")
	writeCLIFile(t, filepath.Join(dir, protocol.BacklogFile), `{"version":1,"features":[{"id":"pending-drift","name":"Pending drift","status":"done"}]}`)

	out, err := run4x(dir, "status", "--pending")
	if err != nil {
		t.Fatalf("status --pending failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `WARN: feature_list.json mismatch for feature "pending-drift" field "status": canonical "not-started", mirror "done"`) {
		t.Fatalf("output = %q, want backlog drift warning", out)
	}
}

func TestCheckJSON_IncludesBacklogDriftWarning(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Check drift")
	checkDriftDir := filepath.Join(dir, protocol.DirName, "check-drift")
	if err := os.MkdirAll(checkDriftDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(checkDriftDir, protocol.StateFile), `{"featureId":"check-drift","phase":"init","role":"","round":0,"maxRounds":5,"active":false,"runner":"mock"}`)
	writeCLIFile(t, filepath.Join(dir, protocol.BacklogFile), `{"version":1,"features":[{"id":"check-drift","name":"Check drift","description":"Check drift","status":"done"}]}`)

	out, err := run4x(dir, "check", "check-drift", "--json")
	if err != nil {
		t.Fatalf("check --json failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"warnings"`) {
		t.Fatalf("output = %q, want warnings key", out)
	}
	if !strings.Contains(out, `feature_list.json mismatch for feature \"check-drift\" field \"status\": canonical \"not-started\", mirror \"done\"`) {
		t.Fatalf("output = %q, want JSON backlog drift warning", out)
	}
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestTransition_TestingToAcceptingRequiresTesterArtifacts(t *testing.T) {
	dir := t.TempDir()
	run4x(dir, "init")
	run4x(dir, "new", "Manual gate")

	featureID := "manual-gate"
	featureDir := filepath.Join(dir, protocol.DirName, featureID)
	roundDir := filepath.Join(featureDir, protocol.RoundsDir, "round-1")
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}

	state := `{"featureId":"manual-gate","phase":"testing","role":"tester","round":1,"maxRounds":5,"active":true,"runner":"mock"}`
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
