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

	"github.com/ggwhite/4x/internal/envutil"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// Group 表示一組依序執行的 verify commands。同一 Group 內的 commands 依序跑，
// 不同 Group 之間由 RunGroups 平行執行。
type Group struct {
	Name     string
	Commands []string
}

// FallbackGroups 在 test-strategy.yaml 不存在時，從 settings.json 的 Build/Test/Lint 指令
// 組合出單一 fallback group，讓 verify 仍可執行。
func FallbackGroups(cfg protocol.ProjectConfig) ([]Group, error) {
	var commands []string
	commands = append(commands, cfg.Build...)
	commands = append(commands, cfg.Test...)
	commands = append(commands, cfg.Lint...)
	if len(commands) == 0 {
		return nil, fmt.Errorf("settings.json has no build/test/lint commands for fallback")
	}
	return []Group{{Name: "fallback", Commands: commands}}, nil
}

// BuildGateGroups 從 settings.json 的 Build + Lint 指令組合出單一 build-gate group。
// Build 指令在前、Lint 在後，復用 runGroup 的「前一個失敗就 skip 後續」語意。
// 不含 Test 指令——test 是 Tester 的職責。
func BuildGateGroups(cfg protocol.ProjectConfig) ([]Group, error) {
	var commands []string
	commands = append(commands, cfg.Build...)
	commands = append(commands, cfg.Lint...)
	if len(commands) == 0 {
		return nil, fmt.Errorf("settings.json has no build/lint commands for build-gate")
	}
	return []Group{{Name: "build-gate", Commands: commands}}, nil
}

// DocsGateGroups 從 settings.json 的 DocsCheck 指令組合出 docs-gate groups。
// 與 BuildGateGroups 不同，每個 DocsCheck 指令各自成為一個獨立 group，
// 讓 RunGroups 平行執行且互不連帶 skip——docs/i18n 檢查彼此獨立，coder 應一次看到
// 全部問題，而非在第一個失敗後其餘被標 Skipped。DocsCheck 未設定時回傳 error，
// 讓呼叫端據以判斷此專案是否啟用 docs-gate（opt-in，見 F136）。
func DocsGateGroups(cfg protocol.ProjectConfig) ([]Group, error) {
	if len(cfg.DocsCheck) == 0 {
		return nil, fmt.Errorf("settings.json has no docs_check commands for docs-gate")
	}
	groups := make([]Group, 0, len(cfg.DocsCheck))
	for _, cmd := range cfg.DocsCheck {
		groups = append(groups, Group{Name: "docs-gate", Commands: []string{cmd}})
	}
	return groups, nil
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

// CommandsPassed 回傳 cmds 是否整體通過：所有非 skipped command 的 exit code 皆為 0。
// 這是「build 通過」的單一權威定義——RunGroups 與 guard 的 runGroupsAcrossRoots 皆
// 呼叫此 helper，避免同一條判定式在兩層各自複製、日後 drift。
func CommandsPassed(cmds []protocol.VerifyCommand) bool {
	for _, cmd := range cmds {
		if cmd.ExitCode != 0 && !cmd.Skipped {
			return false
		}
	}
	return true
}

// RunGroups 平行執行所有 group（組內依序），回傳組裝好的 VerifyEvidence（不含 Round 和 Role，由呼叫端補上）。
// 任一 group 失敗不會中斷其他 group——全部跑完才彙總。
// Passed 為 true 的條件：所有非 skipped command 的 exit code 皆為 0（見 CommandsPassed）。
// allowlist 為 verify 命令的允許前綴清單：非空時每條命令執行前先經 CommandAllowed 比對，
// 不符者不執行、記為 ExitCode 126 / Error "blocked"；為空時完全不強制（見 CommandAllowed）。
func RunGroups(ctx context.Context, groups []Group, workDir string, allowlist []string) protocol.VerifyEvidence {
	results := make([]groupResult, len(groups))
	var wg sync.WaitGroup

	for i, g := range groups {
		wg.Add(1)
		go func(idx int, grp Group) {
			defer wg.Done()
			results[idx] = runGroup(ctx, grp, workDir, allowlist)
		}(i, g)
	}
	wg.Wait()

	var allCommands []protocol.VerifyCommand
	for _, r := range results {
		allCommands = append(allCommands, r.commands...)
	}

	return protocol.VerifyEvidence{
		Passed:   CommandsPassed(allCommands),
		Commands: allCommands,
	}
}

// runGroup 依序執行單一 group 的 commands；任一 command 失敗後，剩餘 commands 標記 Skipped 不執行。
func runGroup(ctx context.Context, g Group, workDir string, allowlist []string) groupResult {
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

		vc := executeCommand(ctx, cmdStr, g.Name, workDir, allowlist)
		commands = append(commands, vc)
		if vc.ExitCode != 0 {
			failed = true
		}
	}

	return groupResult{commands: commands}
}

// executeCommand 用 sh -c 執行單一 command，cwd 設為 workDir，並透過 ctx 套用整體 timeout。
// Summary 過長（>500 字元）時截頭尾保留前後各 250 字元。
// allowlist 非空且 cmdStr 不被 CommandAllowed 放行時，命令不執行，直接回傳
// ExitCode 126 / Error "blocked" 的顯式失敗紀錄（見 CommandAllowed）。
func executeCommand(ctx context.Context, cmdStr, group, workDir string, allowlist []string) protocol.VerifyCommand {
	start := time.Now()

	if !CommandAllowed(cmdStr, allowlist) {
		return protocol.VerifyCommand{
			Command:    cmdStr,
			ExitCode:   126,
			Summary:    "blocked by verify_command_allowlist: no allowed prefix / disallowed shell substitution",
			StartedAt:  start,
			FinishedAt: start,
			Group:      group,
			Error:      "blocked",
		}
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = workDir
	cmd.Env = gitops.ApplyWorktreeEnv(envutil.EnrichedEnv(), workDir)

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

	errMsg := ""
	if err != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			errMsg = "timeout"
		case context.Canceled:
			errMsg = "canceled"
		}
	}

	return protocol.VerifyCommand{
		Command:    cmdStr,
		ExitCode:   exitCode,
		DurationMs: finished.Sub(start).Milliseconds(),
		Summary:    summary,
		StartedAt:  start,
		FinishedAt: finished,
		Group:      group,
		Error:      errMsg,
	}
}
