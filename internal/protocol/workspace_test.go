package protocol

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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
		Priority:    ptrInt(2),
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

func TestReadTestStrategy_Absent(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-ts"); err != nil {
		t.Fatal(err)
	}
	ts, err := ws.ReadTestStrategy("feat-ts")
	if err != nil {
		t.Fatalf("expected nil error for absent file, got %v", err)
	}
	if ts.HealthCheck != nil {
		t.Errorf("expected nil HealthCheck, got %v", ts.HealthCheck)
	}
}

func TestReadTestStrategy_WithHealthCheck(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-ts"); err != nil {
		t.Fatal(err)
	}
	content := "verify_commands:\n  - go test ./...\nhealth_check:\n  commands:\n    - make build\n  recovery:\n    - make dev-up\n  timeout: 60\n"
	path := filepath.Join(ws.FeatureDir("feat-ts"), TestStratFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ts, err := ws.ReadTestStrategy("feat-ts")
	if err != nil {
		t.Fatalf("ReadTestStrategy: %v", err)
	}
	if ts.HealthCheck == nil {
		t.Fatal("expected non-nil HealthCheck")
	}
	if len(ts.HealthCheck.Commands) != 1 || ts.HealthCheck.Commands[0] != "make build" {
		t.Errorf("commands = %v", ts.HealthCheck.Commands)
	}
	if ts.HealthCheck.Timeout != 60 {
		t.Errorf("timeout = %d, want 60", ts.HealthCheck.Timeout)
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

func TestDiscoverScreenshots(t *testing.T) {
	ws := setupWorkspace(t)
	const featureID = "feat-shot"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	round2Dir := ws.RoundDir(featureID, 2)
	if err := os.MkdirAll(round2Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	verify := VerifyEvidence{
		Passed: true,
		Round:  2,
		Role:   RoleTester,
		Screenshots: []Screenshot{
			{Path: ".4x/e2e/feat-shot/screenshot/02-round-two.png", Step: "02", Description: "round two"},
		},
	}
	verifyData, _ := json.Marshal(verify)
	if err := os.WriteFile(filepath.Join(round2Dir, VerifyFile), verifyData, 0o644); err != nil {
		t.Fatal(err)
	}

	shotDir := filepath.Join(ws.DotDir(), "e2e", featureID, "screenshot")
	if err := os.MkdirAll(shotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"01-round-one.png", "02-round-two.png", "03-third-shot.webp"} {
		if err := os.WriteFile(filepath.Join(shotDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := ws.DiscoverScreenshots(featureID, DefaultScreenshotDir)
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	// DefaultScreenshotDir 沒有 {round} 佔位符，dir 掃描會分配到最新已知 round（2）。
	// round 2 包含 verify.json 的 02-round-two.png，加上 dir 掃描的 01-round-one.png 與 03-third-shot.webp。
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (all go to latest round)", len(groups))
	}
	if groups[0].Round != 2 || len(groups[0].Screenshots) != 3 {
		t.Fatalf("round2 = %+v, want 3 screenshots", groups[0])
	}
	var thirdDesc string
	for _, s := range groups[0].Screenshots {
		if filepath.Base(s.Path) == "03-third-shot.webp" {
			thirdDesc = s.Description
		}
	}
	if thirdDesc != "third shot" {
		t.Errorf("description = %q, want %q", thirdDesc, "third shot")
	}
}

func TestDiscoverScreenshotsNoDir(t *testing.T) {
	ws := setupWorkspace(t)
	const featureID = "feat-no-shot"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	groups, err := ws.DiscoverScreenshots(featureID, ".4x/e2e/{feature-id}/missing/")
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("groups = %d, want 0", len(groups))
	}
}

func TestDiscoverScreenshotsMerge(t *testing.T) {
	ws := setupWorkspace(t)
	const featureID = "feat-merge-shot"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	round1Dir := ws.RoundDir(featureID, 1)
	if err := os.MkdirAll(round1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	verify := VerifyEvidence{
		Passed: true,
		Round:  1,
		Role:   RoleTester,
		Screenshots: []Screenshot{
			{Path: "e2e/feat-merge-shot/screenshot/01-login.png", Step: "01", Description: "login"},
		},
	}
	verifyData, _ := json.Marshal(verify)
	if err := os.WriteFile(filepath.Join(round1Dir, VerifyFile), verifyData, 0o644); err != nil {
		t.Fatal(err)
	}

	shotDir := filepath.Join(ws.DotDir(), "e2e", featureID, "screenshot")
	if err := os.MkdirAll(shotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"01-login.png", "02-run-modal.png"} {
		if err := os.WriteFile(filepath.Join(shotDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := ws.DiscoverScreenshots(featureID, DefaultScreenshotDir)
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	paths := []string{groups[0].Screenshots[0].Path, groups[0].Screenshots[1].Path}
	want := []string{
		"e2e/feat-merge-shot/screenshot/01-login.png",
		"e2e/feat-merge-shot/screenshot/02-run-modal.png",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestDiscoverScreenshotsSameFilenameAcrossRounds(t *testing.T) {
	ws := setupWorkspace(t)
	const featureID = "feat-same-name"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	for _, round := range []int{1, 2} {
		if err := os.MkdirAll(ws.RoundDir(featureID, round), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for _, dir := range []string{
		filepath.Join(ws.DotDir(), "e2e", featureID, "round-1"),
		filepath.Join(ws.DotDir(), "e2e", featureID, "round-2"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "01-login.png"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := ws.DiscoverScreenshots(featureID, ".4x/e2e/{feature-id}/round-{round}/")
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if len(groups[0].Screenshots) != 1 || len(groups[1].Screenshots) != 1 {
		t.Fatalf("screenshots = %+v, want one per round", groups)
	}
	if groups[0].Screenshots[0].Path == groups[1].Screenshots[0].Path {
		t.Fatalf("paths should differ by round, got %+v", groups)
	}
}

func TestDiscoverScreenshotsRoundPlaceholderFallbackDiscovery(t *testing.T) {
	ws := setupWorkspace(t)
	const featureID = "feat-round-fallback"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	for _, round := range []int{2, 3} {
		dir := filepath.Join(ws.DotDir(), "e2e", featureID, "round-"+strconv.Itoa(round))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "01-shot.png"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := ws.DiscoverScreenshots(featureID, ".4x/e2e/{feature-id}/round-{round}/")
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Round != 2 || groups[1].Round != 3 {
		t.Fatalf("rounds = [%d %d], want [2 3]", groups[0].Round, groups[1].Round)
	}
	if len(groups[0].Screenshots) != 1 || len(groups[1].Screenshots) != 1 {
		t.Fatalf("screenshots = %+v, want one per discovered round", groups)
	}
}

func TestDiscoverScreenshotsInvalidRoundsRejected(t *testing.T) {
	ws := setupWorkspace(t)
	const featureID = "feat-invalid-rounds"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	// Create directories with invalid round numbers (0, -1, 1)
	for _, roundStr := range []string{"round-0", "round--1", "round-1"} {
		dir := filepath.Join(ws.DotDir(), "e2e", featureID, roundStr)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "01-shot.png"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := ws.DiscoverScreenshots(featureID, ".4x/e2e/{feature-id}/round-{round}/")
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	// Only round 1 should be discovered; round-0 and round--1 should be rejected
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (only valid round)", len(groups))
	}
	if groups[0].Round != 1 {
		t.Fatalf("round = %d, want 1", groups[0].Round)
	}
	if len(groups[0].Screenshots) != 1 {
		t.Fatalf("screenshots = %d, want 1", len(groups[0].Screenshots))
	}
}

func TestDiscoverScreenshotsRoundUnion(t *testing.T) {
	// verify.json 有 round 1，目錄額外有 round 2 與 round 3，應合併為三組
	ws := setupWorkspace(t)
	const featureID = "feat-round-union"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	// round 1 有 verify.json，記錄 screenshots
	round1Dir := ws.RoundDir(featureID, 1)
	if err := os.MkdirAll(round1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	verify := VerifyEvidence{
		Passed: true, Round: 1, Role: RoleTester,
		Screenshots: []Screenshot{
			{Path: "e2e/feat-round-union/round-1/01-login.png", Step: "01", Description: "login"},
		},
	}
	verifyData, _ := json.Marshal(verify)
	if err := os.WriteFile(filepath.Join(round1Dir, VerifyFile), verifyData, 0o644); err != nil {
		t.Fatal(err)
	}

	// round 2 與 round 3 只有截圖目錄，沒有 verify.json
	for _, round := range []int{2, 3} {
		dir := filepath.Join(ws.DotDir(), "e2e", featureID, "round-"+strconv.Itoa(round))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "01-shot.png"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := ws.DiscoverScreenshots(featureID, ".4x/e2e/{feature-id}/round-{round}/")
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3 (rounds 1,2,3 merged)", len(groups))
	}
	for i, want := range []int{1, 2, 3} {
		if groups[i].Round != want {
			t.Fatalf("groups[%d].Round = %d, want %d", i, groups[i].Round, want)
		}
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

func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
	if ProcessAlive(0) {
		t.Error("pid 0 should not be alive")
	}
	if ProcessAlive(-1) {
		t.Error("pid -1 should not be alive")
	}
}

func TestReconcileActive_DeadPid(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-recon"); err != nil {
		t.Fatal(err)
	}
	s := State{
		FeatureID: "feat-recon",
		Phase:     PhaseCoding,
		Active:    true,
		Pid:       999999999,
	}
	if err := ws.WriteState("feat-recon", s); err != nil {
		t.Fatal(err)
	}

	ws.ReconcileActive("feat-recon", &s)

	if s.Active {
		t.Error("Active should be false after reconcile with dead PID")
	}
	if s.StopReason != "process-gone" {
		t.Errorf("StopReason = %q, want process-gone", s.StopReason)
	}

	persisted, err := ws.ReadState("feat-recon")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Active {
		t.Error("persisted Active should be false")
	}
}

func TestReconcileActive_LivePid(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-live"); err != nil {
		t.Fatal(err)
	}
	s := State{
		FeatureID: "feat-live",
		Phase:     PhaseCoding,
		Active:    true,
		Pid:       os.Getpid(),
	}
	if err := ws.WriteState("feat-live", s); err != nil {
		t.Fatal(err)
	}

	ws.ReconcileActive("feat-live", &s)

	if !s.Active {
		t.Error("Active should remain true for live PID")
	}
}

func TestReconcileActive_ZeroPid(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.InitFeatureDir("feat-zero"); err != nil {
		t.Fatal(err)
	}
	s := State{
		FeatureID: "feat-zero",
		Phase:     PhaseCoding,
		Active:    true,
		Pid:       0,
	}
	if err := ws.WriteState("feat-zero", s); err != nil {
		t.Fatal(err)
	}

	ws.ReconcileActive("feat-zero", &s)

	if s.Active {
		t.Error("Active should be false when PID is 0")
	}
}

func TestProcessAlive_EPERM(t *testing.T) {
	// PID 1 通常是 launchd/init，kill(1, 0) 回傳 EPERM 但表示 process 存在
	err := syscall.Kill(1, 0)
	if err == syscall.EPERM {
		if !ProcessAlive(1) {
			t.Error("PID 1 should be considered alive (EPERM means it exists)")
		}
	}
}

func TestIsScreenshotFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"photo.png", true},
		{"photo.PNG", true},
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"photo.webp", true},
		{"photo.gif", false},
		{"photo.txt", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsScreenshotFile(tt.name); got != tt.want {
			t.Errorf("IsScreenshotFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestNormalizeScreenshotPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{".4x/e2e/feat/screenshot/01.png", "e2e/feat/screenshot/01.png"},
		{"./e2e/feat/01.png", "e2e/feat/01.png"},
		{"e2e/feat/01.png", "e2e/feat/01.png"},
		{"  .4x/foo.png  ", "foo.png"},
	}
	for _, tt := range tests {
		if got := NormalizeScreenshotPath(tt.input); got != tt.want {
			t.Errorf("NormalizeScreenshotPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDiscoverScreenshots_NoRoundPlaceholder_UsesLatestRound(t *testing.T) {
	ws := setupWorkspace(t)
	const featureID = "feat-no-round"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	for _, r := range []int{1, 3} {
		rd := ws.RoundDir(featureID, r)
		if err := os.MkdirAll(rd, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	shotDir := filepath.Join(ws.DotDir(), "e2e", featureID, "screenshot")
	if err := os.MkdirAll(shotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shotDir, "01-login.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	groups, err := ws.DiscoverScreenshots(featureID, DefaultScreenshotDir)
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if groups[0].Round != 3 {
		t.Errorf("round = %d, want 3 (latest known round)", groups[0].Round)
	}
}

func TestDiscoverScreenshots_OutsideDotDir(t *testing.T) {
	ws := setupWorkspace(t)
	const featureID = "feat-outside"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	shotDir := filepath.Join(ws.Root, "screenshots", featureID)
	if err := os.MkdirAll(shotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shotDir, "01-login.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	groups, err := ws.DiscoverScreenshots(featureID, "screenshots/{feature-id}/")
	if err != nil {
		t.Fatalf("DiscoverScreenshots: %v", err)
	}
	total := 0
	for _, g := range groups {
		total += len(g.Screenshots)
	}
	if total != 1 {
		t.Errorf("total screenshots = %d, want 1", total)
	}
}

func TestNormalizeScreenshotPath_CleansDotDot(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"foo/../../etc/passwd.png", "../etc/passwd.png"},
		{"e2e/feat/../feat2/01.png", "e2e/feat2/01.png"},
	}
	for _, tt := range tests {
		got := NormalizeScreenshotPath(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeScreenshotPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func ptrInt(v int) *int { return &v }

// TestWriteState_Atomic_NoPartialRead 驗證 W14：並行 WriteState/ReadState 下，
// ReadState 永遠讀到完整可 Unmarshal 的 JSON，不會撞到 truncated/partial 寫入。
func TestWriteState_Atomic_NoPartialRead(t *testing.T) {
	ws := setupWorkspace(t)
	const fid = "atomic-feat"
	if err := ws.InitFeatureDir(fid); err != nil {
		t.Fatal(err)
	}

	base := State{FeatureID: fid, Phase: PhaseCoding, Role: RoleCoder, Round: 1}
	if err := ws.WriteState(fid, base); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// writer：持續以不同 Round 覆寫
	wg.Add(1)
	go func() {
		defer wg.Done()
		for round := 0; ; round++ {
			select {
			case <-stop:
				return
			default:
			}
			s := base
			s.Round = round
			if err := ws.WriteState(fid, s); err != nil {
				t.Errorf("WriteState: %v", err)
				return
			}
		}
	}()

	// readers：持續讀，任何一次讀到 partial JSON 都會讓 ReadState 回 error
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if _, err := ws.ReadState(fid); err != nil {
					t.Errorf("ReadState saw partial/truncated state: %v", err)
					return
				}
			}
		}()
	}

	// 跑一小段時間後停 writer
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	// 不該有殘留的 .state-*.json temp 檔
	entries, err := os.ReadDir(ws.FeatureDir(fid))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".state-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// AC-2：WriteBatchConflict → ReadBatchConflict 取回相同內容。
func TestBatchConflictRoundtrip(t *testing.T) {
	ws := setupWorkspace(t)
	want := BatchConflict{
		FeatureID:    "F099",
		FeatureName:  "Some Feature",
		ConflictRepo: "core",
		Files:        []string{"main.go", "util.go"},
		DetectedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := ws.WriteBatchConflict(want); err != nil {
		t.Fatalf("WriteBatchConflict: %v", err)
	}
	got, err := ws.ReadBatchConflict()
	if err != nil {
		t.Fatalf("ReadBatchConflict: %v", err)
	}
	if got == nil {
		t.Fatal("ReadBatchConflict returned nil after write")
	}
	if got.FeatureID != want.FeatureID || got.FeatureName != want.FeatureName ||
		got.ConflictRepo != want.ConflictRepo || !reflect.DeepEqual(got.Files, want.Files) {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", *got, want)
	}
}

// AC-2：檔案不存在時 ReadBatchConflict 回 (nil, nil)。
func TestReadBatchConflict_Missing(t *testing.T) {
	ws := setupWorkspace(t)
	got, err := ws.ReadBatchConflict()
	if err != nil {
		t.Fatalf("ReadBatchConflict on missing file: err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("ReadBatchConflict on missing file: got %+v, want nil", got)
	}
}

// AC-2：ClearBatchConflict 在檔案不存在時不報錯；存在時刪除之。
func TestClearBatchConflict(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.ClearBatchConflict(); err != nil {
		t.Errorf("ClearBatchConflict on missing file: err = %v, want nil", err)
	}
	if err := ws.WriteBatchConflict(BatchConflict{FeatureID: "F1"}); err != nil {
		t.Fatalf("WriteBatchConflict: %v", err)
	}
	if err := ws.ClearBatchConflict(); err != nil {
		t.Fatalf("ClearBatchConflict: %v", err)
	}
	if got, _ := ws.ReadBatchConflict(); got != nil {
		t.Error("conflict file should be gone after ClearBatchConflict")
	}
}

// F075：WriteBatchPID / ReadBatchPID 往返寫讀同一 PID。
func TestBatchPIDRoundtrip(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.WriteBatchPID(12345); err != nil {
		t.Fatalf("WriteBatchPID: %v", err)
	}
	got, err := ws.ReadBatchPID()
	if err != nil {
		t.Fatalf("ReadBatchPID: %v", err)
	}
	if got != 12345 {
		t.Errorf("ReadBatchPID = %d, want 12345", got)
	}
}

// F075：檔案不存在時 ReadBatchPID 回 (0, nil)。
func TestReadBatchPID_Missing(t *testing.T) {
	ws := setupWorkspace(t)
	got, err := ws.ReadBatchPID()
	if err != nil {
		t.Fatalf("ReadBatchPID on missing file: err = %v, want nil", err)
	}
	if got != 0 {
		t.Errorf("ReadBatchPID on missing file: got %d, want 0", got)
	}
}

// F075：內容無法解析時 ReadBatchPID 回 (0, error)。
func TestReadBatchPID_Unparseable(t *testing.T) {
	ws := setupWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws.DotDir(), BatchPIDFile), []byte("not-a-pid"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ws.ReadBatchPID()
	if err == nil {
		t.Error("ReadBatchPID on unparseable content: err = nil, want error")
	}
	if got != 0 {
		t.Errorf("ReadBatchPID on unparseable content: got %d, want 0", got)
	}
}

// F075：ClearBatchPID 在檔案不存在時不報錯；存在時刪除之。
func TestClearBatchPID(t *testing.T) {
	ws := setupWorkspace(t)
	if err := ws.ClearBatchPID(); err != nil {
		t.Errorf("ClearBatchPID on missing file: err = %v, want nil", err)
	}
	if err := ws.WriteBatchPID(999); err != nil {
		t.Fatalf("WriteBatchPID: %v", err)
	}
	if err := ws.ClearBatchPID(); err != nil {
		t.Fatalf("ClearBatchPID: %v", err)
	}
	if got, _ := ws.ReadBatchPID(); got != 0 {
		t.Error("pid file should be gone after ClearBatchPID")
	}
}

func TestSyncFeatureStatus(t *testing.T) {
	ws := setupWorkspace(t)

	f := Feature{ID: "feat-1", Name: "Test", Status: StatusNotStarted}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	if err := ws.SyncFeatureStatus("feat-1", PhaseCoding); err != nil {
		t.Fatal(err)
	}

	got, err := ws.LoadFeature("feat-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusInProgress {
		t.Errorf("status = %s, want %s", got.Status, StatusInProgress)
	}
}

func TestSyncFeatureStatus_Done(t *testing.T) {
	ws := setupWorkspace(t)

	f := Feature{ID: "feat-2", Name: "Test", Status: StatusInProgress}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	if err := ws.SyncFeatureStatus("feat-2", PhaseDone); err != nil {
		t.Fatal(err)
	}

	got, err := ws.LoadFeature("feat-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDone {
		t.Errorf("status = %s, want %s", got.Status, StatusDone)
	}
}

func TestSyncFeatureStatus_NotFound(t *testing.T) {
	ws := setupWorkspace(t)

	err := ws.SyncFeatureStatus("nonexist", PhaseCoding)
	if err == nil {
		t.Error("expected error for missing feature")
	}
}

func TestLoadMergedConfig(t *testing.T) {
	tmp := t.TempDir()
	dotDir := filepath.Join(tmp, ".4x")
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Project: ProjectConfig{Name: "test-proj"},
		Default: "claude",
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dotDir, "settings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := &Workspace{Root: tmp}
	got, err := ws.LoadMergedConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Project.Name != "test-proj" {
		t.Errorf("Project.Name = %q, want test-proj", got.Project.Name)
	}
	if got.Default != "claude" {
		t.Errorf("Default = %q, want claude", got.Default)
	}
}

func TestLoadMergedConfig_NoProjectConfig_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	ws := &Workspace{Root: tmp}
	if _, err := ws.LoadMergedConfig(); err == nil {
		t.Fatal("expected error when settings.json missing")
	}
}
