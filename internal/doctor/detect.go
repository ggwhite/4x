package doctor

import (
	"context"
	"os/exec"
	"regexp"
	"sort"
	"sync"
	"time"
)

var versionRe = regexp.MustCompile(`\d+(\.\d+)+`)

// parseVersion 從 --version 輸出中擷取第一個 semver-like 版本號
func parseVersion(output string) string {
	if len(output) > 200 {
		output = output[:200]
	}
	return versionRe.FindString(output)
}

// DetectRunners 並行偵測每個 runner 的安裝狀態與版本。
// runners 是 name → command 的對應（從 settings.json 的 RunnerConfig.Command 取得）。
func DetectRunners(runners map[string]string) []RunnerHealth {
	results := make([]RunnerHealth, len(runners))
	var wg sync.WaitGroup

	i := 0
	for name, command := range runners {
		results[i] = RunnerHealth{Name: name, Command: command}
		wg.Add(1)
		go func(idx int, cmd string) {
			defer wg.Done()
			detectOne(&results[idx], cmd)
		}(i, command)
		i++
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

func detectOne(rh *RunnerHealth, command string) {
	if _, err := exec.LookPath(command); err != nil {
		return
	}
	rh.Installed = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, command, "--version").CombinedOutput()
	if err == nil {
		rh.Version = parseVersion(string(out))
	}
}
