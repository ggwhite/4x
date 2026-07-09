package protocol

import (
	"encoding/json"

	"github.com/ggwhite/4x/internal/feature"
)

// WorkspaceConfig 描述 multi-repo workspace 的 repo 映射。
// 沒有設定時代表 monorepo 模式。
type WorkspaceConfig struct {
	Repos map[string]RepoConfig `json:"repos,omitempty"`
}

// RepoConfig 描述 workspace 中單一 repo 的設定。
type RepoConfig struct {
	Path string `json:"path"`
	Hub  bool   `json:"hub,omitempty"`
}

// Config 是 .4x/settings.json 的專案設定
type Config struct {
	Project           ProjectConfig                  `json:"project"`
	Runners           map[string]RunnerConfig        `json:"runners"`
	Default           string                         `json:"default_runner"`
	Roles             map[string]RoleConfig          `json:"roles,omitempty"`
	Rules             []string                       `json:"rules,omitempty"`
	HubRepos          []string                       `json:"hub_repos,omitempty"`
	Isolation         string                         `json:"isolation,omitempty"`
	MaxConcurrentRuns int                            `json:"max_concurrent_runs,omitempty"`
	Commit            string                         `json:"commit,omitempty"`
	ModelTiers        map[string]map[string]string   `json:"model_tiers,omitempty"`
	Workspace         WorkspaceConfig                `json:"workspace,omitempty"`
	Hooks             map[string][]feature.HookEntry `json:"hooks,omitempty"`
	// Profiles 定義 pipeline profile（名稱 → 啟用的 phase 子集 + 每 phase 的 runner/model 覆寫），
	// 供依 feature priority 自動選擇或 --profile 手動覆蓋；為空時所有 feature 一律走 full（6 phase 全跑）。
	Profiles map[string]ProfileConfig `json:"profiles,omitempty"`
	// DefaultProfile 指定無 --profile flag 且未 resume 時要採用的 profile 名稱（如 full/normal/quick）。
	// 為空時 fallback 既有行為（無 profiles 區段→full；有→依 priority auto-select）。
	DefaultProfile string `json:"default_profile,omitempty"`
	// ParallelReviewTest 啟用後，reviewer 與 tester 在 reviewing phase 並行執行（共用 worktree）。
	ParallelReviewTest bool `json:"parallel_review_test,omitempty"`
	// HealthCheck 是全域（settings.json）的 testing phase 前環境檢查設定，
	// 未設為 nil（跳過）；可被 per-feature test-strategy.yaml 整組覆蓋。
	HealthCheck *HealthCheck `json:"health_check,omitempty"`
	// TestProfiles 讓專案覆寫或擴充內建 test profile（key 為 profile 名稱）。
	// 與 Profiles（pipeline profile）語意不同，不可混用。
	TestProfiles map[string]TestProfileOverride `json:"test_profiles,omitempty"`
	// AutoDiscoverFeatures 啟用後，run loop 在 final deep review PASS 後會 parse
	// deep-review-report.md 的 [NEW-FEATURE] 標記並自動建立 feature。預設 false。
	AutoDiscoverFeatures bool `json:"auto_discover_features,omitempty"`
	// MaxDiscoveredFeatures 限制單次 run 最多自動建立幾張 feature；未設定或 <= 0 時套預設值（3）。
	MaxDiscoveredFeatures int `json:"max_discovered_features,omitempty"`
	// EnrichDiscoveredFeatures 啟用後，auto-discover 產出的 candidate 會經 LLM enrichment
	// 補齊 subtasks/repos/rules/priority 後才存入 backlog；關閉時走舊路徑直接存薄 feature。預設 false。
	EnrichDiscoveredFeatures bool `json:"enrich_discovered_features,omitempty"`
	// EnrichAutoApprove 控制 enrich 後的 feature 狀態：true → not-started（全自動），
	// false → draft（需人工 approve）。僅在 EnrichDiscoveredFeatures 開啟時有意義。預設 false。
	EnrichAutoApprove bool `json:"enrich_auto_approve,omitempty"`
	// FeatureIDPrefix 設定 feature ID 的前綴字串（如 "F"、"ws-"），預設 "F"。
	FeatureIDPrefix string `json:"feature_id_prefix,omitempty"`
	// FeatureIDDigits 設定 feature ID 流水號的最小位數（零補齊），預設 3。
	FeatureIDDigits int `json:"feature_id_digits,omitempty"`
	// DesignDocDirs 指定設計文件（spec/plan）的額外搜尋目錄，優先於預設 docs/design/。
	// 例如 ["docs/feature"] 會讓 ResolveDesignDoc 先找 docs/feature/{id}-spec.md。
	DesignDocDirs []string `json:"design_doc_dirs,omitempty"`
	// Notifications 控制 run 結束時是否推送 OS 原生通知；用 pointer 區分「未設定」與「明確 false」，
	// nil（未設定）時 NotificationsEnabled 視為啟用。project 端非 nil 會覆蓋 user 端設定。
	Notifications *bool `json:"notifications,omitempty"`
	// SelfMod 設定 meta-loop 跑自己時對「改自己核心地基」變更的額外保護（受保護路徑、單輪 diff 上限、
	// 是否要求對應測試）；nil 時 guard 套用內建預設（見 guard.ResolveSelfMod）。project 端非 nil 會覆蓋 user 端。
	SelfMod *SelfModSettings `json:"self_mod_guard,omitempty"`
	// Evolution 設定 F097 evolve pipeline 的價值閘門與收斂上限；nil 時由 evolution.ResolveEvolution 套全部預設值。
	Evolution *EvolutionConfig `json:"evolution,omitempty"`
	// IssueTracker 控制 `4x new`/`4x done` 是否串接 GitHub/GitLab issue 與 MR/PR。
	// 純 project-level 欄位，預設 false，關閉時所有既有行為零改動。
	IssueTracker IssueTrackerConfig `json:"issue_tracker,omitempty"`
	// Worktree 是 scaffold worktree 相關的設定（如 post-scaffold hook）。
	Worktree WorktreeConfig `json:"worktree,omitempty"`
}

// WorktreeConfig 是 settings.json 內 worktree 區段的設定。
type WorktreeConfig struct {
	// PostScaffold 宣告 scaffold 建立 worktree 後、在 worktree 根目錄依序執行的命令清單；
	// 單一命令失敗只 warn 不中止 scaffold。內容由使用端專案自理（4x 不內建專案特定邏輯）。
	PostScaffold []string `json:"post_scaffold,omitempty"`
}

// IssueTrackerConfig 控制 issue-first MR flow 是否啟用。
type IssueTrackerConfig struct {
	// Enabled 為 true 時，`4x new` 建/連 issue，`4x done` 改成 push + 開 MR/PR 取代本地 squash-merge。
	Enabled bool `json:"enabled"`
}

// EvolutionConfig 是 .4x/settings.json 內 evolution 區段的設定，描述 candidate feature
// 進 backlog 前的價值閘門門檻與收斂上限。各數值欄位為零值時由 evolution.ResolveEvolution 套預設。
type EvolutionConfig struct {
	// ValueFloor 為 gate role 給的 value_score 最低門檻，低於此一律拒；0 時套預設 0.6。
	ValueFloor float64 `json:"value_floor,omitempty"`
	// MaxAcceptPerRun 限制單次 gate 最多接受幾筆 candidate（convergence cap）；0 時套預設 3。
	MaxAcceptPerRun int `json:"max_accept_per_run,omitempty"`
	// MaxBacklogUndone 為 backlog 未做數上限，超過即停止接受新 candidate；0 時套預設 15。
	MaxBacklogUndone int `json:"max_backlog_undone,omitempty"`
	// GateRunner 指定 gate role 用哪個 runner；空字串不補預設（由下游決定）。
	GateRunner string `json:"gate_runner,omitempty"`
	// GateModel 指定 gate role 用哪個 model；空字串用 runner 預設。
	GateModel string `json:"gate_model,omitempty"`
	// DedupThreshold 為 candidate 去重的 Jaccard 門檻；0 時套預設 0.6。
	DedupThreshold float64 `json:"dedup_threshold,omitempty"`
	// MaxIdleRounds 控制 anti-spin 防空轉：連續這麼多輪未接受任何 candidate 即早退停跑。
	// 用 pointer 區分「未設定」（nil → evolution.ResolveEvolution 套預設 3）與「明確設值」；
	// 明確設為 <= 0 表示停用 halt（永遠跑），正數才啟用門檻。
	MaxIdleRounds *int `json:"max_idle_rounds,omitempty"`
	// CandidateMaxIdleDays 為「從未使用的 candidate」老化為 stale 的天數門檻，僅 `4x learn prune` 消費。
	// 用 pointer 區分「未設定」（nil → 套預設 30）與「明確設值」；明確設為 0 表示停用老化，正數為閾值。
	CandidateMaxIdleDays *int `json:"candidate_max_idle_days,omitempty"`
}

// SelfModSettings 是 .4x/settings.json 內 self_mod_guard 區段的設定，
// 描述哪些路徑視為「核心地基」受保護、單輪 diff 行數上限、以及是否要求對應測試。
// 各欄位零值（含 nil）代表「未設定」，由 guard.ResolveSelfMod 退回內建預設。
type SelfModSettings struct {
	// ProtectedPaths 是受保護路徑前綴白名單（相對 scope root），如 "internal/state/"。
	ProtectedPaths []string `json:"protected_paths,omitempty"`
	// MaxDiffLines 是受保護路徑單輪 diff 行數上限，超過即擋下轉 needs-attention；<= 0 視為未設定。
	// 計數只含 production 檔案：*_test.go 的行數不計入（補測不受此上限約束）。
	MaxDiffLines int `json:"max_diff_lines,omitempty"`
	// RequireTests 控制改受保護路徑是否必須附帶對應測試才能進 accepting；
	// 用 pointer 區分「未設定」（預設 true）與「明確 false」。
	RequireTests *bool `json:"require_tests,omitempty"`
}

// PhaseSpec 描述一個 profile 內某個 phase 的設定：是否啟用該 phase，以及覆寫該 phase 的
// runner 與 model tier。Runner / Model 為空時代表繼承下層（feature override → default_runner、
// roles model → runner model → defaultTier）。
type PhaseSpec struct {
	// Phase 必須屬於 profileSelectablePhases 白名單（designing/coding/reviewing/
	// deep-reviewing/testing/accepting）。
	Phase string `json:"phase"`
	// Runner 覆寫此 phase 使用的 runner，空字串代表繼承下層（default_runner）。
	Runner string `json:"runner,omitempty"`
	// Model 覆寫此 phase 的 model tier，空字串代表繼承下層（roles model）。
	Model string `json:"model,omitempty"`
}

// ProfileConfig 描述一個 pipeline profile：啟用哪些 phase、以及每個 phase 的 runner/model 覆寫。
// 未列入 Phases 的 phase（對應的 role）在 run loop 中以 pass-through 方式沿合法 state 邊跳過、
// 不呼叫 runner。coding phase 為唯一必要 phase。
//
// Roles / CoderModel 為已廢棄欄位，僅供向後相容解析：載入舊格式 settings.json 時由 normalize
// 自動轉成 Phases（見 profile.go），不再被任何解析路徑直接讀取。
type ProfileConfig struct {
	// Phases 是啟用的 phase 規格列表；順序不影響行為（執行順序由 canonical pipeline 決定）。
	Phases []PhaseSpec `json:"phases,omitempty"`
	// Roles Deprecated：舊格式啟用的 role 名稱列表，僅供向後相容解析（normalize 轉成 Phases）。
	Roles []string `json:"roles,omitempty"`
	// CoderModel Deprecated：舊格式 coder 的 model tier 覆寫，僅供向後相容解析（轉成 coding phase 的 Model）。
	CoderModel string `json:"coder_model,omitempty"`
}

// UnmarshalJSON 解析 ProfileConfig 並在載入後自動 normalize，
// 把舊格式 Roles[]string / CoderModel 轉成新的 Phases 結構，達成向後相容。
func (pc *ProfileConfig) UnmarshalJSON(data []byte) error {
	type alias ProfileConfig
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*pc = ProfileConfig(a)
	pc.normalize()
	return nil
}

// ProjectConfig 是專案基本設定，包含既有工具鏈的描述
type ProjectConfig struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Language    string   `json:"language,omitempty"`
	Setup       []string `json:"setup,omitempty"`
	Build       []string `json:"build,omitempty"`
	Test        []string `json:"test,omitempty"`
	Lint        []string `json:"lint,omitempty"`
	// DocsCheck 是文件/多語系同步的驗證指令（如 make check-docs-sync、check-i18n）。
	// 由 coding/amending phase 的 docs-gate 於框架層自動執行，結果寫入 docs-gate.json，
	// 讓 coder 在 coder-report 中對這些驗證的聲明可被獨立核對（見 F136）。
	// 未設定時 docs-gate 完全跳過，非 4x 專案（無此類指令）不受影響。
	DocsCheck []string `json:"docs_check,omitempty"`
	Docs      []string `json:"docs,omitempty"`
	Rules     []string `json:"rules,omitempty"`
	Includes  []string `json:"includes,omitempty"`
}

// RunnerConfig 是 LLM runner 的設定
type RunnerConfig struct {
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	Model        string            `json:"model,omitempty"`
	Tiers        map[string]string `json:"tiers,omitempty"`
	Stdin        *bool             `json:"stdin,omitempty"`
	Tty          *bool             `json:"tty,omitempty"`
	Quiet        *bool             `json:"quiet,omitempty"`
	OutputFormat string            `json:"output_format,omitempty"`
	// TransientRetries 設定暫態錯誤（socket closed、connection reset、rate limit、5xx 等）的
	// 自動重試上限：nil 表示用預設 3 次、0 表示停用重試、>0 為自訂上限。
	TransientRetries *int `json:"transient_retries,omitempty"`
}

// RoleConfig 是各角色的模型與行為設定
type RoleConfig struct {
	Model         string   `json:"model,omitempty"`
	DeepModel     string   `json:"deep_model,omitempty"`
	ScreenshotDir string   `json:"screenshot_dir,omitempty"`
	Instructions  []string `json:"instructions,omitempty"`
	Includes      []string `json:"includes,omitempty"`
	// MaxFixRounds 限制 deep-reviewing phase 內自癒循環（mini-coder + re-verifier）
	// 的最大迭代次數，僅對 deep-reviewer role 有意義；未設定時由 ResolveMaxFixRounds 套預設值。
	MaxFixRounds int `json:"max_fix_rounds,omitempty"`
	// ParallelReviewers 設定平行 deep review 要 spawn 幾個 sub-reviewer，僅對 deep-reviewer
	// role 有意義；未設定或 <=1 時走 fallback 單 agent 模式（不分檔、不跑 synthesizer）。
	ParallelReviewers int `json:"parallel_reviewers,omitempty"`
	// AnglesPerReviewer 設定每個 sub-reviewer 負責的 review angle 數量，僅對 deep-reviewer
	// role 有意義；未設定時由 ResolveAnglesPerReviewer 回 0，改用 ceil(總 angle/N) 平均分配。
	AnglesPerReviewer int `json:"angles_per_reviewer,omitempty"`
	// AngleMapping 定義 diff 檔案路徑前綴到 deep review angle 編號的映射，僅對 deep-reviewer
	// role 有意義。key 為路徑前綴（如 "internal/state/"）；以 "*" 開頭的 key 為後綴匹配
	// （如 "*_test.go"）。value 為該路徑應啟動的 angle 編號清單（1-based）。
	// 未設定時由 DefaultAngleMapping 提供預設映射。
	AngleMapping map[string][]int `json:"angle_mapping,omitempty"`
}

// UserConfig 是 ~/.4x/settings.json 的使用者層級設定
type UserConfig struct {
	Locale        string                  `json:"locale,omitempty"`
	Theme         string                  `json:"theme,omitempty"`
	DefaultRunner string                  `json:"default_runner,omitempty"`
	Runners       map[string]RunnerConfig `json:"runners,omitempty"`
	Roles         map[string]RoleConfig   `json:"roles,omitempty"`
	// LogLevel 設定 structured logging 的最低輸出等級（debug/info/warn/error），
	// 為空時預設 "info"；可被環境變數 FOURX_LOG_LEVEL 覆蓋。
	LogLevel string `json:"logLevel,omitempty"`
	// LogRetainDays 設定 ~/.4x/logs/ 下 log 檔的保留天數，
	// 超過此天數的 log 檔會在 Init() 時自動清除；為零時預設 7 天。
	LogRetainDays int `json:"logRetainDays,omitempty"`
	// Notifications 為使用者層級的 OS 通知開關；用 pointer 區分「未設定」與「明確 false」，
	// 會被 project 端非 nil 的設定覆蓋（見 MergeConfig）。
	Notifications *bool `json:"notifications,omitempty"`
	// SelfMod 為使用者層級的 self-mod guard 設定，project 端非 nil 時被覆蓋（見 MergeConfig）。
	SelfMod *SelfModSettings `json:"self_mod_guard,omitempty"`
	// DashboardAuth 控制 4x live dashboard 是否啟用 bearer-token 認證。
	// 用 pointer 區分「未設定」與「明確 false」：nil = 預設啟用（secure by default），
	// false = 停用認證，true = 啟用。
	DashboardAuth *bool `json:"dashboard_auth,omitempty"`
}

// RunnerPreset 描述一個受支援 runner 的預設設定
type RunnerPreset struct {
	Name   string       `json:"name"`
	Config RunnerConfig `json:"config"`
}
