package protocol

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/learning"
)

// DefaultFailPatternThreshold 是 fail-pattern 掃描器把一群相似 issue 升級為 candidate
// 所需的「不同 feature 數」門檻預設值。由 4x mine 的 --min-occurrences flag 覆寫。
const DefaultFailPatternThreshold = 3

// CandidateSource 標記一筆 candidate 的來源分類，供後續 F097 閘門追溯與分流。
type CandidateSource string

const (
	// SourceEscalation 來自 round dir 的 escalation.json（spec-mismatch/criteria-wrong/blocker/scope-change）。
	SourceEscalation CandidateSource = "escalation"
	// SourceStuck 來自卡在 needs-attention/abandoned/blocked 的 feature。
	SourceStuck CandidateSource = "stuck"
	// SourceFailPattern 來自跨 feature 反覆出現的 reviewer/deep-reviewer FAIL issue。
	SourceFailPattern CandidateSource = "fail-pattern"
)

// Candidate 是一筆候選 feature，由 history miner 從歷史失敗訊號彙整而成。
// 只進候選池（candidates.json），是否升級為正式 feature 交由後續 F097 閘門決定。
type Candidate struct {
	Title       string          `json:"title"`       // 候選 feature 標題
	Description string          `json:"description"` // 描述（含失敗細節）
	Source      CandidateSource `json:"source"`      // 來源分類
	Origin      string          `json:"origin"`      // 追溯字串（feature id / round / reason / pattern）
	// ValueScore 是 F097 gate role 給的價值分數（0–1），僅 gate 接受後（accepted-candidates.json）有值。
	ValueScore float64 `json:"value_score,omitempty"`
	// WhyNotHack 是 F097 gate role 強制要求的「為何此 candidate 真有價值、非為看起來有產出而生」論述，
	// 僅 gate 接受後（accepted-candidates.json）有值。
	WhyNotHack string `json:"why_not_hack,omitempty"`
}

// CandidateLearning 是一筆候選 learning，沿用 internal/learning 的 category 白名單。
// 與 Candidate 同樣只進候選池，promotion 進 learnings.json 屬後續 F097。
type CandidateLearning struct {
	Category learning.Category `json:"category"`
	Content  string            `json:"content"`
	Source   CandidateSource   `json:"source"`
	Origin   string            `json:"origin"`
}

// CandidatePool 對應 .4x/candidates.json，是 history miner 的輸出容器。
type CandidatePool struct {
	Version     int                 `json:"version"`     // 固定為 1
	GeneratedAt time.Time           `json:"generatedAt"` // 由 CLI 層 mine 命令設定，protocol 層不取系統時間
	Candidates  []Candidate         `json:"candidates"`
	Learnings   []CandidateLearning `json:"learnings"`
}

// LoadCandidates 讀取 candidates.json；檔案不存在時回傳空 pool（Version=1），不視為錯誤
// （對齊 learning.LoadStore 慣例）。JSON 解析失敗才回傳 error。
func LoadCandidates(path string) (CandidatePool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CandidatePool{Version: 1}, nil
		}
		return CandidatePool{}, fmt.Errorf("read candidates: %w", err)
	}
	var pool CandidatePool
	if err := json.Unmarshal(data, &pool); err != nil {
		return CandidatePool{}, fmt.Errorf("parse candidates: %w", err)
	}
	if pool.Version == 0 {
		pool.Version = 1
	}
	return pool, nil
}

// Save 把 candidate pool 以 indented JSON 寫入指定路徑。
func (p CandidatePool) Save(path string) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal candidates: %w", err)
	}
	return AtomicWriteFile(filepath.Dir(path), filepath.Base(path), ".candidates-*.json", append(data, '\n'), 0o644)
}

// ScanEscalations 走訪所有 feature 的所有 round dir，讀取 escalation.json，
// 對 Needed==true 且 reason 在白名單內的條目產出一筆 candidate。
// best-effort：讀檔失敗、JSON 壞掉、reason 不在白名單都只 slog.Warn 後略過，
// 不 panic、不中斷其他 feature 或其他掃描器。
// features 由呼叫端傳入，避免重複呼叫 ListFeatures。
func ScanEscalations(ws *Workspace, features []feature.Feature) []Candidate {
	var cands []Candidate
	for _, f := range features {
		for _, round := range listRoundNumbers(ws, f.ID) {
			path := filepath.Join(ws.RoundDir(f.ID, round), EscalationFile)
			data, err := os.ReadFile(path)
			if err != nil {
				if !os.IsNotExist(err) {
					slog.Warn("miner: read escalation failed", "feature", f.ID, "round", round, "error", err)
				}
				continue
			}
			var esc Escalation
			if err := json.Unmarshal(data, &esc); err != nil {
				slog.Warn("miner: parse escalation failed", "feature", f.ID, "round", round, "error", err)
				continue
			}
			if !esc.Needed {
				continue
			}
			if !isValidEscalationReason(esc.Reason) {
				slog.Warn("miner: unknown escalation reason", "feature", f.ID, "round", round, "reason", esc.Reason)
				continue
			}
			cands = append(cands, Candidate{
				Title:       escalationTitle(esc.Reason, esc.Detail),
				Description: esc.Detail,
				Source:      SourceEscalation,
				Origin:      fmt.Sprintf("%s round-%d %s", f.ID, round, esc.Reason),
			})
		}
	}
	return cands
}

// ScanStuckFeatures 掃描卡在 needs-attention/abandoned/blocked 的 feature，抽出阻塞原因產出 candidate。
// 阻塞原因優先取 state.json 的 StopReason/StopMessage，皆空時回退讀最新 round 的 escalation.json Detail。
// best-effort：state 讀不到或壞掉都只 slog.Warn 後略過。
// features 由呼叫端傳入，避免重複呼叫 ListFeatures。
func ScanStuckFeatures(ws *Workspace, features []feature.Feature) []Candidate {
	var cands []Candidate
	for _, f := range features {
		state, err := ws.ReadState(f.ID)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("miner: read state failed", "feature", f.ID, "error", err)
			}
			continue
		}
		if !isStuckPhase(state.Phase) {
			continue
		}
		reason := stuckReason(ws, f.ID, state)
		cands = append(cands, Candidate{
			Title:       fmt.Sprintf("Stuck feature (%s): %s", state.Phase, truncate(stuckTitleText(f, reason), 80)),
			Description: reason,
			Source:      SourceStuck,
			Origin:      fmt.Sprintf("%s %s", f.ID, state.Phase),
		})
	}
	return cands
}

// ScanFailPatterns 走訪每個 feature 每個 round 的 review-report.md 與 deep-review-report.md，
// 對 verdict 非 PASS 的報告蒐集其 issue 標題，用 IsSimilarFeature（Jaccard）跨 feature 聚類，
// 統計每群涵蓋的「不同 feature 數」（同一 feature 多輪只算一次）。涵蓋數 >= minOccurrences 的群
// 升級為一筆 candidate，並同時產出一筆 CategoryReview 的 CandidateLearning。
// best-effort：讀檔失敗只 slog.Warn 後略過。
// features 由呼叫端傳入，避免重複呼叫 ListFeatures。
func ScanFailPatterns(ws *Workspace, features []feature.Feature, minOccurrences int) ([]Candidate, []CandidateLearning) {
	type issueItem struct {
		title     string
		featureID string
	}
	var items []issueItem
	for _, f := range features {
		for _, round := range listRoundNumbers(ws, f.ID) {
			for _, reportName := range []string{ReviewReport, DeepReviewReport} {
				path := filepath.Join(ws.RoundDir(f.ID, round), reportName)
				data, err := os.ReadFile(path)
				if err != nil {
					if !os.IsNotExist(err) {
						slog.Warn("miner: read review report failed", "feature", f.ID, "round", round, "report", reportName, "error", err)
					}
					continue
				}
				passed, issues := ParseReportIssues(string(data))
				if passed {
					continue
				}
				for _, title := range issues {
					items = append(items, issueItem{title: title, featureID: f.ID})
				}
			}
		}
	}

	// 跨 feature 聚類：以代表標題 Jaccard 相似度歸群。
	type cluster struct {
		repTitle string
		features map[string]struct{}
	}
	var clusters []*cluster
	for _, it := range items {
		var matched *cluster
		for _, c := range clusters {
			if IsSimilarFeature(it.title, c.repTitle) {
				matched = c
				break
			}
		}
		if matched == nil {
			matched = &cluster{repTitle: it.title, features: map[string]struct{}{}}
			clusters = append(clusters, matched)
		}
		matched.features[it.featureID] = struct{}{}
	}

	var cands []Candidate
	var learnings []CandidateLearning
	for _, c := range clusters {
		if len(c.features) < minOccurrences {
			continue
		}
		ids := sortedKeys(c.features)
		origin := fmt.Sprintf("%d features: %s", len(ids), strings.Join(ids, ", "))
		cands = append(cands, Candidate{
			Title:       fmt.Sprintf("Recurring review issue: %s", truncate(c.repTitle, 80)),
			Description: fmt.Sprintf("此類 review/deep-review FAIL issue（如「%s」）跨 %d 個 feature 反覆出現：%s", c.repTitle, len(ids), strings.Join(ids, ", ")),
			Source:      SourceFailPattern,
			Origin:      origin,
		})
		learnings = append(learnings, CandidateLearning{
			Category: learning.CategoryReview,
			Content:  fmt.Sprintf("「%s」類 issue 跨 %d 個 feature 反覆出現，應升級為 review checklist／模板以提前攔截。", c.repTitle, len(ids)),
			Source:   SourceFailPattern,
			Origin:   origin,
		})
	}
	return cands, learnings
}

// ParseReportIssues 解析 review-report.md / deep-review-report.md，回傳 verdict 是否 PASS
// 及所有 issue 標題。issue 取行首 [CRITICAL] / ### [CRITICAL] / #### [CRITICAL]（WARNING 同規則）
// 之後的文字；verdict 取 "## Verdict" 段第一個非空行是否以 PASS / CONDITIONAL PASS 開頭。
// 在 protocol 層獨立實作，不依賴 cmd/4x 的 parseReviewVerdict。
func ParseReportIssues(report string) (passed bool, issues []string) {
	lines := strings.Split(report, "\n")
	inVerdict := false
	verdictFound := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 去掉行首的 markdown heading 標記後再比對 issue tag。
		body := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		upper := strings.ToUpper(body)
		if title, ok := issueTitle(body, upper, "[CRITICAL]"); ok {
			issues = append(issues, title)
		} else if title, ok := issueTitle(body, upper, "[WARNING]"); ok {
			issues = append(issues, title)
		}

		if strings.HasPrefix(trimmed, "## Verdict") {
			inVerdict = true
			continue
		}
		if inVerdict && !verdictFound && trimmed != "" {
			clean := strings.ToUpper(strings.Trim(trimmed, "*_"))
			if strings.HasPrefix(clean, "PASS") || strings.HasPrefix(clean, "CONDITIONAL PASS") {
				passed = true
			}
			verdictFound = true
		}
	}
	return passed, issues
}

// DedupeCandidates 過濾候選：保留的 candidate 必須與每個 existingFeatures（Name+" "+Description）、
// 每個 existingCands（前一份 candidates.json 既有 candidate）、及本批次已保留 candidate 都不相似
// （沿用 IsSimilarFeature Jaccard 去重）。比對對象是 candidate 的 Title+" "+Description。
// 回傳順序維持輸入順序（對齊 DedupeDiscovered 行為）。
func DedupeCandidates(cands []Candidate, existingFeatures []feature.Feature, existingCands []Candidate) []Candidate {
	var kept []Candidate
	for _, c := range cands {
		ctext := c.Title + " " + c.Description

		dup := false
		for _, e := range existingFeatures {
			if IsSimilarFeature(ctext, e.Name+" "+e.Description) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		for _, e := range existingCands {
			if IsSimilarFeature(ctext, e.Title+" "+e.Description) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		for _, k := range kept {
			if IsSimilarFeature(ctext, k.Title+" "+k.Description) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

// listRoundNumbers 列出 feature 的所有 round dir 並回傳排序後的 round 編號。
// rounds dir 不存在或讀取失敗時回傳 nil（best-effort，不中斷掃描）。
func listRoundNumbers(ws *Workspace, featureID string) []int {
	entries, err := os.ReadDir(filepath.Join(ws.FeatureDir(featureID), RoundsDir))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("miner: read rounds dir failed", "feature", featureID, "error", err)
		}
		return nil
	}
	var rounds []int
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "round-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "round-"))
		if err != nil || n <= 0 {
			continue
		}
		rounds = append(rounds, n)
	}
	sort.Ints(rounds)
	return rounds
}

// isValidEscalationReason 檢查 escalation reason 是否在白名單內。
func isValidEscalationReason(reason string) bool {
	switch reason {
	case "spec-mismatch", "criteria-wrong", "blocker", "scope-change":
		return true
	default:
		return false
	}
}

// escalationTitle 把 reason 與 detail 組成一行簡短的 candidate 標題。
func escalationTitle(reason, detail string) string {
	phrase := map[string]string{
		"spec-mismatch":  "Spec mismatch",
		"criteria-wrong": "Acceptance criteria wrong",
		"blocker":        "Blocker",
		"scope-change":   "Scope change",
	}[reason]
	if phrase == "" {
		phrase = reason
	}
	if d := strings.TrimSpace(detail); d != "" {
		return fmt.Sprintf("%s: %s", phrase, truncate(firstLine(d), 80))
	}
	return phrase
}

// isStuckPhase 判斷 phase 是否為「卡住」狀態（needs-attention/abandoned/blocked）。
func isStuckPhase(phase Phase) bool {
	switch phase {
	case PhaseNeedsAttention, PhaseAbandoned, PhaseBlocked:
		return true
	default:
		return false
	}
}

// stuckReason 抽出 stuck feature 的阻塞原因：優先 StopReason/StopMessage，
// 皆空時回退讀最新 round 的 escalation.json Detail；仍無則回傳通用描述。
func stuckReason(ws *Workspace, featureID string, state State) string {
	var parts []string
	if r := strings.TrimSpace(state.StopReason); r != "" {
		parts = append(parts, r)
	}
	if m := strings.TrimSpace(state.StopMessage); m != "" {
		parts = append(parts, m)
	}
	if len(parts) > 0 {
		return strings.Join(parts, ": ")
	}
	if detail := latestEscalationDetail(ws, featureID); detail != "" {
		return detail
	}
	return fmt.Sprintf("feature stuck in %s with no recorded reason", state.Phase)
}

// latestEscalationDetail 讀取 feature 最新 round 的 escalation.json，回傳其 Detail。
// 讀不到或壞掉時回空字串（best-effort）。
func latestEscalationDetail(ws *Workspace, featureID string) string {
	rounds := listRoundNumbers(ws, featureID)
	for i := len(rounds) - 1; i >= 0; i-- {
		path := filepath.Join(ws.RoundDir(featureID, rounds[i]), EscalationFile)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var esc Escalation
		if err := json.Unmarshal(data, &esc); err != nil {
			continue
		}
		if d := strings.TrimSpace(esc.Detail); d != "" {
			return d
		}
	}
	return ""
}

// stuckTitleText 取 stuck candidate 標題用的描述文字：reason 非通用時用 reason，否則退回 feature 名稱。
func stuckTitleText(f feature.Feature, reason string) string {
	if strings.HasPrefix(reason, "feature stuck in ") {
		if f.Name != "" {
			return f.Name
		}
		return f.ID
	}
	return firstLine(reason)
}

// issueTitle 若 body（已去 heading 標記）以 marker 開頭，回傳 marker 之後 trim 的標題與 true。
func issueTitle(body, upperBody, marker string) (string, bool) {
	if !strings.HasPrefix(upperBody, marker) {
		return "", false
	}
	title := strings.TrimSpace(body[len(marker):])
	return title, true
}

// firstLine 回傳字串第一行（去頭尾空白）。
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}

// truncate 將字串截斷至 max 個 rune，超過時附 "..."。
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

// sortedKeys 回傳 set 的排序後 key 切片。
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
