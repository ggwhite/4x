// Package codexlog 提供 codex runner 逐行 JSONL round-log 的 on-the-fly 可讀化與
// token 統計工具。原始 log 檔內容完全不變，轉換只在讀取／顯示當下發生。
//
// 本套件只依賴標準庫，不 import internal/*，供 internal/orchestrator 與
// internal/server 共用而不製造 import cycle。
package codexlog

import (
	"bytes"
	"encoding/json"
	"strings"
)

// codexEvent 是 codex round-log 單行事件的解析目標（只取本套件關心的欄位）。
type codexEvent struct {
	Type string `json:"type"`
	Item struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Message string `json:"message"`
	} `json:"item"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// isCodexType 判斷 type 是否為 codex 事件前綴（thread. / turn. / item.）。
func isCodexType(t string) bool {
	return strings.HasPrefix(t, "thread.") ||
		strings.HasPrefix(t, "turn.") ||
		strings.HasPrefix(t, "item.")
}

// ConvertLine 逐行轉換單一 codex JSONL 行為可讀文字。
//
// 回傳 (text, handled)：
//   - handled==false 代表「非 codex 行」，呼叫端應原樣保留該行（claude 的
//     [assistant]/[tool_use]/[result] 文字、任意非 codex JSON、空行皆屬此類）。
//   - handled==true 且 text!="" 代表 codex 事件且有可顯示文字。
//   - handled==true 且 text=="" 代表「是 codex 事件但刻意丟棄」（不輸出 raw JSON）。
//
// 具體對應：
//   - item.completed + item.type==agent_message → (item.text, true)
//   - item.completed + item.type==error → ("⚠️ "+item.message, true)
//   - item.completed 其他 item.type（reasoning / command_execution 等）→ ("", true)
//   - turn.failed → ("[turn failed] "+error.message, true)
//   - 其餘 codex 前綴事件（thread.started / turn.started / turn.completed
//     / item.started / item.updated 等）→ ("", true)
func ConvertLine(line []byte) (text string, handled bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return "", false
	}
	var ev codexEvent
	if err := json.Unmarshal(trimmed, &ev); err != nil {
		return "", false
	}
	if ev.Type == "" || !isCodexType(ev.Type) {
		return "", false
	}
	switch ev.Type {
	case "item.completed":
		switch ev.Item.Type {
		case "agent_message":
			return ev.Item.Text, true
		case "error":
			return "⚠️ " + ev.Item.Message, true
		default:
			return "", true
		}
	case "turn.failed":
		return "[turn failed] " + ev.Error.Message, true
	default:
		return "", true
	}
}

// RenderContent 把整段 JSONL 內容轉為可讀文字（供 REST 一次性讀取路徑）。
//
// 以 \n 逐行處理：handled && text!="" → 寫 text+"\n"；handled && text=="" →
// 略過該行；!handled → 原樣寫回該行（含其原本的換行結構，逐 byte 保留，
// 確保 claude log 穿透後與原始檔完全一致）。空輸入回空。
func RenderContent(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte{}
	}
	var out bytes.Buffer
	rest := raw
	for len(rest) > 0 {
		idx := bytes.IndexByte(rest, '\n')
		var line []byte
		hasNL := idx >= 0
		if hasNL {
			line = rest[:idx]
			rest = rest[idx+1:]
		} else {
			line = rest
			rest = nil
		}
		text, handled := ConvertLine(line)
		if handled {
			if text != "" {
				out.WriteString(text)
				out.WriteByte('\n')
			}
			continue
		}
		// 非 codex：原樣寫回，僅在原行本就帶換行時補回 \n，維持 byte 一致。
		out.Write(line)
		if hasNL {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

// SumTurnTokens 掃描整段內容，對每筆 type==turn.completed 累加
// usage.input_tokens + usage.output_tokens + usage.reasoning_output_tokens。
//
// 只要出現過任一 turn.completed（含 usage 全 0）即回傳 found=true；完全無
// turn.completed 時回傳 (0, false)。壞行（無法 JSON parse）略過不失敗。
func SumTurnTokens(raw []byte) (total int, found bool) {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Usage struct {
				InputTokens           int `json:"input_tokens"`
				OutputTokens          int `json:"output_tokens"`
				ReasoningOutputTokens int `json:"reasoning_output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(trimmed, &ev); err != nil {
			continue
		}
		if ev.Type != "turn.completed" {
			continue
		}
		found = true
		total += ev.Usage.InputTokens + ev.Usage.OutputTokens + ev.Usage.ReasoningOutputTokens
	}
	return total, found
}
