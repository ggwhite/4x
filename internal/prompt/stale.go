package prompt

import (
	"os"
	"path/filepath"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// StaleReportInfo 描述 resume 情境下偵測到的「本階段報告 mtime 落後本輪最新程式碼變更」事實，
// 供 template 注入強制警示。所有時間欄位為已格式化字串（RFC3339），供人類閱讀。
type StaleReportInfo struct {
	ReportName        string // 過期報告檔名，如 review-report.md
	ReportModTime     string // 報告 mtime（RFC3339）
	CodeChangeName    string // 作為基準的 code-change artifact 檔名，如 coder-report.md
	CodeChangeModTime string // 該 artifact mtime（RFC3339）
}

// staleReportTarget 解析目標角色對應的本階段報告檔名與所在目錄。
// 只對「消費程式碼/diff 並產出結論報告、且執行順序在 coding 之後」的角色回傳非空 reportName；
// 其餘角色（coder/fixer/mini-coder/designer/design-reviewer）回傳空字串代表不偵測。
func staleReportTarget(ws *protocol.Workspace, featureID string, role protocol.Role, round int) (reportName, dir string) {
	switch role {
	case protocol.RoleReviewer:
		return protocol.ReviewReport, ws.RoundDir(featureID, round)
	case protocol.RoleTester:
		return protocol.TestReport, ws.RoundDir(featureID, round)
	case protocol.RoleDeepReviewer:
		return protocol.DeepReviewReport, ws.RoundDir(featureID, round)
	case protocol.RoleAcceptor:
		return protocol.FinalReport, ws.FeatureDir(featureID)
	default:
		return "", ""
	}
}

// newestCodeChangeMtime 回傳 round 目錄下 coder-report.md / fixer-report.md 中存在者的最大 mtime、
// 對應檔名與是否有任一存在。兩者皆不存在時回傳零值與 false（無程式碼變更基準）。
func newestCodeChangeMtime(roundDir string) (time.Time, string, bool) {
	var newest time.Time
	var name string
	found := false
	for _, candidate := range []string{protocol.CoderReport, protocol.FixerReport} {
		info, err := os.Stat(filepath.Join(roundDir, candidate))
		if err != nil {
			continue
		}
		if !found || info.ModTime().After(newest) {
			newest = info.ModTime()
			name = candidate
			found = true
		}
	}
	return newest, name, found
}

// detectStaleReport 在 resume 情境下判斷本階段報告是否 mtime 落後本輪最新程式碼變更。
// 回傳非 nil 代表偵測到過期；報告不存在、非目標角色、或無程式碼變更基準時回傳 nil。
// 比對採嚴格大於（codeMtime.After(reportMtime)），mtime 相等視為不過期，避免同秒寫入造成 false positive。
func detectStaleReport(ws *protocol.Workspace, featureID string, role protocol.Role, round int) *StaleReportInfo {
	reportName, dir := staleReportTarget(ws, featureID, role, round)
	if reportName == "" {
		return nil
	}
	reportInfo, err := os.Stat(filepath.Join(dir, reportName))
	if err != nil {
		return nil
	}
	codeMtime, codeName, ok := newestCodeChangeMtime(ws.RoundDir(featureID, round))
	if !ok {
		return nil
	}
	if !codeMtime.After(reportInfo.ModTime()) {
		return nil
	}
	return &StaleReportInfo{
		ReportName:        reportName,
		ReportModTime:     reportInfo.ModTime().Format(time.RFC3339),
		CodeChangeName:    codeName,
		CodeChangeModTime: codeMtime.Format(time.RFC3339),
	}
}
