package evolution

import (
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// Rejection 記錄一筆被 POST-veto 否決的 candidate 與首個命中的否決原因。
type Rejection struct {
	Title  string
	Reason string
}

// PostVeto 對 gate role 的 verdict 套用不可翻硬否決與 convergence cap。
// 依序檢查每筆 candidate，任一規則命中即 reject 並記第一個命中原因——reject 永遠蓋過 accept，不用加權平均。
// 否決規則順序：非 accept → 缺 why_not_hack → 低於 value_floor → 重複既有 feature →
// 超 max_accept_per_run → 超 max_backlog_undone。接受時把 verdict 的 ValueScore/WhyNotHack 寫回 candidate。
func PostVeto(cands []protocol.Candidate, verdicts []Verdict, existing []feature.Feature, cfg ResolvedEvolution) (accepted []protocol.Candidate, rejected []Rejection) {
	byTitle := make(map[string]Verdict, len(verdicts))
	for _, v := range verdicts {
		byTitle[v.Title] = v
	}

	undone := 0
	for _, e := range existing {
		if isUndone(e.Status) {
			undone++
		}
	}

	for _, c := range cands {
		v, ok := byTitle[c.Title]
		reason := ""
		switch {
		case !ok || v.Verdict != VerdictAccept:
			reason = "gate did not accept"
		case strings.TrimSpace(v.WhyNotHack) == "":
			reason = "missing why_not_hack justification"
		case v.ValueScore < cfg.ValueFloor:
			reason = "value_score below floor"
		case duplicatesExisting(c, existing, cfg.DedupThreshold):
			reason = "duplicates existing feature"
		case len(accepted) >= cfg.MaxAcceptPerRun:
			reason = "max_accept_per_run reached"
		case undone+len(accepted) >= cfg.MaxBacklogUndone:
			reason = "max_backlog_undone reached"
		}
		if reason != "" {
			rejected = append(rejected, Rejection{Title: c.Title, Reason: reason})
			continue
		}
		c.ValueScore = v.ValueScore
		c.WhyNotHack = v.WhyNotHack
		accepted = append(accepted, c)
	}
	return accepted, rejected
}

// isUndone 回報 status 是否計入 backlog 未做數：
// not-started/in-progress/blocked/needs-attention 計入；draft/ready-for-review/abandoned/done 不計。
func isUndone(s feature.Status) bool {
	switch s {
	case feature.StatusNotStarted, feature.StatusInProgress, feature.StatusBlocked, feature.StatusNeedsAttention:
		return true
	default:
		return false
	}
}

// duplicatesExisting 二次確認 candidate 是否與任一既有 feature 相似（防 LLM 漏看）。
func duplicatesExisting(c protocol.Candidate, existing []feature.Feature, threshold float64) bool {
	ctext := c.Title + " " + c.Description
	for _, e := range existing {
		if protocol.IsSimilarFeatureThreshold(ctext, e.Name+" "+e.Description, threshold) {
			return true
		}
	}
	return false
}
