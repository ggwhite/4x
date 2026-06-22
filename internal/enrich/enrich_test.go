package enrich

import (
	"context"
	"os"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// mockRunner 把預設的 log 內容寫進 temp file，模擬 LLM runner 輸出。
type mockRunner struct {
	logContent string
	exitCode   int
	err        error
}

func (m *mockRunner) Run(_ context.Context, _ string) (*runner.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	tmp, _ := os.CreateTemp("", "enrich-test-*.log")
	tmp.WriteString(m.logContent)
	tmp.Close()
	return &runner.Result{ExitCode: m.exitCode, LogFile: tmp.Name()}, nil
}

func newTestWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	dir := t.TempDir()
	if err := protocol.Init(dir, protocol.Config{Project: protocol.ProjectConfig{Name: "enrich-test"}}); err != nil {
		t.Fatal(err)
	}
	return &protocol.Workspace{Root: dir}
}

const validEnrichLog = `Thinking about the feature...
[ENRICHMENT-RESULT]
{
  "subtasks": [
    {"id": "impl-core", "name": "Implement core", "description": "Core logic"},
    {"id": "add-tests", "name": "Add tests", "description": "Unit tests"}
  ],
  "repos": ["internal/protocol"],
  "rules": ["no breaking changes"],
  "priority": 3,
  "description": "Enhanced: add retry logic for failed operations"
}
[/ENRICHMENT-RESULT]
Done.`

func TestEnrich_Success_AutoApprove(t *testing.T) {
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: validEnrichLog}, true)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title:       "Add retry logic",
		Description: "Retry failed operations",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if result.Discarded {
		t.Fatalf("Enrich() discarded: %s", result.Reason)
	}
	f := result.Feature
	if len(f.Subtasks) != 2 {
		t.Errorf("subtasks = %d, want 2", len(f.Subtasks))
	}
	if f.Status != feature.StatusNotStarted {
		t.Errorf("status = %q, want %q", f.Status, feature.StatusNotStarted)
	}
	if f.Priority == nil || *f.Priority != 3 {
		t.Errorf("priority = %v, want 3", f.Priority)
	}
}

func TestEnrich_Success_DraftMode(t *testing.T) {
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: validEnrichLog}, false)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title:       "Add retry logic",
		Description: "Retry failed operations",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if result.Feature.Status != feature.StatusDraft {
		t.Errorf("status = %q, want %q", result.Feature.Status, feature.StatusDraft)
	}
}

func TestEnrich_Discarded_InsufficientSubtasks(t *testing.T) {
	log := `[ENRICHMENT-RESULT]
{"subtasks":[{"id":"only-one","name":"Only one","description":"desc"}],"repos":["x"],"priority":3,"description":"desc"}
[/ENRICHMENT-RESULT]`
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: log}, true)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title: "Foo", Description: "bar",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if !result.Discarded {
		t.Error("expected Discarded = true")
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason")
	}
}

func TestEnrich_Discarded_InvalidJSON(t *testing.T) {
	log := "[ENRICHMENT-RESULT]\n{bad json}\n[/ENRICHMENT-RESULT]"
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: log}, true)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title: "Foo", Description: "bar",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if !result.Discarded {
		t.Error("expected Discarded = true")
	}
}

func TestEnrich_RunnerError(t *testing.T) {
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{err: context.DeadlineExceeded}, true)
	_, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title: "Foo", Description: "bar",
	})
	if err == nil {
		t.Error("expected error from runner failure")
	}
}

func TestEnrich_RunnerNonZeroExit(t *testing.T) {
	ws := newTestWorkspace(t)
	e := New(ws, &mockRunner{logContent: "no markers", exitCode: 1}, true)
	result, err := e.Enrich(context.Background(), protocol.DiscoveredFeature{
		Title: "Foo", Description: "bar",
	})
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}
	if !result.Discarded {
		t.Error("expected Discarded for non-zero exit + no markers")
	}
}
