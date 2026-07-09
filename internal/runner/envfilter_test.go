package runner

import (
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestDefaultEnvDenylist 鎖定內建 denylist 清單字面（集合相等，順序不拘，AC-1）。
func TestDefaultEnvDenylist(t *testing.T) {
	want := []string{
		"*_TOKEN", "*_KEY", "*_SECRET", "*_PASSWORD", "*_CREDENTIALS",
		"*_SECRETS", "AWS_*", "SECRET_*", "GITHUB_TOKEN", "GH_TOKEN",
	}
	got := DefaultEnvDenylist()
	if !reflect.DeepEqual(sortedCopy(got), sortedCopy(want)) {
		t.Errorf("DefaultEnvDenylist() = %v, want set %v", got, want)
	}
}

// TestFilterEnv_Denylist 驗證預設 denylist 濾掉敏感變數、保留不命中者（AC-1）。
func TestFilterEnv_Denylist(t *testing.T) {
	env := []string{
		"FOO_TOKEN=x",
		"MY_SECRET=y",
		"AWS_ACCESS_KEY_ID=z",
		"GITHUB_TOKEN=t",
		"EDITOR=vim",
	}
	kept, filtered := FilterEnv(env, DefaultEnvDenylist(), nil)
	if !contains(kept, "EDITOR=vim") {
		t.Errorf("EDITOR=vim should be kept, kept=%v", kept)
	}
	for _, k := range []string{"FOO_TOKEN=x", "MY_SECRET=y", "AWS_ACCESS_KEY_ID=z", "GITHUB_TOKEN=t"} {
		if contains(kept, k) {
			t.Errorf("%q should have been filtered, kept=%v", k, kept)
		}
	}
	for _, k := range []string{"FOO_TOKEN", "MY_SECRET", "AWS_ACCESS_KEY_ID", "GITHUB_TOKEN"} {
		if !contains(filtered, k) {
			t.Errorf("%q should be in filteredKeys=%v", k, filtered)
		}
	}
	// filteredKeys 只含 key，不含 value
	for _, f := range filtered {
		if f == "FOO_TOKEN=x" {
			t.Errorf("filteredKeys must not contain value: %q", f)
		}
	}
}

// TestFilterEnv_AllowlistWins 驗證 allowlist 覆蓋 denylist：同時命中則保留（AC-2）。
func TestFilterEnv_AllowlistWins(t *testing.T) {
	env := []string{"ANTHROPIC_API_KEY=keep", "OTHER_KEY=drop"}
	kept, filtered := FilterEnv(env, DefaultEnvDenylist(), []string{"ANTHROPIC_*"})
	if !contains(kept, "ANTHROPIC_API_KEY=keep") {
		t.Errorf("ANTHROPIC_API_KEY should be kept via allowlist, kept=%v", kept)
	}
	if contains(kept, "OTHER_KEY=drop") {
		t.Errorf("OTHER_KEY should be filtered, kept=%v", kept)
	}
	if !contains(filtered, "OTHER_KEY") {
		t.Errorf("OTHER_KEY should be in filteredKeys=%v", filtered)
	}
}

// TestDefaultRunnerEnvAllowlist 驗證 per-runner allowlist 內容與未知 runner 回空（AC-2）。
func TestDefaultRunnerEnvAllowlist(t *testing.T) {
	claude := DefaultRunnerEnvAllowlist("claude", "claude")
	for _, want := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_*", "CLAUDE_*"} {
		if !contains(claude, want) {
			t.Errorf("claude allowlist missing %q: %v", want, claude)
		}
	}
	copilot := DefaultRunnerEnvAllowlist("copilot", "copilot")
	if !contains(copilot, "GITHUB_TOKEN") {
		t.Errorf("copilot allowlist missing GITHUB_TOKEN: %v", copilot)
	}
	if got := DefaultRunnerEnvAllowlist("nope", "nope"); len(got) != 0 {
		t.Errorf("unknown runner should return empty slice, got %v", got)
	}
}

// TestDefaultRunnerEnvAllowlist_CommandPath 驗證 name 為主鍵、command basename 為 fallback，
// command 為絕對路徑/wrapper/.exe 仍生效（AC-11 a-c）。
func TestDefaultRunnerEnvAllowlist_CommandPath(t *testing.T) {
	// (a) name=claude，command 絕對路徑或裸名皆含 ANTHROPIC_API_KEY
	for _, cmd := range []string{"/usr/local/bin/claude", "claude"} {
		got := DefaultRunnerEnvAllowlist("claude", cmd)
		if !contains(got, "ANTHROPIC_API_KEY") {
			t.Errorf("DefaultRunnerEnvAllowlist(claude, %q) missing ANTHROPIC_API_KEY: %v", cmd, got)
		}
	}
	// (b) name 未知（my-claude），basename 命中 claude
	got := DefaultRunnerEnvAllowlist("my-claude", "/opt/homebrew/bin/claude")
	if !contains(got, "ANTHROPIC_API_KEY") {
		t.Errorf("basename fallback failed: %v", got)
	}
	// (c) codex.exe basename 命中
	gotExe := DefaultRunnerEnvAllowlist("codex", "/path/to/codex.exe")
	if !contains(gotExe, "OPENAI_API_KEY") {
		t.Errorf("codex.exe should map to codex allowlist: %v", gotExe)
	}
}

// TestAlwaysKeepEnv 驗證必需變數清單至少含 PATH/HOME（Windows 另含 USERPROFILE/SYSTEMROOT）（AC-3）。
func TestAlwaysKeepEnv(t *testing.T) {
	keep := alwaysKeepEnv()
	for _, want := range []string{"PATH", "HOME"} {
		if !contains(keep, want) {
			t.Errorf("alwaysKeepEnv missing %q: %v", want, keep)
		}
	}
	if runtime.GOOS == "windows" {
		for _, want := range []string{"USERPROFILE", "SYSTEMROOT"} {
			if !contains(keep, want) {
				t.Errorf("alwaysKeepEnv (windows) missing %q: %v", want, keep)
			}
		}
	}
}

// TestFilterEnv_EssentialKept 驗證即使把 PATH/HOME 放進 denylist，essential 變數（以
// alwaysKeepEnv 作 allowlist）仍被保留（AC-3）。
func TestFilterEnv_EssentialKept(t *testing.T) {
	env := []string{"PATH=/usr/bin", "HOME=/home/x", "FOO_TOKEN=secret"}
	deny := append(DefaultEnvDenylist(), "PATH", "HOME")
	kept, _ := FilterEnv(env, deny, alwaysKeepEnv())
	if !contains(kept, "PATH=/usr/bin") {
		t.Errorf("PATH should always be kept, kept=%v", kept)
	}
	if !contains(kept, "HOME=/home/x") {
		t.Errorf("HOME should always be kept, kept=%v", kept)
	}
	if contains(kept, "FOO_TOKEN=secret") {
		t.Errorf("FOO_TOKEN should be filtered, kept=%v", kept)
	}
}

// TestMatchEnvPattern 驗證 glob 大小寫不敏感且 * 可匹配空字串。
func TestMatchEnvPattern(t *testing.T) {
	cases := []struct {
		pattern, key string
		want         bool
	}{
		{"*_TOKEN", "FOO_TOKEN", true},
		{"*_token", "FOO_TOKEN", true},      // 大小寫不敏感
		{"ANTHROPIC_*", "ANTHROPIC_", true}, // * 匹配空字串
		{"GITHUB_TOKEN", "GITHUB_TOKEN", true},
		{"*_KEY", "EDITOR", false},
		{"AWS_*", "AWS_ACCESS_KEY_ID", true},
	}
	for _, c := range cases {
		if got := matchEnvPattern(c.pattern, c.key); got != c.want {
			t.Errorf("matchEnvPattern(%q,%q)=%v want %v", c.pattern, c.key, got, c.want)
		}
	}
}

// TestResolveEnvFilter 驗證 ResolveEnvFilter 回傳 cfg.RunnerEnv 的內容（AC-6）。
func TestResolveEnvFilter(t *testing.T) {
	cfg := protocol.Config{
		RunnerEnv: protocol.RunnerEnvConfig{
			Denylist:  []string{"CUSTOM_*"},
			Allowlist: []string{"MY_FAKE_TOKEN"},
		},
	}
	ef := ResolveEnvFilter(cfg)
	if !reflect.DeepEqual(ef.Denylist, []string{"CUSTOM_*"}) {
		t.Errorf("Denylist = %v", ef.Denylist)
	}
	if !reflect.DeepEqual(ef.Allowlist, []string{"MY_FAKE_TOKEN"}) {
		t.Errorf("Allowlist = %v", ef.Allowlist)
	}
}
