package runner

import (
	"log/slog"
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
//
// 這份清單本質是 best-effort、非窮舉：無法列舉所有第三方憑證變數命名慣例（如
// PGPASSWORD、MYSQL_PWD 這類不遵循 `*_KEYWORD` 後綴的變數）。除了本清單的 glob pattern，
// FilterEnv 另外對每個 key 做一次憑證關鍵字 substring 比對（見 credentialKeywordSubstrings），
// 涵蓋大多數不在此清單字面內、但名稱含 PASSWORD/SECRET/TOKEN 等關鍵字的變數。
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
		// 常見資料庫/快取連線字串與非 `*_KEYWORD` 命名慣例的憑證變數。
		"DATABASE_URL",
		"REDIS_URL",
		"MONGODB_URI",
		"PGPASSWORD",
		"MYSQL_PWD",
	}
}

// credentialKeywordSubstrings 是憑證關鍵字的 substring 比對清單（大小寫不敏感）。用來補強
// DefaultEnvDenylist 的 glob pattern 涵蓋不到的命名慣例（如 PGPASSWORD、MYSQL_PWD 關鍵字
// 前面無底線）。這是刻意寬鬆的比對：可能誤擋名稱恰好含這些關鍵字但非憑證的變數
// （如 TOKENIZER 含 TOKEN），安全優先接受此代價；誤擋時可用 allowlist（settings.json
// runner_env.allowlist 或 per-runner env_allowlist）救回。
var credentialKeywordSubstrings = []string{
	"PASSWORD",
	"SECRET",
	"TOKEN",
	"CREDENTIAL",
	"PASSWD",
	"PWD",
}

// hasCredentialKeyword 回傳 key 是否（大小寫不敏感）含任一憑證關鍵字 substring。
func hasCredentialKeyword(key string) bool {
	upper := strings.ToUpper(key)
	for _, kw := range credentialKeywordSubstrings {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
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
// （path.Match 回 ErrBadPattern）視為不匹配——用於 allowlist 情境：壞的 allowlist pattern
// 不應意外多放行變數（fail-closed for permission grants）。denylist 情境請改用
// matchAnyDenylistPattern（fail-safe：壞 pattern 視為命中，避免打錯 glob 導致憑證洩漏）。
func matchEnvPattern(pattern, key string) bool {
	ok, err := path.Match(strings.ToUpper(pattern), strings.ToUpper(key))
	if err != nil {
		return false
	}
	return ok
}

// matchAnyEnvPattern 回傳 key 是否命中 patterns 中任一 glob（allowlist 情境，見 matchEnvPattern）。
func matchAnyEnvPattern(patterns []string, key string) bool {
	for _, p := range patterns {
		if matchEnvPattern(p, key) {
			return true
		}
	}
	return false
}

// warnInvalidDenylistPatterns 對 denylist 每條 pattern 做一次語法驗證（用固定 dummy key，
// 不依賴實際環境變數），偵測到 path.Match 回 ErrBadPattern 時記一次 slog.Warn。避免使用者在
// settings.json runner_env.denylist 打錯 glob（如 "INTERNAL[SECRET" 缺右括號）導致該規則
// 靜默失效、憑證悄悄洩漏給 runner 子程序卻無人發現。每次 FilterEnv 呼叫驗證一次（不在
// per-key 迴圈內重覆記錄，避免對同一條壞 pattern 洗版 log）。
func warnInvalidDenylistPatterns(denylist []string) {
	for _, p := range denylist {
		if _, err := path.Match(strings.ToUpper(p), "X"); err != nil {
			slog.Warn("runner env denylist pattern is invalid glob, failing safe (deny-all for this pattern)",
				"pattern", p, "error", err)
		}
	}
}

// matchAnyDenylistPattern 回傳 key 是否命中 denylist 中任一 glob；語法錯誤的 pattern 採
// fail-safe——視為命中所有 key（寧可多擋，見 warnInvalidDenylistPatterns 的說明）。
func matchAnyDenylistPattern(patterns []string, key string) bool {
	for _, p := range patterns {
		ok, err := path.Match(strings.ToUpper(p), strings.ToUpper(key))
		if err != nil {
			return true
		}
		if ok {
			return true
		}
	}
	return false
}

// isDenied 回傳 key 是否應被 denylist 擋下：命中 denylist 任一 glob（含 fail-safe 的壞
// pattern），或命中內建憑證關鍵字 substring（見 hasCredentialKeyword，補強 glob 涵蓋不到的
// 命名慣例，如 PGPASSWORD/MYSQL_PWD）。
func isDenied(denylist []string, key string) bool {
	return matchAnyDenylistPattern(denylist, key) || hasCredentialKeyword(key)
}

// FilterEnv 是純函式：對每筆 `KEY=VALUE` 取 KEY，依「allowlist 覆蓋 denylist」規則保留——
// 保留條件為 matchAny(allowlist, KEY) || !isDenied(denylist, KEY)。被濾掉的變數只收集其
// KEY（不含 value）到 filteredKeys。輸入順序保留。
func FilterEnv(env, denylist, allowlist []string) (kept []string, filteredKeys []string) {
	warnInvalidDenylistPatterns(denylist)
	for _, e := range env {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		if matchAnyEnvPattern(allowlist, key) || !isDenied(denylist, key) {
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

// altProviderAuthAllowlist 是 claude-code 一等公民支援的替代 provider 認證（AWS Bedrock /
// Google Vertex / Azure），claude 與 agy（皆為 Claude Code 系 runner）共用。
var altProviderAuthAllowlist = []string{
	"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION", "AWS_PROFILE",
	"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_CLOUD_PROJECT", "CLOUD_ML_REGION",
	"AZURE_*", "ANTHROPIC_VERTEX_*",
}

// baseRunnerEnvAllowlist 是所有 runner 共通放行的環境變數，不限定特定 runner——任何 role 的
// LLM 子程序都可能在其內部 shell 出 git/gh（如 push、開 PR/MR），需要這些憑證才能正常運作，
// 故獨立於 per-runner allowlist 之外、對所有 runner（含未知 runner）一律放行。
var baseRunnerEnvAllowlist = []string{"GITHUB_TOKEN", "GH_TOKEN"}

// runnerEnvAllowlists 是各內建 runner（以 runner name 為主鍵）正常運作所必需、應放行的
// 環境變數 glob 清單。這些認證變數多半會命中 DefaultEnvDenylist 的 *_KEY/*_TOKEN pattern，
// 必須在 spawn 該 runner 時被 allowlist 放行，否則 runner 起不來。
var runnerEnvAllowlists = map[string][]string{
	"claude":  append([]string{"ANTHROPIC_API_KEY", "ANTHROPIC_*", "CLAUDE_*", "CLAUDE_CODE_*"}, altProviderAuthAllowlist...),
	"codex":   {"OPENAI_API_KEY", "OPENAI_*", "CODEX_*"},
	"gemini":  {"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_*", "GEMINI_*"},
	"agy":     append([]string{"ANTHROPIC_API_KEY", "ANTHROPIC_*", "CLAUDE_*"}, altProviderAuthAllowlist...),
	"copilot": {"GITHUB_TOKEN", "GH_TOKEN", "COPILOT_*", "GITHUB_COPILOT_*"},
	"opencode": {
		"ANTHROPIC_*", "OPENAI_*", "OPENROUTER_*", "OPENCODE_*",
		"GROQ_API_KEY", "DEEPSEEK_API_KEY", "MISTRAL_API_KEY", "XAI_API_KEY",
		"GEMINI_API_KEY", "GOOGLE_*", "TOGETHER_API_KEY",
	},
	"agent":  {"CURSOR_*", "ANTHROPIC_*", "OPENAI_*"},
	"cursor": {"CURSOR_*", "ANTHROPIC_*", "OPENAI_*"},
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
// (name, command) 正規化出的 runner 主鍵，回傳 baseRunnerEnvAllowlist（所有 runner 通用，
// 如 GITHUB_TOKEN/GH_TOKEN）疊加該 runner 專屬 glob 清單。主鍵為 runner name、command
// basename 為 fallback（見 canonicalRunnerKey）；未知 runner 仍回傳 baseRunnerEnvAllowlist
// （僅缺 per-runner 專屬項）。
func DefaultRunnerEnvAllowlist(name, command string) []string {
	allow := append([]string{}, baseRunnerEnvAllowlist...)
	if key := canonicalRunnerKey(name, command); key != "" {
		allow = append(allow, runnerEnvAllowlists[key]...)
	}
	return allow
}
