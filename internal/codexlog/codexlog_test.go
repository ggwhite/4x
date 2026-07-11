package codexlog

import (
	"strings"
	"testing"
)

// TestConvertLine 驗證 AC-1：三種可顯示的 codex 事件轉換結果。
func TestConvertLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantText    string
		wantHandled bool
	}{
		{
			"agent_message",
			`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"HELLO"}}`,
			"HELLO", true,
		},
		{
			"error item",
			`{"type":"item.completed","item":{"type":"error","message":"boom"}}`,
			"⚠️ boom", true,
		},
		{
			"turn.failed",
			`{"type":"turn.failed","error":{"message":"nope"}}`,
			"[turn failed] nope", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, handled := ConvertLine([]byte(tt.line))
			if handled != tt.wantHandled {
				t.Errorf("handled = %v, want %v", handled, tt.wantHandled)
			}
			if text != tt.wantText {
				t.Errorf("text = %q, want %q", text, tt.wantText)
			}
		})
	}
	// error / turn.failed 訊息須包含原始 message。
	if text, _ := ConvertLine([]byte(`{"type":"item.completed","item":{"type":"error","message":"boom"}}`)); !strings.Contains(text, "boom") {
		t.Errorf("error text %q missing 'boom'", text)
	}
	if text, _ := ConvertLine([]byte(`{"type":"turn.failed","error":{"message":"nope"}}`)); !strings.Contains(text, "nope") {
		t.Errorf("turn.failed text %q missing 'nope'", text)
	}
}

// TestConvertLine_Passthrough 驗證 AC-2：非 codex 行 handled==false（呼叫端保留原行）；
// codex 前綴但未知/忽略事件回 ("", true)（辨識為 codex 但丟棄、不輸出 raw JSON）。
func TestConvertLine_Passthrough(t *testing.T) {
	notCodex := []string{
		`[assistant] hi`,
		`[tool_use] Read: /x`,
		`{"foo":1}`,
		`{"type":"assistant"}`,
		``,
		`   `,
	}
	for _, line := range notCodex {
		t.Run("notcodex:"+line, func(t *testing.T) {
			text, handled := ConvertLine([]byte(line))
			if handled {
				t.Errorf("line %q: handled = true, want false", line)
			}
			if text != "" {
				t.Errorf("line %q: text = %q, want empty", line, text)
			}
		})
	}

	ignored := []string{
		`{"type":"thread.started","thread_id":"abc"}`,
		`{"type":"turn.started"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2,"reasoning_output_tokens":3}}`,
		`{"type":"item.started","item":{"type":"agent_message"}}`,
		`{"type":"item.completed","item":{"type":"reasoning"}}`,
		`{"type":"item.completed","item":{"type":"command_execution"}}`,
	}
	for _, line := range ignored {
		t.Run("ignored:"+line, func(t *testing.T) {
			text, handled := ConvertLine([]byte(line))
			if !handled {
				t.Errorf("line %q: handled = false, want true (codex event, dropped)", line)
			}
			if text != "" {
				t.Errorf("line %q: text = %q, want empty (dropped)", line, text)
			}
		})
	}
}

// TestRenderContent 驗證 AC-3：混合 JSONL 轉為可讀文字，輸出不含原始 JSON、含期望片段、
// 壞 JSON 行原樣保留。
func TestRenderContent(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"thread.started","thread_id":"abc"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"hello world"}}`,
		`{"type":"item.completed","item":{"type":"error","message":"disk full"}}`,
		`{"type":"turn.failed","error":{"message":"model refused"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1}}`,
		`{bad json here`,
	}, "\n") + "\n"

	out := string(RenderContent([]byte(raw)))

	if strings.Contains(out, `"type":`) {
		t.Errorf("output still contains raw JSON:\n%s", out)
	}
	for _, want := range []string{"hello world", "disk full", "model refused", "{bad json here"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestRenderContent_ClaudePassthrough 驗證 AC-7 前提：非 codex（claude）內容經 RenderContent
// 後與原始 byte 完全一致（帶/不帶結尾換行、含空行皆穿透）。
func TestRenderContent_ClaudePassthrough(t *testing.T) {
	cases := []string{
		"[assistant] hi\n[tool_use] Read: /x\n[result] success (1.0s, $0.5)\n",
		"line without trailing newline",
		"a\n\nb\n",
		"",
	}
	for _, in := range cases {
		got := string(RenderContent([]byte(in)))
		if got != in {
			t.Errorf("passthrough mismatch:\n in  = %q\n got = %q", in, got)
		}
	}
}

// TestSumTurnTokens 驗證 AC-4：累加所有 turn.completed 的三欄 usage、found 布林、壞行略過。
func TestSumTurnTokens(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"thread.started"}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2,"reasoning_output_tokens":1}}`,
		`{not valid json`,
		`{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":20,"reasoning_output_tokens":5}}`,
	}, "\n")

	total, found := SumTurnTokens([]byte(raw))
	if !found {
		t.Fatal("found = false, want true")
	}
	if want := 10 + 2 + 1 + 100 + 20 + 5; total != want {
		t.Errorf("total = %d, want %d", total, want)
	}

	// 完全無 turn.completed → (0, false)。
	noTurn := "[assistant] hi\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"x\"}}\n"
	if total, found := SumTurnTokens([]byte(noTurn)); total != 0 || found {
		t.Errorf("no turn.completed = (%d, %v), want (0, false)", total, found)
	}

	// turn.completed 但 usage 全 0 → found=true, total=0。
	zero := `{"type":"turn.completed","usage":{"input_tokens":0}}`
	if total, found := SumTurnTokens([]byte(zero)); total != 0 || !found {
		t.Errorf("zero-usage turn = (%d, %v), want (0, true)", total, found)
	}
}

// TestBadInputs 驗證 AC-11：壞輸入一律不 panic、不回 error，回合理零值/部分值。
func TestBadInputs(t *testing.T) {
	bad := [][]byte{
		nil,
		[]byte(""),
		[]byte("   "),
		[]byte("{not json"),
		[]byte("\x00\x01\x02"),
		[]byte(`{"type":`),
		[]byte("plain text line"),
	}
	for _, b := range bad {
		// 皆不得 panic。
		_, _ = ConvertLine(b)
		_ = RenderContent(b)
		_, _ = SumTurnTokens(b)
	}
	// 明確斷言零值。
	if out := RenderContent(nil); len(out) != 0 {
		t.Errorf("RenderContent(nil) = %q, want empty", out)
	}
	if total, found := SumTurnTokens([]byte("{garbage")); total != 0 || found {
		t.Errorf("SumTurnTokens(garbage) = (%d, %v), want (0, false)", total, found)
	}
}
