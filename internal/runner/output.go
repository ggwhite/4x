package runner

import (
	"log/slog"
	"os"
)

// CleanStaleOutputs 在呼叫 runner 前刪除指定的產出檔殘留。
//
// LLM runner 產出檔（如 gate-verdicts.json）在下一輪執行前不會自動消失；若 runner 回 nil
// 卻沒寫新檔，下游會靜默讀到上一輪的舊資料。先清掉這些檔，能讓「沒寫新檔」退化為明確的
// 讀取／parse error，而非吃到 stale data。
//
// 只刪傳入的明確清單，不觸及其他 artifact（如手動建立的 escalation.json）。每次成功刪除記一筆
// slog.Debug 以利排查；檔案不存在視為正常（無聲略過），其餘刪除失敗記 slog.Warn 但不中斷。
func CleanStaleOutputs(paths ...string) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			slog.Warn("failed to remove stale runner output", "path", path, "error", err)
			continue
		}
		slog.Debug("removed stale runner output", "path", path)
	}
}
