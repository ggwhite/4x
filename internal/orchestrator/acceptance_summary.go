package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// GenerateAcceptanceSummary 彙整指定 round 的 verify.json、review-report.md、deep-review-report.md，
// 產出 acceptance-summary.md 的內容，供 Acceptor 讀取取代重複讀取原始報告全文。任何一段沒有資料
// 就跳過該段；完全沒有資料可彙整時回傳空字串，呼叫端應 fallback 不寫檔，讓 acceptor template
// 直接讀原始報告。
func GenerateAcceptanceSummary(ws *protocol.Workspace, featureID string, round int) string {
	roundDir := ws.RoundDir(featureID, round)

	var b strings.Builder
	wrote := false

	if table := acStatusTable(ws.FeatureDir(featureID), roundDir); table != "" {
		b.WriteString("## AC Status\n\n")
		b.WriteString(table)
		b.WriteString("\n")
		wrote = true
	}

	if table := reportVerdictTable(roundDir); table != "" {
		b.WriteString("## Round Reports\n\n")
		b.WriteString(table)
		b.WriteString("\n")
		wrote = true
	}

	if items := unverifiableItems(roundDir); items != "" {
		b.WriteString("## Unverifiable Items\n\n")
		b.WriteString(items)
		b.WriteString("\n")
		wrote = true
	}

	if cmds := verificationCommandsList(roundDir); cmds != "" {
		b.WriteString("## Verification Commands Run\n\n")
		b.WriteString(cmds)
		wrote = true
	}

	if !wrote {
		return ""
	}
	return "# Acceptance Summary\n\n" + b.String()
}

// acCriteriaRowRe 比對 acceptance-criteria.md 表格列（見 templates/designer.md.tmpl 的
// `| AC-1 | ... | ... | unit-test |` 格式），取出 AC ID 與 Criterion 欄位文字。
var acCriteriaRowRe = regexp.MustCompile(`^\|\s*(AC-[A-Za-z0-9_-]+)\s*\|\s*([^|]*)\|`)

// acCriteriaText 解析 acceptance-criteria.md，回傳 AC ID → Criterion 描述的對照表。
// 檔案不存在（如 profile 跳過 Designer）時回傳空 map，不視為錯誤。
func acCriteriaText(featureDir string) map[string]string {
	data, err := os.ReadFile(filepath.Join(featureDir, protocol.Criteria))
	if err != nil {
		return nil
	}
	out := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		m := acCriteriaRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

// acStatusTable 讀取 round 的 verify.json，逐一 AC 產出狀態列表。verify.json 不存在或無
// ACResults 時回傳空字串。
func acStatusTable(featureDir, roundDir string) string {
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.VerifyFile))
	if err != nil {
		return ""
	}
	var ve protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ve); err != nil || len(ve.ACResults) == 0 {
		return ""
	}
	criteria := acCriteriaText(featureDir)

	var b strings.Builder
	b.WriteString("| # | Criterion | Status | Evidence Source |\n|---|---|---|---|\n")
	for _, ac := range ve.ACResults {
		status := "FAIL"
		if ac.Passed {
			status = "PASS"
		}
		criterion := criteria[ac.ID]
		if criterion == "" {
			criterion = "—"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", ac.ID, criterion, status, protocol.VerifyFile)
	}
	return b.String()
}

// reportVerdictTable 彙整 review-report.md / deep-review-report.md 的 verdict 與 issue 計數。
// 兩份報告皆不存在時回傳空字串。
func reportVerdictTable(roundDir string) string {
	entries := []struct{ file, label string }{
		{protocol.ReviewReport, protocol.ReviewReport},
		{protocol.DeepReviewReport, protocol.DeepReviewReport},
	}

	var b strings.Builder
	b.WriteString("| Report | Verdict | Critical | Warning |\n|---|---|---|---|\n")
	found := false
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(roundDir, e.file))
		if err != nil {
			continue
		}
		res := ParseReviewVerdict(string(data))
		verdict := "FAIL"
		if res.Passed {
			verdict = "PASS"
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d |\n", e.label, verdict, res.CriticalCount, res.WarningCount)
		found = true
	}
	if !found {
		return ""
	}
	return b.String()
}

// unverifiableItems 掃描 review-report.md / deep-review-report.md 中標記為 `[UNVERIFIABLE]`
// 的項目（見 templates/reviewer.md.tmpl、deep-reviewer.md.tmpl 的 UNVERIFIABLE_FROM_DIFF
// 指引），逐行列出供 Acceptor 快速定位需要 runtime 驗證的項目。無標記時回傳空字串。
func unverifiableItems(roundDir string) string {
	var b strings.Builder
	for _, file := range []string{protocol.ReviewReport, protocol.DeepReviewReport} {
		data, err := os.ReadFile(filepath.Join(roundDir, file))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, "[UNVERIFIABLE]") {
				continue
			}
			fmt.Fprintf(&b, "- [%s] %s\n", file, strings.TrimLeft(trimmed, "#*- "))
		}
	}
	return b.String()
}

// verificationCommandsList 讀取 round 的 verify.json，逐條列出跑過的 verify command 及結果。
// verify.json 不存在或無 Commands 時回傳空字串。
func verificationCommandsList(roundDir string) string {
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.VerifyFile))
	if err != nil {
		return ""
	}
	var ve protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ve); err != nil || len(ve.Commands) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range ve.Commands {
		status := fmt.Sprintf("exit %d", c.ExitCode)
		switch {
		case c.Skipped:
			status = "SKIPPED"
		case c.ExpectedExitCode != nil && c.ExitCode == *c.ExpectedExitCode:
			status = "PASS"
		case c.ExpectedExitCode == nil && c.ExitCode == 0:
			status = "PASS"
		}
		summary := c.Summary
		if summary != "" {
			summary = " — " + summary
		}
		fmt.Fprintf(&b, "- %s: %s%s\n", c.Command, status, summary)
	}
	return b.String()
}
