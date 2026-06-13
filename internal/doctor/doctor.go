package doctor

import (
	"sync"

	"github.com/ggwhite/4x/internal/protocol"
)

const ccusageInstallHint = "npm i -g ccusage"

// GenerateReport 產生完整的 doctor 報告，包含所有 runner 健康狀態與 LLM 用量。
// ws 可為 nil（尚未 init），此時只讀 user config 的 runners。
func GenerateReport(ws *protocol.Workspace) DoctorReport {
	report := DoctorReport{}

	runners := make(map[string]string)

	userCfg, err := protocol.ReadUserConfig()
	if err == nil {
		for name, rc := range userCfg.Runners {
			runners[name] = rc.Command
		}
	}

	if ws != nil {
		cfg, err := ws.ReadConfig()
		if err == nil {
			for name, rc := range cfg.Runners {
				runners[name] = rc.Command
			}
		}
	}

	if len(runners) == 0 {
		report.CcusageAvailable = CcusageAvailable()
		if !report.CcusageAvailable {
			report.CcusageHint = ccusageInstallHint
		}
		return report
	}

	health := DetectRunners(runners)
	report.CcusageAvailable = CcusageAvailable()
	if !report.CcusageAvailable {
		report.CcusageHint = ccusageInstallHint
		for _, h := range health {
			report.Runners = append(report.Runners, RunnerUsage{RunnerHealth: h})
		}
		return report
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	results := make([]RunnerUsage, len(health))

	for i, h := range health {
		results[i] = RunnerUsage{RunnerHealth: h}
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			r := FetchRunnerUsage(name)
			mu.Lock()
			defer mu.Unlock()
			results[idx].Block5h = r.Block5h
			results[idx].Block7d = r.Block7d
			results[idx].Recent7d = r.Daily7d
			if r.Err != nil && report.CcusageError == "" {
				report.CcusageError = r.Err.Error()
			}
		}(i, h.Name)
	}
	wg.Wait()

	report.Runners = results
	return report
}
