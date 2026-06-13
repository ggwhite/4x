package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type ccusageResponse struct {
	Daily []UsageDailyEntry `json:"daily"`
}

// parseCcusageOutput 解析 ccusage daily --json 的輸出，回傳 daily 陣列
func parseCcusageOutput(data []byte) ([]UsageDailyEntry, error) {
	var resp ccusageResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse ccusage output: %w", err)
	}
	return resp.Daily, nil
}

// FetchUsage 呼叫 ccusage daily --json 取得用量資料。
// 回傳 (entries, available, error)：
//   - available=false 表示 ccusage 及 npx 均不可用，此時 error 為 nil
//   - error 只在 ccusage 可用但執行或解析失敗時回傳
func FetchUsage() ([]UsageDailyEntry, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := exec.LookPath("ccusage"); err == nil {
		out, err := exec.CommandContext(ctx, "ccusage", "daily", "--json").CombinedOutput()
		if err != nil {
			return nil, true, fmt.Errorf("ccusage failed: %w", err)
		}
		entries, parseErr := parseCcusageOutput(out)
		if parseErr != nil {
			return nil, true, parseErr
		}
		return entries, true, nil
	}

	npxPath, err := exec.LookPath("npx")
	if err != nil {
		return nil, false, nil
	}
	out, err := exec.CommandContext(ctx, npxPath, "ccusage", "daily", "--json").CombinedOutput()
	if err != nil {
		return nil, false, nil
	}
	entries, parseErr := parseCcusageOutput(out)
	if parseErr != nil {
		return nil, true, parseErr
	}
	return entries, true, nil
}
