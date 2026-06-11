package protocol

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupWorkspace(t *testing.T) *Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := Config{
		Project: ProjectConfig{Name: "test-project"},
		Default: "claude",
	}
	if err := Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ws, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	return ws
}

func TestFind_FromRoot(t *testing.T) {
	root := t.TempDir()
	if err := Init(root, Config{Project: ProjectConfig{Name: "x"}}); err != nil {
		t.Fatal(err)
	}
	ws, err := Find(root)
	if err != nil {
		t.Fatalf("Find from root: %v", err)
	}
	if ws.Root != root {
		t.Errorf("Root = %s, want %s", ws.Root, root)
	}
}

func TestFind_WalkUp(t *testing.T) {
	root := t.TempDir()
	if err := Init(root, Config{Project: ProjectConfig{Name: "x"}}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := Find(sub)
	if err != nil {
		t.Fatalf("Find from sub: %v", err)
	}
	if ws.Root != root {
		t.Errorf("Root = %s, want %s", ws.Root, root)
	}
}

func TestFind_NotFound(t *testing.T) {
	tmp := t.TempDir()
	_, err := Find(tmp)
	if err == nil {
		t.Error("expected error when .4x/ not found")
	}
}

func TestInit_CreatesStructure(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Project: ProjectConfig{Name: "init-test"}}
	if err := Init(root, cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}

	dotDir := filepath.Join(root, DirName)
	if _, err := os.Stat(dotDir); err != nil {
		t.Errorf(".4x/ not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dotDir, FeaturesDir)); err != nil {
		t.Errorf(".4x/features/ not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dotDir, ConfigFile)); err != nil {
		t.Errorf(".4x/config.yaml not created: %v", err)
	}
}

func TestConfigRoundtrip(t *testing.T) {
	ws := setupWorkspace(t)
	want := Config{
		Project: ProjectConfig{Name: "roundtrip-test", Description: "desc"},
		Default: "claude",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "claude", Model: "opus"},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
			"coder":    {Model: "sonnet"},
		},
	}
	if err := WriteConfig(ws.DotDir(), want); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	got, err := ws.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if got.Project.Name != want.Project.Name {
		t.Errorf("Name = %s, want %s", got.Project.Name, want.Project.Name)
	}
	if got.Default != want.Default {
		t.Errorf("Default = %s, want %s", got.Default, want.Default)
	}
	if got.Roles["designer"].Model != "opus" {
		t.Errorf("designer model = %s, want opus", got.Roles["designer"].Model)
	}
	if got.Roles["coder"].Model != "sonnet" {
		t.Errorf("coder model = %s, want sonnet", got.Roles["coder"].Model)
	}
}

func TestFeatureCRUD(t *testing.T) {
	ws := setupWorkspace(t)

	f := Feature{
		ID:          "test-feature",
		Name:        "Test Feature",
		Description: "A test",
		Status:      "not-started",
	}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	got, err := ws.LoadFeature("test-feature")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if got.ID != f.ID || got.Name != f.Name {
		t.Errorf("LoadFeature = %+v, want ID=%s Name=%s", got, f.ID, f.Name)
	}

	f2 := Feature{ID: "second-feature", Name: "Second", Status: "done"}
	if err := ws.SaveFeature(f2); err != nil {
		t.Fatalf("SaveFeature 2: %v", err)
	}

	list, err := ws.ListFeatures()
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListFeatures count = %d, want 2", len(list))
	}
}

func TestFeatureLoad_NotFound(t *testing.T) {
	ws := setupWorkspace(t)
	_, err := ws.LoadFeature("nonexistent")
	if err == nil {
		t.Error("expected error loading nonexistent feature")
	}
}

func TestStateRoundtrip(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-1"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	want := State{
		FeatureID: "feat-1",
		Phase:     PhaseCoding,
		Role:      RoleCoder,
		Round:     2,
		MaxRounds: 5,
		Active:    true,
		Runner:    "claude",
	}
	if err := ws.WriteState("feat-1", want); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	got, err := ws.ReadState("feat-1")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.FeatureID != want.FeatureID {
		t.Errorf("FeatureID = %s, want %s", got.FeatureID, want.FeatureID)
	}
	if got.Phase != want.Phase {
		t.Errorf("Phase = %s, want %s", got.Phase, want.Phase)
	}
	if got.Round != want.Round {
		t.Errorf("Round = %d, want %d", got.Round, want.Round)
	}
	if !got.UpdatedAt.After(want.UpdatedAt) {
		t.Error("UpdatedAt should be set by WriteState")
	}
}

func TestAppendEvent(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-1"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	events := []Event{
		{Type: "phase-start", Phase: PhaseDesigning, Round: 1},
		{Type: "step", Detail: "analyzing codebase", Round: 1},
		{Type: "phase-end", Phase: PhaseDesigning, Round: 1},
	}
	for _, e := range events {
		if err := ws.AppendEvent("feat-1", e); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	f, err := os.Open(filepath.Join(ws.FeatureDir("feat-1"), EventsFile))
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var count int
	for scanner.Scan() {
		var evt Event
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			t.Errorf("line %d: invalid JSON: %v", count+1, err)
		}
		if evt.Timestamp == "" {
			t.Errorf("line %d: timestamp should be auto-filled", count+1)
		}
		count++
	}
	if count != len(events) {
		t.Errorf("event count = %d, want %d", count, len(events))
	}
}

func TestInitFeatureDir_CreatesRoundsDir(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("my-feat"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}
	roundsDir := filepath.Join(ws.FeatureDir("my-feat"), RoundsDir)
	if _, err := os.Stat(roundsDir); err != nil {
		t.Errorf("rounds/ not created: %v", err)
	}
}

func TestRoundDir(t *testing.T) {
	ws := &Workspace{Root: "/fake"}
	got := ws.RoundDir("feat-1", 3)
	want := "/fake/.4x/feat-1/rounds/round-3"
	if got != want {
		t.Errorf("RoundDir = %s, want %s", got, want)
	}
}
