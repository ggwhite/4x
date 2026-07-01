package protocol

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

// AngleSelection 是 deep review 角度選擇結果，序列化為 deep-review-angles.json 供 resume 與稽核。
type AngleSelection struct {
	SelectedAngles []int        `json:"selected_angles"`
	TotalAngles    int          `json:"total_angles"`
	ForceAll       bool         `json:"force_all"`
	Matches        []AngleMatch `json:"matches"`
}

// AngleMatch 描述單一變更檔案匹配到的 mapping 規則與其貢獻的 angle 編號。
type AngleMatch struct {
	File   string `json:"file"`
	Rule   string `json:"rule"`
	Angles []int  `json:"angles"`
}
