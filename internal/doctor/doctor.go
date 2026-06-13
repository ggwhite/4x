package doctor

import "github.com/ggwhite/4x/internal/protocol"

const ccusageInstallHint = "npm i -g ccusage"

// GenerateReport 產生完整的 doctor 報告，包含所有 runner 健康狀態與 LLM 用量。
// ws 可為 nil（尚未 init），此時 Runners 為空，仍會嘗試取得 usage。
func GenerateReport(ws *protocol.Workspace) DoctorReport {
	report := DoctorReport{}

	if ws != nil {
		cfg, err := ws.ReadConfig()
		if err == nil {
			runners := make(map[string]string, len(cfg.Runners))
			for name, rc := range cfg.Runners {
				runners[name] = rc.Command
			}
			report.Runners = DetectRunners(runners)
		}
	}

	entries, available, _ := FetchUsage()
	report.CcusageAvailable = available
	if available {
		report.Usage = entries
	} else {
		report.CcusageHint = ccusageInstallHint
	}

	return report
}
