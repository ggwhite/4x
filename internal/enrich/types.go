// Package enrich 把 auto-discover / miner 撿到的薄 candidate 透過 LLM Runner 補強為完整
// feature.Feature（subtasks/repos/rules/priority），供後續 Designer 取得足夠脈絡。
// CLI 層不直接呼叫 LLM——一律透過 runner.Runner 介面委託。
package enrich

import "github.com/ggwhite/4x/internal/feature"

// EnrichResult 是 Enrich 的回傳結果。Discarded 為 true 時代表 candidate 品質不足被丟棄，
// Reason 說明原因；Discarded 為 false 時 Feature 為補強後的完整 feature（ID/Name 由呼叫端填）。
type EnrichResult struct {
	Feature   feature.Feature
	Discarded bool
	Reason    string
}

// enrichResponse 是 LLM 在 [ENRICHMENT-RESULT] 區塊回傳的 JSON 結構。
type enrichResponse struct {
	Subtasks    []enrichSubtask `json:"subtasks"`
	Repos       []string        `json:"repos"`
	Rules       []string        `json:"rules"`
	Priority    int             `json:"priority"`
	Description string          `json:"description"`
}

// enrichSubtask 是 LLM 回傳的單一 subtask JSON 結構。
type enrichSubtask struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
