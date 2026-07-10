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
	// BaseCommit 記錄 feature 首次進入 coding phase 時的 HEAD commit sha，供 review-package.md
	// 計算 diff 使用。amending 階段沿用同一個 base，讓 review package 累積本 feature 的完整 diff。
	BaseCommit string `json:"baseCommit,omitempty"`
	// ManualPhase 標記本次 phase 是由 `4x transition` / `4x retry` 手動設定，供 RecoverState
	// 尊重人為介入：偵測到此旗標時直接照 state.json 的 phase 派對應 role，不進 SmartResumePhase
	// 依磁碟 artifacts 重推導，並在消費後清除（避免下一輪真 crash 時仍被當成人為介入）。
	// 正常 run loop 內部的 state.Transition 不設置此旗標，故 recovery 對真 crash 的行為維持不變。
	ManualPhase bool `json:"manualPhase,omitempty"`
	// ParallelReview 是 per-run 訊號：為 true 表示本輪 reviewer 與 tester 正並行執行於同一個
	// reviewing phase（parallel_review_test 模式）。tester agent 的自保啟發式會讀 state.json，
	// 若發現 phase=reviewing/role=reviewer 而非 tester 便可能拒絕執行；此旗標讓 tester/reviewer
	// 的 prompt 能明確識別「並行執行合法」。生命週期由 RunReviewTestParallel 掌控：進入並行段落
	// 前（SyncFeatureToWorktree 之前）設為 true 並寫入 main state.json 以傳播到 worktree；兩者
	// 完成後、任何 transition 之前設回 false，確保標記不外漏到後續 phase。序列路徑不設此欄位。
	ParallelReview bool `json:"parallelReview,omitempty"`
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
	// Index 標記同一 round 內平行執行的同 role 事件屬於第幾個子任務（1-based），
	// 目前僅 deep-reviewer 的平行 sub-reviewer 使用。多個平行事件共用同一個 Role
	// 字串時，消費端不能再用單一遞增計數器區分 phase-start/run-end 配對（多個
	// phase-start 會連續推高計數器，導致之後的 run-end 全部收斂到同一格互相覆蓋），
	// 必須靠這個明確的索引還原正確配對。0（zero value）表示不適用，沿用既有的
	// 循序重試（如 designer 被打回重做）計數器邏輯。
	Index int `json:"index,omitempty"`
	// Codex 記錄 codex runner 本次 invocation 的即時額度用量（取自 rollout jsonl），
	// 僅 codex 的 run-end event 填寫。整個指標為 nil 代表未觀測（claude run-end 或解析
	// 失敗）；非 nil 時 PrimaryPercent 為 0.0 是真值（窗剛重置），故用指標而非零值語意。
	Codex *CodexUsage `json:"codex,omitempty"`
}

// CodexUsage 記錄 codex runner 一次 invocation 的即時額度用量，取自 rollout jsonl
// 的 rate_limits。codex 走 ChatGPT 訂閱制、無 USD 計量，故只記百分比與重置時間。
type CodexUsage struct {
	PrimaryPercent    float64 `json:"primary_pct"`                 // 5 小時窗 used_percent（window_minutes=300）
	SecondaryPercent  float64 `json:"secondary_pct"`               // 週窗 used_percent（window_minutes=10080）
	PrimaryResetsAt   int64   `json:"primary_resets_at,omitempty"` // 5 小時窗 resets_at（unix 秒）
	SecondaryResetsAt int64   `json:"secondary_resets_at,omitempty"`
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
