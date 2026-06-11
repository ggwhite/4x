package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

func testContextWithTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 100*time.Millisecond)
}

func setupServerWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "server-test"}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	f := protocol.Feature{ID: "test-feat", Name: "Test Feature", Status: "in-progress"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("test-feat"); err != nil {
		t.Fatal(err)
	}
	state := protocol.State{
		FeatureID: "test-feat",
		Phase:     protocol.PhaseCoding,
		Role:      protocol.RoleCoder,
		Round:     1,
		Active:    true,
		Runner:    "claude",
	}
	if err := ws.WriteState("test-feat", state); err != nil {
		t.Fatal(err)
	}

	return ws
}

func TestGetTasks(t *testing.T) {
	ws := setupServerWorkspace(t)
	srv := httptest.NewServer(NewMux(ws))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var tasks []taskInfo
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if tasks[0].ID != "test-feat" {
		t.Errorf("ID = %s, want test-feat", tasks[0].ID)
	}
	if tasks[0].Phase != "coding" {
		t.Errorf("Phase = %s, want coding", tasks[0].Phase)
	}
	if !tasks[0].Active {
		t.Error("Active should be true")
	}
}

func TestGetEvents_Empty(t *testing.T) {
	ws := setupServerWorkspace(t)
	srv := httptest.NewServer(NewMux(ws))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/events/test-feat")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var events []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events = %d, want 0", len(events))
	}
}

func TestGetEvents_WithData(t *testing.T) {
	ws := setupServerWorkspace(t)

	evt := protocol.Event{Type: "phase-start", Phase: protocol.PhaseDesigning, Round: 1}
	if err := ws.AppendEvent("test-feat", evt); err != nil {
		t.Fatal(err)
	}
	evt2 := protocol.Event{Type: "step", Detail: "analyzing", Round: 1}
	if err := ws.AppendEvent("test-feat", evt2); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(NewMux(ws))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/events/test-feat")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var events []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("events = %d, want 2", len(events))
	}
}

func TestGetMessages_Empty(t *testing.T) {
	ws := setupServerWorkspace(t)
	srv := httptest.NewServer(NewMux(ws))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/messages/test-feat")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestIndexHTML(t *testing.T) {
	ws := setupServerWorkspace(t)
	srv := httptest.NewServer(NewMux(ws))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/html" {
		t.Errorf("Content-Type = %s, want text/html", ct)
	}
}

func TestSSEEndpoint_ContentType(t *testing.T) {
	ws := setupServerWorkspace(t)
	srv := httptest.NewServer(NewMux(ws))
	defer srv.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("GET", srv.URL+"/sse/events/test-feat", nil)
	req.Header.Set("Accept", "text/event-stream")

	// SSE will block, just check headers then cancel
	ctx, cancel := testContextWithTimeout(t)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return // expected: context canceled
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", ct)
	}
}
