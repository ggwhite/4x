package runner

import (
	"bytes"
	"testing"
)

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
