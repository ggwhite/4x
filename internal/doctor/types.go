package doctor

// RunnerHealth 是單一 runner 的健康狀態，記錄 CLI 是否安裝及版本號
type RunnerHealth struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
}

// UsageModelBreakdown 是單一 model 的用量明細
type UsageModelBreakdown struct {
	ModelName           string  `json:"modelName"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	Cost                float64 `json:"cost"`
}

// UsageDailyEntry 是 ccusage daily 回傳的單日資料
type UsageDailyEntry struct {
	Period              string                `json:"period"`
	Agent               string                `json:"agent"`
	InputTokens         int64                 `json:"inputTokens"`
	OutputTokens        int64                 `json:"outputTokens"`
	CacheReadTokens     int64                 `json:"cacheReadTokens"`
	CacheCreationTokens int64                 `json:"cacheCreationTokens"`
	TotalTokens         int64                 `json:"totalTokens"`
	TotalCost           float64               `json:"totalCost"`
	ModelsUsed          []string              `json:"modelsUsed"`
	Metadata            map[string]any        `json:"metadata"`
	ModelBreakdowns     []UsageModelBreakdown `json:"modelBreakdowns"`
}

// DoctorReport 是 `4x doctor` 的完整報告，包含 runner 狀態與 LLM 用量
type DoctorReport struct {
	Runners          []RunnerHealth    `json:"runners"`
	Usage            []UsageDailyEntry `json:"usage"`
	CcusageAvailable bool              `json:"ccusageAvailable"`
	CcusageHint      string            `json:"ccusageHint,omitempty"`
	CcusageError     string            `json:"ccusageError,omitempty"`
}
