package runner

import (
	"bytes"
	"log/slog"
	"reflect"
	"runtime"
	"sort"
	"strings"
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
		"DATABASE_URL", "REDIS_URL", "MONGODB_URI", "PGPASSWORD", "MYSQL_PWD",
	}
	got := DefaultEnvDenylist()
	if !reflect.DeepEqual(sortedCopy(got), sortedCopy(want)) {
		t.Errorf("DefaultEnvDenylist() = %v, want set %v", got, want)
	}
}

// TestFilterEnv_CredentialKeywordSubstring 驗證不遵循 `*_KEYWORD` 命名慣例的常見憑證變數
// （PGPASSWORD、MYSQL_PWD 前面無底線、DATABASE_URL/REDIS_URL/MONGODB_URI 不含關鍵字後綴）
// 仍會被過濾（F163 post-merge 缺陷1）。
func TestFilterEnv_CredentialKeywordSubstring(t *testing.T) {
	env := []string{
		"PGPASSWORD=x",
		"MYSQL_PWD=y",
		"DATABASE_URL=postgres://u:p@host/db",
		"REDIS_URL=redis://host",
		"MONGODB_URI=mongodb://host",
		"DB_PASSWD=z",       // substring PASSWD
		"SOME_CREDENTIAL=w", // substring CREDENTIAL
		"EDITOR=vim",        // 應保留
	}
	kept, filtered := FilterEnv(env, DefaultEnvDenylist(), nil)
	for _, k := range []string{"PGPASSWORD", "MYSQL_PWD", "DATABASE_URL", "REDIS_URL", "MONGODB_URI", "DB_PASSWD", "SOME_CREDENTIAL"} {
		if !contains(filtered, k) {
			t.Errorf("%q should have been filtered, filtered=%v", k, filtered)
		}
	}
	if !contains(kept, "EDITOR=vim") {
		t.Errorf("EDITOR=vim should be kept, kept=%v", kept)
	}
}

// TestFilterEnv_CredentialKeywordCanBeRescuedByAllowlist 驗證 substring 誤擋（如 TOKENIZER）
// 可透過 allowlist 救回（F163 post-merge 缺陷1 的權衡設計）。
func TestFilterEnv_CredentialKeywordCanBeRescuedByAllowlist(t *testing.T) {
	env := []string{"MY_TOKENIZER=keep-me"}
	kept, _ := FilterEnv(env, DefaultEnvDenylist(), []string{"MY_TOKENIZER"})
	if !contains(kept, "MY_TOKENIZER=keep-me") {
		t.Errorf("MY_TOKENIZER should be rescued by allowlist, kept=%v", kept)
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

// TestDefaultRunnerEnvAllowlist 驗證 per-runner allowlist 內容；未知 runner 仍回傳 base
// allowlist（GITHUB_TOKEN/GH_TOKEN，AC-2、F163 post-merge 缺陷6）。
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
	unknown := DefaultRunnerEnvAllowlist("nope", "nope")
	if !contains(unknown, "GITHUB_TOKEN") || !contains(unknown, "GH_TOKEN") {
		t.Errorf("unknown runner should still get base allowlist (GITHUB_TOKEN/GH_TOKEN), got %v", unknown)
	}
}

// TestDefaultRunnerEnvAllowlist_ClaudeAgyAltProviders 驗證 claude/agy allowlist 補齊
// Bedrock/Vertex/Azure 替代 provider 認證（F163 post-merge 缺陷3）。
func TestDefaultRunnerEnvAllowlist_ClaudeAgyAltProviders(t *testing.T) {
	want := []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION", "AWS_PROFILE",
		"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_CLOUD_PROJECT", "CLOUD_ML_REGION",
		"AZURE_*", "ANTHROPIC_VERTEX_*",
	}
	for _, name := range []string{"claude", "agy"} {
		got := DefaultRunnerEnvAllowlist(name, name)
		for _, w := range want {
			if !contains(got, w) {
				t.Errorf("%s allowlist missing %q: %v", name, w, got)
			}
		}
	}
}

// TestDefaultRunnerEnvAllowlist_Opencode 驗證 opencode allowlist 補齊多 provider keys
// （F163 post-merge 缺陷4）。
func TestDefaultRunnerEnvAllowlist_Opencode(t *testing.T) {
	got := DefaultRunnerEnvAllowlist("opencode", "opencode")
	for _, want := range []string{
		"GROQ_API_KEY", "DEEPSEEK_API_KEY", "MISTRAL_API_KEY", "XAI_API_KEY",
		"GEMINI_API_KEY", "GOOGLE_*", "TOGETHER_API_KEY",
	} {
		if !contains(got, want) {
			t.Errorf("opencode allowlist missing %q: %v", want, got)
		}
	}
}

// TestCanonicalRunnerKey_Cursor 驗證 name="cursor" 直接命中 cursor allowlist，不依賴 basename
// fallback（F163 post-merge 缺陷5）。
func TestCanonicalRunnerKey_Cursor(t *testing.T) {
	if got := canonicalRunnerKey("cursor", "/opt/bin/cursor-agent"); got != "cursor" {
		t.Errorf("canonicalRunnerKey(cursor, .../cursor-agent) = %q, want \"cursor\"", got)
	}
	got := DefaultRunnerEnvAllowlist("cursor", "/opt/bin/cursor-agent")
	for _, want := range []string{"CURSOR_*", "ANTHROPIC_*", "OPENAI_*"} {
		if !contains(got, want) {
			t.Errorf("cursor allowlist missing %q: %v", want, got)
		}
	}
}

// TestDefaultRunnerEnvAllowlist_BaseGithubToken 驗證任一 runner（含未知 runner）的 effective
// allowlist 都含 GITHUB_TOKEN/GH_TOKEN——任何 role 都可能 shell 出 git/gh（F163 post-merge 缺陷6）。
func TestDefaultRunnerEnvAllowlist_BaseGithubToken(t *testing.T) {
	for _, name := range []string{"claude", "codex", "gemini", "agy", "copilot", "opencode", "cursor", "totally-unknown"} {
		got := DefaultRunnerEnvAllowlist(name, name)
		if !contains(got, "GITHUB_TOKEN") || !contains(got, "GH_TOKEN") {
			t.Errorf("%s allowlist missing base GITHUB_TOKEN/GH_TOKEN: %v", name, got)
		}
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

// TestFilterEnv_BadDenylistGlobFailsSafe 驗證使用者 denylist 打錯 glob（ErrBadPattern）時
// 不會靜默 fail-open。因為 path.Match 對格式錯誤的 pattern 一律回 ErrBadPattern（與被比對的
// key 無關，無法判定該 pattern「原本想擋哪個變數」），fail-safe 設計是把該壞 pattern 視為
// 「對所有 key 都命中」（deny-all for this pattern）——寧可過度過濾，也不要讓打錯字的規則
// 悄悄失效造成憑證洩漏；仍可用 allowlist 個別救回真正需要的變數。同時必須觸發 slog.Warn
// 讓使用者能觀察到設定錯誤（F163 post-merge 缺陷2）。
func TestFilterEnv_BadDenylistGlobFailsSafe(t *testing.T) {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(orig)

	env := []string{"INTERNAL_SECRET_VALUE=leak", "EDITOR=vim"}
	// "INTERNAL[SECRET" 缺右括號，對 path.Match 是 ErrBadPattern。
	badDenylist := []string{"INTERNAL[SECRET"}
	kept, filtered := FilterEnv(env, badDenylist, nil)

	if contains(kept, "INTERNAL_SECRET_VALUE=leak") {
		t.Errorf("bad glob pattern should fail safe (deny), but var leaked: kept=%v", kept)
	}
	if !contains(filtered, "INTERNAL_SECRET_VALUE") {
		t.Errorf("INTERNAL_SECRET_VALUE should be in filteredKeys=%v", filtered)
	}
	// deny-all for this pattern：連無關的 EDITOR 也一併被擋下，這是刻意的 fail-safe 代價。
	if contains(kept, "EDITOR=vim") {
		t.Errorf("deny-all fail-safe should also filter unrelated vars while pattern is broken, kept=%v", kept)
	}
	if !strings.Contains(buf.String(), "INTERNAL[SECRET") {
		t.Errorf("expected slog.Warn to mention the invalid pattern, got:\n%s", buf.String())
	}

	// allowlist 仍能個別救回被壞 pattern 波及的變數。
	keptRescued, _ := FilterEnv(env, badDenylist, []string{"EDITOR"})
	if !contains(keptRescued, "EDITOR=vim") {
		t.Errorf("allowlist should rescue EDITOR despite deny-all fail-safe, kept=%v", keptRescued)
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
