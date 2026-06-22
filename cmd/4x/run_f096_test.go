package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// mockEnrichRunner 把固定 log 內容寫進 temp file，模擬 enrichment 的 LLM runner 輸出。
type mockEnrichRunner struct {
	logContent string
}

func (m *mockEnrichRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	tmp, _ := os.CreateTemp("", "enrich-f096-*.log")
	tmp.WriteString(m.logContent)
	tmp.Close()
	return &runner.Result{ExitCode: 0, LogFile: tmp.Name()}, nil
}

func setupDiscoverWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "f096-test"}}); err != nil {
		t.Fatal(err)
	}
	return &protocol.Workspace{Root: root}
}

func writeDeepReviewReport(t *testing.T, ws *protocol.Workspace, featureID string, round int, content string) {
	t.Helper()
	dir := ws.RoundDir(featureID, round)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, protocol.DeepReviewReport), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// discoveredOther 回傳 features 中第一個 ID 不等於 seed 的 feature，nil 表示沒有新建。
func discoveredOther(t *testing.T, ws *protocol.Workspace, seedID string) *feat.Feature {
	t.Helper()
	features, err := ws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	for i := range features {
		if features[i].ID != seedID {
			return &features[i]
		}
	}
	return nil
}

const validEnrichLogForTest = `[ENRICHMENT-RESULT]
{
  "subtasks": [
    {"id": "impl", "name": "Implement", "description": "Do it"},
    {"id": "test", "name": "Test", "description": "Test it"}
  ],
  "repos": ["internal/foo"],
  "rules": [],
  "priority": 3,
  "description": "Enriched description"
}
[/ENRICHMENT-RESULT]`

func TestAutoDiscover_EnrichDisabled(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	seed := feat.Feature{ID: "F001-test", Name: "F001: Test", Status: feat.StatusInProgress}
	ws.SaveFeature(seed)

	cfg := protocol.Config{AutoDiscoverFeatures: true, EnrichDiscoveredFeatures: false}
	writeDeepReviewReport(t, ws, seed.ID, 1, "[NEW-FEATURE] Thin Feature\nSome description")

	autoDiscoverFeatures(ws, seed, cfg, 1, nil)

	found := discoveredOther(t, ws, seed.ID)
	if found == nil {
		t.Fatal("expected discovered feature to be created (enrich disabled = old path)")
	}
	if len(found.Subtasks) > 0 {
		t.Error("enrich disabled should produce thin feature without subtasks")
	}
	if found.Status != feat.StatusNotStarted {
		t.Errorf("status = %q, want %q", found.Status, feat.StatusNotStarted)
	}
}

func TestAutoDiscover_EnrichEnabled_DraftMode(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	seed := feat.Feature{ID: "F001-test", Name: "F001: Test", Status: feat.StatusInProgress}
	ws.SaveFeature(seed)

	cfg := protocol.Config{
		AutoDiscoverFeatures:     true,
		EnrichDiscoveredFeatures: true,
		EnrichAutoApprove:        false,
	}
	writeDeepReviewReport(t, ws, seed.ID, 1, "[NEW-FEATURE] Rich Feature\nNeeds enrichment")

	r := &mockEnrichRunner{logContent: validEnrichLogForTest}
	autoDiscoverFeatures(ws, seed, cfg, 1, r)

	found := discoveredOther(t, ws, seed.ID)
	if found == nil {
		t.Fatal("expected discovered feature to be created")
	}
	if found.Status != feat.StatusDraft {
		t.Errorf("status = %q, want %q", found.Status, feat.StatusDraft)
	}
	if len(found.Subtasks) < 2 {
		t.Errorf("subtasks = %d, want >= 2", len(found.Subtasks))
	}
}

func TestAutoDiscover_EnrichEnabled_AutoApprove(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	seed := feat.Feature{ID: "F001-test", Name: "F001: Test", Status: feat.StatusInProgress}
	ws.SaveFeature(seed)

	cfg := protocol.Config{
		AutoDiscoverFeatures:     true,
		EnrichDiscoveredFeatures: true,
		EnrichAutoApprove:        true,
	}
	writeDeepReviewReport(t, ws, seed.ID, 1, "[NEW-FEATURE] Rich Feature\nNeeds enrichment")

	r := &mockEnrichRunner{logContent: validEnrichLogForTest}
	autoDiscoverFeatures(ws, seed, cfg, 1, r)

	found := discoveredOther(t, ws, seed.ID)
	if found == nil {
		t.Fatal("expected discovered feature to be created")
	}
	if found.Status != feat.StatusNotStarted {
		t.Errorf("status = %q, want %q", found.Status, feat.StatusNotStarted)
	}
	if found.Priority == nil || *found.Priority != 3 {
		t.Errorf("priority = %v, want 3", found.Priority)
	}
}

func TestAutoDiscover_EnrichFailed_Discarded(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	seed := feat.Feature{ID: "F001-test", Name: "F001: Test", Status: feat.StatusInProgress}
	ws.SaveFeature(seed)

	cfg := protocol.Config{
		AutoDiscoverFeatures:     true,
		EnrichDiscoveredFeatures: true,
		EnrichAutoApprove:        true,
	}
	writeDeepReviewReport(t, ws, seed.ID, 1, "[NEW-FEATURE] Bad Feature\nVague description")

	r := &mockEnrichRunner{logContent: "[ENRICHMENT-RESULT]\n{\"subtasks\":[],\"priority\":0}\n[/ENRICHMENT-RESULT]"}
	autoDiscoverFeatures(ws, seed, cfg, 1, r)

	if found := discoveredOther(t, ws, seed.ID); found != nil {
		t.Errorf("expected 0 discovered features (enrich failed), got %s", found.ID)
	}

	report, err := os.ReadFile(filepath.Join(ws.FeatureDir(seed.ID), "discovered-features.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "Enrichment Failed") {
		t.Error("report missing Enrichment Failed section")
	}
}
