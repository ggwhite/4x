package doctor

// RunnerHealth 是單一 runner 的健康狀態，記錄 CLI 是否安裝及版本號
type RunnerHealth struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
}

// TokenCounts 是各類 token 的用量明細
type TokenCounts struct {
	InputTokens              int64 `json:"inputTokens"`
	OutputTokens             int64 `json:"outputTokens"`
	CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
}

// BurnRate 是目前的消耗速率
type BurnRate struct {
	CostPerHour     float64 `json:"costPerHour"`
	TokensPerMinute float64 `json:"tokensPerMinute"`
}

// Projection 是當前 block 到結束的預估
type Projection struct {
	RemainingMinutes int     `json:"remainingMinutes"`
	TotalCost        float64 `json:"totalCost"`
	TotalTokens      int64   `json:"totalTokens"`
}

// UsageBlock 是 ccusage blocks --active 回傳的單一 5h block
type UsageBlock struct {
	ID          string      `json:"id"`
	StartTime   string      `json:"startTime"`
	EndTime     string      `json:"endTime"`
	ActualEnd   string      `json:"actualEndTime"`
	IsActive    bool        `json:"isActive"`
	CostUSD     float64     `json:"costUSD"`
	TotalTokens int64       `json:"totalTokens"`
	Entries     int         `json:"entries"`
	Models      []string    `json:"models"`
	TokenCounts TokenCounts `json:"tokenCounts"`
	BurnRate    BurnRate    `json:"burnRate"`
	Projection  Projection  `json:"projection"`
}

// UsageSummary 是一段期間的用量彙總（非 Claude runner 使用）
type UsageSummary struct {
	TotalTokens int64   `json:"totalTokens"`
	TotalCost   float64 `json:"totalCost"`
	Days        int     `json:"days"`
}

// RunnerUsage 是單一 runner 的完整資訊（健康 + 用量）
type RunnerUsage struct {
	RunnerHealth
	Block5h  *UsageBlock   `json:"block5h,omitempty"`
	Block7d  *UsageBlock   `json:"block7d,omitempty"`
	Recent7d *UsageSummary `json:"recent7d,omitempty"`
}

// DoctorReport 是 `4x doctor` 的完整報告
type DoctorReport struct {
	Runners          []RunnerUsage `json:"runners"`
	CcusageAvailable bool          `json:"ccusageAvailable"`
	CcusageHint      string        `json:"ccusageHint,omitempty"`
	CcusageError     string        `json:"ccusageError,omitempty"`
}
