package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// minimalResponseWriter 實作最基本的 http.ResponseWriter，刻意不實作 http.Flusher，
// 用於驗證 handleSSE 在不支援 streaming 時回傳 500。
// 不能用 httptest.ResponseRecorder，因為該型別已內建 Flush() 方法。
type minimalResponseWriter struct {
	header     http.Header
	body       strings.Builder
	statusCode int
}

func newMinimalResponseWriter() *minimalResponseWriter {
	return &minimalResponseWriter{header: make(http.Header)}
}

func (w *minimalResponseWriter) Header() http.Header  { return w.header }
func (w *minimalResponseWriter) WriteHeader(code int) { w.statusCode = code }
func (w *minimalResponseWriter) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.body.WriteString(string(b))
	return n, err
}

// TestHandleSSE_NonFlusher 驗證 ResponseWriter 不支援 http.Flusher 時，handleSSE 回傳 500。
func TestHandleSSE_NonFlusher(t *testing.T) {
	ws := setupServerWorkspace(t)
	cws := protocol.NewCachedWorkspace(ws)

	// 使用不含 Flush() 的最小 ResponseWriter，讓型別斷言 w.(http.Flusher) 失敗。
	w := newMinimalResponseWriter()
	req := httptest.NewRequest(http.MethodGet, "/sse/events/test-feat", nil)

	handleSSE(cws, "test-feat", w, req)

	if w.statusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.statusCode)
	}
	if !strings.Contains(w.body.String(), "streaming not supported") {
		t.Errorf("body = %q, want 'streaming not supported'", w.body.String())
	}
}

// TestHandleSSE_SendsExistingEvents 驗證 events.jsonl 中已有內容時，
// handleSSE 在下一個 tick 會以 SSE data 推送這些行。
func TestHandleSSE_SendsExistingEvents(t *testing.T) {
	ws := setupServerWorkspace(t)
	cws := protocol.NewCachedWorkspace(ws)

	// 在 events.jsonl 寫入兩行測試資料，供 SSE handler polling 讀取。
	eventsPath := filepath.Join(ws.FeatureDir("test-feat"), protocol.EventsFile)
	if err := os.MkdirAll(filepath.Dir(eventsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"phase-start","detail":"evt-alpha"}` + "\n" +
		`{"type":"run-output","detail":"evt-beta"}` + "\n"
	if err := os.WriteFile(eventsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// 以 httptest.Server 搭配真實 HTTP client 測試 SSE，
	// 讓 http.Flusher 行為由標準函式庫自動滿足。
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	srv := newTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSSE(cws, "test-feat", w, r)
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// 驗證 Content-Type 是 text/event-stream。
	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// 從 SSE stream 收集 data 行，直到找到兩個預期 event 或 timeout。
	received := make(map[string]bool)
	want := []string{"evt-alpha", "evt-beta"}

	buf := make([]byte, 4096)
	accumulated := ""
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout: received events %v, still missing some of %v", received, want)
		default:
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			accumulated += string(buf[:n])
			// 逐行掃描已累積的內容，找出 data: 開頭的行。
			for {
				idx := strings.Index(accumulated, "\n")
				if idx < 0 {
					break
				}
				line := accumulated[:idx]
				accumulated = accumulated[idx+1:]
				if strings.HasPrefix(line, "data: ") {
					payload := strings.TrimPrefix(line, "data: ")
					for _, w := range want {
						if strings.Contains(payload, w) {
							received[w] = true
						}
					}
				}
			}
		}
		if len(received) >= len(want) {
			cancel() // 已收到所有預期事件，取消連線。
			return
		}
		if readErr != nil {
			break
		}
	}

	for _, w := range want {
		if !received[w] {
			t.Errorf("event %q not received in SSE stream", w)
		}
	}
}
