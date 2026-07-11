package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// codexRoundLogSample 是一段真實形狀的 codex round log（對照 codex exec --json 實機輸出）。
const codexRoundLogSample = `{"type":"thread.started","thread_id":"019f51d2-907e-7682-b637-ea85bacfcb30"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"HELLO_CODEX"}}
{"type":"turn.completed","usage":{"input_tokens":16700,"cached_input_tokens":5504,"output_tokens":19,"reasoning_output_tokens":12}}
[assistant] passthrough tail line
`

// TestHandleLogs 驗證 AC-7：REST 對 codex log 回傳轉換後可讀文字並帶 X-Log-Raw-Size；
// 對 claude log 逐 byte 穿透、X-Log-Raw-Size 等於原始檔長度。
func TestHandleLogs(t *testing.T) {
	cws, logsDir := setupLogsWorkspace(t)

	t.Run("codex log converted with raw-size header", func(t *testing.T) {
		raw := []byte(codexRoundLogSample)
		if err := os.WriteFile(filepath.Join(logsDir, "round-1-coder.log"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		handleLogs(cws, "test-feat/round-1-coder.log", rec)

		if rec.Code != 200 {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, `"type":`) {
			t.Errorf("body still contains raw JSON:\n%s", body)
		}
		if !strings.Contains(body, "HELLO_CODEX") {
			t.Errorf("body missing agent_message text:\n%s", body)
		}
		if !strings.Contains(body, "passthrough tail line") {
			t.Errorf("body missing passthrough line:\n%s", body)
		}
		if got := rec.Header().Get("X-Log-Raw-Size"); got != strconv.Itoa(len(raw)) {
			t.Errorf("X-Log-Raw-Size = %q, want %d", got, len(raw))
		}
	})

	t.Run("claude log byte-identical passthrough", func(t *testing.T) {
		raw := []byte("[assistant] hi\n[tool_use] Read: /x\n[result] success (1.0s, $0.5)\n")
		if err := os.WriteFile(filepath.Join(logsDir, "round-1-reviewer.log"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		handleLogs(cws, "test-feat/round-1-reviewer.log", rec)

		if rec.Body.String() != string(raw) {
			t.Errorf("body = %q, want byte-identical %q", rec.Body.String(), raw)
		}
		if got := rec.Header().Get("X-Log-Raw-Size"); got != strconv.Itoa(len(raw)) {
			t.Errorf("X-Log-Raw-Size = %q, want %d", got, len(raw))
		}
	})
}

// sseCollector 開一條 handleLogSSE 連線並在背景累積收到的 content，供測試斷言。
type sseCollector struct {
	mu   sync.Mutex
	sb   strings.Builder
	resp *http.Response
}

func (c *sseCollector) content() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sb.String()
}

// startLogSSE 啟動一條指定 file/offset 的 log SSE 連線，回傳背景累積內容的 collector。
func startLogSSE(t *testing.T, ws *protocol.Workspace, file string, offset int64) (*sseCollector, func()) {
	t.Helper()
	srv := newTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	url := srv.URL + "?file=" + file + "&offset=" + strconv.FormatInt(offset, 10)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatal(err)
	}
	c := &sseCollector{resp: resp}
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
				c.mu.Lock()
				c.sb.WriteString(msg.Content)
				c.mu.Unlock()
			}
		}
	}()
	cleanup := func() {
		cancel()
		resp.Body.Close()
		srv.Close()
	}
	return c, cleanup
}

func appendToFile(t *testing.T, path string, b []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(b); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// TestHandleLogSSE 驗證 AC-8：codex log 新增行 emit 轉換後可讀文字（不含 raw JSON）；
// claude/非 codex log 新增行逐 byte 穿透。
func TestHandleLogSSE(t *testing.T) {
	t.Run("codex append converted", func(t *testing.T) {
		ws := setupServerWorkspace(t)
		logsDir := filepath.Join(ws.FeatureDir("test-feat"), "logs")
		if err := os.MkdirAll(logsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		logName := "round-1-coder.log"
		logPath := filepath.Join(logsDir, logName)
		raw := []byte(codexRoundLogSample)
		if err := os.WriteFile(logPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		// 比照前端：REST 已讀完整內容後，SSE 從 raw-byte 檔尾續傳。
		c, cleanup := startLogSSE(t, ws, logName, int64(len(raw)))
		defer cleanup()
		time.Sleep(1200 * time.Millisecond)

		appendToFile(t, logPath, []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"SECOND_MSG"}}`+"\n"))
		time.Sleep(1500 * time.Millisecond)

		got := c.content()
		if !strings.Contains(got, "SECOND_MSG") {
			t.Errorf("SSE content missing converted text 'SECOND_MSG': %q", got)
		}
		if strings.Contains(got, `"type":`) {
			t.Errorf("SSE content leaked raw codex JSON: %q", got)
		}
	})

	t.Run("claude append byte passthrough", func(t *testing.T) {
		ws := setupServerWorkspace(t)
		logsDir := filepath.Join(ws.FeatureDir("test-feat"), "logs")
		if err := os.MkdirAll(logsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		logName := "round-1-reviewer.log"
		logPath := filepath.Join(logsDir, logName)
		raw := []byte("[assistant] start\n")
		if err := os.WriteFile(logPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		c, cleanup := startLogSSE(t, ws, logName, int64(len(raw)))
		defer cleanup()
		time.Sleep(1200 * time.Millisecond)

		newLine := "[assistant] more output here\n"
		appendToFile(t, logPath, []byte(newLine))
		time.Sleep(1500 * time.Millisecond)

		if got := c.content(); got != newLine {
			t.Errorf("claude SSE content = %q, want byte-identical %q", got, newLine)
		}
	})
}

// TestHandleLogSSE_CodexLineSplitAcrossTicks 驗證 AC-8 tick 邊界拆行的最壞情境：從**空檔**開始，
// 連「用於內容式辨識的首個 codex 事件行」本身都被拆成兩次 Write、分屬相鄰兩個 tick。
// 第一次 tick 只有半截 JSON（無 \n），detectCodexLog 必須回 undecided、不得永久快取成非 codex
// 走 rune passthrough 洩漏破碎原文；等第二次 tick 收齊完整首行後才判定為 codex 並轉換 emit。
// 刻意不預先種完整 thread.started，才能真正打到「首行尚未成形」的辨識 undecided 路徑。
func TestHandleLogSSE_CodexLineSplitAcrossTicks(t *testing.T) {
	ws := setupServerWorkspace(t)
	logsDir := filepath.Join(ws.FeatureDir("test-feat"), "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logName := "round-1-coder.log"
	logPath := filepath.Join(logsDir, logName)
	// 空檔起步：SSE 從 offset 0 續傳，首個 codex 事件行即為被拆的辨識行。
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	c, cleanup := startLogSSE(t, ws, logName, 0)
	defer cleanup()
	time.Sleep(1200 * time.Millisecond)

	// 第一次 tick：只寫入首個 codex 事件行的前半段（不含 \n）。此時檔內尚無任何 \n，
	// detectCodexLog 應回 undecided（不快取、不前進 offset、不 emit）——不得洩漏半截原文。
	firstHalf := `{"type":"item.completed","item":{"type":"agent_message","text":"SPLIT`
	appendToFile(t, logPath, []byte(firstHalf))
	time.Sleep(1500 * time.Millisecond)
	if got := c.content(); got != "" {
		t.Errorf("after first half, content = %q, want empty (detection undecided, half line held)", got)
	}

	// 第二次 tick：寫入含 \n 的後半段，湊齊完整首行後才判定為 codex 並轉換 emit。
	secondHalf := `_DONE"}}` + "\n"
	appendToFile(t, logPath, []byte(secondHalf))
	time.Sleep(1500 * time.Millisecond)

	got := c.content()
	if !strings.Contains(got, "SPLIT_DONE") {
		t.Errorf("after second half, content = %q, want to contain 'SPLIT_DONE'", got)
	}
	if strings.Contains(got, `"type":`) {
		t.Errorf("content leaked raw codex JSON: %q", got)
	}
}
