package logging

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cleanOldLogs 刪除 dir 目錄下超過 retainDays 天的 4x-YYYY-MM-DD.log 檔案。
// 僅處理符合命名格式的 .log 檔，其他檔案不受影響。
func cleanOldLogs(dir string, retainDays int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retainDays)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, "4x-") || !strings.HasSuffix(name, ".log") {
			continue
		}

		// 從檔名解析日期：4x-YYYY-MM-DD.log
		dateStr := strings.TrimPrefix(name, "4x-")
		dateStr = strings.TrimSuffix(dateStr, ".log")

		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

		if fileDate.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}
