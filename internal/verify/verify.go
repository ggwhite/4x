// Package verify 封裝 verify commands 的分組解析與平行執行邏輯。
// CLI 層（cmd/4x/verify.go）只做參數解析，實際分組與 goroutine 控制都在這裡，
// 確保平行驗證完全由 Go 程式碼負責、不依賴 LLM。
package verify

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// Group 表示一組依序執行的 verify commands。同一 Group 內的 commands 依序跑，
// 不同 Group 之間由 RunGroups 平行執行。
type Group struct {
	Name     string
	Commands []string
}

// ResolveGroups 從 TestStrategy 解析出 verify groups。
// verify_groups 存在時依 group 名稱排序後回傳（確保輸出順序穩定可測）；
// 否則 fallback 到 verify_commands 作為單一 default group，commands 維持原序。
// verify_groups 與 verify_commands 同時存在、或兩者皆不存在時回傳 error。
func ResolveGroups(ts protocol.TestStrategy) ([]Group, error) {
	hasGroups := len(ts.VerifyGroups) > 0
	hasCommands := len(ts.Verify) > 0

	if hasGroups && hasCommands {
		return nil, fmt.Errorf("test-strategy.yaml has both verify_groups and verify_commands; use only one")
	}
	if !hasGroups && !hasCommands {
		return nil, fmt.Errorf("test-strategy.yaml has no verify_groups or verify_commands")
	}

	if hasGroups {
		names := make([]string, 0, len(ts.VerifyGroups))
		for name := range ts.VerifyGroups {
			names = append(names, name)
		}
		sort.Strings(names)

		groups := make([]Group, 0, len(names))
		for _, name := range names {
			groups = append(groups, Group{Name: name, Commands: ts.VerifyGroups[name]})
		}
		return groups, nil
	}

	return []Group{{Name: "default", Commands: ts.Verify}}, nil
}

// groupResult 暫存單一 group 平行執行後的 command 結果。
type groupResult struct {
	commands []protocol.VerifyCommand
}

// RunGroups 平行執行所有 group（組內依序），回傳組裝好的 VerifyEvidence（不含 Round 和 Role，由呼叫端補上）。
// 任一 group 失敗不會中斷其他 group——全部跑完才彙總。
// Passed 為 true 的條件：所有非 skipped command 的 exit code 皆為 0。
func RunGroups(ctx context.Context, groups []Group, workDir string) protocol.VerifyEvidence {
	results := make([]groupResult, len(groups))
	var wg sync.WaitGroup

	for i, g := range groups {
		wg.Add(1)
		go func(idx int, grp Group) {
			defer wg.Done()
			results[idx] = runGroup(ctx, grp, workDir)
		}(i, g)
	}
	wg.Wait()

	var allCommands []protocol.VerifyCommand
	allPassed := true
	for _, r := range results {
		for _, cmd := range r.commands {
			allCommands = append(allCommands, cmd)
			if cmd.ExitCode != 0 && !cmd.Skipped {
				allPassed = false
			}
		}
	}

	return protocol.VerifyEvidence{
		Passed:   allPassed,
		Commands: allCommands,
	}
}

// runGroup 依序執行單一 group 的 commands；任一 command 失敗後，剩餘 commands 標記 Skipped 不執行。
func runGroup(ctx context.Context, g Group, workDir string) groupResult {
	var commands []protocol.VerifyCommand
	failed := false

	for _, cmdStr := range g.Commands {
		if failed {
			commands = append(commands, protocol.VerifyCommand{
				Command: cmdStr,
				Group:   g.Name,
				Skipped: true,
			})
			continue
		}

		vc := executeCommand(ctx, cmdStr, g.Name, workDir)
		commands = append(commands, vc)
		if vc.ExitCode != 0 {
			failed = true
		}
	}

	return groupResult{commands: commands}
}

// executeCommand 用 sh -c 執行單一 command，cwd 設為 workDir，並透過 ctx 套用整體 timeout。
// Summary 過長（>500 字元）時截頭尾保留前後各 250 字元。
func executeCommand(ctx context.Context, cmdStr, group, workDir string) protocol.VerifyCommand {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = workDir

	out, err := cmd.CombinedOutput()
	finished := time.Now()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	summary := strings.TrimSpace(string(out))
	if len(summary) > 500 {
		summary = summary[:250] + "\n...\n" + summary[len(summary)-250:]
	}

	return protocol.VerifyCommand{
		Command:    cmdStr,
		ExitCode:   exitCode,
		DurationMs: finished.Sub(start).Milliseconds(),
		Summary:    summary,
		StartedAt:  start,
		FinishedAt: finished,
		Group:      group,
	}
}
