package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

func setupDiscoverWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}); err != nil {
		t.Fatal(err)
	}
	return &protocol.Workspace{Root: root}
}

func writeFeatureFile(t *testing.T, ws *protocol.Workspace, featureID, name, content string) {
	t.Helper()
	dir := ws.FeatureDir(featureID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir feature dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
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

type mockEnrichRunner struct {
	logContent string
}

func (m *mockEnrichRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	tmp, _ := os.CreateTemp("", "enrich-*.log")
	tmp.WriteString(m.logContent)
	tmp.Close()
	return &runner.Result{ExitCode: 0, LogFile: tmp.Name()}, nil
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
