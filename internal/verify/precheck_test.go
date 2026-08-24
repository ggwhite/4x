package verify

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// precheckWorkDir 是 precheck 測試的路徑解析基準：repo root（測試 cwd 為 internal/verify）。
// 用 repo root 而非 t.TempDir()，讓 `./internal/verify` 這類真實存在的路徑不被誤判。
const precheckWorkDir = "../.."

// knownFailurePattern 是一筆已知失敗樣態：命令加上它必須觸發的 rule ID。
type knownFailurePattern struct {
	name      string
	cmd       string
	wantRules []string
}

// knownFailurePatterns 是 feature 蒐集到的十種已知失敗樣態（元驗證的輸入）。
// TestPrecheckKnownFailurePatterns 與 TestPrecheckLintAllowEscape 共用同一份清單。
var knownFailurePatterns = []knownFailurePattern{
	{"gowork-relative-path", "GOWORK=../nope/go.work go test ./...", []string{ruleMissingPath}},
	{"buf-lint-bad-path", "buf lint ./proto/nonexistent", []string{ruleMissingPath}},
	{"cd-missing-dir", "cd nonexistent-dir && make test", []string{ruleMissingPath}},
	{"unknown-binary", "definitely-not-a-real-binary --check", []string{ruleUnknownExecutable}},
	{
		"semicolon-swallows-exit-code",
		"go test ./internal/verify > /tmp/x.log 2>&1; grep -q -- '^--- PASS: TestX ' /tmp/x.log",
		[]string{ruleExitCodeSwallowed},
	},
	{
		"pipe-swallows-exit-code",
		"go test -race -count=1 -v -run 'TestProfitGainEnums' ./internal/verify/ 2>&1 | grep -q -- '--- PASS: TestProfitGainEnums'",
		[]string{ruleExitCodeSwallowed, ruleUnanchoredPassGrep},
	},
	{
		"grep-not-anchored",
		"go test -v ./internal/verify && grep -q -- '--- PASS: TestX ' out.log",
		[]string{ruleUnanchoredPassGrep},
	},
	{
		"grep-no-trailing-space",
		"go test -v ./internal/verify && grep -q -- '^--- PASS: TestX' out.log",
		[]string{ruleUnanchoredPassGrep},
	},
	{"unbalanced-quote", "go test -run 'TestX ./...", []string{ruleUnparseable}},
	{"dangling-connector", "make build &&", []string{ruleUnparseable}},
}

// ruleSet 把 findings 的 Rule 收成集合，供斷言比對。
func ruleSet(findings []Finding) map[string]bool {
	set := make(map[string]bool, len(findings))
	for _, f := range findings {
		set[f.Rule] = true
	}
	return set
}

// TestPrecheckKnownFailurePatterns 是元驗證：每個已知失敗樣態都必須被判成指定的 rule ID，
// 只斷言「有 finding」不足夠。（AC-2）
func TestPrecheckKnownFailurePatterns(t *testing.T) {
	for _, tc := range knownFailurePatterns {
		t.Run(tc.name, func(t *testing.T) {
			findings := PrecheckCommand("verify_commands", tc.cmd, precheckWorkDir)
			got := ruleSet(findings)
			for _, want := range tc.wantRules {
				if !got[want] {
					t.Fatalf("rule %q not reported for %q; got findings: %v", want, tc.cmd, findings)
				}
			}
			for _, f := range findings {
				if !strings.Contains(f.Error(), tc.cmd) {
					t.Fatalf("finding message must contain the raw command; got %q", f.Error())
				}
				if f.Severity != SeverityError {
					t.Fatalf("severity = %q, want %q", f.Severity, SeverityError)
				}
			}
		})
	}
}

// TestPrecheckNegativeCases 固定 precheck 的排除邊界：合法命令一律回零 finding。
// 其中 `go test -race ./...` 是 DR-5 的具名邊界——它執行期不會 exit 0 是動態事實，
// 靜態不可判，precheck 不得攔它。（AC-3）
func TestPrecheckNegativeCases(t *testing.T) {
	cases := []struct{ name, cmd string }{
		{"race-all-packages", "go test -race ./..."},
		{"make-chain", "make build && make test && make lint"},
		{"cd-dot", "cd . && go test ./internal/verify"},
		{"binary-with-slash", "bin/4x status"},
		{
			"redirect-then-anchored-grep",
			"go test -v ./internal/verify > /tmp/x.log 2>&1 && grep -q -- '^--- PASS: TestX ' /tmp/x.log",
		},
		{"echo-then-real-command", "echo start && make build"},
		{"quoted-regex-alternation", "go test ./internal/verify -run '^TestA$|^TestB$'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if findings := PrecheckCommand("verify_commands", tc.cmd, precheckWorkDir); len(findings) != 0 {
				t.Fatalf("expected 0 findings for %q, got %v", tc.cmd, findings)
			}
		})
	}
}

// TestPrecheckNoSideEffects 證明 precheck 完全不執行受檢命令。（AC-4）
func TestPrecheckNoSideEffects(t *testing.T) {
	canary := filepath.Join(t.TempDir(), "f188-precheck-canary")
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatalf("canary must not exist before the call: %v", err)
	}

	PrecheckCommand("verify_commands", "touch "+canary+" && rm -rf /tmp/f188-precheck-victim", precheckWorkDir)

	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatalf("precheck executed the command: canary %s exists", canary)
	}
}

// TestPrecheckLintAllowEscape 驗證 `4x-lint:allow` 逃生口：加上後整條命令跳過全部規則。（AC-5）
func TestPrecheckLintAllowEscape(t *testing.T) {
	for _, tc := range knownFailurePatterns {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd + " # 4x-lint:allow"
			if findings := PrecheckCommand("verify_commands", cmd, precheckWorkDir); len(findings) != 0 {
				t.Fatalf("4x-lint:allow must skip all rules for %q, got %v", cmd, findings)
			}
		})
	}
}

// TestPrecheckStrategySources 驗證三個來源的 Finding.Source 格式與走訪排序。（AC-6）
func TestPrecheckStrategySources(t *testing.T) {
	const bad = "definitely-not-a-real-binary --check"

	t.Run("three-sources", func(t *testing.T) {
		ts := protocol.TestStrategy{
			VerifyGroups: map[string][]string{"core": {bad}},
			ACChecks:     map[string][]string{"AC-1": {bad}},
		}
		// verify_groups 與 verify_commands 不可並存於 ResolveGroups，但 precheck 走訪兩者互不影響，
		// 此處刻意同時放三個來源，證明 Source 標記各自正確。
		ts.Verify = []string{bad}

		got := make([]string, 0, 3)
		for _, f := range PrecheckStrategy(ts, precheckWorkDir) {
			got = append(got, f.Source)
		}
		sort.Strings(got)
		want := []string{"ac_checks[AC-1]", "verify_commands", "verify_groups[core]"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("sources = %v, want %v", got, want)
		}
	})

	t.Run("sorted-traversal", func(t *testing.T) {
		ts := protocol.TestStrategy{
			VerifyGroups: map[string][]string{"zeta": {bad}, "alpha": {bad}},
			ACChecks:     map[string][]string{"AC-2": {bad}, "AC-1": {bad}},
		}
		got := make([]string, 0, 4)
		for _, f := range PrecheckStrategy(ts, precheckWorkDir) {
			got = append(got, f.Source)
		}
		want := []string{"verify_groups[alpha]", "verify_groups[zeta]", "ac_checks[AC-1]", "ac_checks[AC-2]"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("traversal order = %v, want %v", got, want)
		}
	})
}

// TestSplitShellSegmentsOp 驗證 shellSegment.op 記錄前導運算子，且 CommandAllowed 行為不變。（AC-7）
func TestSplitShellSegmentsOp(t *testing.T) {
	segs := splitShellSegments("a; b && c || d | e & f")
	gotOps := make([]string, 0, len(segs))
	for _, s := range segs {
		gotOps = append(gotOps, s.op)
	}
	wantOps := []string{"", ";", "&&", "||", "|", "&"}
	if strings.Join(gotOps, ",") != strings.Join(wantOps, ",") {
		t.Fatalf("ops = %v, want %v", gotOps, wantOps)
	}
	// afterPipe 與 op 必須一致：只有單一 `|` 為 true。
	for _, s := range segs {
		if s.afterPipe != (s.op == "|") {
			t.Fatalf("segment %q: afterPipe=%v but op=%q", s.text, s.afterPipe, s.op)
		}
	}

	cases := []struct {
		name      string
		cmd       string
		allowlist []string
		want      bool
	}{
		{"pipe downstream safe filter allowed", "go test ./... | grep -v '^ok'", []string{"go test"}, true},
		{"compound segment not relaxed", "go test ./... ; grep -v ok", []string{"go test"}, false},
		{"substitution rejected", "go test $(whoami)", []string{"go test"}, false},
		{"redirection rejected", "go test ./... > /tmp/out.log", []string{"go test"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CommandAllowed(tc.cmd, tc.allowlist); got != tc.want {
				t.Fatalf("CommandAllowed(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}
