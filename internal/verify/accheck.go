package verify

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// RunACChecks 對每條有 ac_checks 的 AC 依序執行其命令（沿用 runGroup 的
// 「前一條失敗即 skip 後續」語意），回傳每條 AC 的 ACEvidence：
// Passed = 所有非 skipped check 的 exit code 皆 0（呼叫 CommandsPassed）；
// Checks = 每條 check 的 VerifyCommand（含實際 exit code / summary）；
// Evidence = 機器自動生成的說明行（如 "$ <cmd> → exit 0 (12ms)"）。
// 為輸出穩定可測，AC ID 需先排序後執行。
func RunACChecks(ctx context.Context, acChecks map[string][]string, workDir string) []protocol.ACEvidence {
	acIDs := make([]string, 0, len(acChecks))
	for id := range acChecks {
		acIDs = append(acIDs, id)
	}
	sort.Strings(acIDs)

	results := make([]protocol.ACEvidence, 0, len(acIDs))
	for _, id := range acIDs {
		// 復用 runGroup 的依序執行 + 首失敗後標 Skipped 語意，group 名以 AC ID 標記。
		gr := runGroup(ctx, Group{Name: id, Commands: acChecks[id]}, workDir)
		checks := gr.commands

		evidence := make([]string, 0, len(checks))
		for _, c := range checks {
			if c.Skipped {
				evidence = append(evidence, fmt.Sprintf("$ %s → skipped (prior check failed)", c.Command))
				continue
			}
			evidence = append(evidence, fmt.Sprintf("$ %s → exit %d (%dms)", c.Command, c.ExitCode, c.DurationMs))
		}

		results = append(results, protocol.ACEvidence{
			ID:       id,
			Passed:   CommandsPassed(checks),
			Evidence: evidence,
			Checks:   checks,
		})
	}
	return results
}

var (
	// lintNoOp 攔 `true` / `:` 這類永遠成功、什麼都沒驗證的命令。
	lintNoOp = regexp.MustCompile(`^(true|:)\s*$`)
	// lintEchoOnly 攔以 echo/printf 開頭的命令（在無 pipe 時，等於印字串而不跑受測程式）。
	lintEchoOnly = regexp.MustCompile(`^(echo|printf)\b`)
	// lintGrep 攔以 grep 家族開頭的命令（在無 pipe 時，可能只是掃 source 而不執行）。
	lintGrep = regexp.MustCompile(`^(grep|egrep|fgrep|rg|ag)\b`)
	// lintGoSource 判斷命令是否引用 Go source 路徑樣式（.go 檔或 internal/cmd/pkg 目錄）。
	lintGoSource = regexp.MustCompile(`(\.go(\b|:)|(^|\s)(internal|cmd|pkg)/)`)
)

// LintACCheck 檢查單一 ac_check 命令是否為「假驗證」pattern。
// 回傳非空字串表示違規（字串為原因）；回傳 "" 表示合法。
//
// 判定由上而下、命中即回：含 allowlist token `4x-lint:allow` 一律先放行；
// 其餘攔空命令、no-op（true/:）、純 echo/printf（無 pipe）、以及只 grep Go source
// （無 pipe）等明確假驗證 pattern。
//
// 設計意圖：保守——寧可漏放合法造假也不誤擋合法驗證。管線化的 grep
// （`go test ... | grep`）因命令非以 grep 開頭/含 pipe 而放行；grep 產出物
// （`docs/*.md`、`verify.json`、任意 `out.txt`）因非 Go-source 路徑而放行。
func LintACCheck(cmd string) string {
	trimmed := strings.TrimSpace(cmd)

	// allowlist：命令含字面 token 4x-lint:allow → 無條件放行。
	if strings.Contains(trimmed, "4x-lint:allow") {
		return ""
	}
	if trimmed == "" {
		return "empty command"
	}
	if lintNoOp.MatchString(trimmed) {
		return "no-op command always passes without verifying anything"
	}
	hasPipe := strings.Contains(trimmed, "|")
	if lintEchoOnly.MatchString(trimmed) && !hasPipe {
		return "echo/printf-only command does not execute the program under test"
	}
	if lintGrep.MatchString(trimmed) && !hasPipe && lintGoSource.MatchString(trimmed) {
		return "greps source code without executing it (fake verification); pipe from a real command or add 4x-lint:allow if this checks generated output"
	}
	return ""
}
