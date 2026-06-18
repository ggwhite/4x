package protocol

import (
	"path/filepath"
	"time"

	"github.com/ggwhite/4x/internal/feature"
)

// Phase 表示 4x 狀態機的各階段
type Phase string

const (
	PhaseInit           Phase = "init"
	PhaseDesigning      Phase = "designing"
	PhaseCoding         Phase = "coding"
	PhaseReviewing      Phase = "reviewing"
	PhaseDeepReviewing  Phase = "deep-reviewing"
	PhaseTesting        Phase = "testing"
	PhaseAmending       Phase = "amending"
	PhaseAccepting      Phase = "accepting"
	PhasePendingReview  Phase = "pending-review"
	PhaseDone           Phase = "done"
	PhaseAbandoned      Phase = "abandoned"
	PhaseBlocked        Phase = "blocked"
	PhaseNeedsAttention Phase = "needs-attention"
)

// Role 表示 4x 的四個角色
type Role string

const (
	RoleDesigner     Role = "designer"
	RoleCoder        Role = "coder"
	RoleReviewer     Role = "reviewer"
	RoleDeepReviewer Role = "deep-reviewer"
	RoleTester       Role = "tester"
	RoleAcceptor     Role = "acceptor"
	// RoleMiniCoder 與 RoleReVerifier 是 deep-reviewing phase 內自癒循環的子 role，
	// 不對應任何 state machine phase（全程維持 deep-reviewing），僅用於 prompt template
	// 與 event/log 辨識。
	RoleMiniCoder  Role = "mini-coder"
	RoleReVerifier Role = "re-verifier"
	// RoleSynthesizer 是平行 deep review 模式下合併多份 partial report 的子 role，
	// 同樣全程維持 deep-reviewing phase，僅用於 prompt template 與 event/log 辨識。
	RoleSynthesizer Role = "synthesizer"
)

// SubPhase 表示 deep-reviewing phase 內的子步驟，僅在 phase==deep-reviewing 時有意義；
// 其餘 phase 一律為空字串。用於 dashboard 顯示細部進度與 crash recovery 推斷重跑起點。
type SubPhase string

const (
	SubPhaseReviewing    SubPhase = "reviewing"    // sub-reviewer 平行審查中
	SubPhaseSynthesizing SubPhase = "synthesizing" // synthesizer 合併 partial 中
	SubPhaseFixing       SubPhase = "fixing"       // mini-coder 修正 issue 中
	SubPhaseReverifying  SubPhase = "reverifying"  // re-verifier 複驗中
)

// DeepReviewAngleCount 是 deep-reviewer 模板定義的 review angle 總數（angle 1..11）。
// 平行 deep review 依此把 angle 平均分配給各 sub-reviewer。
const DeepReviewAngleCount = 11

// Severity 表示 review issue 的嚴重等級
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityLow      Severity = "low"
)

// PhaseToStatus 將 state machine 的 Phase 映射為面向 dashboard 的 feature.Status
func PhaseToStatus(phase Phase) feature.Status {
	switch phase {
	case PhasePendingReview:
		return feature.StatusReadyForReview
	case PhaseDone:
		return feature.StatusDone
	case PhaseAbandoned:
		return feature.StatusAbandoned
	case PhaseBlocked:
		return feature.StatusBlocked
	case PhaseNeedsAttention:
		return feature.StatusNeedsAttention
	case PhaseInit:
		return feature.StatusNotStarted
	default:
		return feature.StatusInProgress
	}
}

// State 是 .4x/{feature-id}/state.json 的權威狀態
type State struct {
	FeatureID             string    `json:"featureId"`
	Phase                 Phase     `json:"phase"`
	Role                  Role      `json:"role"`
	SubPhase              SubPhase  `json:"subPhase,omitempty"`
	Round                 int       `json:"round"`
	MaxRounds             int       `json:"maxRounds"`
	Active                bool      `json:"active"`
	Pid                   int       `json:"pid,omitempty"`
	Runner                string    `json:"runner"`
	Label                 string    `json:"label,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
	Since                 time.Time `json:"since,omitempty"`
	ConsecutiveNoProgress int       `json:"consecutiveNoProgress"`
	LastFailCount         int       `json:"lastFailCount"`
	StopReason            string    `json:"stopReason,omitempty"`
	StopMessage           string    `json:"stopMessage,omitempty"`
	Runners               []string  `json:"runners,omitempty"`
	// Profile 記錄本次 run 使用的 pipeline profile 名稱，供 dashboard 顯示與 resume 沿用。
	Profile string `json:"profile,omitempty"`
}

// Event 是 events.jsonl 的一行
type Event struct {
	Timestamp string `json:"ts"`
	Phase     Phase  `json:"phase"`
	Type      string `json:"type"`
	Role      Role   `json:"role,omitempty"`
	Round     int    `json:"round,omitempty"`
	Action    string `json:"action,omitempty"`
	Command   string `json:"cmd,omitempty"`
	Status    string `json:"status,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Runner    string `json:"runner,omitempty"`
	Model     string `json:"model,omitempty"`
	// Notify 為前端統一判斷通知等級的提示，值域 NotifySuccess / NotifyError / NotifyWarning，
	// 空字串代表不通知。新增欄位向下相容（omitempty），不改既有 event 的 Status 語意。
	Notify string `json:"notify,omitempty"`
}

// 通知等級常量，供 server 端標注 Event.Notify 及前端判斷顯示樣式，避免散落字串。
const (
	NotifySuccess = "success" // run 正常結束（done / pending-review）
	NotifyError   = "error"   // 失敗或 guard 攔截
	NotifyWarning = "warning" // 中斷或 escalation
)

// Baseline 是 baseline.json 的結構
type Baseline struct {
	CreatedAt time.Time      `json:"createdAt"`
	Repos     []BaselineRepo `json:"repos"`
}

// BaselineRepo 是 baseline 中每個 repo 的快照
type BaselineRepo struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Branch     string   `json:"branch"`
	Head       string   `json:"head"`
	DirtyFiles []string `json:"dirtyFiles"`
}

// VerifyEvidence 是 rounds/round-N/verify.json 的結構
type VerifyEvidence struct {
	Passed      bool                 `json:"passed"`
	Round       int                  `json:"round"`
	Role        Role                 `json:"role"`
	Commands    []VerifyCommand      `json:"commands"`
	Screenshots []feature.Screenshot `json:"screenshots,omitempty"`
}

// VerifyCommand 是單一 verify command 的結果
type VerifyCommand struct {
	Command          string    `json:"command"`
	ExitCode         int       `json:"exitCode"`
	ExpectedExitCode *int      `json:"expectedExitCode,omitempty"`
	DurationMs       int64     `json:"durationMs"`
	Summary          string    `json:"summary"`
	StartedAt        time.Time `json:"startedAt"`
	FinishedAt       time.Time `json:"finishedAt"`
	Group            string    `json:"group,omitempty"`
	Skipped          bool      `json:"skipped,omitempty"`
}

// HealthCheck 是 testing phase 啟動前的環境檢查設定。
// Commands 逐一執行任一失敗即停；失敗時若有 Recovery 則逐一執行後重跑一次 Commands。
// Timeout 為每個 command 的逾時秒數，未設定（0）時由呼叫端套用預設 30 秒。
type HealthCheck struct {
	Commands []string `yaml:"commands" json:"commands"`
	Recovery []string `yaml:"recovery,omitempty" json:"recovery,omitempty"`
	Timeout  int      `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// TestStrategy 是 test-strategy.yaml 的結構
type TestStrategy struct {
	Web          bool                `yaml:"web" json:"web"`
	API          bool                `yaml:"api" json:"api"`
	Gate         bool                `yaml:"gate" json:"gate"`
	CoderOnly    bool                `yaml:"coder_only" json:"coder_only"`
	Verify       []string            `yaml:"verify_commands" json:"verify_commands"`
	HealthCheck  *HealthCheck        `yaml:"health_check,omitempty" json:"health_check,omitempty"`
	VerifyGroups map[string][]string `yaml:"verify_groups,omitempty" json:"verify_groups,omitempty"`
	// Profiles 標記本 feature 適用的測試 profile（如 unit/web/api/e2e），
	// Tester prompt 會依此自動注入對應的測試方法論；為空時行為與舊版一致。
	Profiles []string `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

// TestProfileOverride 允許專案在 settings.json 覆寫或新增 test profile。
// Content 直接指定內容；Include 指定相對於 workspace root 的檔案路徑。兩者擇一，整組取代內建。
type TestProfileOverride struct {
	Content string `json:"content,omitempty"`
	Include string `json:"include,omitempty"`
}

// ReviewIssue 是 reviewer 發現的問題
type ReviewIssue struct {
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Detail   string   `json:"detail"`
}

// ReviewResult 是 review verdict 的結構化結果，包含通過與否及 critical/warning issue 計數
type ReviewResult struct {
	Passed        bool `json:"passed"`
	CriticalCount int  `json:"criticalCount"`
	WarningCount  int  `json:"warningCount"`
}

// Escalation 是 Coder/Tester 觸發 Designer 重新介入
type Escalation struct {
	Needed bool   `json:"needed"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// BatchConflict 是 batch auto-merge 遇衝突暫停時寫入的信號（.4x/batch-conflict.json）。
// dashboard 讀此檔得知是哪個 feature、哪個 repo、哪些檔案發生衝突，供使用者解完後 Continue Batch。
type BatchConflict struct {
	FeatureID    string    `json:"featureId"`
	FeatureName  string    `json:"featureName"`
	ConflictRepo string    `json:"conflictRepo"`
	Files        []string  `json:"files"`
	DetectedAt   time.Time `json:"detectedAt"`
}

// Batch run 的結束結果（BatchReport.Outcome）列舉值，描述「批次如何終止」而非「是否全數成功」。
// 即使 outcome=completed，仍可能有 feature 失敗被跳過（看 Failed/Remaining 計數判斷實際結果）。
const (
	BatchOutcomeCompleted   = "completed"   // 主迴圈自然跑完排程（含失敗達上限被跳過），非被停止/中斷/crash
	BatchOutcomeStopped     = "stopped"     // 使用者按 Stop / 衝突暫停等 graceful 提前結束
	BatchOutcomeInterrupted = "interrupted" // 收到 SIGTERM/SIGINT 中斷
	BatchOutcomeCrashed     = "crashed"     // 行程 panic
)

// BatchReport 是一次 batch run 結束後（正常 / stop / interrupt / crash）寫入 .4x/batch-report.json
// 的整體報告。dashboard 在 batch 沒在跑時讀此檔顯示「上次 batch 報告」摘要與每個 feature 的最終狀態。
type BatchReport struct {
	StartedAt      time.Time            `json:"startedAt"`
	FinishedAt     time.Time            `json:"finishedAt"`
	DurationMs     int64                `json:"durationMs"`
	Outcome        string               `json:"outcome"`
	Total          int                  `json:"total"`
	Completed      int                  `json:"completed"`
	Failed         int                  `json:"failed"`
	Remaining      int                  `json:"remaining"`
	Runner         string               `json:"runner"`
	Features       []BatchFeatureReport `json:"features"`
	PanicMessage   string               `json:"panicMessage,omitempty"`   // 僅 crashed
	RunningFeature string               `json:"runningFeature,omitempty"` // interrupted/crashed 時正在跑的 feature id
}

// BatchFeatureReport 是 BatchReport 內單一 feature 的最終狀態快照。
type BatchFeatureReport struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	FinalStatus feature.Status `json:"finalStatus"`
	DurationMs  int64          `json:"durationMs"`
	Rounds      int            `json:"rounds"`
	StopReason  string         `json:"stopReason,omitempty"`
}

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
	// Profiles 定義 pipeline profile（名稱 → 啟用的 role 子集），供依 feature priority
	// 自動選擇或 --profile 手動覆蓋；為空時所有 feature 一律走 full（6 role 全跑）。
	Profiles map[string]ProfileConfig `json:"profiles,omitempty"`
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
	// DesignDocDirs 指定設計文件（spec/plan）的額外搜尋目錄，優先於預設 docs/design/。
	// 例如 ["docs/feature"] 會讓 ResolveDesignDoc 先找 docs/feature/{id}-spec.md。
	DesignDocDirs []string `json:"design_doc_dirs,omitempty"`
	// Notifications 控制 run 結束時是否推送 OS 原生通知；用 pointer 區分「未設定」與「明確 false」，
	// nil（未設定）時 NotificationsEnabled 視為啟用。project 端非 nil 會覆蓋 user 端設定。
	Notifications *bool `json:"notifications,omitempty"`
}

// NotificationsEnabled 回報合併後設定是否啟用 OS 通知。
// Notifications 為 nil（使用者未設定）時預設啟用，回傳 true。
func NotificationsEnabled(cfg Config) bool {
	if cfg.Notifications == nil {
		return true
	}
	return *cfg.Notifications
}

// ProfileConfig 描述一個 pipeline profile：啟用哪些 role、以及 coder 的 model tier 覆蓋。
// 被停用的 role 在 run loop 中以 pass-through 方式沿合法 state 邊跳過、不呼叫 runner。
type ProfileConfig struct {
	// Roles 是啟用的 role 名稱列表；順序不影響行為（執行順序由 canonical pipeline 決定）。
	// 必須包含 "coder"（唯一必要 role）。
	Roles []string `json:"roles"`
	// CoderModel 覆蓋 roles.coder.model 的 tier；為空時沿用既有 coder model 設定。
	CoderModel string `json:"coder_model,omitempty"`
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
	Docs        []string `json:"docs,omitempty"`
	Rules       []string `json:"rules,omitempty"`
	Includes    []string `json:"includes,omitempty"`
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
}

// RunnerPreset 描述一個受支援 runner 的預設設定
type RunnerPreset struct {
	Name   string       `json:"name"`
	Config RunnerConfig `json:"config"`
}

// SupportedRunners 回傳所有受支援 runner 的預設設定，作為 init 和 dashboard 的單一真相源
func SupportedRunners() []RunnerPreset {
	return []RunnerPreset{
		{Name: "claude", Config: RunnerConfig{
			Command:      "claude",
			Args:         []string{"--dangerously-skip-permissions", "-p", "{prompt}", "--output-format", "stream-json", "--verbose"},
			Model:        "opus",
			OutputFormat: "stream-json",
			Tiers:        map[string]string{"opus": "opus", "sonnet": "sonnet"},
		}},
		{Name: "codex", Config: RunnerConfig{
			Command: "codex",
			Args:    []string{"exec"},
			Stdin:   BoolPtr(true),
			Quiet:   BoolPtr(true),
			Tiers:   map[string]string{"opus": "gpt-5.5", "sonnet": "gpt-5.5"},
		}},
		{Name: "gemini", Config: RunnerConfig{
			Command: "gemini",
			Args:    []string{"-y", "-p", "{prompt}"},
			Tiers:   map[string]string{"opus": "gemini-2.5-flash", "sonnet": "gemini-2.5-flash"},
		}},
		{Name: "agy", Config: RunnerConfig{
			Command: "agy",
			Args:    []string{"--dangerously-skip-permissions", "-p", "{prompt}"},
			Tiers:   map[string]string{"opus": "claude-opus-4-6-thinking", "sonnet": "claude-sonnet-4-6-thinking"},
		}},
		{Name: "copilot", Config: RunnerConfig{
			Command: "copilot",
			Args:    []string{"--yolo", "-p", "{prompt}"},
			Tiers:   map[string]string{"opus": "auto", "sonnet": "auto"},
		}},
		{Name: "cursor", Config: RunnerConfig{
			Command: "agent",
			Args:    []string{"-p", "{prompt}"},
		}},
	}
}

// SupportedRunnerMap 回傳 name → RunnerConfig 的 map
func SupportedRunnerMap() map[string]RunnerConfig {
	m := make(map[string]RunnerConfig)
	for _, p := range SupportedRunners() {
		m[p.Name] = p.Config
	}
	return m
}

// BoolPtr 建立 *bool 指標，用於 RunnerConfig 布林欄位的初始化
func BoolPtr(b bool) *bool {
	return &b
}

// BoolVal 安全取 *bool 的值，nil 視為 false
func BoolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

// ResolveRepoPaths 從 workspace config 解析 repo name → absolute path。
// monorepo 模式回傳 {"." : root}。
func ResolveRepoPaths(cfg Config, root string) map[string]string {
	if len(cfg.Workspace.Repos) == 0 {
		return map[string]string{".": root}
	}
	paths := make(map[string]string, len(cfg.Workspace.Repos))
	for name, rc := range cfg.Workspace.Repos {
		paths[name] = filepath.Join(root, rc.Path)
	}
	return paths
}

// ResolveFeatureRepoPaths 解析 feature 涉及的 repo name → absolute path。
// feature.Repos 為空時：multi-repo 回傳所有 workspace repos，monorepo 回傳 {".": root}。
func ResolveFeatureRepoPaths(f feature.Feature, cfg Config, root string) map[string]string {
	all := ResolveRepoPaths(cfg, root)
	if len(f.Repos) == 0 {
		return all
	}
	result := make(map[string]string, len(f.Repos))
	for _, name := range f.Repos {
		if p, ok := all[name]; ok {
			result[name] = p
		}
	}
	return result
}

// EffectiveHubRepos 合併 Config.HubRepos 與 workspace config 中 Hub: true 的 repo。
func EffectiveHubRepos(cfg Config) []string {
	seen := make(map[string]bool)
	var hubs []string
	for _, h := range cfg.HubRepos {
		if !seen[h] {
			seen[h] = true
			hubs = append(hubs, h)
		}
	}
	for name, rc := range cfg.Workspace.Repos {
		if rc.Hub && !seen[name] {
			seen[name] = true
			hubs = append(hubs, name)
		}
	}
	return hubs
}
