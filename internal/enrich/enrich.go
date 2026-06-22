package enrich

import (
	"context"
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// Enricher 負責將薄 candidate 透過 LLM Runner 補強為完整 feature。
type Enricher struct {
	ws          *protocol.Workspace
	runner      runner.Runner
	autoApprove bool
}

// New 建立 Enricher。autoApprove 控制 enrich 後的 feature 狀態：
// true → not-started（全自動），false → draft（需人工 approve）。
func New(ws *protocol.Workspace, r runner.Runner, autoApprove bool) *Enricher {
	return &Enricher{ws: ws, runner: r, autoApprove: autoApprove}
}

// Enrich 對單一 candidate 執行 LLM enrichment：collectContext → buildPrompt → runner.Run →
// 讀 log → parseResponse → validate。
//
// 回傳語意分三類：
//   - 成功：EnrichResult{Discarded:false, Feature: 補強後 feature}，error 為 nil。
//     Feature 的 ID/Name 不在此填（由呼叫端依 backlog 編號補上）。
//   - 品質不足（parse/validate 失敗）：EnrichResult{Discarded:true, Reason:...}，error 為 nil。
//   - Runner 執行錯誤（逾時等）或脈絡收集失敗：error 非 nil，由呼叫端決定處理。
func (e *Enricher) Enrich(ctx context.Context, candidate protocol.DiscoveredFeature) (*EnrichResult, error) {
	ectx, err := collectContext(e.ws, candidate.Title)
	if err != nil {
		return nil, fmt.Errorf("collect context: %w", err)
	}

	prompt, err := buildPrompt(candidate, ectx)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	res, err := e.runner.Run(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("runner: %w", err)
	}

	logContent, err := os.ReadFile(res.LogFile)
	if err != nil {
		return nil, fmt.Errorf("read runner log: %w", err)
	}

	resp, err := parseResponse(string(logContent))
	if err != nil {
		return &EnrichResult{Discarded: true, Reason: fmt.Sprintf("invalid response format: %v", err)}, nil
	}

	if err := validate(resp); err != nil {
		return &EnrichResult{Discarded: true, Reason: err.Error()}, nil
	}

	status := feature.StatusNotStarted
	if !e.autoApprove {
		status = feature.StatusDraft
	}

	priority := resp.Priority
	subtasks := make([]feature.Subtask, len(resp.Subtasks))
	for i, st := range resp.Subtasks {
		subtasks[i] = feature.Subtask{
			ID:          st.ID,
			Name:        st.Name,
			Description: st.Description,
		}
	}

	f := feature.Feature{
		Description: resp.Description,
		Status:      status,
		Priority:    &priority,
		Repos:       resp.Repos,
		Subtasks:    subtasks,
		Rules:       resp.Rules,
	}

	return &EnrichResult{Feature: f}, nil
}
