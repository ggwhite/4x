package runner

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// EnvFilter 是由 settings（.4x/settings.json 的 runner_env 區段）解析出的**額外**
// denylist/allowlist pattern，不含內建預設。實際過濾時會與 DefaultEnvDenylist()、
// alwaysKeepEnv()、DefaultRunnerEnvAllowlist() 及 per-runner env_allowlist 合併後套用。
type EnvFilter struct {
	// Denylist 追加到內建預設 denylist（DefaultEnvDenylist）之後。
	Denylist []string
	// Allowlist 覆蓋 denylist（allowlist 命中即保留），可用來移除內建預設 pattern。
	Allowlist []string
}

// DefaultEnvDenylist 回傳內建的敏感環境變數 denylist SoT。命中這些 glob pattern 的變數
// 在 spawn LLM runner 子程序前會被過濾掉，避免把認證 token / key / secret 暴露給自主
// 執行的 AI 子程序。單一 SoT，測試斷言以此清單字面為準。
func DefaultEnvDenylist() []string {
	return []string{
		"*_TOKEN",
		"*_KEY",
		"*_SECRET",
		"*_PASSWORD",
		"*_CREDENTIALS",
		"*_SECRETS",
		"AWS_*",
		"SECRET_*",
		"GITHUB_TOKEN",
		"GH_TOKEN",
	}
}

// alwaysKeepEnv 回傳永不被過濾的必需環境變數名稱（平台感知）。這些變數是 runner 子程序
// 正常啟動所必需（PATH 用於解析命令、HOME 用於各 CLI 讀設定），即使被誤放進 denylist
// 也一律保留。runOnce 會把此清單當成 allowlist 的一部分傳入 FilterEnv。
func alwaysKeepEnv() []string {
	keep := []string{"PATH", "HOME"}
	if runtime.GOOS == "windows" {
		keep = append(keep, "USERPROFILE", "SYSTEMROOT")
	}
	return keep
}

// matchEnvPattern 以 glob 比對 pattern 與環境變數 key：支援 `*` 萬用字元（可匹配空字串），
// pattern 與 key 皆先轉大寫做**大小寫不敏感**比對。不引入外部套件——用 path.Match 於大寫後的
// key 比對（環境變數 key 不含 `/`，故 path.Match 的 Separator 語意安全）。pattern 格式錯誤
// （path.Match 回 ErrBadPattern）視為不匹配。
func matchEnvPattern(pattern, key string) bool {
	ok, err := path.Match(strings.ToUpper(pattern), strings.ToUpper(key))
	if err != nil {
		return false
	}
	return ok
}

// matchAnyEnvPattern 回傳 key 是否命中 patterns 中任一 glob。
func matchAnyEnvPattern(patterns []string, key string) bool {
	for _, p := range patterns {
		if matchEnvPattern(p, key) {
			return true
		}
	}
	return false
}

// FilterEnv 是純函式：對每筆 `KEY=VALUE` 取 KEY，依「allowlist 覆蓋 denylist」規則保留——
// 保留條件為 matchAny(allowlist, KEY) || !matchAny(denylist, KEY)。被濾掉的變數只收集其
// KEY（不含 value）到 filteredKeys。輸入順序保留。
func FilterEnv(env, denylist, allowlist []string) (kept []string, filteredKeys []string) {
	for _, e := range env {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		if matchAnyEnvPattern(allowlist, key) || !matchAnyEnvPattern(denylist, key) {
			kept = append(kept, e)
		} else {
			filteredKeys = append(filteredKeys, key)
		}
	}
	return kept, filteredKeys
}

// ResolveEnvFilter 從 merged config 的 runner_env 區段取出額外的 Denylist/Allowlist pattern。
func ResolveEnvFilter(cfg protocol.Config) EnvFilter {
	return EnvFilter{
		Denylist:  cfg.RunnerEnv.Denylist,
		Allowlist: cfg.RunnerEnv.Allowlist,
	}
}

// runnerEnvAllowlists 是各內建 runner（以 runner name 為主鍵）正常運作所必需、應放行的
// 環境變數 glob 清單。這些認證變數多半會命中 DefaultEnvDenylist 的 *_KEY/*_TOKEN pattern，
// 必須在 spawn 該 runner 時被 allowlist 放行，否則 runner 起不來。
var runnerEnvAllowlists = map[string][]string{
	"claude":   {"ANTHROPIC_API_KEY", "ANTHROPIC_*", "CLAUDE_*", "CLAUDE_CODE_*"},
	"codex":    {"OPENAI_API_KEY", "OPENAI_*", "CODEX_*"},
	"gemini":   {"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_*", "GEMINI_*"},
	"agy":      {"ANTHROPIC_API_KEY", "ANTHROPIC_*", "CLAUDE_*"},
	"copilot":  {"GITHUB_TOKEN", "GH_TOKEN", "COPILOT_*", "GITHUB_COPILOT_*"},
	"opencode": {"ANTHROPIC_*", "OPENAI_*", "OPENROUTER_*", "OPENCODE_*"},
	"agent":    {"CURSOR_*", "ANTHROPIC_*", "OPENAI_*"},
}

// canonicalRunnerKey 把 (name, command) 正規化成已知 runner 的主鍵：先用小寫 name 比對已知
// 集合命中即回傳；否則取 command 的 basename、去掉 .exe/.cmd/.bat 副檔名並小寫後再比對；
// 皆不命中回傳空字串。主鍵為 name（cfg.Runners 的 map key，如 claude/codex），與 command
// 是否為絕對路徑或 wrapper 無關；basename fallback 涵蓋「runner 取名 my-claude 但
// command 指向 .../claude」的情境。
func canonicalRunnerKey(name, command string) string {
	if k := strings.ToLower(name); k != "" {
		if _, ok := runnerEnvAllowlists[k]; ok {
			return k
		}
	}
	b := strings.ToLower(filepath.Base(command))
	for _, suf := range []string{".exe", ".cmd", ".bat"} {
		b = strings.TrimSuffix(b, suf)
	}
	if _, ok := runnerEnvAllowlists[b]; ok {
		return b
	}
	return ""
}

// DefaultRunnerEnvAllowlist 是 per-runner 內建認證 allowlist 的單一 SoT：在 spawn 當下依
// (name, command) 正規化出的 runner 主鍵回傳該 runner 必需的環境變數 glob 清單。主鍵為
// runner name、command basename 為 fallback（見 canonicalRunnerKey）；未知 runner 回空 slice
// （僅靠 alwaysKeepEnv + 使用者 allowlist）。
func DefaultRunnerEnvAllowlist(name, command string) []string {
	key := canonicalRunnerKey(name, command)
	if key == "" {
		return nil
	}
	return append([]string{}, runnerEnvAllowlists[key]...)
}
