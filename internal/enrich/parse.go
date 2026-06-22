package enrich

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	enrichResultStart = "[ENRICHMENT-RESULT]"
	enrichResultEnd   = "[/ENRICHMENT-RESULT]"
)

// parseResponse 從 runner log 中提取 [ENRICHMENT-RESULT] … [/ENRICHMENT-RESULT] 區塊並解析 JSON。
// 缺任一 marker 或 JSON 非法時回 error，由呼叫端轉為 Discarded 結果。
func parseResponse(logContent string) (*enrichResponse, error) {
	startIdx := strings.Index(logContent, enrichResultStart)
	if startIdx < 0 {
		return nil, fmt.Errorf("enrichment marker %s not found", enrichResultStart)
	}
	after := logContent[startIdx+len(enrichResultStart):]

	endIdx := strings.Index(after, enrichResultEnd)
	if endIdx < 0 {
		return nil, fmt.Errorf("enrichment marker %s not found", enrichResultEnd)
	}
	jsonBlock := strings.TrimSpace(after[:endIdx])

	var resp enrichResponse
	if err := json.Unmarshal([]byte(jsonBlock), &resp); err != nil {
		return nil, fmt.Errorf("invalid enrichment JSON: %w", err)
	}
	return &resp, nil
}

// validate 驗證 LLM 回傳的 enrichment 結果是否滿足最低品質要求：
// subtasks ≥ 2、每個 subtask 有 id/name、description 非空、priority 落在 [1,5]。
func validate(resp *enrichResponse) error {
	if len(resp.Subtasks) < 2 {
		return fmt.Errorf("insufficient subtasks: got %d, need >= 2", len(resp.Subtasks))
	}
	for i, st := range resp.Subtasks {
		if st.ID == "" {
			return fmt.Errorf("subtask[%d] has empty ID", i)
		}
		if st.Name == "" {
			return fmt.Errorf("subtask[%d] has empty name", i)
		}
	}
	if resp.Description == "" {
		return fmt.Errorf("description is empty")
	}
	if resp.Priority < 1 || resp.Priority > 5 {
		return fmt.Errorf("priority %d out of range [1,5]", resp.Priority)
	}
	return nil
}
