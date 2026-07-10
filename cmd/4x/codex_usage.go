package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ggwhite/4x/internal/orchestrator"
	"github.com/ggwhite/4x/internal/protocol"
)

// codexUsageRow 是單一 codex round 的最新額度用量，供 `4x status` 與 `4x cost` 共用顯示。
type codexUsageRow struct {
	Round            int     `json:"round"`
	PrimaryPercent   float64 `json:"primary_pct"`
	SecondaryPercent float64 `json:"secondary_pct"`
	Tokens           int     `json:"tokens"`
}

// latestCodexUsageByRound 直接讀 events.jsonl，挑出 type=="run-end" 且 codex!=nil 的事件，
// per-round 取最後一筆（同 round 多次 codex invocation 以最新為準），依 round 升冪回傳。
//
// 刻意不經 collectFeatureCost：後者為 stream-first，混合 feature（claude 有 stream + codex 有
// events run-end）會被 stream 優先邏輯遮蔽 codex 用量。此 helper 與 USD 收集完全解耦（見 F168
// Design Ruling 11）。無資料時回傳 nil。
func latestCodexUsageByRound(ws *protocol.Workspace, featureID string) []codexUsageRow {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	byRound := map[int]codexUsageRow{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var e struct {
			Type       string               `json:"type"`
			Round      int                  `json:"round"`
			TokensUsed int                  `json:"tokens_used"`
			Codex      *protocol.CodexUsage `json:"codex"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if e.Type != "run-end" || e.Codex == nil {
			continue
		}
		byRound[e.Round] = codexUsageRow{
			Round:            e.Round,
			PrimaryPercent:   e.Codex.PrimaryPercent,
			SecondaryPercent: e.Codex.SecondaryPercent,
			Tokens:           e.TokensUsed,
		}
	}
	if len(byRound) == 0 {
		return nil
	}
	rows := make([]codexUsageRow, 0, len(byRound))
	for _, row := range byRound {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Round < rows[j].Round })
	return rows
}

// formatCodexRow 把單一 round 額度用量格式化為 `round N: 5h X% / 1wk Y% (N tokens)`，
// 供 status 與 cost 的 codex 區塊共用。
func formatCodexRow(row codexUsageRow) string {
	return fmt.Sprintf("round %d: 5h %s / 1wk %s (%s tokens)",
		row.Round, orchestrator.FormatPct(row.PrimaryPercent), orchestrator.FormatPct(row.SecondaryPercent),
		orchestrator.FormatTokens(row.Tokens))
}

// printCodexUsage 印出 codex 額度用量區塊（含前導空行與標頭）；rows 為空時不印任何內容。
func printCodexUsage(rows []codexUsageRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Println("\nCodex usage:")
	for _, row := range rows {
		fmt.Printf("  %s\n", formatCodexRow(row))
	}
}
