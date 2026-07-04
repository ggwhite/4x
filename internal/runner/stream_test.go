package runner

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStreamProcessor_AssistantText(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}` + "\n"
	_, err := p.Write([]byte(line))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := logBuf.String(); got != "[assistant] Hello world\n" {
		t.Errorf("log = %q, want %q", got, "[assistant] Hello world\n")
	}
	if got := rawBuf.String(); got != line {
		t.Errorf("raw = %q, want %q", got, line)
	}
}

func TestStreamProcessor_ToolUse(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"runner.go","old_string":"a","new_string":"b"}}]}}` + "\n"
	_, err := p.Write([]byte(line))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := logBuf.String()
	if got != "[tool_use] Edit: runner.go\n" {
		t.Errorf("log = %q, want %q", got, "[tool_use] Edit: runner.go\n")
	}
}

func TestStreamProcessor_ToolUseBash(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n"
	_, err := p.Write([]byte(line))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := logBuf.String()
	if got != "[tool_use] Bash: go test ./...\n" {
		t.Errorf("log = %q, want %q", got, "[tool_use] Bash: go test ./...\n")
	}
}

func TestStreamProcessor_Result(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	line := `{"type":"result","subtype":"success","duration_ms":45200,"total_cost_usd":0.12}` + "\n"
	_, err := p.Write([]byte(line))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := logBuf.String()
	if got != "[result] success (45.2s, $0.1200)\n" {
		t.Errorf("log = %q, want %q", got, "[result] success (45.2s, $0.1200)\n")
	}
}

func TestStreamProcessor_SystemEventsFiltered(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	events := []string{
		`{"type":"system","subtype":"init","session_id":"abc"}`,
		`{"type":"system","subtype":"hook_started","hook_name":"test"}`,
		`{"type":"system","subtype":"hook_response","hook_name":"test"}`,
		`{"type":"system","subtype":"thinking_tokens","estimated_tokens":50}`,
		`{"type":"rate_limit_event","rate_limit_info":{}}`,
	}
	for _, e := range events {
		_, err := p.Write([]byte(e + "\n"))
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if logBuf.Len() != 0 {
		t.Errorf("log should be empty for system events, got %q", logBuf.String())
	}
	if lines := strings.Count(rawBuf.String(), "\n"); lines != 5 {
		t.Errorf("raw line count = %d, want 5", lines)
	}
}

func TestStreamProcessor_InvalidJSON(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	_, err := p.Write([]byte("not json at all\n"))
	if err != nil {
		t.Fatalf("Write bad line: %v", err)
	}
	_, err = p.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"after bad line"}]}}` + "\n"))
	if err != nil {
		t.Fatalf("Write good line: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if logBuf.String() != "[assistant] after bad line\n" {
		t.Errorf("log = %q, want only the valid assistant line", logBuf.String())
	}
	if lines := strings.Count(rawBuf.String(), "\n"); lines != 2 {
		t.Errorf("raw line count = %d, want 2", lines)
	}
}

func TestStreamProcessor_SplitWrite(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	full := `{"type":"assistant","message":{"content":[{"type":"text","text":"split test"}]}}` + "\n"
	mid := len(full) / 2
	_, err := p.Write([]byte(full[:mid]))
	if err != nil {
		t.Fatalf("Write first part: %v", err)
	}
	_, err = p.Write([]byte(full[mid:]))
	if err != nil {
		t.Fatalf("Write second part: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := logBuf.String(); got != "[assistant] split test\n" {
		t.Errorf("log = %q, want %q", got, "[assistant] split test\n")
	}
}

func TestStreamProcessor_AssistantTextDelta(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	_, err := p.Write([]byte(`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Hello"}]}}` + "\n"))
	if err != nil {
		t.Fatalf("Write first line: %v", err)
	}
	_, err = p.Write([]byte(`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Hello world"}]}}` + "\n"))
	if err != nil {
		t.Fatalf("Write second line: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	want := "[assistant] Hello\n[assistant]  world\n"
	if got := logBuf.String(); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestTruncateForLog_MultibyteBoundary(t *testing.T) {
	// "百" is 3 bytes (E7 99 BE); the cut point at byte 120 lands on its
	// first byte, which used to split the rune and yield invalid UTF-8.
	prefix := strings.Repeat("a", 119)
	s := prefix + "百盛extra"

	got := truncateForLog(s, 120)

	if !utf8.ValidString(got) {
		t.Errorf("truncateForLog(%d) produced invalid UTF-8: %q", 120, got)
	}
}
