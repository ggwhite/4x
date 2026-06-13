package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

var ccusageRunners = map[string]bool{
	"claude": true, "codex": true, "gemini": true, "copilot": true,
}

type blocksResponse struct {
	Blocks []UsageBlock `json:"blocks"`
}

// parseBlocksOutput 解析 ccusage blocks --active --json 的輸出
func parseBlocksOutput(data []byte) (*UsageBlock, error) {
	var resp blocksResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse ccusage blocks: %w", err)
	}
	for i := range resp.Blocks {
		if resp.Blocks[i].IsActive {
			return &resp.Blocks[i], nil
		}
	}
	return nil, nil
}

// runCcusage 執行 ccusage 指令，優先用系統安裝的 ccusage，其次 npx
func runCcusage(ctx context.Context, args ...string) ([]byte, bool, error) {
	if _, err := exec.LookPath("ccusage"); err == nil {
		out, err := exec.CommandContext(ctx, "ccusage", args...).CombinedOutput()
		if err != nil {
			return nil, true, fmt.Errorf("ccusage %v failed: %w", args, err)
		}
		return out, true, nil
	}

	npxPath, err := exec.LookPath("npx")
	if err != nil {
		return nil, false, nil
	}
	npxArgs := append([]string{"ccusage"}, args...)
	out, err := exec.CommandContext(ctx, npxPath, npxArgs...).CombinedOutput()
	if err != nil {
		return nil, true, fmt.Errorf("npx ccusage %v failed: %w", args, err)
	}
	return out, true, nil
}

// fetchBlock 取得指定 session length 的 active block
func fetchBlock(ctx context.Context, name string, sessionHours int) (*UsageBlock, bool, error) {
	args := []string{name, "blocks", "--active", "--json"}
	if sessionHours != 5 {
		args = append(args, "--session-length", fmt.Sprintf("%d", sessionHours))
	}
	out, avail, err := runCcusage(ctx, args...)
	if !avail || err != nil {
		return nil, avail, err
	}
	block, parseErr := parseBlocksOutput(out)
	return block, true, parseErr
}

type dailyEntry struct {
	TotalTokens int64   `json:"totalTokens"`
	TotalCost   float64 `json:"totalCost"`
	CostUSD     float64 `json:"costUSD"`
}

type dailyResponse struct {
	Daily []dailyEntry `json:"daily"`
}

// parseDailySummary 解析 ccusage daily --json 並彙總為 UsageSummary
func parseDailySummary(data []byte) (*UsageSummary, error) {
	var resp dailyResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse ccusage daily: %w", err)
	}
	if len(resp.Daily) == 0 {
		return nil, nil
	}
	s := &UsageSummary{Days: len(resp.Daily)}
	for _, e := range resp.Daily {
		s.TotalTokens += e.TotalTokens
		cost := e.TotalCost
		if cost == 0 {
			cost = e.CostUSD
		}
		s.TotalCost += cost
	}
	return s, nil
}

// RunnerUsageResult 是 FetchRunnerUsage 的回傳結構
type RunnerUsageResult struct {
	Block5h  *UsageBlock
	Block7d  *UsageBlock
	Daily7d  *UsageSummary
	Available bool
	Err       error
}

// FetchRunnerUsage 取得單一 runner 的用量資料。
// Claude: 5h block + 7d block（blocks --session-length 168）
// 其他 runner: 7d daily 彙總
func FetchRunnerUsage(name string) RunnerUsageResult {
	if !ccusageRunners[name] {
		return RunnerUsageResult{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var res RunnerUsageResult

	if name == "claude" {
		wg.Add(2)
		go func() {
			defer wg.Done()
			b, avail, e := fetchBlock(ctx, name, 5)
			mu.Lock()
			defer mu.Unlock()
			res.Available = res.Available || avail
			res.Block5h = b
			if e != nil && res.Err == nil {
				res.Err = e
			}
		}()
		go func() {
			defer wg.Done()
			b, avail, e := fetchBlock(ctx, name, 168)
			mu.Lock()
			defer mu.Unlock()
			res.Available = res.Available || avail
			res.Block7d = b
			if e != nil && res.Err == nil {
				res.Err = e
			}
		}()
	} else {
		since := time.Now().AddDate(0, 0, -7).Format("20060102")
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, avail, e := runCcusage(ctx, name, "daily", "--since", since, "--json")
			mu.Lock()
			defer mu.Unlock()
			res.Available = avail
			if e != nil {
				res.Err = e
				return
			}
			if out != nil {
				res.Daily7d, _ = parseDailySummary(out)
			}
		}()
	}

	wg.Wait()
	return res
}

// CcusageAvailable 檢查 ccusage 是否可用
func CcusageAvailable() bool {
	if _, err := exec.LookPath("ccusage"); err == nil {
		return true
	}
	if _, err := exec.LookPath("npx"); err == nil {
		return true
	}
	return false
}
