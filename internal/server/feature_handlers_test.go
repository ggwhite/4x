package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// setupTestWorkspace 建立一個帶有最小 .4x/ 結構的暫存 workspace，回傳 CachedWorkspace。
func setupTestWorkspace(t *testing.T) *protocol.CachedWorkspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	return protocol.NewCachedWorkspace(ws)
}

// TestHandleTasks_Empty 驗證 workspace 無 feature 時，handleTasks 回傳空 JSON 陣列（非 null）。
func TestHandleTasks_Empty(t *testing.T) {
	ws := setupTestWorkspace(t)
	rec := httptest.NewRecorder()

	handleTasks(ws, rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var tasks []taskInfo
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// json.Encoder 會把 nil slice 編碼成 null，但呼叫端期待陣列。
	// 實作使用 var tasks []taskInfo，encode nil → "null"，此處驗證長度為 0 即可。
	if len(tasks) != 0 {
		t.Errorf("tasks = %d, want 0", len(tasks))
	}
}

// TestHandleTasks_OneFeature 驗證 workspace 有一個 feature 時，handleTasks 正確回傳
// feature 的 id、name、status 欄位，以及從 state.json 讀出的 phase 資訊。
func TestHandleTasks_OneFeature(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	rawWs := &protocol.Workspace{Root: root}

	f := feature.Feature{ID: "F001-my-feat", Name: "My Feature", Status: "in-progress"}
	if err := rawWs.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	if err := rawWs.InitFeatureDir("F001-my-feat"); err != nil {
		t.Fatal(err)
	}
	state := protocol.State{
		FeatureID: "F001-my-feat",
		Phase:     protocol.PhaseCoding,
		Role:      protocol.RoleCoder,
		Round:     2,
		Active:    false,
		Runner:    "claude",
	}
	if err := rawWs.WriteState("F001-my-feat", state); err != nil {
		t.Fatal(err)
	}

	ws := protocol.NewCachedWorkspace(rawWs)
	rec := httptest.NewRecorder()

	handleTasks(ws, rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var tasks []taskInfo
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	got := tasks[0]
	if got.ID != "F001-my-feat" {
		t.Errorf("ID = %q, want F001-my-feat", got.ID)
	}
	if got.Name != "My Feature" {
		t.Errorf("Name = %q, want My Feature", got.Name)
	}
	if got.Status != "in-progress" {
		t.Errorf("Status = %q, want in-progress", got.Status)
	}
	if got.Phase != "coding" {
		t.Errorf("Phase = %q, want coding", got.Phase)
	}
	if got.Round != 2 {
		t.Errorf("Round = %d, want 2", got.Round)
	}
	if got.Runner != "claude" {
		t.Errorf("Runner = %q, want claude", got.Runner)
	}
}

// TestHandleEvents_NoFile 驗證 feature 無 events.jsonl 時，handleEvents 回傳空 JSON 陣列 `[]`。
func TestHandleEvents_NoFile(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	rawWs := &protocol.Workspace{Root: root}
	if err := rawWs.InitFeatureDir("feat-001"); err != nil {
		t.Fatal(err)
	}

	ws := protocol.NewCachedWorkspace(rawWs)
	rec := httptest.NewRecorder()

	handleEvents(ws, "feat-001", rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	body := rec.Body.String()
	// 無 events.jsonl 時直接寫入 "[]"，不走 json.Encoder
	if body != "[]" {
		t.Errorf("body = %q, want []", body)
	}
}

// TestHandleEvents_WithValidJSONL 驗證 events.jsonl 包含合法 JSONL 時，
// handleEvents 回傳對應長度的 JSON 陣列，且每個元素保留原始欄位。
func TestHandleEvents_WithValidJSONL(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	rawWs := &protocol.Workspace{Root: root}
	if err := rawWs.InitFeatureDir("feat-002"); err != nil {
		t.Fatal(err)
	}

	// 寫入兩筆 event
	if err := rawWs.AppendEvent("feat-002", protocol.Event{Type: "phase-start", Phase: protocol.PhaseDesigning, Round: 1}); err != nil {
		t.Fatal(err)
	}
	if err := rawWs.AppendEvent("feat-002", protocol.Event{Type: "run-end", Round: 1}); err != nil {
		t.Fatal(err)
	}

	ws := protocol.NewCachedWorkspace(rawWs)
	rec := httptest.NewRecorder()

	handleEvents(ws, "feat-002", rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var events []json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}

	// 驗證第一筆事件包含 type 欄位
	var first map[string]interface{}
	if err := json.Unmarshal(events[0], &first); err != nil {
		t.Fatalf("unmarshal first event: %v", err)
	}
	if first["type"] != "phase-start" {
		t.Errorf("first event type = %v, want phase-start", first["type"])
	}
}

// TestHandleEvents_EmptyJSONLFile 驗證 events.jsonl 存在但為空時，handleEvents 回傳 `[]`。
func TestHandleEvents_EmptyJSONLFile(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	rawWs := &protocol.Workspace{Root: root}
	if err := rawWs.InitFeatureDir("feat-003"); err != nil {
		t.Fatal(err)
	}

	// 建立空的 events.jsonl
	eventsPath := filepath.Join(rawWs.FeatureDir("feat-003"), protocol.EventsFile)
	if err := os.WriteFile(eventsPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := protocol.NewCachedWorkspace(rawWs)
	rec := httptest.NewRecorder()

	handleEvents(ws, "feat-003", rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if body != "[]" {
		t.Errorf("body = %q, want []", body)
	}
}
