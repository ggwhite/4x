package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

type streamJSONProcessor struct {
	logW io.Writer
	rawW io.Writer

	pending []byte
	err     error

	lastMessageID    string
	lastTextLenByIdx map[int]int
	lastToolUseByIdx map[int]string
}

type streamEvent struct {
	Type         string         `json:"type"`
	Subtype      string         `json:"subtype,omitempty"`
	Result       string         `json:"result,omitempty"`
	DurationMs   float64        `json:"duration_ms,omitempty"`
	TotalCostUSD float64        `json:"total_cost_usd,omitempty"`
	Message      *streamMessage `json:"message,omitempty"`
}

type streamMessage struct {
	ID      string          `json:"id,omitempty"`
	Content []streamContent `json:"content,omitempty"`
}

type streamContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

func newStreamJSONProcessor(logW, rawW io.Writer) *streamJSONProcessor {
	return &streamJSONProcessor{
		logW:             logW,
		rawW:             rawW,
		lastTextLenByIdx: make(map[int]int),
		lastToolUseByIdx: make(map[int]string),
	}
}

func (p *streamJSONProcessor) Write(data []byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}

	p.pending = append(p.pending, data...)
	for {
		idx := bytes.IndexByte(p.pending, '\n')
		if idx < 0 {
			break
		}
		line := append([]byte(nil), p.pending[:idx]...)
		p.pending = p.pending[idx+1:]

		if _, err := p.rawW.Write(line); err != nil {
			return len(data), err
		}
		if _, err := p.rawW.Write([]byte("\n")); err != nil {
			return len(data), err
		}

		p.processLine(line)
	}

	return len(data), nil
}

func (p *streamJSONProcessor) Close() error {
	if p.err != nil {
		return p.err
	}
	if len(p.pending) == 0 {
		return nil
	}

	line := append([]byte(nil), p.pending...)
	p.pending = nil

	if _, err := p.rawW.Write(line); err != nil {
		return err
	}
	p.processLine(line)
	return p.err
}

func (p *streamJSONProcessor) processLine(line []byte) {
	var ev streamEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}

	switch ev.Type {
	case "assistant":
		p.handleAssistant(&ev)
	case "result":
		p.handleResult(&ev)
	case "system", "rate_limit_event":
		return
	default:
		return
	}
}

func (p *streamJSONProcessor) handleAssistant(ev *streamEvent) {
	if ev.Message == nil {
		return
	}

	p.syncMessageState(ev.Message)

	for i, c := range ev.Message.Content {
		switch c.Type {
		case "text":
			p.writeAssistantTextDelta(i, c.Text)
		case "tool_use":
			p.writeToolUseDelta(i, c.Name, c.Input)
		}
	}
}

func (p *streamJSONProcessor) syncMessageState(msg *streamMessage) {
	msgID := msg.ID
	if msgID == "" {
		msgID = "__no_id__"
	}

	if p.lastMessageID == "" {
		p.lastMessageID = msgID
		return
	}

	if msgID != p.lastMessageID {
		p.lastMessageID = msgID
		p.lastTextLenByIdx = make(map[int]int)
		p.lastToolUseByIdx = make(map[int]string)
		return
	}

	// 沒有 message id 時，用 text 前綴關係判斷是否為新的 assistant 訊息。
	if msg.ID == "" {
		prev := p.lastTextLenByIdx[0]
		if prev > 0 && len(msg.Content) > 0 && msg.Content[0].Type == "text" && len(msg.Content[0].Text) < prev {
			p.lastTextLenByIdx = make(map[int]int)
			p.lastToolUseByIdx = make(map[int]string)
		}
	}
}

func (p *streamJSONProcessor) writeAssistantTextDelta(idx int, text string) {
	if text == "" {
		return
	}

	prevLen := p.lastTextLenByIdx[idx]
	if prevLen > len(text) {
		prevLen = 0
	}

	delta := text
	if prevLen > 0 {
		delta = text[prevLen:]
	}
	p.lastTextLenByIdx[idx] = len(text)

	if delta == "" {
		return
	}
	p.writeLogf("[assistant] %s\n", delta)
}

func (p *streamJSONProcessor) writeToolUseDelta(idx int, name string, input json.RawMessage) {
	summary := summarizeToolInput(input)
	key := name + "|" + summary
	if key == p.lastToolUseByIdx[idx] {
		return
	}
	p.lastToolUseByIdx[idx] = key

	if summary == "" {
		p.writeLogf("[tool_use] %s\n", name)
		return
	}
	p.writeLogf("[tool_use] %s: %s\n", name, summary)
}

func (p *streamJSONProcessor) handleResult(ev *streamEvent) {
	subtype := ev.Subtype
	if subtype == "" {
		subtype = "unknown"
	}
	p.writeLogf("[result] %s (%.1fs, $%.4f)\n", subtype, ev.DurationMs/1000.0, ev.TotalCostUSD)
}

func summarizeToolInput(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}

	if cmd, ok := payload["command"].(string); ok && strings.TrimSpace(cmd) != "" {
		return truncateForLog(cmd, 120)
	}
	if filePath, ok := payload["file_path"].(string); ok && strings.TrimSpace(filePath) != "" {
		return filePath
	}

	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}

	max := len(keys)
	if max > 3 {
		max = 3
	}
	return strings.Join(keys[:max], ",")
}

func truncateForLog(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "..."
}

func (p *streamJSONProcessor) writeLogf(format string, args ...any) {
	if p.err != nil {
		return
	}
	if _, err := fmt.Fprintf(p.logW, format, args...); err != nil {
		p.err = err
	}
}
