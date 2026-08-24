package verify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// Severity 標記 precheck finding 的嚴重度。
type Severity string

const (
	// SeverityError 表示該命令必須修正，會擋下 designing 出口閘門。
	SeverityError Severity = "error"
	// SeverityWarn 表示可疑但不擋。目前無規則使用，保留供後續擴充。
	SeverityWarn Severity = "warn"
)

// precheck 規則 ID。每個 Finding.Rule 必為其中之一，訊息與文件皆以這些字面值為準。
const (
	ruleUnparseable        = "unparseable"
	ruleUnknownExecutable  = "unknown-executable"
	ruleMissingPath        = "missing-path"
	ruleExitCodeSwallowed  = "exit-code-swallowed"
	ruleUnanchoredPassGrep = "unanchored-pass-grep"
)

// lintAllowToken 是整條命令跳過全部規則的逃生口，與 LintACCheck 共用同一 token。
const lintAllowToken = "4x-lint:allow"

// Finding 是單一 precheck 違規。
type Finding struct {
	// Source 標記命令來源，值為 "verify_commands" / "verify_groups[<name>]" / "ac_checks[<AC ID>]"。
	Source string
	// Command 是命令原文（未經任何正規化），讓 Designer 能直接定位要改哪一條。
	Command string
	// Rule 是規則 ID，見本檔的 rule* 常量。
	Rule string
	// Reason 是人讀的失敗原因，含修正方向。
	Reason string
	// Severity 目前一律為 SeverityError。
	Severity Severity
}

// Error 回傳可直接寫進 guard error 清單的單行訊息，含命令原文與規則 ID。
func (f Finding) Error() string {
	return fmt.Sprintf("%s: command %q rejected by %s: %s", f.Source, f.Command, f.Rule, f.Reason)
}

var (
	// precheckRedirect 匹配 I/O 重導向片段（`> file`、`>> file`、`< file`、`2>&1`、`&> file`）。
	// 切段前先移除：splitShellSegments 遇到 `2>&1` 的 `&` 會 flush，切出 `1` 這種假段。
	// 目標檔名以 [^\s;&|<>]* 匹配，刻意停在 shell 控制字元，避免吞掉後面的 `;` / `&&` 分隔符。
	precheckRedirect = regexp.MustCompile(`&?[0-9]*(>>|>|<)&?\s*[^\s;&|<>]*`)
	// precheckQuoted 抓取單引號或雙引號包住的字串。非 quote-aware，只取最近的配對。
	precheckQuoted = regexp.MustCompile(`'([^']*)'|"([^"]*)"`)
	// precheckAssign 匹配段首的 `VAR=` 環境變數指派前綴。
	precheckAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// shellBuiltins 是不需 exec.LookPath 查找的 shell 內建命令。
var shellBuiltins = map[string]bool{
	"cd": true, "echo": true, "printf": true, "test": true, "true": true,
	"false": true, ":": true, "set": true, "export": true, "unset": true,
	"exit": true, "source": true, ".": true, "eval": true, "read": true,
	"shift": true, "wait": true, "trap": true, "local": true, "return": true,
	"[": true,
}

// grepTools 是 grep 家族命令，供 unanchored-pass-grep 規則辨識。
var grepTools = map[string]bool{"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true}

// goTestMarkers 是 go test 結果行的前綴。grep pattern 含其一即套用錨定規則。
var goTestMarkers = []string{"--- PASS:", "--- FAIL:", "--- SKIP:"}

// PrecheckCommand 對單一 verify 命令跑全部靜態規則，回傳所有違規 finding（合法命令回長度 0）。
// source 標記命令來源（填入 Finding.Source），workDir 是相對路徑的解析基準。
//
// 全程不執行受檢命令——唯一的外部呼叫是 exec.LookPath 與 os.Stat，不實跑、不看 exit code、
// 不判斷命令在執行期會不會通過。命令含字面 token `4x-lint:allow` 時整條跳過全部規則。
//
// 五條規則：unparseable（空命令 / 引號不成對 / 以連接符結尾）、unknown-executable
// （段首命令不在 PATH）、missing-path（段首 `VAR=路徑` 指派、`cd` 目標、以 `./` 或 `../`
// 開頭的參數所指路徑不存在）、exit-code-swallowed（上游命令的 exit code 被 `;` 或
// 管線到唯讀過濾工具丟棄）、unanchored-pass-grep（grep 的 go test 結果 pattern 未錨行首
// 或未帶尾隨空白）。
func PrecheckCommand(source, cmd, workDir string) []Finding {
	if strings.Contains(cmd, lintAllowToken) {
		return nil
	}
	newFinding := func(rule, reason string) Finding {
		return Finding{Source: source, Command: cmd, Rule: rule, Reason: reason, Severity: SeverityError}
	}

	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return []Finding{newFinding(ruleUnparseable, "command is empty")}
	}
	if strings.Count(trimmed, "'")%2 != 0 || strings.Count(trimmed, `"`)%2 != 0 {
		return []Finding{newFinding(ruleUnparseable, "unbalanced quotes")}
	}

	raw := splitShellSegments(precheckRedirect.ReplaceAllString(trimmed, " "))
	if len(raw) > 1 && strings.TrimSpace(raw[len(raw)-1].text) == "" {
		return []Finding{newFinding(ruleUnparseable, "command ends with a dangling shell connector (&&, ||, |, ; or &)")}
	}

	segs := make([]shellSegment, 0, len(raw))
	for _, seg := range raw {
		if strings.TrimSpace(seg.text) != "" {
			segs = append(segs, seg)
		}
	}
	if len(segs) == 0 {
		return []Finding{newFinding(ruleUnparseable, "command has no executable segment")}
	}

	var findings []Finding
	checked := make(map[string]bool)
	for _, seg := range segs {
		findings = append(findings, checkSegment(seg.text, workDir, checked, newFinding)...)
	}
	return append(findings, checkExitCodeSwallowed(segs, newFinding)...)
}

// PrecheckStrategy 對 TestStrategy 內全部 verify 命令跑 PrecheckCommand 並彙總 finding。
// 走訪順序固定：verify_groups（group 名稱排序）→ verify_commands → ac_checks（AC ID 排序），
// 確保輸出穩定可測。workDir 是相對路徑的解析基準，一般傳 workspace root。
func PrecheckStrategy(ts protocol.TestStrategy, workDir string) []Finding {
	var findings []Finding

	names := make([]string, 0, len(ts.VerifyGroups))
	for name := range ts.VerifyGroups {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source := fmt.Sprintf("verify_groups[%s]", name)
		for _, cmd := range ts.VerifyGroups[name] {
			findings = append(findings, PrecheckCommand(source, cmd, workDir)...)
		}
	}

	for _, cmd := range ts.Verify {
		findings = append(findings, PrecheckCommand("verify_commands", cmd, workDir)...)
	}

	acIDs := make([]string, 0, len(ts.ACChecks))
	for id := range ts.ACChecks {
		acIDs = append(acIDs, id)
	}
	sort.Strings(acIDs)
	for _, id := range acIDs {
		source := fmt.Sprintf("ac_checks[%s]", id)
		for _, cmd := range ts.ACChecks[id] {
			findings = append(findings, PrecheckCommand(source, cmd, workDir)...)
		}
	}

	return findings
}

// checkSegment 對單一 segment 跑 unknown-executable、missing-path 與 unanchored-pass-grep。
// checked 跨 segment 記錄已判定過的路徑 token，避免同一 token 被重複報。
func checkSegment(text, workDir string, checked map[string]bool, newFinding func(rule, reason string) Finding) []Finding {
	var findings []Finding
	tokens := strings.Fields(text)

	// (a) 段首的 VAR=value 指派：value 含 `/` 即視為路徑，必須存在（涵蓋 GOWORK=../go.work）。
	idx := 0
	for idx < len(tokens) && precheckAssign.MatchString(tokens[idx]) {
		value := tokens[idx][strings.Index(tokens[idx], "=")+1:]
		if strings.Contains(value, "/") && !isOpaqueToken(value) && !checked[value] {
			checked[value] = true
			if _, err := os.Stat(resolvePrecheckPath(workDir, value)); err != nil {
				findings = append(findings, newFinding(ruleMissingPath,
					fmt.Sprintf("env assignment %q points to %q which does not exist", tokens[idx], value)))
			}
		}
		idx++
	}
	if idx >= len(tokens) {
		return findings
	}

	head := tokens[idx]
	// 含 `/` 的可執行檔（bin/4x、./scripts/x.sh）是 build 產物，designing 當下不保證已存在，不查。
	if !isOpaqueToken(head) && !strings.Contains(head, "/") && !shellBuiltins[head] {
		if _, err := exec.LookPath(head); err != nil {
			findings = append(findings, newFinding(ruleUnknownExecutable,
				fmt.Sprintf("executable %q not found in PATH", head)))
		}
	}

	// (b) cd 的第一個參數必須存在且為目錄。
	if head == "cd" && idx+1 < len(tokens) {
		arg := tokens[idx+1]
		if !isOpaqueToken(arg) && !checked[arg] {
			checked[arg] = true
			info, err := os.Stat(resolvePrecheckPath(workDir, arg))
			switch {
			case err != nil:
				findings = append(findings, newFinding(ruleMissingPath, fmt.Sprintf("cd target %q does not exist", arg)))
			case !info.IsDir():
				findings = append(findings, newFinding(ruleMissingPath, fmt.Sprintf("cd target %q is not a directory", arg)))
			}
		}
	}

	// (c) 以 ./ 或 ../ 開頭的參數 token。剝除尾端 `/...` 後判定，讓 `./...` 退化成 workDir 本身。
	for _, tok := range tokens[idx+1:] {
		if !strings.HasPrefix(tok, "./") && !strings.HasPrefix(tok, "../") {
			continue
		}
		if isOpaqueToken(tok) || checked[tok] {
			continue
		}
		checked[tok] = true
		if _, err := os.Stat(resolvePrecheckPath(workDir, strings.TrimSuffix(tok, "/..."))); err != nil {
			findings = append(findings, newFinding(ruleMissingPath, fmt.Sprintf("path %q does not exist", tok)))
		}
	}

	if grepTools[head] {
		findings = append(findings, checkPassGrep(text, newFinding)...)
	}
	return findings
}

// checkPassGrep 檢查段內引號包住的 go test 結果 pattern 是否錨行首且帶尾隨空白。
// 未加引號的 pattern 略過不判（strings.Fields 無法還原原始邊界）。
func checkPassGrep(text string, newFinding func(rule, reason string) Finding) []Finding {
	var findings []Finding
	for _, m := range precheckQuoted.FindAllStringSubmatch(text, -1) {
		pattern := m[1]
		if pattern == "" {
			pattern = m[2]
		}
		if !containsGoTestMarker(pattern) {
			continue
		}
		if strings.HasPrefix(pattern, "^") && strings.HasSuffix(pattern, " ") {
			continue
		}
		findings = append(findings, newFinding(ruleUnanchoredPassGrep, fmt.Sprintf(
			"grep pattern %q is unanchored; use '^--- PASS: TestX ' (anchor line start + trailing space) "+
				"to avoid matching indented subtest lines and prefix-sharing test names", pattern)))
	}
	return findings
}

// checkExitCodeSwallowed 找出 exit code 被丟棄的上游段。
// 只有兩種情況算丟棄：下游段以 `;` / 換行 銜接，或以單一 `|` 銜接且下游是唯讀過濾工具。
// `&&` / `||` / `&` 一律不報。pipe 分支刻意要求下游屬 safeFilterTools——splitShellSegments
// 不做 quote-aware 解析，`-run '^TestA$|^TestB$'` 的引號內 `|` 也會被切段，加上這個條件
// 才不會把合法的 regex alternation 誤判為吞 exit code。
func checkExitCodeSwallowed(segs []shellSegment, newFinding func(rule, reason string) Finding) []Finding {
	var findings []Finding
	for i := 1; i < len(segs); i++ {
		if !isExecBearing(segs[i-1].text) {
			continue
		}
		op := segs[i].op
		swallowed := (op == "|" && isSafeFilter(strings.TrimSpace(segs[i].text))) || op == ";" || op == "\n"
		if !swallowed {
			continue
		}
		findings = append(findings, newFinding(ruleExitCodeSwallowed, fmt.Sprintf(
			"exit code of %q is discarded by the following %q operator; join the commands with && "+
				"(redirect the upstream output to a file first if the downstream command reads it)",
			strings.TrimSpace(segs[i-1].text), op)))
	}
	return findings
}

// isExecBearing 回報 segment 是否真的執行受測程式：第一個非 `VAR=value` 的 token
// 既不屬 safeFilterTools 也不屬 shellBuiltins。
func isExecBearing(text string) bool {
	tokens := strings.Fields(text)
	idx := 0
	for idx < len(tokens) && precheckAssign.MatchString(tokens[idx]) {
		idx++
	}
	if idx >= len(tokens) {
		return false
	}
	return !safeFilterTools[tokens[idx]] && !shellBuiltins[tokens[idx]]
}

// isOpaqueToken 回報 token 是否含引號、變數展開或 glob 字元。這類 token 經 strings.Fields
// 粗切後不保證仍是完整字面值，一律跳過 PATH 與存在性判定，避免誤報。
func isOpaqueToken(tok string) bool {
	return strings.ContainsAny(tok, "'\"$`*?")
}

// containsGoTestMarker 回報 pattern 是否含 go test 結果行前綴。
func containsGoTestMarker(pattern string) bool {
	for _, marker := range goTestMarkers {
		if strings.Contains(pattern, marker) {
			return true
		}
	}
	return false
}

// resolvePrecheckPath 把命令內的路徑 token 解析成絕對路徑：絕對路徑原樣回傳，
// 相對路徑接在 workDir 之後。
func resolvePrecheckPath(workDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(workDir, p)
}
