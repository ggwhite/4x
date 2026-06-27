package prompt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeepReviewPartialName 回傳平行 deep review 中第 index 個 sub-reviewer 的 partial report
// 檔名（deep-review-partial-<index>.md，index 為 1-based）。
func DeepReviewPartialName(index int) string {
	return fmt.Sprintf("deep-review-partial-%d.md", index)
}

// DeepPartialComplete 判斷單一 deep-review-partial 檔是否已完整寫出。
// 完整判準：檔案存在、去空白後非空，且含 partial 模板的結尾段落標記 `## Statistics`。
func DeepPartialComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return strings.Contains(string(data), "## Statistics")
}

// MissingDeepPartials 回傳 1..want 中尚未完整的 partial index 清單（升冪）。
func MissingDeepPartials(roundDir string, want int) []int {
	var missing []int
	for i := 1; i <= want; i++ {
		if !DeepPartialComplete(filepath.Join(roundDir, DeepReviewPartialName(i))) {
			missing = append(missing, i)
		}
	}
	return missing
}

// WithParallelDeepReviewer 把第 index/count 個 sub-reviewer 的 angle 指派與 partial report
// 檔名注入 Data，讓 deep-reviewer 模板只 render 被分配的 angle 並輸出到 partial report。
func WithParallelDeepReviewer(index, count int, angles []int, partialName string) Option {
	return func(d *Data) {
		d.ReviewerIndex = index
		d.ReviewerCount = count
		d.AssignedAngles = angles
		d.PartialReportName = partialName
	}
}

// WithSelectedAngles 把選中的 angle 編號注入 Data，讓 deep-reviewer 模板只 render 被選中的角度。
// 與 WithParallelDeepReviewer 不同，此 Option 不設定平行 metadata（ReviewerCount/ReviewerIndex），
// 適用於單 agent deep review 僅想限縮角度的場景。
func WithSelectedAngles(angles []int) Option {
	return func(d *Data) {
		d.AssignedAngles = angles
	}
}

// WithSynthesizerReports 把所有 sub-reviewer 的 partial report 完整內文注入 Data，
// 供 synthesizer 模板內嵌合併。
func WithSynthesizerReports(reports []IncludeContent) Option {
	return func(d *Data) {
		d.ReviewerCount = len(reports)
		d.PartialReports = reports
	}
}
