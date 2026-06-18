package runner

import (
	"strings"
	"testing"
	"time"
)

func TestIsTransientError(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   bool
	}{
		// 暫態 pattern：應回 true
		{"socket closed", "Error: socket closed", true},
		{"socket hang up", "request failed: socket hang up", true},
		{"connection reset", "read tcp: connection reset", true},
		{"connection reset by peer", "connection reset by peer", true},
		{"connection refused", "dial tcp: connection refused", true},
		{"rate limit", "API rate limit exceeded", true},
		{"rate_limit underscore", `{"type":"error","error":{"type":"rate_limit_error"}}`, true},
		{"too many requests", "HTTP 429 Too Many Requests", true},
		{"bare 429", "status code 429", true},
		{"500", "received status 500", true},
		{"502", "502 received", true},
		{"503", "503 from upstream", true},
		{"504", "got 504", true},
		{"internal server error", "Internal Server Error", true},
		{"bad gateway", "502 Bad Gateway", true},
		{"service unavailable", "Service Unavailable", true},
		{"gateway timeout", "504 Gateway Timeout", true},
		{"overloaded", `{"error":{"type":"overloaded_error"}}`, true},
		{"server_error", `{"error":{"type":"server_error"}}`, true},
		{"unexpected eof", "stream error: unexpected EOF", true},
		{"io timeout", "read tcp 1.2.3.4:443: i/o timeout", true},
		{"tls handshake timeout", "net/http: TLS handshake timeout", true},

		// 大小寫混合：仍應命中
		{"mixed case socket", "SoCkEt ClOsEd", true},
		{"upper connection reset", "CONNECTION RESET BY PEER", true},

		// 非暫態：應回 false
		{"compilation error", "main.go:10: compilation error: undefined foo", false},
		{"assertion failed", "assertion failed: expected 1 got 2", false},
		{"panic", "panic: runtime error: index out of range", false},
		{"normal output", "Build succeeded in 3.2s", false},
		{"empty", "", false},
		{"unrelated number", "processed 5000 records", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientError(tc.stderr); got != tc.want {
				t.Errorf("isTransientError(%q) = %v, want %v", tc.stderr, got, tc.want)
			}
		})
	}
}

func TestBackoffDuration(t *testing.T) {
	base := 100 * time.Millisecond

	// 指數成長：base, 2*base, 4*base, 8*base
	wantByAttempt := map[int]time.Duration{
		1: base,
		2: 2 * base,
		3: 4 * base,
		4: 8 * base,
	}
	for attempt, want := range wantByAttempt {
		if got := backoffDuration(attempt, base); got != want {
			t.Errorf("backoffDuration(%d, %v) = %v, want %v", attempt, base, got, want)
		}
	}

	// attempt 越大越久（嚴格遞增直到 cap）
	if backoffDuration(1, base) >= backoffDuration(2, base) {
		t.Error("backoff should grow with attempt")
	}

	// cap 上限：極大 attempt 不得超過 maxBackoff
	if got := backoffDuration(50, base); got != maxBackoff {
		t.Errorf("backoffDuration(50, %v) = %v, want cap %v", base, got, maxBackoff)
	}

	// base <= 0 時套用 defaultBackoffBase
	if got := backoffDuration(1, 0); got != defaultBackoffBase {
		t.Errorf("backoffDuration(1, 0) = %v, want default %v", got, defaultBackoffBase)
	}
	if got := backoffDuration(1, -5*time.Second); got != defaultBackoffBase {
		t.Errorf("backoffDuration(1, -5s) = %v, want default %v", got, defaultBackoffBase)
	}
}

func TestTailWriter(t *testing.T) {
	// 未超過上限：完整保留
	w := newTailWriter(100)
	w.Write([]byte("hello "))
	w.Write([]byte("world"))
	if got := w.String(); got != "hello world" {
		t.Errorf("String() = %q, want %q", got, "hello world")
	}

	// 超過上限：只保留最後 limit bytes
	w2 := newTailWriter(5)
	w2.Write([]byte("abcdefghij"))
	if got := w2.String(); got != "fghij" {
		t.Errorf("String() = %q, want %q", got, "fghij")
	}

	// 跨多次寫入仍維持 bounded
	w3 := newTailWriter(4)
	for i := 0; i < 10; i++ {
		w3.Write([]byte("xy"))
	}
	if got := w3.String(); got != "xyxy" {
		t.Errorf("String() = %q, want %q", got, "xyxy")
	}
	if len(w3.buf) > 4 {
		t.Errorf("buf length = %d, want <= 4", len(w3.buf))
	}
}

func TestResolveMaxRetries(t *testing.T) {
	cases := []struct {
		field int
		want  int
	}{
		{0, DefaultTransientRetries}, // 零值 → 預設
		{-1, 0},                      // 負值 → 停用
		{5, 5},                       // 正值 → 原值
	}
	for _, tc := range cases {
		r := &SubprocessRunner{MaxTransientRetries: tc.field}
		if got := r.resolveMaxRetries(); got != tc.want {
			t.Errorf("MaxTransientRetries=%d → resolveMaxRetries()=%d, want %d", tc.field, got, tc.want)
		}
	}
}

func TestTransientLogSnippet(t *testing.T) {
	// 多行壓成單行
	if got := transientLogSnippet("line1\nline2"); strings.Contains(got, "\n") {
		t.Errorf("snippet should not contain newline, got %q", got)
	}
	// 過長截斷至尾段
	long := strings.Repeat("a", 300) + "TAIL"
	got := transientLogSnippet(long)
	if len(got) > 200 {
		t.Errorf("snippet length = %d, want <= 200", len(got))
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Errorf("snippet should keep tail, got suffix %q", got[len(got)-10:])
	}
}
