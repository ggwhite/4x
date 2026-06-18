package runner

import (
	"bytes"
	"errors"
	"testing"
)

// errWriter 模擬 inner writer 寫入失敗（如磁碟滿、檔案被關）。
type errWriter struct {
	err   error
	wrote int // 成功前累計寫入次數（用於只在第 N 次失敗的情境）
	failAt int
}

func (e *errWriter) Write(p []byte) (int, error) {
	e.wrote++
	if e.failAt == 0 || e.wrote >= e.failAt {
		return 0, e.err
	}
	return len(p), nil
}

func TestAnsiStripper_BasicCSI(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("hello \x1b[31mred\x1b[0m world"))
	if got := buf.String(); got != "hello red world" {
		t.Errorf("got %q, want %q", got, "hello red world")
	}
}

func TestAnsiStripper_PrivateMode(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("\x1b[?25lhidden\x1b[?25h"))
	if got := buf.String(); got != "hidden" {
		t.Errorf("got %q, want %q", got, "hidden")
	}
}

func TestAnsiStripper_BracketedPaste(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("\x1b[?2004htext\x1b[?2004l"))
	if got := buf.String(); got != "text" {
		t.Errorf("got %q, want %q", got, "text")
	}
}

func TestAnsiStripper_OSC_BEL(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("\x1b]0;my title\x07content"))
	if got := buf.String(); got != "content" {
		t.Errorf("got %q, want %q", got, "content")
	}
}

func TestAnsiStripper_OSC_ST(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\"))
	if got := buf.String(); got != "link" {
		t.Errorf("got %q, want %q", got, "link")
	}
}

func TestAnsiStripper_ChunkBoundary_CSI(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("before\x1b[3"))
	w.Write([]byte("1mafter"))
	if got := buf.String(); got != "beforeafter" {
		t.Errorf("got %q, want %q", got, "beforeafter")
	}
}

func TestAnsiStripper_ChunkBoundary_ESC(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("hello\x1b"))
	w.Write([]byte("[0mworld"))
	if got := buf.String(); got != "helloworld" {
		t.Errorf("got %q, want %q", got, "helloworld")
	}
}

func TestAnsiStripper_ChunkBoundary_OSC(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("\x1b]0;my ti"))
	w.Write([]byte("tle\x07done"))
	if got := buf.String(); got != "done" {
		t.Errorf("got %q, want %q", got, "done")
	}
}

func TestAnsiStripper_Charset(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("\x1b(Btext\x1b)0"))
	if got := buf.String(); got != "text" {
		t.Errorf("got %q, want %q", got, "text")
	}
}

// TestAnsiStripper_ChunkBoundary_Charset 驗證 charset designation 的 final byte
// 被切到下一個 Write buffer 時，狀態機正確跨 buffer 消費，不會洩漏 final byte。
func TestAnsiStripper_ChunkBoundary_Charset(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("a\x1b("))
	w.Write([]byte("Bb"))
	if got := buf.String(); got != "ab" {
		t.Errorf("got %q, want %q", got, "ab")
	}
}

// TestAnsiStripper_WriteError 驗證 inner writer 失敗時 error 會被傳播，
// 且後續 Write 短路回報同一 error。
func TestAnsiStripper_WriteError(t *testing.T) {
	ew := &errWriter{err: errors.New("disk full")}
	w := newAnsiStripper(ew)
	if _, err := w.Write([]byte("hello")); err == nil {
		t.Fatal("expected error from inner writer, got nil")
	}
	if _, err := w.Write([]byte("world")); err == nil {
		t.Fatal("expected short-circuit error on subsequent write, got nil")
	}
}

func TestAnsiStripper_SingleCharESC(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("\x1b7save\x1b8restore\x1bMup"))
	if got := buf.String(); got != "saverestoreup" {
		t.Errorf("got %q, want %q", got, "saverestoreup")
	}
}

func TestAnsiStripper_PlainText(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	w.Write([]byte("no escapes here\n"))
	if got := buf.String(); got != "no escapes here\n" {
		t.Errorf("got %q, want %q", got, "no escapes here\n")
	}
}

func TestAnsiStripper_EmptyWrite(t *testing.T) {
	var buf bytes.Buffer
	w := newAnsiStripper(&buf)
	n, err := w.Write([]byte{})
	if n != 0 || err != nil {
		t.Errorf("got n=%d err=%v, want 0/nil", n, err)
	}
}
