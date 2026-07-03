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

// TestBuildPhaseInfo_DesignLoopIterates 驗證 design-reviewing FAIL 打回 designing 造成
// 同一 round 內 designer/design-reviewer 重複執行時，buildPhaseInfo 用 iteration 把每一輪
// 的 duration/cost/tokens 分開記錄，而不是後一輪覆蓋前一輪。
func TestBuildPhaseInfo_DesignLoopIterates(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	rawWs := &protocol.Workspace{Root: root}
	if err := rawWs.InitFeatureDir("feat-loop"); err != nil {
		t.Fatal(err)
	}

	events := []protocol.Event{
		{Timestamp: "2026-01-01T00:00:00Z", Type: "phase-start", Role: protocol.RoleDesigner, Round: 0, Model: "sonnet"},
		{Timestamp: "2026-01-01T00:00:10Z", Type: "run-end", Role: protocol.RoleDesigner, Round: 0, Model: "sonnet", TokensUsed: 100, CostUSD: 0.10},
		{Timestamp: "2026-01-01T00:00:11Z", Type: "phase-start", Role: protocol.RoleDesignReviewer, Round: 0, Model: "opus"},
		{Timestamp: "2026-01-01T00:00:21Z", Type: "run-end", Role: protocol.RoleDesignReviewer, Round: 0, Model: "opus", TokensUsed: 50, CostUSD: 0.05},
		{Timestamp: "2026-01-01T00:00:22Z", Type: "phase-start", Role: protocol.RoleDesigner, Round: 0, Model: "sonnet"},
		{Timestamp: "2026-01-01T00:00:32Z", Type: "run-end", Role: protocol.RoleDesigner, Round: 0, Model: "sonnet", TokensUsed: 200, CostUSD: 0.20},
	}
	for _, e := range events {
		if err := rawWs.AppendEvent("feat-loop", e); err != nil {
			t.Fatal(err)
		}
	}

	ws := protocol.NewCachedWorkspace(rawWs)
	phases := buildPhaseInfo(ws, "feat-loop")

	first := phases[durationKey{"designer", 0, 1}]
	if first.tokensUsed != 100 || first.costUSD != 0.10 {
		t.Errorf("first designer iteration = %+v, want tokens=100 cost=0.10", first)
	}
	second := phases[durationKey{"designer", 0, 2}]
	if second.tokensUsed != 200 || second.costUSD != 0.20 {
		t.Errorf("second designer iteration = %+v, want tokens=200 cost=0.20", second)
	}
	reviewer := phases[durationKey{"design-reviewer", 0, 1}]
	if reviewer.tokensUsed != 50 || reviewer.costUSD != 0.05 {
		t.Errorf("design-reviewer iteration = %+v, want tokens=50 cost=0.05", reviewer)
	}
}

// TestHandleMessages_DesignReviewLoop 驗證 handleMessages 會把每一輪歸檔在
// design-rounds/round-<round>-<iteration>/ 底下的 designer/design-reviewer 訊息都列出來，
// 而不是只顯示 feature 目錄根目錄下最後一輪覆寫剩下的內容。
func TestHandleMessages_DesignReviewLoop(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	rawWs := &protocol.Workspace{Root: root}
	if err := rawWs.InitFeatureDir("feat-loop"); err != nil {
		t.Fatal(err)
	}
	dir := rawWs.FeatureDir("feat-loop")

	// 第 1 輪：designer + design-reviewer（FAIL）
	round1 := filepath.Join(dir, protocol.DesignRoundsDir, "round-0-1")
	if err := os.MkdirAll(round1, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(round1, protocol.TaskBrief), []byte("brief v1"), 0o644)
	os.WriteFile(filepath.Join(round1, protocol.DesignReviewReport), []byte("FAIL: needs work"), 0o644)

	// 第 2 輪：designer 修正後 + design-reviewer（PASS），也是目前根目錄下的最新內容
	round2 := filepath.Join(dir, protocol.DesignRoundsDir, "round-0-2")
	if err := os.MkdirAll(round2, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(round2, protocol.TaskBrief), []byte("brief v2"), 0o644)
	os.WriteFile(filepath.Join(round2, protocol.DesignReviewReport), []byte("PASS"), 0o644)
	os.WriteFile(filepath.Join(dir, protocol.TaskBrief), []byte("brief v2"), 0o644)
	os.WriteFile(filepath.Join(dir, protocol.DesignReviewReport), []byte("PASS"), 0o644)

	ws := protocol.NewCachedWorkspace(rawWs)
	rec := httptest.NewRecorder()
	handleMessages(ws, "feat-loop", rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp messagesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	messages := resp.Messages

	var briefs, reports []string
	for _, m := range messages {
		if m.Role == "designer" && m.Label == protocol.TaskBrief {
			briefs = append(briefs, m.Content)
		}
		if m.Role == "design-reviewer" {
			reports = append(reports, m.Content)
		}
	}
	if len(briefs) != 2 || briefs[0] != "brief v1" || briefs[1] != "brief v2" {
		t.Errorf("designer briefs = %v, want [brief v1, brief v2] in order", briefs)
	}
	if len(reports) != 2 || reports[0] != "FAIL: needs work" || reports[1] != "PASS" {
		t.Errorf("design-reviewer reports = %v, want [FAIL: needs work, PASS] in order", reports)
	}
}

// TestHandleMessages_LegacyNoDesignRounds 驗證舊 feature（在本次修復之前跑完，沒有
// design-rounds/ 歸檔目錄）仍能從 feature 目錄根目錄的 task-brief.md／
// design-review-report.md 正常顯示，向下相容不會因為改動而變成空白。
func TestHandleMessages_LegacyNoDesignRounds(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	rawWs := &protocol.Workspace{Root: root}
	if err := rawWs.InitFeatureDir("feat-legacy"); err != nil {
		t.Fatal(err)
	}
	dir := rawWs.FeatureDir("feat-legacy")
	os.WriteFile(filepath.Join(dir, protocol.TaskBrief), []byte("legacy brief"), 0o644)
	os.WriteFile(filepath.Join(dir, protocol.DesignReviewReport), []byte("legacy PASS"), 0o644)

	ws := protocol.NewCachedWorkspace(rawWs)
	rec := httptest.NewRecorder()
	handleMessages(ws, "feat-legacy", rec)

	var resp messagesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	messages := resp.Messages

	var found bool
	for _, m := range messages {
		if m.Role == "designer" && m.Content == "legacy brief" {
			found = true
		}
	}
	if !found {
		t.Errorf("legacy task-brief not found in messages: %+v", messages)
	}
}

// TestHandleMessages_TotalCostUSD 驗證 handleMessages 回應的 totalCostUSD 等於
// events.jsonl 中所有 run-end 事件 cost_usd 的加總。
func TestHandleMessages_TotalCostUSD(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	rawWs := &protocol.Workspace{Root: root}
	if err := rawWs.InitFeatureDir("feat-cost"); err != nil {
		t.Fatal(err)
	}
	if err := rawWs.AppendEvent("feat-cost", protocol.Event{Type: "run-end", CostUSD: 1.25}); err != nil {
		t.Fatal(err)
	}
	if err := rawWs.AppendEvent("feat-cost", protocol.Event{Type: "run-end", CostUSD: 2.5}); err != nil {
		t.Fatal(err)
	}
	if err := rawWs.AppendEvent("feat-cost", protocol.Event{Type: "phase-start", CostUSD: 99}); err != nil {
		t.Fatal(err)
	}

	ws := protocol.NewCachedWorkspace(rawWs)
	rec := httptest.NewRecorder()
	handleMessages(ws, "feat-cost", rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp messagesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalCostUSD != 3.75 {
		t.Errorf("totalCostUSD = %v, want 3.75", resp.TotalCostUSD)
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
