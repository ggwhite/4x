package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func mockExec(responses map[string]string) ExecFunc {
	return func(ctx context.Context, args ...string) (json.RawMessage, error) {
		key := args[0]
		if resp, ok := responses[key]; ok {
			return json.RawMessage(resp), nil
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	}
}

func TestStatusTool_NoArgs(t *testing.T) {
	exec := mockExec(map[string]string{
		"status": `{"features":[{"id":"F001","name":"test","status":"not-started"}]}`,
	})
	h := &Handlers{Exec: exec}

	input := StatusInput{}
	result, err := h.Status(context.Background(), input)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestStatusTool_WithFeatureID(t *testing.T) {
	exec := mockExec(map[string]string{
		"status": `{"feature":{"id":"F001","name":"test"},"state":null}`,
	})
	h := &Handlers{Exec: exec}

	input := StatusInput{FeatureID: "F001"}
	result, err := h.Status(context.Background(), input)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestNewTool(t *testing.T) {
	exec := mockExec(map[string]string{
		"new": `{"featureId":"F002-hello","name":"hello","path":".4x/features/F002-hello.yaml"}`,
	})
	h := &Handlers{Exec: exec}

	input := NewInput{Name: "hello"}
	result, err := h.New(context.Background(), input)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestCheckTool(t *testing.T) {
	exec := mockExec(map[string]string{
		"check": `{"pass":true,"errors":[],"warnings":[]}`,
	})
	h := &Handlers{Exec: exec}

	input := CheckInput{FeatureID: "F001"}
	result, err := h.Check(context.Background(), input)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestStopTool(t *testing.T) {
	dir := t.TempDir()
	dotDir := filepath.Join(dir, ".4x")
	featuresDir := filepath.Join(dotDir, "features")
	featureDir := filepath.Join(dotDir, "run", "F001-test")
	os.MkdirAll(featuresDir, 0o755)
	os.MkdirAll(featureDir, 0o755)

	state := `{"featureId":"F001-test","phase":"coding","role":"coder","round":1,"maxRounds":5,"active":true,"runner":"claude","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}`
	os.WriteFile(filepath.Join(featureDir, "state.json"), []byte(state), 0o644)
	os.WriteFile(filepath.Join(featuresDir, "F001-test.yaml"), []byte("id: F001-test\nname: test\nstatus: in-progress\n"), 0o644)

	h := &Handlers{WorkspaceRoot: dir}
	input := StopInput{FeatureID: "F001-test"}
	result, err := h.Stop(context.Background(), input)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	var parsed struct {
		FeatureID string `json:"featureId"`
		Stopped   bool   `json:"stopped"`
	}
	json.Unmarshal(result, &parsed)
	if !parsed.Stopped {
		t.Error("stopped should be true")
	}
	if parsed.FeatureID != "F001-test" {
		t.Errorf("featureId = %q, want F001-test", parsed.FeatureID)
	}

	// F082：Stop 改為寫 stop signal 檔，不直接改寫 state.json，避免與 run loop 競寫。
	// 確認 state.json 維持原樣（仍 active），由 run loop 後續消費 signal 才收斂。
	data, _ := os.ReadFile(filepath.Join(featureDir, "state.json"))
	var s protocol.State
	json.Unmarshal(data, &s)
	if !s.Active {
		t.Error("state.json should be left untouched by Stop (run loop is the sole writer)")
	}

	// 確認 stop signal 檔被建立。
	ws := &protocol.Workspace{Root: dir}
	if !ws.StopRequested("F001-test") {
		t.Error("stop signal file should exist after Stop")
	}
}
