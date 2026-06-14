package protocol

import (
	"path/filepath"
	"strings"
	"time"
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
)

// Severity 表示 review issue 的嚴重等級
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityLow      Severity = "low"
)

// Status 表示 feature 的狀態，對應 Phase 但面向 dashboard 與人類可讀
type Status string

const (
	StatusNotStarted     Status = "not-started"
	StatusInProgress     Status = "in-progress"
	StatusDone           Status = "done"
	StatusAbandoned      Status = "abandoned"
	StatusBlocked        Status = "blocked"
	StatusNeedsAttention Status = "needs-attention"
	StatusReadyForReview Status = "ready-for-review"
)

// PhaseToStatus 將 state machine 的 Phase 映射為面向 dashboard 的 Status
func PhaseToStatus(phase Phase) Status {
	switch phase {
	case PhasePendingReview:
		return StatusReadyForReview
	case PhaseDone:
		return StatusDone
	case PhaseAbandoned:
		return StatusAbandoned
	case PhaseBlocked:
		return StatusBlocked
	case PhaseNeedsAttention:
		return StatusNeedsAttention
	case PhaseInit:
		return StatusNotStarted
	default:
		return StatusInProgress
	}
}

// HookEntry 描述一個 phase hook 的 shell command 與失敗策略。
// 失敗策略 on_fail 未設定時預設為 "block"（中止 phase 轉換）；
// 設為 "warn" 則只記錄警告，不中止流程。
type HookEntry struct {
	Run    string `json:"run" yaml:"run"`
	OnFail string `json:"on_fail,omitempty" yaml:"on_fail,omitempty"`
}

// EffectiveOnFail 回傳實際的失敗策略，未設定時預設 "block"。
func (h HookEntry) EffectiveOnFail() string {
	if h.OnFail == "" {
		return "block"
	}
	return strings.ToLower(h.OnFail)
}

// Feature 是 features/*.yaml 的結構
type Feature struct {
	ID          string                 `yaml:"id" json:"id"`
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Status      Status                 `yaml:"status" json:"status"`
	Priority    *int                   `yaml:"priority,omitempty" json:"priority,omitempty"`
	Repos       []string               `yaml:"repos,omitempty" json:"repos,omitempty"`
	Subtasks    []Subtask              `yaml:"subtasks,omitempty" json:"subtasks,omitempty"`
	Rules       []string               `yaml:"rules,omitempty" json:"rules,omitempty"`
	Depends     []string               `yaml:"depends,omitempty" json:"depends,omitempty"`
	Spec        string                 `yaml:"spec,omitempty" json:"-"`
	Plan        string                 `yaml:"plan,omitempty" json:"-"`
	Hooks       map[string][]HookEntry `yaml:"hooks,omitempty" json:"hooks,omitempty"`
}

// BacklogMirror 是根目錄 feature_list.json 的 legacy mirror 結構。
type BacklogMirror struct {
	Version  int              `json:"version"`
	Features []BacklogFeature `json:"features"`
}

// BacklogFeature 表示 feature_list.json 中單一 legacy backlog entry。
type BacklogFeature struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Area        string `json:"area,omitempty"`
	Description string `json:"description,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
}

// BacklogDriftKind 表示 feature_list.json 與 .4x/features/*.yaml 的差異類型。
type BacklogDriftKind string

const (
	BacklogDriftMissing  BacklogDriftKind = "missing"
	BacklogDriftExtra    BacklogDriftKind = "extra"
	BacklogDriftMismatch BacklogDriftKind = "mismatch"
)

// BacklogDrift 表示一筆 feature_list.json legacy mirror 漂移結果。
type BacklogDrift struct {
	Kind      BacklogDriftKind `json:"kind"`
	FeatureID string           `json:"featureId"`
	Field     string           `json:"field,omitempty"`
	Canonical string           `json:"canonical,omitempty"`
	Mirror    string           `json:"mirror,omitempty"`
	Message   string           `json:"message"`
}

// Subtask 是 feature 內的子任務
type Subtask struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Status      string   `yaml:"status" json:"status"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Depends     []string `yaml:"depends,omitempty" json:"depends,omitempty"`
}

// State 是 .4x/{feature-id}/state.json 的權威狀態
type State struct {
	FeatureID             string    `json:"featureId"`
	Phase                 Phase     `json:"phase"`
	Role                  Role      `json:"role"`
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
}

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

// Screenshot 是 tester 在 verify.json 記錄的截圖 metadata。
type Screenshot struct {
	Path        string `json:"path"`
	Step        string `json:"step"`
	Description string `json:"description"`
}

// VerifyEvidence 是 rounds/round-N/verify.json 的結構
type VerifyEvidence struct {
	Passed      bool            `json:"passed"`
	Round       int             `json:"round"`
	Role        Role            `json:"role"`
	Commands    []VerifyCommand `json:"commands"`
	Screenshots []Screenshot    `json:"screenshots,omitempty"`
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
	Web         bool         `yaml:"web" json:"web"`
	API         bool         `yaml:"api" json:"api"`
	Gate        bool         `yaml:"gate" json:"gate"`
	CoderOnly   bool         `yaml:"coder_only" json:"coder_only"`
	Verify      []string     `yaml:"verify_commands" json:"verify_commands"`
	HealthCheck *HealthCheck `yaml:"health_check,omitempty" json:"health_check,omitempty"`
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
	Project           ProjectConfig                `json:"project"`
	Runners           map[string]RunnerConfig      `json:"runners"`
	Default           string                       `json:"default_runner"`
	Roles             map[string]RoleConfig        `json:"roles,omitempty"`
	Rules             []string                     `json:"rules,omitempty"`
	HubRepos          []string                     `json:"hub_repos,omitempty"`
	Isolation         string                       `json:"isolation,omitempty"`
	MaxConcurrentRuns int                          `json:"max_concurrent_runs,omitempty"`
	Commit            string                       `json:"commit,omitempty"`
	ModelTiers        map[string]map[string]string `json:"model_tiers,omitempty"`
	Workspace         WorkspaceConfig              `json:"workspace,omitempty"`
	Hooks             map[string][]HookEntry       `json:"hooks,omitempty"`
	// Profiles 定義 pipeline profile（名稱 → 啟用的 role 子集），供依 feature priority
	// 自動選擇或 --profile 手動覆蓋；為空時所有 feature 一律走 full（6 role 全跑）。
	Profiles map[string]ProfileConfig `json:"profiles,omitempty"`
	// ParallelReviewTest 啟用後，reviewer 與 tester 在 reviewing phase 並行執行（共用 worktree）。
	ParallelReviewTest bool `json:"parallel_review_test,omitempty"`
	// HealthCheck 是全域（settings.json）的 testing phase 前環境檢查設定，
	// 未設為 nil（跳過）；可被 per-feature test-strategy.yaml 整組覆蓋。
	HealthCheck *HealthCheck `json:"health_check,omitempty"`
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
}

// DefaultScreenshotDir 是 tester 預設截圖目錄，可用 {feature-id}、{round} 變數。
const DefaultScreenshotDir = ".4x/e2e/{feature-id}/screenshot/"

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
func ResolveFeatureRepoPaths(f Feature, cfg Config, root string) map[string]string {
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
