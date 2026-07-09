package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/doctor"
	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestConfigValue_Branches 覆蓋 configValue 的多個 key 解析分支（top-level / runner.* / role.*
// 與未知 key/field），斷言各自回傳值或 error。
func TestConfigValue_Branches(t *testing.T) {
	cfg := protocol.UserConfig{
		Locale: "zh-TW",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude-cli", Model: "opus"},
		},
		Roles: map[string]protocol.RoleConfig{
			"coder": {Model: "sonnet", ParallelReviewers: 3},
		},
	}
	ok := []struct{ key, want string }{
		{"locale", "zh-TW"},
		{"theme", "(not set)"},
		{"runner.claude.command", "claude-cli"},
		{"runner.claude.model", "opus"},
		{"runner.missing.command", "(not set)"},
		{"role.coder.model", "sonnet"},
		{"role.coder.parallel_reviewers", "3"},
		{"role.missing.model", "(not set)"},
	}
	for _, tt := range ok {
		got, err := configValue(cfg, tt.key)
		if err != nil {
			t.Errorf("configValue(%q) error: %v", tt.key, err)
			continue
		}
		if got != tt.want {
			t.Errorf("configValue(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
	// 未知 key / field → error
	for _, bad := range []string{"bogus", "runner.claude.bogus", "role.coder.bogus"} {
		if _, err := configValue(cfg, bad); err == nil {
			t.Errorf("configValue(%q) should error", bad)
		}
	}
}

func TestCategorize(t *testing.T) {
	tests := []struct {
		name   string
		status feature.Status
		active bool
		want   int
	}{
		{"in-progress active is running", feature.StatusInProgress, true, 0},
		{"ready-for-review is review", feature.StatusReadyForReview, false, 1},
		{"in-progress inactive is pending", feature.StatusInProgress, false, 2},
		{"needs-attention is pending", feature.StatusNeedsAttention, false, 2},
		{"done is done bucket", feature.StatusDone, false, 4},
		{"abandoned is done bucket", feature.StatusAbandoned, false, 4},
		{"not-started is todo", feature.StatusNotStarted, false, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := categorize(feature.Feature{Status: tt.status}, tt.active)
			if got != tt.want {
				t.Errorf("categorize(%s, active=%v) = %d, want %d", tt.status, tt.active, got, tt.want)
			}
		})
	}
}

func TestDocsLabel(t *testing.T) {
	tests := []struct {
		spec, plan bool
		want       string
	}{
		{true, true, "S+P"},
		{true, false, "S"},
		{false, true, "P"},
		{false, false, "-"},
	}
	for _, tt := range tests {
		if got := docsLabel(tt.spec, tt.plan); got != tt.want {
			t.Errorf("docsLabel(%v,%v) = %q, want %q", tt.spec, tt.plan, got, tt.want)
		}
	}
}

func TestPct(t *testing.T) {
	if got := pct(0, 0); got != 0 {
		t.Errorf("pct div-by-zero = %v, want 0", got)
	}
	if got := pct(25, 100); got != 25 {
		t.Errorf("pct(25,100) = %v, want 25", got)
	}
	if got := pct(1, 4); got != 25 {
		t.Errorf("pct(1,4) = %v, want 25", got)
	}
}

func TestAvg(t *testing.T) {
	if got := avg(10, 0); got != 0 {
		t.Errorf("avg div-by-zero = %v, want 0", got)
	}
	if got := avg(10, 4); got != 2.5 {
		t.Errorf("avg(10,4) = %v, want 2.5", got)
	}
}

func TestSeverityIcon(t *testing.T) {
	tests := []struct {
		sev  doctor.Severity
		want string
	}{
		{doctor.SeverityPass, "✅"},
		{doctor.SeverityWarn, "⚠️"},
		{doctor.SeverityFail, "❌"},
		{doctor.Severity("unknown"), "•"},
	}
	for _, tt := range tests {
		if got := severityIcon(tt.sev); got != tt.want {
			t.Errorf("severityIcon(%v) = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

func TestSectionOrder(t *testing.T) {
	tests := []struct {
		section string
		want    int
	}{
		{"settings", 0},
		{"runners", 1},
		{"roles", 2},
		{"workspace", 3},
		{"mystery", 99}, // 未知 section fallback
	}
	for _, tt := range tests {
		if got := sectionOrder(tt.section); got != tt.want {
			t.Errorf("sectionOrder(%q) = %d, want %d", tt.section, got, tt.want)
		}
	}
}

func TestIntValOrDefault(t *testing.T) {
	if got := intValOrDefault(0, 5); got != "(not set, default: 5)" {
		t.Errorf("intValOrDefault(0,5) = %q", got)
	}
	if got := intValOrDefault(-1, 5); got != "(not set, default: 5)" {
		t.Errorf("intValOrDefault(-1,5) = %q, want default hint", got)
	}
	if got := intValOrDefault(7, 5); got != "7" {
		t.Errorf("intValOrDefault(7,5) = %q, want 7", got)
	}
}

func TestRelWithinWorkspace(t *testing.T) {
	// workspace 內的相對路徑 → (rel, true)
	if rel, ok := relWithinWorkspace("/ws", "/ws", "cmd/4x/foo.go"); !ok || rel != "cmd/4x/foo.go" {
		t.Errorf("in-workspace: got (%q,%v), want (cmd/4x/foo.go, true)", rel, ok)
	}
	// 絕對路徑落在 workspace 內
	if rel, ok := relWithinWorkspace("/ws", "/ws", "/ws/a/b.go"); !ok || rel != "a/b.go" {
		t.Errorf("abs in-workspace: got (%q,%v), want (a/b.go, true)", rel, ok)
	}
	// 越界路徑 → ("", false)
	if rel, ok := relWithinWorkspace("/ws", "/ws", "../outside.go"); ok || rel != "" {
		t.Errorf("out-of-workspace: got (%q,%v), want (\"\", false)", rel, ok)
	}
	// 絕對路徑落在 workspace 外
	if _, ok := relWithinWorkspace("/ws", "/ws", "/etc/passwd"); ok {
		t.Error("abs out-of-workspace should be rejected")
	}
}

func TestQuoteAppleScript(t *testing.T) {
	// 雙引號與反斜線需 escape，外層包雙引號
	got := quoteAppleScript(`say "hi"\n`)
	want := `"say \"hi\"\\n"`
	if got != want {
		t.Errorf("quoteAppleScript = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Errorf("quoteAppleScript result must be double-quoted: %q", got)
	}
}

// TestJsonError 斷言 jsonError 的行為：把 msg 以 {"error": msg} 的 JSON 印到 stdout 並以
// exit code 1 結束。因 jsonError 內部呼叫 os.Exit(1) 會終止測試程序，採 Go 標準的
// subprocess pattern——子程序（帶 F165_JSONERROR_CHILD=1）實際呼叫 jsonError，父程序捕捉其
// stdout 與 exit code 做行為斷言。
func TestJsonError(t *testing.T) {
	if os.Getenv("F165_JSONERROR_CHILD") == "1" {
		// 子程序：實際觸發 jsonError（會 os.Exit(1)，以下不會返回）。
		_ = jsonError("boom-msg")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestJsonError")
	cmd.Env = append(os.Environ(), "F165_JSONERROR_CHILD=1")
	out, err := cmd.Output()

	// 斷言 exit code 為 1（jsonError 的 os.Exit(1)）。
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("jsonError child should exit with error, got err=%v out=%q", err, out)
	}
	if code := ee.ExitCode(); code != 1 {
		t.Errorf("jsonError exit code = %d, want 1", code)
	}

	// 斷言 stdout 是可解析的 JSON 且 error 欄位含 msg。
	var parsed struct {
		Error string `json:"error"`
	}
	if uErr := json.Unmarshal(out, &parsed); uErr != nil {
		t.Fatalf("jsonError output not valid JSON: %v (out=%q)", uErr, out)
	}
	if parsed.Error != "boom-msg" {
		t.Errorf("jsonError JSON error field = %q, want %q", parsed.Error, "boom-msg")
	}
}

func TestQuoteJS(t *testing.T) {
	// 反斜線、單引號 escape，換行轉 \n，外層包單引號
	got := quoteJS("a'b\\c\nd")
	want := `'a\'b\\c\nd'`
	if got != want {
		t.Errorf("quoteJS = %q, want %q", got, want)
	}
	// 回車轉 \r
	if got := quoteJS("x\ry"); got != `'x\ry'` {
		t.Errorf("quoteJS CR = %q, want 'x\\ry'", got)
	}
}
