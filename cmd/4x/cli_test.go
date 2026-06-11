package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

	if _, err := os.Stat(filepath.Join(dir, ".4x", "config.yaml")); err != nil {
		t.Error(".4x/config.yaml not created")
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

	data, _ := os.ReadFile(filepath.Join(dir, ".4x", "config.yaml"))
	cfg := struct {
		Project struct {
			Language string   `yaml:"language"`
			Build    []string `yaml:"build"`
		} `yaml:"project"`
	}{}
	yaml.Unmarshal(data, &cfg)

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

	data, _ := os.ReadFile(filepath.Join(dir, ".4x", "config.yaml"))
	cfg := struct {
		Project struct {
			Language string `yaml:"language"`
		} `yaml:"project"`
	}{}
	yaml.Unmarshal(data, &cfg)

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
