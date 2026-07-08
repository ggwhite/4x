package prompt

import "strings"

// extractReviewDelta 從 review-report.md 內容抽出 amending 輪 coder 真正需要的 delta：
// `## Issues` 與 `## Verdict` 兩段（verbatim 串接），丟棄 `## Summary`、`## Checklist`。
// 目的是壓低 retry 輪注入 coder prompt 的冗餘 context（已 PASS 的 checklist、格式樣板等）。
//
// 兩段皆逐字保留，不改寫、不摘要。任一 heading 找不到時回傳 (content, false)——
// fallback 全文，寧可多花 token 也不漏資訊。成功抽取回傳 (delta, true)。
//
// heading 比對用 strings.TrimSpace 後 HasPrefix，與 orchestrator.ParseReviewVerdict
// 的比對風格一致（容許行首縮排與 trailing 內容）。
func extractReviewDelta(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	issues := extractSection(lines, "## Issues")
	verdict := extractSection(lines, "## Verdict")
	if issues == nil || verdict == nil {
		return content, false
	}
	return strings.Join(issues, "\n") + "\n" + strings.Join(verdict, "\n"), true
}

// extractTestDelta 從 test-report.md 內容抽出 delta：`## Results` 表格中 Status 欄為
// FAIL 或 SKIP 的資料列（保留表頭列與 `|---|` 分隔列以維持可讀性），加上 `## Verdict` 段落，
// verbatim 回傳。丟棄 PASS 列與 `## Summary`。
//
// Status 欄為表格以 `|` 切欄後的第 3 欄（`| # | Criterion | Status | Evidence |`
// → index 3）trim 後大寫比對。任一 heading 找不到時回傳 (content, false) fallback 全文。
func extractTestDelta(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	results := extractSection(lines, "## Results")
	verdict := extractSection(lines, "## Verdict")
	if results == nil || verdict == nil {
		return content, false
	}
	var kept []string
	for _, line := range results {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			// heading、空行、表格外的 prose 一律 verbatim 保留。
			kept = append(kept, line)
			continue
		}
		if isTableSeparator(trimmed) {
			kept = append(kept, line)
			continue
		}
		cols := strings.Split(trimmed, "|")
		if len(cols) < 4 {
			// 欄數不符預期的表格列保守保留，避免漏資訊。
			kept = append(kept, line)
			continue
		}
		status := strings.ToUpper(strings.TrimSpace(cols[3]))
		if status == "STATUS" || status == "FAIL" || status == "SKIP" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n") + "\n" + strings.Join(verdict, "\n"), true
}

// extractSection 回傳 lines 中自 heading 行（含）起、到下一個 `## ` heading 之前
// （或 EOF）的所有行；找不到 heading 時回傳 nil。heading 比對容許行首縮排與 trailing 內容。
func extractSection(lines []string, heading string) []string {
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), heading) {
			start = i
			break
		}
	}
	if start == -1 {
		return nil
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	return lines[start:end]
}

// isTableSeparator 判斷一列是否為 Markdown 表格分隔列（僅由 `|`、`-`、`:`、空白組成且至少含一個 `-`）。
func isTableSeparator(s string) bool {
	hasDash := false
	for _, r := range s {
		switch r {
		case '|', ':', ' ', '\t':
		case '-':
			hasDash = true
		default:
			return false
		}
	}
	return hasDash
}
