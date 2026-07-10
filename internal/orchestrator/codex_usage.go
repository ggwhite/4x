package orchestrator

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/protocol"
)

// ResolveCodexHome 決定 codex rollout 檔的根目錄。
// env 非空時原樣回傳（尊重 CODEX_HOME 覆寫）；env 為空時回退 <userHome>/.codex。
// 取不到 user home（os.UserHomeDir 失敗）時回傳空字串，讓上層 glob 落空 → skip。
func ResolveCodexHome(env string) string {
	if env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// ParseCodexUsage 從 codex round log 定位對應 rollout jsonl，擷取即時額度用量。
//
// 流程（任一步失敗一律回 (nil, 0)，不 panic、不阻塞）：
//  1. 掃 round log（codex 加 --json 後為 JSONL），取第一筆 type=="thread.started"
//     事件的 thread_id 當 session id。
//  2. 以遞迴 glob `<codexHome>/sessions/*/*/*/rollout-*-<sessionID>.jsonl` 定位
//     rollout 檔（*/*/* 涵蓋 YYYY/MM/DD，避免跨日界算錯目錄）。
//  3. 逐行解析 rollout，取最後一筆 rate_limits 非 null 的 token_count 事件，回傳
//     其 primary/secondary 的 used_percent+resets_at，以及 total_token_usage.total_tokens。
func ParseCodexUsage(logPath, codexHome string) (*protocol.CodexUsage, int) {
	sessionID, ok := codexSessionIDFromLog(logPath)
	if !ok {
		return nil, 0
	}
	rolloutPath, ok := findCodexRollout(codexHome, sessionID)
	if !ok {
		return nil, 0
	}
	usage, tokens, ok := codexRateLimitsFromRollout(rolloutPath)
	if !ok {
		return nil, 0
	}
	return usage, tokens
}

// codexSessionIDFromLog 掃描 round log 各行，回傳第一筆能 JSON parse 且
// type=="thread.started" 事件的 thread_id。log 開頭的非 JSON 行（如
// `Reading prompt from stdin...`）parse 失敗略過。
func codexSessionIDFromLog(logPath string) (string, bool) {
	f, err := os.Open(logPath)
	if err != nil {
		return "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Type == "thread.started" && ev.ThreadID != "" {
			return ev.ThreadID, true
		}
	}
	return "", false
}

// findCodexRollout 以遞迴 glob 定位 sessionID 對應的 rollout 檔，取第一個 match。
func findCodexRollout(codexHome, sessionID string) (string, bool) {
	if codexHome == "" {
		return "", false
	}
	pattern := filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*-"+sessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

// codexRateLimitsFromRollout 逐行解析 rollout jsonl，保留最後一筆 rate_limits
// 非 null 的 token_count 事件，回傳其額度百分比/重置時間與累計 token 總量。
// 找不到任何符合的事件（含全部 rate_limits 為 null、壞 JSON 整檔無有效事件）回 ok=false。
func codexRateLimitsFromRollout(rolloutPath string) (*protocol.CodexUsage, int, bool) {
	f, err := os.Open(rolloutPath)
	if err != nil {
		return nil, 0, false
	}
	defer f.Close()

	type window struct {
		UsedPercent float64 `json:"used_percent"`
		ResetsAt    int64   `json:"resets_at"`
	}
	var (
		last  *protocol.CodexUsage
		lastN int
	)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)
	for sc.Scan() {
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Info struct {
					TotalTokenUsage struct {
						TotalTokens int `json:"total_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
				RateLimits *struct {
					Primary   *window `json:"primary"`
					Secondary *window `json:"secondary"`
				} `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Type != "event_msg" || ev.Payload.Type != "token_count" || ev.Payload.RateLimits == nil {
			continue
		}
		usage := &protocol.CodexUsage{}
		if p := ev.Payload.RateLimits.Primary; p != nil {
			usage.PrimaryPercent = p.UsedPercent
			usage.PrimaryResetsAt = p.ResetsAt
		}
		if s := ev.Payload.RateLimits.Secondary; s != nil {
			usage.SecondaryPercent = s.UsedPercent
			usage.SecondaryResetsAt = s.ResetsAt
		}
		last = usage
		lastN = ev.Payload.Info.TotalTokenUsage.TotalTokens
	}
	if last == nil {
		return nil, 0, false
	}
	return last, lastN, true
}
