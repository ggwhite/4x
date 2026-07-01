package protocol

import "time"

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
	// SelfModTouched 標記本 feature 最新一輪 coding/amending 是否觸及受保護路徑（self-mod guard）。
	// 由 guard.Check 在 coding 後偵測並持久化，供後續 test-gate 與 done/merge 的人工 approve 關卡讀取。
	SelfModTouched bool `json:"selfModTouched,omitempty"`
	// SelfModPaths 記錄最新一輪觸及的受保護檔案路徑（相對 scope root），test-gate 以此判斷是否附帶對應測試。
	SelfModPaths []string `json:"selfModPaths,omitempty"`
	// SelfModApproved 表示人工已透過 --approve-self-mod 核可此 feature 的受保護路徑變更，允許 merge。
	SelfModApproved bool `json:"selfModApproved,omitempty"`
	// GuardRetries 記錄全域 guard retry 次數（跨 round 累計），超過上限時停止重試。
	GuardRetries int `json:"guardRetries,omitempty"`
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
	// TokensUsed 記錄本次 runner invocation 使用的 token 數量（從 runner log 解析），
	// 0 表示該 runner 未回報或解析失敗。僅 run-end event 填寫。
	TokensUsed int     `json:"tokens_used,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	DurationMs int64   `json:"duration_ms,omitempty"`
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
