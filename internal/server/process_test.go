package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

func setupPMWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "pm-test"},
		Runners: map[string]protocol.RunnerConfig{
			"test": {Command: "echo", Args: []string{"hello"}},
		},
		Default: "test",
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	f := protocol.Feature{ID: "test-feat", Name: "Test", Status: "not-started"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("test-feat"); err != nil {
		t.Fatal(err)
	}
	return ws
}

func fakeRunCommand(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-4x")
	script := "#!/bin/sh\necho stdout-ready\necho stderr-ready >&2\nsleep 60\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProcessManager_StartAndList(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, fakeRunCommand(t))
	defer pm.Shutdown()

	info, err := pm.Start("test-feat", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if info.FeatureID != "test-feat" {
		t.Errorf("FeatureID = %q, want test-feat", info.FeatureID)
	}

	runs := pm.List()
	if len(runs) != 1 {
		t.Fatalf("List() = %d runs, want 1", len(runs))
	}
	if runs[0].ID != info.ID {
		t.Errorf("List()[0].ID = %q, want %q", runs[0].ID, info.ID)
	}
}

func TestProcessManager_Stop(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, fakeRunCommand(t))
	defer pm.Shutdown()

	info, err := pm.Start("test-feat", "", 5)
	if err != nil {
		t.Fatal(err)
	}

	if err := pm.Stop(info.ID); err != nil {
		t.Fatal(err)
	}

	runs := pm.List()
	if len(runs) != 0 {
		t.Errorf("List() = %d runs after stop, want 0", len(runs))
	}
}

func TestProcessManager_PipeOutputToEvents(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, fakeRunCommand(t))
	defer pm.Shutdown()

	if _, err := pm.Start("test-feat", "", 5); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(ws.FeatureDir("test-feat"), protocol.EventsFile)
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			content := string(data)
			if strings.Contains(content, `"type":"run-output"`) &&
				strings.Contains(content, `"detail":"stdout-ready"`) &&
				strings.Contains(content, `"type":"run-error"`) &&
				strings.Contains(content, `"detail":"stderr-ready"`) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("events did not contain subprocess output before deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProcessManager_MaxParallel(t *testing.T) {
	ws := setupPMWorkspace(t)

	f2 := protocol.Feature{ID: "test-feat-2", Name: "Test 2", Status: "not-started"}
	if err := ws.SaveFeature(f2); err != nil {
		t.Fatal(err)
	}

	pm := NewProcessManager(ws, 1, fakeRunCommand(t))
	defer pm.Shutdown()

	if _, err := pm.Start("test-feat", "", 5); err != nil {
		t.Fatal(err)
	}

	if _, err := pm.Start("test-feat-2", "", 5); err == nil {
		t.Error("expected error for exceeding max parallel, got nil")
	}
}

func TestProcessManager_DuplicateFeature(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, fakeRunCommand(t))
	defer pm.Shutdown()

	if _, err := pm.Start("test-feat", "", 5); err != nil {
		t.Fatal(err)
	}

	_, err := pm.Start("test-feat", "", 5)
	if err == nil {
		t.Fatal("expected error for duplicate feature run, got nil")
	}
	if !strings.Contains(err.Error(), "already has a running process") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestProcessManager_Shutdown(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, fakeRunCommand(t))

	if _, err := pm.Start("test-feat", "", 5); err != nil {
		t.Fatal(err)
	}

	pm.Shutdown()

	runs := pm.List()
	if len(runs) != 0 {
		t.Errorf("List() = %d runs after shutdown, want 0", len(runs))
	}
}

func TestProcessManager_StopNotFound(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 1, fakeRunCommand(t))
	defer pm.Shutdown()

	if err := pm.Stop("nonexistent-id"); err == nil {
		t.Error("expected error for nonexistent run, got nil")
	}
}
