package protocol

import "time"

// Phase 表示 4x 狀態機的各階段
type Phase string

const (
	PhaseInit           Phase = "init"
	PhaseDesigning      Phase = "designing"
	PhaseCoding         Phase = "coding"
	PhaseReviewing      Phase = "reviewing"
	PhaseTesting        Phase = "testing"
	PhaseAmending       Phase = "amending"
	PhaseAccepting      Phase = "accepting"
	PhasePendingReview  Phase = "pending-review"
	PhaseDone           Phase = "done"
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
)

// Severity 表示 review issue 的嚴重等級
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityLow      Severity = "low"
)

// Feature 是 features/*.yaml 的結構
type Feature struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Status      string            `yaml:"status" json:"status"`
	Priority    int               `yaml:"priority,omitempty" json:"priority,omitempty"`
	Repos       map[string]string `yaml:"repos,omitempty" json:"repos,omitempty"`
	Subtasks    []Subtask         `yaml:"subtasks,omitempty" json:"subtasks,omitempty"`
	Rules       []string          `yaml:"rules,omitempty" json:"rules,omitempty"`
	Depends     []string          `yaml:"depends,omitempty" json:"depends,omitempty"`
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
	Runner                string    `json:"runner"`
	Label                 string    `json:"label,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
	Since                 time.Time `json:"since,omitempty"`
	ConsecutiveNoProgress int       `json:"consecutiveNoProgress"`
	LastFailCount         int       `json:"lastFailCount"`
	StopReason            string    `json:"stopReason,omitempty"`
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

// VerifyEvidence 是 rounds/round-N/verify.json 的結構
type VerifyEvidence struct {
	Passed   bool            `json:"passed"`
	Round    int             `json:"round"`
	Role     Role            `json:"role"`
	Commands []VerifyCommand `json:"commands"`
}

// VerifyCommand 是單一 verify command 的結果
type VerifyCommand struct {
	Command    string    `json:"command"`
	ExitCode   int       `json:"exitCode"`
	DurationMs int64     `json:"durationMs"`
	Summary    string    `json:"summary"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

// TestStrategy 是 test-strategy.yaml 的結構
type TestStrategy struct {
	Web       bool     `yaml:"web" json:"web"`
	API       bool     `yaml:"api" json:"api"`
	Gate      bool     `yaml:"gate" json:"gate"`
	CoderOnly bool     `yaml:"coder_only" json:"coder_only"`
	Verify    []string `yaml:"verify_commands" json:"verify_commands"`
}

// ReviewIssue 是 reviewer 發現的問題
type ReviewIssue struct {
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Detail   string   `json:"detail"`
}

// ReviewResult 是 review verdict 的結構化結果，包含通過與否及 critical issue 計數
type ReviewResult struct {
	Passed        bool `json:"passed"`
	CriticalCount int  `json:"criticalCount"`
}

// Escalation 是 Coder/Tester 觸發 Designer 重新介入
type Escalation struct {
	Needed bool   `json:"needed"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// Config 是 .4x/settings.json 的專案設定
type Config struct {
	Project           ProjectConfig           `json:"project"`
	Runners           map[string]RunnerConfig `json:"runners"`
	Default           string                  `json:"default_runner"`
	Roles             map[string]RoleConfig   `json:"roles,omitempty"`
	Rules             []string                `json:"rules,omitempty"`
	HubRepos          []string                `json:"hub_repos,omitempty"`
	Isolation         string                  `json:"isolation,omitempty"`
	MaxConcurrentRuns int                     `json:"max_concurrent_runs,omitempty"`
	Commit            string                  `json:"commit,omitempty"`
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
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Model   string   `json:"model,omitempty"`
	Stdin   bool     `json:"stdin,omitempty"`
}

// RoleConfig 是各角色的模型與行為設定
type RoleConfig struct {
	Model        string   `json:"model,omitempty"`
	DeepModel    string   `json:"deep_model,omitempty"`
	Instructions []string `json:"instructions,omitempty"`
	Includes     []string `json:"includes,omitempty"`
}

// UserConfig 是 ~/.4x/settings.json 的使用者層級設定
type UserConfig struct {
	Locale string `json:"locale,omitempty"`
}
