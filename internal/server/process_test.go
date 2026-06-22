package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/feature"
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

	f := feature.Feature{ID: "test-feat", Name: "Test", Status: "not-started"}
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

	info, err := pm.Start("test-feat", "", 5, "", nil)
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

	info, err := pm.Start("test-feat", "", 5, "", nil)
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

	if _, err := pm.Start("test-feat", "", 5, "", nil); err != nil {
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

	f2 := feature.Feature{ID: "test-feat-2", Name: "Test 2", Status: "not-started"}
	if err := ws.SaveFeature(f2); err != nil {
		t.Fatal(err)
	}

	pm := NewProcessManager(ws, 1, fakeRunCommand(t))
	defer pm.Shutdown()

	if _, err := pm.Start("test-feat", "", 5, "", nil); err != nil {
		t.Fatal(err)
	}

	if _, err := pm.Start("test-feat-2", "", 5, "", nil); err == nil {
		t.Error("expected error for exceeding max parallel, got nil")
	}
}

func TestBuildRunArgs_ProfileOverrides(t *testing.T) {
	args := buildRunArgs("F001", "claude", 3, "normal", []phaseOverrideReq{
		{Phase: "reviewing", Runner: "gemini"},
		{Phase: "testing", Model: "opus"},
		{Phase: "coding", Runner: "codex", Model: "sonnet"},
	})
	joined := strings.Join(args, " ")
	want := "run F001 --runner claude --profile normal " +
		"--phase-override reviewing:gemini: --phase-override testing::opus " +
		"--phase-override coding:codex:sonnet --max-rounds 3"
	if joined != want {
		t.Errorf("buildRunArgs:\n got: %s\nwant: %s", joined, want)
	}
}

func TestBuildRunArgs_NoProfileOrOverrides(t *testing.T) {
	args := buildRunArgs("F001", "claude", 5, "", nil)
	joined := strings.Join(args, " ")
	want := "run F001 --runner claude --max-rounds 5"
	if joined != want {
		t.Errorf("buildRunArgs: got %q, want %q", joined, want)
	}
}

func TestProcessManager_StartRecordsProfile(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, fakeRunCommand(t))
	defer pm.Shutdown()

	info, err := pm.Start("test-feat", "", 5, "quick", nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Profile != "quick" {
		t.Errorf("info.Profile = %q, want quick", info.Profile)
	}
	if pm.List()[0].Profile != "quick" {
		t.Errorf("List()[0].Profile = %q, want quick (cloneRunInfo)", pm.List()[0].Profile)
	}
}

func TestProcessManager_DuplicateFeature(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 2, fakeRunCommand(t))
	defer pm.Shutdown()

	if _, err := pm.Start("test-feat", "", 5, "", nil); err != nil {
		t.Fatal(err)
	}

	_, err := pm.Start("test-feat", "", 5, "", nil)
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

	if _, err := pm.Start("test-feat", "", 5, "", nil); err != nil {
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

// TestEnsureInactive_SkipsNewerFinalState 驗證 W8：runner 在 process 結束後寫了
// final state（UpdatedAt 晚於 endTime），ensureInactive 不可把 Active 改回去蓋掉它。
func TestEnsureInactive_SkipsNewerFinalState(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 1, "echo")

	endTime := time.Now().UTC()

	// runner 寫的 final state：Active 已為 true 模擬尚未清掉，但 UpdatedAt 在 endTime 之後
	s := protocol.State{
		FeatureID:  "test-feat",
		Phase:      protocol.PhasePendingReview,
		Active:     true,
		StopReason: "completed",
	}
	if err := ws.WriteState("test-feat", s); err != nil {
		t.Fatal(err)
	}
	// WriteState 已把 UpdatedAt 設為 now（>= endTime），符合「runner 較新」的情境

	pm.ensureInactive("test-feat", endTime)

	got, err := ws.ReadState("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != protocol.PhasePendingReview {
		t.Errorf("Phase = %q, want pending-review (final state must not be reverted)", got.Phase)
	}
	if got.StopReason != "completed" {
		t.Errorf("StopReason = %q, want completed", got.StopReason)
	}
}

// TestEnsureInactive_ClearsStaleActive 驗證 W8：process 結束前的舊 active state
// （UpdatedAt 早於 endTime）會被正確標記為 inactive 並設 StopReason。
func TestEnsureInactive_ClearsStaleActive(t *testing.T) {
	ws := setupPMWorkspace(t)
	pm := NewProcessManager(ws, 1, "echo")

	s := protocol.State{
		FeatureID: "test-feat",
		Phase:     protocol.PhaseCoding,
		Active:    true,
		Pid:       12345,
	}
	if err := ws.WriteState("test-feat", s); err != nil {
		t.Fatal(err)
	}

	// endTime 設為未來，保證 state.UpdatedAt 早於它
	endTime := time.Now().UTC().Add(time.Hour)
	pm.ensureInactive("test-feat", endTime)

	got, err := ws.ReadState("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	if got.Active {
		t.Error("Active = true, want false")
	}
	if got.Pid != 0 {
		t.Errorf("Pid = %d, want 0", got.Pid)
	}
	if got.StopReason != "process-exit" {
		t.Errorf("StopReason = %q, want process-exit", got.StopReason)
	}
}
