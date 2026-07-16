package prompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

const sampleReviewReport = `# Review Report — Round 2
## Summary
FAIL — 1 critical

## Checklist
| Item | Status | Notes |
|---|---|---|
| scope | PASS | 只改 allowed 檔案 |
| tests | FAIL | 缺 fallback 案例 |

## Issues
### [CRITICAL] missing-fallback — internal/prompt/delta.go
extractTestDelta 缺 heading 時未 fallback 全文。

## Verdict
FAIL
`

const sampleTestReport = `# Test Report — Round 2
## Summary
FAIL — 2/4 criteria met

## Results
| # | Criterion | Status | Evidence |
|---|---|---|---|
| AC-1 | 正常抽取 | PASS | go test 通過 |
| AC-2 | verbatim | PASS | strings.Contains 通過 |
| AC-3 | fallback | FAIL | 缺 heading 未 fallback |
| AC-4 | test delta | SKIP | 未實作 |

## Verdict
FAIL
`

func TestExtractReviewDelta(t *testing.T) {
	delta, ok := extractReviewDelta(sampleReviewReport)
	if !ok {
		t.Fatal("expected extracted=true")
	}
	// 保留 Issues + Verdict
	if !strings.Contains(delta, "## Issues") || !strings.Contains(delta, "## Verdict") {
		t.Errorf("delta 應含 Issues 與 Verdict heading，得到:\n%s", delta)
	}
	// verbatim：Issue 標題與描述逐字保留
	if !strings.Contains(delta, "### [CRITICAL] missing-fallback — internal/prompt/delta.go") {
		t.Errorf("delta 應 verbatim 保留 Issue 標題，得到:\n%s", delta)
	}
	if !strings.Contains(delta, "extractTestDelta 缺 heading 時未 fallback 全文。") {
		t.Errorf("delta 應 verbatim 保留 Issue 描述，得到:\n%s", delta)
	}
	// 丟棄 Summary / Checklist
	if strings.Contains(delta, "## Summary") || strings.Contains(delta, "## Checklist") {
		t.Errorf("delta 不應含 Summary/Checklist，得到:\n%s", delta)
	}
	if strings.Contains(delta, "只改 allowed 檔案") {
		t.Errorf("delta 不應含 Checklist 內文，得到:\n%s", delta)
	}
}

func TestExtractReviewDeltaFallback(t *testing.T) {
	// 缺 ## Issues
	noIssues := "# Review\n## Summary\nPASS\n## Verdict\nPASS\n"
	if d, ok := extractReviewDelta(noIssues); ok || d != noIssues {
		t.Errorf("缺 Issues 應 fallback 全文且 extracted=false，得到 ok=%v", ok)
	}
	// 缺 ## Verdict
	noVerdict := "# Review\n## Issues\nNone\n"
	if d, ok := extractReviewDelta(noVerdict); ok || d != noVerdict {
		t.Errorf("缺 Verdict 應 fallback 全文且 extracted=false，得到 ok=%v", ok)
	}
}

func TestExtractTestDelta(t *testing.T) {
	delta, ok := extractTestDelta(sampleTestReport)
	if !ok {
		t.Fatal("expected extracted=true")
	}
	// 保留表頭與分隔列
	if !strings.Contains(delta, "| # | Criterion | Status | Evidence |") {
		t.Errorf("delta 應保留表頭列，得到:\n%s", delta)
	}
	if !strings.Contains(delta, "|---|---|---|---|") {
		t.Errorf("delta 應保留分隔列，得到:\n%s", delta)
	}
	// 保留 FAIL / SKIP 列，verbatim
	if !strings.Contains(delta, "| AC-3 | fallback | FAIL | 缺 heading 未 fallback |") {
		t.Errorf("delta 應保留 FAIL 列，得到:\n%s", delta)
	}
	if !strings.Contains(delta, "| AC-4 | test delta | SKIP | 未實作 |") {
		t.Errorf("delta 應保留 SKIP 列，得到:\n%s", delta)
	}
	// 丟棄 PASS 列與 Summary
	if strings.Contains(delta, "AC-1") || strings.Contains(delta, "AC-2") {
		t.Errorf("delta 不應含 PASS 列，得到:\n%s", delta)
	}
	if strings.Contains(delta, "## Summary") {
		t.Errorf("delta 不應含 Summary，得到:\n%s", delta)
	}
	// 保留 Verdict
	if !strings.Contains(delta, "## Verdict") {
		t.Errorf("delta 應含 Verdict，得到:\n%s", delta)
	}
}

func TestExtractTestDeltaFallback(t *testing.T) {
	// 缺 ## Results
	noResults := "# Test\n## Summary\nPASS\n## Verdict\nPASS\n"
	if d, ok := extractTestDelta(noResults); ok || d != noResults {
		t.Errorf("缺 Results 應 fallback 全文且 extracted=false，得到 ok=%v", ok)
	}
	// 缺 ## Verdict
	noVerdict := "# Test\n## Results\n| # | Criterion | Status | Evidence |\n"
	if d, ok := extractTestDelta(noVerdict); ok || d != noVerdict {
		t.Errorf("缺 Verdict 應 fallback 全文且 extracted=false，得到 ok=%v", ok)
	}
}

// TestDeltaMixedReviewPassTestFail 覆蓋 review 全 PASS + test 有 FAIL 的混合回歸情境。
func TestDeltaMixedReviewPassTestFail(t *testing.T) {
	reviewPass := `# Review Report — Round 2
## Summary
PASS

## Checklist
| Item | Status | Notes |
|---|---|---|
| scope | PASS | ok |

## Issues
None

## Verdict
PASS
`
	rDelta, ok := extractReviewDelta(reviewPass)
	if !ok {
		t.Fatal("review delta 應 extracted=true")
	}
	if strings.Contains(rDelta, "## Checklist") || strings.Contains(rDelta, "## Summary") {
		t.Errorf("全 PASS review delta 仍不應含 Checklist/Summary，得到:\n%s", rDelta)
	}
	if !strings.Contains(rDelta, "None") || !strings.Contains(rDelta, "PASS") {
		t.Errorf("review delta 應含 Issues(None) 與 Verdict(PASS)，得到:\n%s", rDelta)
	}

	tDelta, ok := extractTestDelta(sampleTestReport)
	if !ok {
		t.Fatal("test delta 應 extracted=true")
	}
	if strings.Contains(tDelta, "## Summary") {
		t.Errorf("test delta 不應含 Summary，得到:\n%s", tDelta)
	}
	if !strings.Contains(tDelta, "FAIL") || !strings.Contains(tDelta, "## Verdict") {
		t.Errorf("test delta 應含 FAIL 列與 Verdict，得到:\n%s", tDelta)
	}
}

const sampleDesignReviewReport = `# Design Review Report — Round 0
## Summary
FAIL

## Architecture Risks
- ChangeBalance 與 marker Rollback 的順序需明確定義。

## Overengineering
- 不需為單一平台預先抽 registry。

## Missing Requirements
- AC-13 的 grep 'float64' 抓不到 .Float64()。

## Verified References
| Claim | file:line | Verified? |
|---|---|---|
| Loader.Get 存在 | loader.go:42 | YES |

## Verdict
FAIL

## Escalation Verdict
disagree
`

func TestExtractDesignReviewDelta(t *testing.T) {
	delta, ok := extractDesignReviewDelta(sampleDesignReviewReport)
	if !ok {
		t.Fatal("expected extracted=true on a FAIL report")
	}
	// 保留三個問題段落 + Verdict，verbatim
	for _, want := range []string{
		"## Architecture Risks",
		"ChangeBalance 與 marker Rollback 的順序需明確定義。",
		"## Overengineering",
		"## Missing Requirements",
		"AC-13 的 grep 'float64' 抓不到 .Float64()。",
		"## Verdict",
	} {
		if !strings.Contains(delta, want) {
			t.Errorf("delta 應含 %q，得到:\n%s", want, delta)
		}
	}
	// 丟棄 Summary / Verified References / Escalation Verdict
	for _, unwanted := range []string{"## Summary", "## Verified References", "loader.go:42", "## Escalation Verdict", "disagree"} {
		if strings.Contains(delta, unwanted) {
			t.Errorf("delta 不應含 %q，得到:\n%s", unwanted, delta)
		}
	}
}

func TestExtractDesignReviewDeltaPassSkips(t *testing.T) {
	pass := "# Design Review Report — Round 0\n## Missing Requirements\n- none\n## Verdict\nPASS\n"
	if d, ok := extractDesignReviewDelta(pass); ok || d != "" {
		t.Errorf("PASS 報告應回 (\"\", false)，得到 ok=%v d=%q", ok, d)
	}
	// "no failing issues" 這類句子不得被誤判為 FAIL（首行仍是 PASS）
	falsePos := "# DR\n## Missing Requirements\n- x\n## Verdict\nPASS — no failing issues\n"
	if _, ok := extractDesignReviewDelta(falsePos); ok {
		t.Errorf("PASS 首行含 'failing' 不應誤判為 FAIL")
	}
}

func TestExtractDesignReviewDeltaMalformedSkips(t *testing.T) {
	// FAIL 但無任何問題段落 → 無 delta 可注入，回 false 走一般路徑
	noSections := "# DR\n## Summary\nFAIL\n## Verdict\nFAIL\n"
	if d, ok := extractDesignReviewDelta(noSections); ok || d != "" {
		t.Errorf("無問題段落應回 (\"\", false)，得到 ok=%v", ok)
	}
	// 無 Verdict 段落 → 回 false
	noVerdict := "# DR\n## Architecture Risks\n- x\n"
	if _, ok := extractDesignReviewDelta(noVerdict); ok {
		t.Errorf("缺 Verdict 應回 false")
	}
}

// TestDesignerTemplateRevisionBlock 驗證 DesignRevision=true 時 template 渲染 REVISION 區塊與注入前版成品，
// false 時不渲染（避免污染首次分析）。
func TestDesignerTemplateRevisionBlock(t *testing.T) {
	tmpl, err := LoadRoleTemplate("", protocol.RoleDesigner)
	if err != nil {
		t.Fatalf("load designer template: %v", err)
	}
	render := func(d Data) string {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, d); err != nil {
			t.Fatalf("execute designer template: %v", err)
		}
		return buf.String()
	}

	rev := render(Data{
		DesignRevision:   true,
		PrevDesignReview: "## Missing Requirements\n- FIX-THIS-ITEM",
		PrevTaskBrief:    "PREV-BRIEF-BODY",
		PrevCriteria:     "PREV-AC-BODY",
		PrevTestStrategy: "PREV-TS-BODY",
	})
	for _, want := range []string{"REVISION ROUND", "FIX-THIS-ITEM", "PREV-BRIEF-BODY", "PREV-AC-BODY", "PREV-TS-BODY", "Use the Edit tool to modify the existing files"} {
		if !strings.Contains(rev, want) {
			t.Errorf("revision render 應含 %q", want)
		}
	}

	fresh := render(Data{})
	if strings.Contains(fresh, "REVISION ROUND") {
		t.Errorf("非 revision render 不應含 REVISION 區塊")
	}
	if strings.Contains(fresh, "{{") {
		t.Errorf("render 不應外洩未 render 的 {{")
	}
}

// TestReviewerTemplateContractComment 驗證 F142 template comment 未破壞 reviewer.md.tmpl render：
// 輸出仍含 `## Issues` 與 `## Verdict` heading，且不外洩未 render 的 `{{`。
func TestReviewerTemplateContractComment(t *testing.T) {
	tmpl, err := LoadRoleTemplate("", protocol.RoleReviewer)
	if err != nil {
		t.Fatalf("load reviewer template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, Data{Round: 2}); err != nil {
		t.Fatalf("execute reviewer template: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "## Issues") || !strings.Contains(out, "## Verdict") {
		t.Errorf("render 輸出應仍含 Issues 與 Verdict heading")
	}
	if strings.Contains(out, "{{") {
		t.Errorf("render 輸出不應外洩未 render 的 {{")
	}
}

// TestTesterTemplateContractComment 同上，驗證 tester.md.tmpl 的契約 comment 未破壞 render。
func TestTesterTemplateContractComment(t *testing.T) {
	tmpl, err := LoadRoleTemplate("", protocol.RoleTester)
	if err != nil {
		t.Fatalf("load tester template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, Data{Round: 2}); err != nil {
		t.Fatalf("execute tester template: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "## Results") || !strings.Contains(out, "## Verdict") {
		t.Errorf("render 輸出應仍含 Results 與 Verdict heading")
	}
	if strings.Contains(out, "{{") {
		t.Errorf("render 輸出不應外洩未 render 的 {{")
	}
}
