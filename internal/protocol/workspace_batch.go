package protocol

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WriteBatchConflict 將 batch auto-merge 衝突信號寫入 .4x/batch-conflict.json，
// 供 dashboard 顯示衝突細節並提供 Continue Batch 操作。
func (w *Workspace) WriteBatchConflict(c BatchConflict) error {
	return writeJSONAtomic(w.DotDir(), BatchConflictFile, ".batch-conflict-*.json", c)
}

// ReadBatchConflict 讀取 .4x/batch-conflict.json；檔案不存在時回 (nil, nil) 代表目前無衝突。
func (w *Workspace) ReadBatchConflict() (*BatchConflict, error) {
	return readJSONOptional[BatchConflict](filepath.Join(w.DotDir(), BatchConflictFile))
}

// ClearBatchConflict 刪除 .4x/batch-conflict.json；檔案不存在時不視為錯誤。
func (w *Workspace) ClearBatchConflict() error {
	return clearFile(filepath.Join(w.DotDir(), BatchConflictFile))
}

// WriteBatchReport 原子寫入 .4x/batch-report.json（同目錄 temp file + os.Rename，仿 WriteState），
// 確保 dashboard 在 batch run 收尾／中斷的同時讀到的永遠是完整 JSON，不會撞上半寫狀態。
func (w *Workspace) WriteBatchReport(r BatchReport) error {
	return writeJSONAtomic(w.DotDir(), BatchReportFile, ".batch-report-*.json", r)
}

// ReadBatchReport 讀取 .4x/batch-report.json；檔案不存在時回 (nil, nil) 代表尚無 batch 報告。
func (w *Workspace) ReadBatchReport() (*BatchReport, error) {
	return readJSONOptional[BatchReport](filepath.Join(w.DotDir(), BatchReportFile))
}

// WriteBatchPID 將 batch run 子程序的 PID 以十進位字串寫入 .4x/batch-pid，
// 供 server 重啟後辨識並 adopt 仍存活的孤兒子程序。
func (w *Workspace) WriteBatchPID(pid int) error {
	return os.WriteFile(filepath.Join(w.DotDir(), BatchPIDFile), []byte(strconv.Itoa(pid)), 0o644)
}

// ReadBatchPID 讀取 .4x/batch-pid 並解析為 PID；檔案不存在時回 (0, nil) 代表目前無 batch run 紀錄，
// 內容無法解析時回 (0, error)。
func (w *Workspace) ReadBatchPID() (int, error) {
	data, err := os.ReadFile(filepath.Join(w.DotDir(), BatchPIDFile))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// ClearBatchPID 刪除 .4x/batch-pid；檔案不存在時不視為錯誤。
func (w *Workspace) ClearBatchPID() error {
	return clearFile(filepath.Join(w.DotDir(), BatchPIDFile))
}

// WriteBatchStop 寫出 batch-stop 信號檔，讓執行中的 batch 在當前 feature 完成後 graceful 停止。
func (w *Workspace) WriteBatchStop() error {
	return os.WriteFile(filepath.Join(w.DotDir(), BatchStopFile), []byte("stop"), 0o644)
}
