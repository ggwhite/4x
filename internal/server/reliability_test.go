package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

// --- AC-1: handlePostDone 錯誤路徑回 JSON ---

// TestPostDone_ErrorPathsAreJSON 驗證 handlePostDone 各錯誤路徑皆回 application/json
// 且 body 為 {"error":...}，HTTP status code 與原本一致。
func TestPostDone_ErrorPathsAreJSON(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"invalid-json", `{`, http.StatusBadRequest},
		{"id-required", `{}`, http.StatusBadRequest},
		{"feature-not-found", `{"id":"no-such-feature"}`, http.StatusNotFound},
		{"phase-not-pending-review", `{"id":"test-feat"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := setupServerWorkspace(t) // test-feat 預設在 coding phase
			rec := serveRequest(t, NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil)), http.MethodPost, "/api/done", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("body not JSON: %v (%s)", err, rec.Body.String())
			}
			if payload["error"] == "" {
				t.Fatalf("missing error field: %s", rec.Body.String())
			}
		})
	}
}

// --- AC-2: messages / overview endpoint method 檢查 ---

// TestMessagesOverview_NonGetRejected 驗證 /api/messages/ 與 /api/overview/ 對非 GET 回 405。
func TestMessagesOverview_NonGetRejected(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))
	for _, path := range []string{"/api/messages/test-feat", "/api/overview/test-feat"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			rec := serveRequest(t, mux, method, path, "")
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, rec.Code)
			}
		}
		// GET 仍正常（不回歸）
		if rec := serveRequest(t, mux, http.MethodGet, path, ""); rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("GET %s = 405, want allowed", path)
		}
	}
}

// --- AC-3: events SSE 不重複發送已送事件 ---

// TestEventsSSE_ExactlyOnceDelivery 驗證移除 offset clamp 後，連續 append 的事件
// 每一筆只被推送一次（lastOffset 反映實際消費位移，不會 stale clamp 造成重送）。
func TestEventsSSE_ExactlyOnceDelivery(t *testing.T) {
	ws := setupServerWorkspace(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var mu sync.Mutex
	counts := map[string]int{}
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				mu.Lock()
				counts[strings.TrimPrefix(line, "data: ")]++
				mu.Unlock()
			}
		}
	}()

	const total = 6
	for i := 0; i < total; i++ {
		if err := ws.AppendEvent("test-feat", protocol.Event{Type: "run-output", Detail: fmt.Sprintf("evt-%d", i)}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(400 * time.Millisecond)
	}
	// 多等幾個 tick，任何重送都會在此浮現
	time.Sleep(2500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	seen := 0
	for data, c := range counts {
		if !strings.Contains(data, "evt-") {
			continue
		}
		seen++
		if c != 1 {
			t.Errorf("event %q delivered %d times, want exactly 1", data, c)
		}
	}
	if seen != total {
		t.Errorf("distinct evt-* delivered = %d, want %d", seen, total)
	}
}

// --- AC-9: events SSE scanner 錯誤不靜默前進 lastOffset ---

// TestEventsSSE_ScannerErrorRetries 驗證超長行（超過 bufio.Scanner 64KB token 上限）
// 觸發 scanner.Err() 時，lastOffset 不前進到未成功消費的位置：下個 tick 從同位置重試，
// 使前面的合法事件被持續重送（>=2 次），而非被靜默截斷遺失。
func TestEventsSSE_ScannerErrorRetries(t *testing.T) {
	ws := setupServerWorkspace(t)
	path := filepath.Join(ws.FeatureDir("test-feat"), protocol.EventsFile)
	short := `{"type":"run-output","detail":"first-event"}`
	overlong := `{"type":"run-output","detail":"` + strings.Repeat("x", 70000) + `"}`
	if err := os.WriteFile(path, []byte(short+"\n"+overlong+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var mu sync.Mutex
	firstCount := 0
	overlongLeaked := false
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 128*1024), 1<<20) // client 端需能讀到（若有）超長 data
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			mu.Lock()
			if strings.Contains(data, "first-event") {
				firstCount++
			}
			if strings.Contains(data, strings.Repeat("x", 100)) {
				overlongLeaked = true
			}
			mu.Unlock()
		}
	}()

	// 等待 ~3 個 tick，重試應使 first-event 被送出多次
	time.Sleep(3500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if firstCount < 2 {
		t.Errorf("first-event delivered %d times, want >=2 (retry without silent advance)", firstCount)
	}
	if overlongLeaked {
		t.Error("overlong line should not be delivered as a truncated event")
	}
}

// --- AC-7: FinalizeDone role 與 CLI 對齊（空字串）---

// TestFinalizeDone_RoleEmptyMatchesCLI 驗證 state.FinalizeDone 對 PhaseDone 寫出的
// Role 為空字串，CLI 與 server 共用同一函式。
func TestFinalizeDone_RoleEmptyMatchesCLI(t *testing.T) {
	ws := setupServerWorkspace(t)
	makePendingReview(t, ws, "test-feat")

	s, err := ws.ReadState("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	newState, err := state.FinalizeDone(ws, "test-feat", s)
	if err != nil {
		t.Fatal(err)
	}
	if newState.Role != "" {
		t.Errorf("returned role = %q, want empty", newState.Role)
	}

	persisted, err := ws.ReadState("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Role != "" {
		t.Errorf("persisted role = %q, want empty", persisted.Role)
	}

	// 對照 CLI 走的 state.Transition(s, PhaseDone, "")
	want, err := state.Transition(s, protocol.PhaseDone, "")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Role != want.Role {
		t.Errorf("role = %q, want %q (CLI 對齊)", persisted.Role, want.Role)
	}
}

// --- AC-8: per-project keyed mutex ---

// TestKeyedMutex_SameKeyIdentity 驗證相同 key 取得同一鎖、不同 key 取得不同鎖。
func TestKeyedMutex_SameKeyIdentity(t *testing.T) {
	var km keyedMutex
	if km.get("/proj/a") != km.get("/proj/a") {
		t.Error("same key should return the same mutex")
	}
	if km.get("/proj/a") == km.get("/proj/b") {
		t.Error("different keys should return different mutexes")
	}
}

// TestKeyedMutex_DifferentKeysDoNotBlock 驗證不同專案（key）的鎖互不阻塞。
func TestKeyedMutex_DifferentKeysDoNotBlock(t *testing.T) {
	var km keyedMutex
	a := km.get("proj-a")
	a.Lock()
	defer a.Unlock()

	done := make(chan struct{})
	go func() {
		b := km.get("proj-b")
		b.Lock()
		b.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("different-key lock blocked while another key was held")
	}
}

// TestKeyedMutex_SameKeySerializes 驗證相同專案（key）的鎖仍序列化互斥。
func TestKeyedMutex_SameKeySerializes(t *testing.T) {
	var km keyedMutex
	a := km.get("proj-a")
	a.Lock()

	acquired := make(chan struct{})
	go func() {
		m := km.get("proj-a")
		m.Lock()
		close(acquired)
		m.Unlock()
	}()

	select {
	case <-acquired:
		t.Fatal("same-key lock acquired while still held")
	case <-time.After(150 * time.Millisecond):
	}

	a.Unlock()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("same-key lock never acquired after release")
	}
}

// TestKeyedMutex_ConcurrentGetRaceFree 以多 goroutine 併發取鎖，配合 -race 驗證取鎖過程無 data race。
func TestKeyedMutex_ConcurrentGetRaceFree(t *testing.T) {
	var km keyedMutex
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := km.get(fmt.Sprintf("proj-%d", i%4))
			m.Lock()
			m.Unlock()
		}(i)
	}
	wg.Wait()
}

// --- AC-5: handleServeScreenshot symlink 逃逸防護 ---

// TestServeScreenshot_SymlinkEscapeRejected 驗證指向 workspace root 外的 symlink
// 經 EvalSymlinks 後比對 root 前綴失敗 → 回 403，且不洩漏目標檔內容。
func TestServeScreenshot_SymlinkEscapeRejected(t *testing.T) {
	ws := setupServerWorkspace(t)

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.png")
	if err := os.WriteFile(secret, []byte("SECRET-OUTSIDE-ROOT"), 0o644); err != nil {
		t.Fatal(err)
	}

	shotDir := filepath.Join(ws.DotDir(), "run", "test-feat", "screenshot")
	if err := os.MkdirAll(shotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(shotDir, "evil.png")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	token := encodeScreenshotToken(".4x/e2e/test-feat/screenshot/evil.png")
	if token == "" {
		t.Fatal("failed to encode screenshot token")
	}
	rec := serveRequest(t, NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil)), http.MethodGet, "/api/features/test-feat/screenshots/"+token, "")
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 403 or 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "SECRET-OUTSIDE-ROOT") {
		t.Fatal("symlink escape leaked out-of-root file content")
	}
}

// --- AC-10: log SSE UTF-8 carry 跨 tick 保留 ---

// TestLogSSE_UTF8CarryAcrossTicks 驗證多位元組 UTF-8 字元被拆在 tick 邊界寫入時，
// 殘段跨 tick 保留並於後續 bytes 到齊後拼回，輸出不含 U+FFFD、不重複。
func TestLogSSE_UTF8CarryAcrossTicks(t *testing.T) {
	ws := setupServerWorkspace(t)
	logsDir := filepath.Join(ws.FeatureDir("test-feat"), "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logName := "round-1-coder.log"
	logPath := filepath.Join(logsDir, logName)
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?file="+logName, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var mu sync.Mutex
	var sb strings.Builder
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var msg struct {
				File    string `json:"file"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &msg); err == nil {
				mu.Lock()
				sb.WriteString(msg.Content)
				mu.Unlock()
			}
		}
	}()

	appendBytes := func(b []byte) {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(b); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}

	// "世界" = E4 B8 96 E7 95 8C，拆在 tick 邊界寫入：
	full := []byte("世界")
	appendBytes(full[:2]) // 世 的前 2 bytes（不完整 rune）
	time.Sleep(1300 * time.Millisecond)
	appendBytes(full[2:5]) // 補完 世，界 仍缺 1 byte
	time.Sleep(1300 * time.Millisecond)
	appendBytes(full[5:]) // 補完 界
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	got := sb.String()
	mu.Unlock()
	if got != "世界" {
		t.Fatalf("reassembled content = %q, want 世界", got)
	}
	if strings.ContainsRune(got, '�') {
		t.Fatalf("output contains U+FFFD: %q", got)
	}
}
