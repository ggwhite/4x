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
		t.Errorf(".4x/settings.json not created: %v", err)
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

func TestCompareBacklogMirror_Matching(t *testing.T) {
	features := []Feature{{
		ID:          "feat-a",
		Name:        "Feature A",
		Description: "desc",
		Status:      "not-started",
	}}
	mirror := BacklogMirror{Features: []BacklogFeature{{
		ID:          "feat-a",
		Name:        "Feature A",
		Description: "desc",
		Status:      "not-started",
	}}}

	drift := CompareBacklogMirror(features, mirror)
	if len(drift) != 0 {
		t.Fatalf("drift = %+v, want none", drift)
	}
}

func TestCompareBacklogMirror_MissingExtraAndMismatch(t *testing.T) {
	features := []Feature{
		{ID: "feat-b", Name: "Feature B", Description: "desc b", Status: "done"},
		{ID: "feat-a", Name: "Feature A", Description: "desc a", Status: "not-started"},
	}
	mirror := BacklogMirror{Features: []BacklogFeature{
		{ID: "feat-a", Name: "Old Feature A", Description: "desc a", Status: "todo"},
		{ID: "feat-c", Name: "Feature C", Status: "todo"},
	}}

	drift := CompareBacklogMirror(features, mirror)
	if len(drift) != 4 {
		t.Fatalf("drift count = %d, want 4: %+v", len(drift), drift)
	}

	want := []BacklogDrift{
		{Kind: BacklogDriftMismatch, FeatureID: "feat-a", Field: "name"},
		{Kind: BacklogDriftMismatch, FeatureID: "feat-a", Field: "status"},
		{Kind: BacklogDriftMissing, FeatureID: "feat-b"},
		{Kind: BacklogDriftExtra, FeatureID: "feat-c"},
	}
	for i := range want {
		if drift[i].Kind != want[i].Kind || drift[i].FeatureID != want[i].FeatureID || drift[i].Field != want[i].Field {
			t.Fatalf("drift[%d] = %+v, want %+v", i, drift[i], want[i])
		}
		if drift[i].Message == "" {
			t.Fatalf("drift[%d] missing message", i)
		}
	}
}

func TestCompareBacklogMirror_MissingPriority(t *testing.T) {
	features := []Feature{{
		ID:          "feat-a",
		Name:        "Feature A",
		Description: "desc",
		Status:      "not-started",
		Priority:    2,
	}}
	mirror := BacklogMirror{Features: []BacklogFeature{{
		ID:          "feat-a",
		Name:        "Feature A",
		Description: "desc",
		Status:      "not-started",
	}}}

	drift := CompareBacklogMirror(features, mirror)
	if len(drift) != 1 {
		t.Fatalf("drift count = %d, want 1: %+v", len(drift), drift)
	}
	if drift[0].Kind != BacklogDriftMismatch || drift[0].FeatureID != "feat-a" || drift[0].Field != "priority" {
		t.Fatalf("drift[0] = %+v, want priority mismatch", drift[0])
	}
}

func TestWorkspaceCompareBacklogMirror_AbsentFile(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.SaveFeature(Feature{ID: "feat-a", Name: "Feature A", Status: "not-started"}); err != nil {
		t.Fatal(err)
	}

	drift, err := ws.CompareBacklogMirror()
	if err != nil {
		t.Fatalf("CompareBacklogMirror: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("drift = %+v, want none when %s is absent", drift, BacklogFile)
	}
}

func TestWorkspaceCompareBacklogMirror_ReadsRootFeatureList(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.SaveFeature(Feature{ID: "feat-a", Name: "Feature A", Status: "done"}); err != nil {
		t.Fatal(err)
	}
	data := `{"version":1,"features":[{"id":"feat-a","name":"Feature A","status":"todo"}]}`
	if err := os.WriteFile(filepath.Join(ws.Root, BacklogFile), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	drift, err := ws.CompareBacklogMirror()
	if err != nil {
		t.Fatalf("CompareBacklogMirror: %v", err)
	}
	if len(drift) != 1 || drift[0].FeatureID != "feat-a" || drift[0].Field != "status" {
		t.Fatalf("drift = %+v, want status mismatch for feat-a", drift)
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

func TestAppendEventWithRunnerModel(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-rm"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	evt := Event{
		Type:   "phase-start",
		Phase:  PhaseDesigning,
		Round:  1,
		Runner: "claude",
		Model:  "opus",
	}
	if err := ws.AppendEvent("feat-rm", evt); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	f, err := os.Open(filepath.Join(ws.FeatureDir("feat-rm"), EventsFile))
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	defer f.Close()

	var got Event
	scanner := bufio.NewScanner(f)
	scanner.Scan()
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Runner != "claude" {
		t.Errorf("Runner = %q, want %q", got.Runner, "claude")
	}
	if got.Model != "opus" {
		t.Errorf("Model = %q, want %q", got.Model, "opus")
	}
}

func TestEventBackwardCompat(t *testing.T) {
	raw := `{"ts":"2026-01-01T00:00:00Z","phase":"designing","type":"phase-start","round":1}`
	var evt Event
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal old event: %v", err)
	}
	if evt.Runner != "" {
		t.Errorf("Runner should be empty for old events, got %q", evt.Runner)
	}
	if evt.Model != "" {
		t.Errorf("Model should be empty for old events, got %q", evt.Model)
	}
	if evt.Phase != PhaseDesigning {
		t.Errorf("Phase = %q, want %q", evt.Phase, PhaseDesigning)
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

func TestStateRunnersRoundtrip(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-runners"); err != nil {
		t.Fatalf("InitFeatureDir: %v", err)
	}

	want := State{
		FeatureID: "feat-runners",
		Phase:     PhaseCoding,
		Role:      RoleCoder,
		Round:     1,
		MaxRounds: 5,
		Active:    true,
		Runner:    "claude",
		Runners:   []string{"codex", "claude"},
	}
	if err := ws.WriteState("feat-runners", want); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	got, err := ws.ReadState("feat-runners")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if len(got.Runners) != 2 {
		t.Fatalf("Runners length = %d, want 2", len(got.Runners))
	}
	if got.Runners[0] != "codex" || got.Runners[1] != "claude" {
		t.Errorf("Runners = %v, want [codex claude]", got.Runners)
	}
}

func TestStateBackwardCompatNoRunners(t *testing.T) {
	raw := `{"featureId":"feat-old","phase":"coding","role":"coder","round":1,"maxRounds":5,"active":true,"runner":"claude","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}`
	var s State
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("unmarshal old state: %v", err)
	}
	if s.Runners != nil {
		t.Errorf("Runners should be nil for old state, got %v", s.Runners)
	}
	if s.Runner != "claude" {
		t.Errorf("Runner = %q, want %q", s.Runner, "claude")
	}
}

func TestUserConfig_RoundTrip(t *testing.T) {
	tmpHome := t.TempDir()

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	cfg := UserConfig{
		Locale:        "zh-TW",
		Theme:         "dark",
		DefaultRunner: "claude",
		Runners: map[string]RunnerConfig{
			"claude": {Command: "/usr/local/bin/claude", Tty: BoolPtr(true)},
		},
		Roles: map[string]RoleConfig{
			"designer": {Model: "opus"},
		},
	}
	if err := WriteUserConfig(cfg); err != nil {
		t.Fatalf("WriteUserConfig: %v", err)
	}

	got, err := ReadUserConfig()
	if err != nil {
		t.Fatalf("ReadUserConfig: %v", err)
	}
	if got.Locale != "zh-TW" {
		t.Errorf("Locale = %q, want zh-TW", got.Locale)
	}
	if got.Theme != "dark" {
		t.Errorf("Theme = %q, want dark", got.Theme)
	}
	if got.DefaultRunner != "claude" {
		t.Errorf("DefaultRunner = %q, want claude", got.DefaultRunner)
	}
	if got.Runners["claude"].Command != "/usr/local/bin/claude" {
		t.Errorf("Runners[claude].Command = %q", got.Runners["claude"].Command)
	}
	if !BoolVal(got.Runners["claude"].Tty) {
		t.Error("Runners[claude].Tty should be true")
	}
	if got.Roles["designer"].Model != "opus" {
		t.Errorf("Roles[designer].Model = %q, want opus", got.Roles["designer"].Model)
	}
}
