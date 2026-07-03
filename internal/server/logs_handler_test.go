package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// setupLogsWorkspace 建立帶有 logs 目錄的測試 workspace，回傳 CachedWorkspace 與 logsDir 路徑。
func setupLogsWorkspace(t *testing.T) (*protocol.CachedWorkspace, string) {
	t.Helper()
	ws := setupServerWorkspace(t)
	logsDir := filepath.Join(ws.FeatureDir("test-feat"), "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return protocol.NewCachedWorkspace(ws), logsDir
}

// TestHandleLogs_ListEmpty 驗證 logs 目錄沒有 .log 檔時，handleLogs 回傳空 JSON。
func TestHandleLogs_ListEmpty(t *testing.T) {
	cws, _ := setupLogsWorkspace(t)

	rec := httptest.NewRecorder()
	handleLogs(cws, "test-feat", rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// 空目錄時 logs 為 nil slice，json.Encode 會輸出 "null\n"。
	body := strings.TrimSpace(rec.Body.String())
	if body != "null" {
		t.Errorf("body = %q, want null", body)
	}
}

// TestHandleLogs_ListWithFiles 驗證 logs 目錄有 .log 檔時，handleLogs 回傳含名稱與大小的 JSON 陣列。
func TestHandleLogs_ListWithFiles(t *testing.T) {
	cws, logsDir := setupLogsWorkspace(t)

	// 建立兩個 .log 檔，確認兩者皆在回傳清單中。
	files := map[string]string{
		"round-1-coder.log":    "coder output here",
		"round-1-reviewer.log": "reviewer output here",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(logsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 額外建立非 .log 檔，確認不被列入。
	if err := os.WriteFile(filepath.Join(logsDir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handleLogs(cws, "test-feat", rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var logs []logInfo
	if err := json.NewDecoder(rec.Body).Decode(&logs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2", len(logs))
	}

	// 確認每個回傳項目的 Name 在預期清單中，且 Size 大於 0。
	for _, li := range logs {
		if _, ok := files[li.Name]; !ok {
			t.Errorf("unexpected log name: %q", li.Name)
		}
		if li.Size <= 0 {
			t.Errorf("log %q size = %d, want > 0", li.Name, li.Size)
		}
	}
}

// TestHandleLogs_ReadContent 驗證請求特定 .log 檔時，handleLogs 回傳其完整內容。
func TestHandleLogs_ReadContent(t *testing.T) {
	cws, logsDir := setupLogsWorkspace(t)

	content := "line one\nline two\nline three\n"
	if err := os.WriteFile(filepath.Join(logsDir, "round-1-coder.log"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handleLogs(cws, "test-feat/round-1-coder.log", rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain*", ct)
	}
	if rec.Body.String() != content {
		t.Errorf("body = %q, want %q", rec.Body.String(), content)
	}
}

// TestHandleLogs_InvalidFile 驗證請求非 .log 副檔名的路徑時，handleLogs 回傳 400。
func TestHandleLogs_InvalidFile(t *testing.T) {
	cws, _ := setupLogsWorkspace(t)

	rec := httptest.NewRecorder()
	handleLogs(cws, "test-feat/state.json", rec)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestLogKeyFromEvent 驗證各 role 名稱映射到正確的 log 檔名。
// mini-coder → deep-fix-N（帶迭代號），re-verifier → deep-reverify-N，
// deep-reviewer → deep-reviewer-N，其他 role 直接映射 round-N-role.log。
func TestLogKeyFromEvent(t *testing.T) {
	cases := []struct {
		name      string
		round     int
		role      string
		eventType string
		wantKey   string
	}{
		{
			name:      "mini-coder 第一次 phase-start",
			round:     1,
			role:      "mini-coder",
			eventType: "phase-start",
			wantKey:   "round-1-deep-fix-1.log",
		},
		{
			name:      "re-verifier 第一次 phase-start",
			round:     1,
			role:      "re-verifier",
			eventType: "phase-start",
			wantKey:   "round-1-deep-reverify-1.log",
		},
		{
			name:      "deep-reviewer 第一次 phase-start",
			round:     2,
			role:      "deep-reviewer",
			eventType: "phase-start",
			wantKey:   "round-2-deep-reviewer-1.log",
		},
		{
			name:      "coder 一般 role",
			round:     1,
			role:      "coder",
			eventType: "phase-start",
			wantKey:   "round-1-coder.log",
		},
		{
			name:      "designer 第一次 phase-start",
			round:     0,
			role:      "designer",
			eventType: "phase-start",
			wantKey:   "round-0-designer.log",
		},
		{
			name:      "reviewer 一般 role",
			round:     3,
			role:      "reviewer",
			eventType: "run-end",
			wantKey:   "round-3-reviewer.log",
		},
		{
			name:      "tester 一般 role",
			round:     2,
			role:      "tester",
			eventType: "run-end",
			wantKey:   "round-2-tester.log",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iterCount := map[string]int{}
			got := logKeyFromEvent(tc.round, tc.role, tc.eventType, 0, iterCount)
			if got != tc.wantKey {
				t.Errorf("logKeyFromEvent(%d, %q, %q) = %q, want %q",
					tc.round, tc.role, tc.eventType, got, tc.wantKey)
			}
		})
	}
}

// TestLogKeyFromEvent_MiniCoderIterates 驗證同一 round 多次 mini-coder phase-start
// 各自遞增迭代號（deep-fix-1, deep-fix-2, ...）。
func TestLogKeyFromEvent_MiniCoderIterates(t *testing.T) {
	iterCount := map[string]int{}

	k1 := logKeyFromEvent(1, "mini-coder", "phase-start", 0, iterCount)
	if k1 != "round-1-deep-fix-1.log" {
		t.Errorf("first = %q, want round-1-deep-fix-1.log", k1)
	}

	k2 := logKeyFromEvent(1, "mini-coder", "phase-start", 0, iterCount)
	if k2 != "round-1-deep-fix-2.log" {
		t.Errorf("second = %q, want round-1-deep-fix-2.log", k2)
	}

	// run-end 不遞增，回傳目前計數（2）的 key。
	k3 := logKeyFromEvent(1, "mini-coder", "run-end", 0, iterCount)
	if k3 != "round-1-deep-fix-2.log" {
		t.Errorf("run-end = %q, want round-1-deep-fix-2.log", k3)
	}
}

// TestLogKeyFromEvent_DesignerIterates 驗證 design-reviewing FAIL 打回 designing
// 造成同一 round 內 designer 重複執行時，第 1 次沿用 round-<N>-designer.log（向下相容），
// 第 2 次以後才加上 -<iteration> 後綴，避免和前一輪的 log 撞名互相覆寫。
func TestLogKeyFromEvent_DesignerIterates(t *testing.T) {
	iterCount := map[string]int{}

	k1 := logKeyFromEvent(0, "designer", "phase-start", 0, iterCount)
	if k1 != "round-0-designer.log" {
		t.Errorf("first = %q, want round-0-designer.log", k1)
	}

	k2 := logKeyFromEvent(0, "design-reviewer", "phase-start", 0, iterCount)
	if k2 != "round-0-design-reviewer.log" {
		t.Errorf("first design-reviewer = %q, want round-0-design-reviewer.log", k2)
	}

	k3 := logKeyFromEvent(0, "designer", "phase-start", 0, iterCount)
	if k3 != "round-0-designer-2.log" {
		t.Errorf("second designer = %q, want round-0-designer-2.log", k3)
	}

	k4 := logKeyFromEvent(0, "design-reviewer", "phase-start", 0, iterCount)
	if k4 != "round-0-design-reviewer-2.log" {
		t.Errorf("second design-reviewer = %q, want round-0-design-reviewer-2.log", k4)
	}
}

// TestParseRoleTimings 驗證 parseRoleTimings 從 events.jsonl 正確解析各 role 的計時資訊。
// 已有 run-end 的 role 帶 DurationMs；只有 phase-start 的 role 帶 StartedAt。
func TestParseRoleTimings(t *testing.T) {
	ws := setupServerWorkspace(t)
	featureDir := ws.FeatureDir("test-feat")

	now := time.Now().UTC().Truncate(time.Millisecond)
	coderStart := now.Add(-5 * time.Second)
	coderEnd := now.Add(-2 * time.Second)
	reviewerStart := now.Add(-1 * time.Second) // 尚未結束

	// 建立 events.jsonl，包含 coder 完整一對 phase-start/run-end，
	// 以及 reviewer 只有 phase-start（進行中）。
	eventsPath := filepath.Join(featureDir, protocol.EventsFile)
	lines := []string{
		`{"ts":"` + coderStart.Format(time.RFC3339Nano) + `","type":"phase-start","role":"coder","round":1}`,
		`{"ts":"` + coderEnd.Format(time.RFC3339Nano) + `","type":"run-end","role":"coder","round":1}`,
		`{"ts":"` + reviewerStart.Format(time.RFC3339Nano) + `","type":"phase-start","role":"reviewer","round":1}`,
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(eventsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	timings := parseRoleTimings(featureDir)
	if timings == nil {
		t.Fatal("parseRoleTimings returned nil")
	}

	// coder 應有 DurationMs，約 3000ms。
	coderKey := "round-1-coder.log"
	ct, ok := timings[coderKey]
	if !ok {
		t.Fatalf("timings missing key %q, got %v", coderKey, timings)
	}
	if ct.DurationMs == nil {
		t.Errorf("coder DurationMs should not be nil")
	} else {
		wantMs := coderEnd.Sub(coderStart).Milliseconds()
		if *ct.DurationMs != wantMs {
			t.Errorf("coder DurationMs = %d, want %d", *ct.DurationMs, wantMs)
		}
	}
	if ct.StartedAt != nil {
		t.Errorf("coder StartedAt should be nil (already ended)")
	}

	// reviewer 應有 StartedAt 或 DurationMs（以 lastEventTs 估算），不應為 nil。
	reviewerKey := "round-1-reviewer.log"
	rt, ok := timings[reviewerKey]
	if !ok {
		t.Fatalf("timings missing key %q", reviewerKey)
	}
	// parseRoleTimings 邏輯：若 lastEventTs > start，以 DurationMs 估算；否則用 StartedAt。
	// 此 case reviewerStart 是最後一個事件，StartedAt 或 DurationMs 至少其中一個不為 nil。
	if rt.DurationMs == nil && rt.StartedAt == nil {
		t.Error("reviewer timing should have either DurationMs or StartedAt")
	}
}

// TestParseRoleTimings_NoFile 驗證 events.jsonl 不存在時，parseRoleTimings 回傳 nil 而非 panic。
func TestParseRoleTimings_NoFile(t *testing.T) {
	ws := setupServerWorkspace(t)
	// 直接使用不含 events.jsonl 的 featureDir。
	timings := parseRoleTimings(ws.FeatureDir("test-feat"))
	if timings != nil {
		t.Errorf("want nil for missing events.jsonl, got %v", timings)
	}
}
