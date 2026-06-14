package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestIntegration_FullHookCycle(t *testing.T) {
	logDir := t.TempDir()
	markerDir := t.TempDir()
	marker := filepath.Join(markerDir, "marker")

	hooks := []protocol.HookEntry{
		{Run: "touch " + marker, OnFail: "block"},
		{Run: "echo done", OnFail: "warn"},
	}

	results, err := Execute(hooks, logDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("marker file should exist after hook execution")
	}

	for _, r := range results {
		evt := ToEvent(r, protocol.PhaseCoding, "pre_coding")
		if evt.Type != "hook" {
			t.Errorf("event type = %q, want hook", evt.Type)
		}
		if !strings.Contains(evt.Detail, "exit 0") {
			t.Errorf("detail should contain exit code, got %q", evt.Detail)
		}
	}

	entries, _ := os.ReadDir(logDir)
	if len(entries) != 2 {
		t.Errorf("expected 2 log files, got %d", len(entries))
	}
}

func TestIntegration_MixedBlockWarn(t *testing.T) {
	logDir := t.TempDir()

	hooks := []protocol.HookEntry{
		{Run: "echo ok", OnFail: "warn"},
		{Run: "exit 42", OnFail: "warn"},
		{Run: "echo after-warn"},
	}

	results, err := Execute(hooks, logDir)
	if err != nil {
		t.Fatalf("warn hooks should not stop execution: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[1].ExitCode != 42 {
		t.Errorf("second hook exit = %d, want 42", results[1].ExitCode)
	}
	if results[2].Status != "pass" {
		t.Errorf("third hook (after warn fail) should pass, got %q", results[2].Status)
	}
}

func TestIntegration_BlockStopsChain(t *testing.T) {
	logDir := t.TempDir()
	markerDir := t.TempDir()
	shouldNotExist := filepath.Join(markerDir, "should-not-exist")

	hooks := []protocol.HookEntry{
		{Run: "exit 1", OnFail: "block"},
		{Run: "touch " + shouldNotExist},
	}

	results, err := Execute(hooks, logDir)
	if err == nil {
		t.Fatal("expected error for block hook failure")
	}
	if len(results) != 1 {
		t.Errorf("expected early stop at 1 result, got %d", len(results))
	}
	if _, err := os.Stat(shouldNotExist); err == nil {
		t.Error("second hook should not have run after block failure")
	}
}

func TestIntegration_ToEvent_FailDetail(t *testing.T) {
	hooks := []protocol.HookEntry{{Run: "exit 5", OnFail: "warn"}}
	results, _ := Execute(hooks, t.TempDir())

	evt := ToEvent(results[0], protocol.PhaseTesting, "post_testing")
	if evt.Status != "fail" {
		t.Errorf("Status = %q, want fail", evt.Status)
	}
	if !strings.Contains(evt.Detail, "exit 5") {
		t.Errorf("detail should contain exit code 5, got %q", evt.Detail)
	}
	if evt.Phase != protocol.PhaseTesting {
		t.Errorf("Phase = %q, want testing", evt.Phase)
	}
	if evt.Action != "post_testing" {
		t.Errorf("Action = %q, want post_testing", evt.Action)
	}
}
