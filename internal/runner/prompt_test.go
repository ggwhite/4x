package runner

import (
	"bytes"
	"errors"
	"testing"
)

// TestPromptStripper_SkipsPromptEcho 驗證 user...codex 區塊間的 prompt 回顯被丟棄。
func TestPromptStripper_SkipsPromptEcho(t *testing.T) {
	var buf bytes.Buffer
	s := newPromptStripper(&buf)
	s.Write([]byte("header line\nuser\nmy prompt echo\ncodex\nreal output\n"))
	got := buf.String()
	want := "header line\ncodex\nreal output\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestPromptStripper_WriteError 驗證 inner writer 失敗時 error 會被傳播，
// 且後續 Write 短路回報同一 error。
func TestPromptStripper_WriteError(t *testing.T) {
	ew := &errWriter{err: errors.New("disk full")}
	s := newPromptStripper(ew)
	// 第一行（header）就會嘗試寫入 inner writer 並失敗
	if _, err := s.Write([]byte("header\n")); err == nil {
		t.Fatal("expected error from inner writer, got nil")
	}
	if _, err := s.Write([]byte("more\n")); err == nil {
		t.Fatal("expected short-circuit error on subsequent write, got nil")
	}
}

// TestPromptStripper_PassthroughWriteError 驗證進入 passthrough 狀態後
// inner writer 失敗同樣會被傳播。
func TestPromptStripper_PassthroughWriteError(t *testing.T) {
	ew := &errWriter{err: errors.New("disk full"), failAt: 2}
	s := newPromptStripper(ew)
	// 先讓 stripper 進入 passthrough（state=2）：header → user → codex
	if _, err := s.Write([]byte("user\ncodex\n")); err != nil {
		t.Fatalf("unexpected error before passthrough: %v", err)
	}
	// passthrough 階段寫入觸發 failAt
	if _, err := s.Write([]byte("payload")); err == nil {
		t.Fatal("expected error during passthrough write, got nil")
	}
}
