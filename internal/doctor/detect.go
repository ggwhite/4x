package doctor

import (
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strings"
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

// DetectRunners 偵測每個 runner 的安裝狀態與版本。
// runners 是 name → command 的對應（從 settings.json 的 RunnerConfig.Command 取得）。
// 使用 `command -v` 而非 `which`，可偵測 shell function 型 runner（如 agy）。
func DetectRunners(runners map[string]string) []RunnerHealth {
	results := make([]RunnerHealth, 0, len(runners))
	for name, command := range runners {
		rh := RunnerHealth{Name: name, Command: command}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, "sh", "-c", "command -v "+command).Output()
		cancel()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			results = append(results, rh)
			continue
		}
		rh.Installed = true

		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		verOut, err := exec.CommandContext(ctx2, command, "--version").CombinedOutput()
		cancel2()
		if err == nil {
			rh.Version = parseVersion(string(verOut))
		}

		results = append(results, rh)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}
