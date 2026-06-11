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
	FeatureID              string     `json:"featureId"`
	Phase                  Phase      `json:"phase"`
	Role                   Role       `json:"role"`
	Round                  int        `json:"round"`
	MaxRounds              int        `json:"maxRounds"`
	Active                 bool       `json:"active"`
	Runner                 string     `json:"runner"`
	Label                  string     `json:"label,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
	Since                  time.Time  `json:"since,omitempty"`
	ConsecutiveNoProgress  int        `json:"consecutiveNoProgress"`
	LastFailCount          int        `json:"lastFailCount"`
	StopReason             string     `json:"stopReason,omitempty"`
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
	Name       string `json:"name"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	Head       string `json:"head"`
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

// Config 是 .4x/config.yaml 的專案設定
type Config struct {
	Project  ProjectConfig            `yaml:"project"`
	Runners  map[string]RunnerConfig  `yaml:"runners"`
	Default  string                   `yaml:"default_runner"`
	Roles    map[string]RoleConfig    `yaml:"roles,omitempty"`
	Rules    []string                 `yaml:"rules,omitempty"`
	HubRepos []string                 `yaml:"hub_repos,omitempty"`
}

// ProjectConfig 是專案基本設定，包含既有工具鏈的描述
type ProjectConfig struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Language    string   `yaml:"language,omitempty"`
	Setup       []string `yaml:"setup,omitempty"`
	Build       []string `yaml:"build,omitempty"`
	Test        []string `yaml:"test,omitempty"`
	Lint        []string `yaml:"lint,omitempty"`
	Docs        []string `yaml:"docs,omitempty"`
	Rules       []string `yaml:"rules,omitempty"`
}

// RunnerConfig 是 LLM runner 的設定
type RunnerConfig struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Model   string   `yaml:"model,omitempty"`
}

// RoleConfig 是各角色的模型設定
type RoleConfig struct {
	Model     string `yaml:"model,omitempty"`
	DeepModel string `yaml:"deep_model,omitempty"`
}
