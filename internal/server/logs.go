package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

type logInfo struct {
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	DurationMs *int64  `json:"durationMs,omitempty"`
	StartedAt  *string `json:"startedAt,omitempty"`
}

type roleTiming struct {
	DurationMs *int64
	StartedAt  *string
}

var roleOrder = map[string]int{
	"designer":      0,
	"coder":         1,
	"reviewer":      2,
	"tester":        3,
	"deep-reviewer": 4,
	"synthesizer":   5,
	"deep-fix":      6,
	"deep-reverify": 7,
	"acceptor":      8,
}

// handleLogs 處理 /api/logs/<featureId> 列表或 /api/logs/<featureId>/<filename> 內容
func handleLogs(ws *protocol.CachedWorkspace, rest string, w http.ResponseWriter) {
	parts := strings.SplitN(rest, "/", 2)
	featureID := parts[0]
	logsDir := filepath.Join(ws.FeatureDir(featureID), "logs")

	if len(parts) == 1 || parts[1] == "" {
		entries, _ := os.ReadDir(logsDir)
		timings := parseRoleTimings(ws.FeatureDir(featureID))
		var logs []logInfo
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			li := logInfo{Name: e.Name(), Size: info.Size()}
			if t, ok := timings[e.Name()]; ok {
				li.DurationMs = t.DurationMs
				li.StartedAt = t.StartedAt
			}
			logs = append(logs, li)
		}
		sort.Slice(logs, func(i, j int) bool {
			return logSortKey(logs[i].Name) < logSortKey(logs[j].Name)
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
		return
	}

	filename := filepath.Base(parts[1])
	if !strings.HasSuffix(filename, ".log") {
		http.Error(w, "invalid log file", 400)
		return
	}
	data, err := os.ReadFile(filepath.Join(logsDir, filename))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// logSortKey 將 "round-N-role.log" 轉成數字 key，確保按執行順序排列。
// deep-fix-1、deep-reverify-2 等帶迭代號的 role 會去掉尾部數字做 base role 比對，
// 迭代號作為子排序依據。
func logSortKey(name string) int {
	name = strings.TrimSuffix(name, ".log")
	parts := strings.SplitN(name, "-", 3)
	if len(parts) < 3 || parts[0] != "round" {
		return 999999
	}
	round, _ := strconv.Atoi(parts[1])
	role := parts[2]
	iter := 0
	if idx := strings.LastIndex(role, "-"); idx > 0 {
		if n, err := strconv.Atoi(role[idx+1:]); err == nil {
			iter = n
			role = role[:idx]
		}
	}
	order, ok := roleOrder[role]
	if !ok {
		order = 99
	}
	return round*1000 + order*10 + iter
}

// logKeyFromEvent 將 events.jsonl 的 role 名稱映射到實際 log 檔名。
// mini-coder / re-verifier 會帶迭代號（deep-fix-1、deep-reverify-2），
// 用 iterCount 追蹤每個 round+role 的出現次數。
func logKeyFromEvent(round int, role, eventType string, iterCount map[string]int) string {
	switch role {
	case "mini-coder":
		counterKey := fmt.Sprintf("%d-mini-coder", round)
		if eventType == "phase-start" {
			iterCount[counterKey]++
		}
		return fmt.Sprintf("round-%d-deep-fix-%d.log", round, iterCount[counterKey])
	case "re-verifier":
		counterKey := fmt.Sprintf("%d-re-verifier", round)
		if eventType == "phase-start" {
			iterCount[counterKey]++
		}
		return fmt.Sprintf("round-%d-deep-reverify-%d.log", round, iterCount[counterKey])
	case "deep-reviewer":
		counterKey := fmt.Sprintf("%d-deep-reviewer", round)
		if eventType == "phase-start" {
			iterCount[counterKey]++
		}
		cnt := iterCount[counterKey]
		return fmt.Sprintf("round-%d-deep-reviewer-%d.log", round, cnt)
	default:
		return fmt.Sprintf("round-%d-%s.log", round, role)
	}
}

// parseRoleTimings 從 events.jsonl 讀取每個 role 的計時資訊。
// 已結束的 role 帶 DurationMs；仍在執行的 role 帶 StartedAt（供前端動態計算）。
func parseRoleTimings(featureDir string) map[string]roleTiming {
	data, err := os.ReadFile(filepath.Join(featureDir, "events.jsonl"))
	if err != nil {
		return nil
	}
	type event struct {
		Ts    time.Time `json:"ts"`
		Type  string    `json:"type"`
		Role  string    `json:"role"`
		Round int       `json:"round"`
	}
	starts := map[string]time.Time{}
	ended := map[string]bool{}
	timings := map[string]roleTiming{}
	iterCount := map[string]int{}
	var lastEventTs time.Time
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if !ev.Ts.IsZero() {
			lastEventTs = ev.Ts
		}
		if ev.Role == "" {
			continue
		}
		key := logKeyFromEvent(ev.Round, string(ev.Role), ev.Type, iterCount)
		switch ev.Type {
		case "phase-start":
			starts[key] = ev.Ts
		case "run-end":
			if start, ok := starts[key]; ok {
				ms := ev.Ts.Sub(start).Milliseconds()
				timings[key] = roleTiming{DurationMs: &ms}
				ended[key] = true
			}
		}
	}
	for key, ts := range starts {
		if ended[key] {
			continue
		}
		if !lastEventTs.IsZero() && lastEventTs.After(ts) {
			ms := lastEventTs.Sub(ts).Milliseconds()
			timings[key] = roleTiming{DurationMs: &ms}
		} else {
			s := ts.UTC().Format(time.RFC3339)
			timings[key] = roleTiming{StartedAt: &s}
		}
	}
	// 非平行 deep review 的 log 檔名為 round-N-deep-reviewer.log（無後綴），
	// 但 events 的第 1 個 phase-start 會產生 round-N-deep-reviewer-1.log key。
	// 把 -1 的 timing 也寫入無後綴 key，讓兩種模式都能查到。
	for key, t := range timings {
		if strings.Contains(key, "-deep-reviewer-1.log") {
			alt := strings.Replace(key, "-deep-reviewer-1.log", "-deep-reviewer.log", 1)
			if _, exists := timings[alt]; !exists {
				timings[alt] = t
			}
		}
	}
	return timings
}
