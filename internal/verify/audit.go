package verify

import (
	"regexp"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

var (
	// auditGoTestRun 匹配 go test -v 的 `=== RUN` 行（可能有縮排）。
	auditGoTestRun = regexp.MustCompile(`^\s*=== RUN\s`)
	// auditResultTopLevel 匹配頂層的 `--- PASS/FAIL/SKIP: ` 結果行。
	auditResultTopLevel = regexp.MustCompile(`^--- (PASS|FAIL|SKIP): `)
	// auditResultIndented 匹配縮排的 `--- PASS/FAIL/SKIP: ` 結果行（subtest）。
	auditResultIndented = regexp.MustCompile(`^[ \t]+--- (PASS|FAIL|SKIP): `)
)

// ComputeAudit 從命令的完整 combined output 算出可稽核的量（precheck 的對偶）。
// precheck 是靜態閘門，只擋得住能靜態判定的失效樣態；ComputeAudit 則在 verify 實跑時
// 對每條命令留下四個數字，讓 precheck 漏判的失效仍在 verify.json 留下痕跡。
// 空字串輸入回傳四項皆 0。
func ComputeAudit(output string) protocol.CommandAudit {
	var audit protocol.CommandAudit
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			audit.OutputLines++
		}
		if auditGoTestRun.MatchString(line) {
			audit.GoTestsRun++
		}
		if auditResultTopLevel.MatchString(line) {
			audit.PassLinesTopLevel++
		}
		if auditResultIndented.MatchString(line) {
			audit.PassLinesIndented++
		}
	}
	return audit
}
