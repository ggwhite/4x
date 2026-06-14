package hook

import (
	"os"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestExecute_Success(t *testing.T) {
	hooks := []protocol.HookEntry{{Run: "echo hello", OnFail: "block"}}
	results, err := Execute(hooks, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", results[0].ExitCode)
	}
	if results[0].Status != "pass" {
		t.Errorf("Status = %q, want pass", results[0].Status)
	}
}

func TestExecute_BlockFail_ReturnsError(t *testing.T) {
	hooks := []protocol.HookEntry{{Run: "exit 1", OnFail: "block"}}
	_, err := Execute(hooks, t.TempDir())
	if err == nil {
		t.Fatal("expected error for block hook failure")
	}
}

func TestExecute_WarnFail_NoError(t *testing.T) {
	hooks := []protocol.HookEntry{{Run: "exit 1", OnFail: "warn"}}
	results, err := Execute(hooks, t.TempDir())
	if err != nil {
		t.Fatalf("warn hook should not return error, got: %v", err)
	}
	if results[0].Status != "fail" {
		t.Errorf("Status = %q, want fail", results[0].Status)
	}
}

func TestExecute_BlockFail_StopsEarly(t *testing.T) {
	hooks := []protocol.HookEntry{
		{Run: "exit 1", OnFail: "block"},
		{Run: "echo second"},
	}
	results, err := Execute(hooks, t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (stopped early), got %d", len(results))
	}
}

func TestExecute_LogFile_ContainsOutput(t *testing.T) {
	hooks := []protocol.HookEntry{{Run: "echo hello && echo err >&2"}}
	logDir := t.TempDir()
	results, err := Execute(hooks, logDir)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].LogFile == "" {
		t.Fatal("expected LogFile to be set")
	}
	data, err := os.ReadFile(results[0].LogFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "hello") {
		t.Errorf("log should contain stdout, got: %q", content)
	}
	if !strings.Contains(content, "err") {
		t.Errorf("log should contain stderr, got: %q", content)
	}
}

func TestExecute_DefaultOnFail_Block(t *testing.T) {
	// 不設定 on_fail，預設應該是 block
	hooks := []protocol.HookEntry{{Run: "exit 2"}}
	_, err := Execute(hooks, t.TempDir())
	if err == nil {
		t.Fatal("expected error: default on_fail should be block")
	}
}

func TestToEvent(t *testing.T) {
	r := Result{
		Command:  "docker compose up",
		ExitCode: 0,
		Status:   "pass",
		Duration: 1.23,
	}
	evt := ToEvent(r, protocol.PhaseCoding, "pre_coding")
	if evt.Type != "hook" {
		t.Errorf("Type = %q, want hook", evt.Type)
	}
	if evt.Phase != protocol.PhaseCoding {
		t.Errorf("Phase = %q, want coding", evt.Phase)
	}
	if evt.Action != "pre_coding" {
		t.Errorf("Action = %q, want pre_coding", evt.Action)
	}
	if evt.Command != "docker compose up" {
		t.Errorf("Command = %q, want docker compose up", evt.Command)
	}
	if evt.Status != "pass" {
		t.Errorf("Status = %q, want pass", evt.Status)
	}
	if !strings.Contains(evt.Detail, "exit 0") {
		t.Errorf("Detail should contain exit code, got %q", evt.Detail)
	}
}
