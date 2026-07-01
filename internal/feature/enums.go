package feature

// AllStatuses 回傳全部 8 個 Status 常量（enum SoT）。
func AllStatuses() []Status {
	return []Status{
		StatusNotStarted,
		StatusInProgress,
		StatusDone,
		StatusAbandoned,
		StatusBlocked,
		StatusNeedsAttention,
		StatusReadyForReview,
		StatusDraft,
	}
}

// AllSubtaskStatuses 回傳 Subtask.Status 的合法值集合（enum SoT）。
// 依 cmd/4x/subtask.go（寫入路徑）與 internal/mcp/tools.go jsonschema 的權威 5 值：
// 含 ready-for-review（internal/batch/subtask.go 視其為合法完成狀態）。
func AllSubtaskStatuses() []string {
	return []string{
		"not-started",
		"in-progress",
		"blocked",
		"done",
		"ready-for-review",
	}
}
