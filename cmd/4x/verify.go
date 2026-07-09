package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/verify"
	"github.com/spf13/cobra"
)

// newVerifyCmd 建立 `4x verify <feature-id>` subcommand。
// 它讀取 feature 的 test-strategy.yaml，將 verify_groups（或 fallback 的 verify_commands）
// 分組平行執行，把結果寫進 rounds/round-{N}/verify.json，並依整體是否通過決定 exit code。
// 供 Tester agent 呼叫，也供人獨立除錯。
func newVerifyCmd() *cobra.Command {
	var (
		round   int
		timeout time.Duration
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "verify <feature-id>",
		Short: "Run verify commands from test-strategy.yaml (groups run in parallel)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}

			if round <= 0 {
				state, err := ws.ReadState(featureID)
				if err != nil {
					return fmt.Errorf("cannot read state.json (use --round to specify): %w", err)
				}
				round = state.Round
			}

			ts, err := ws.ReadTestStrategy(featureID)
			if err != nil {
				return err
			}

			cfg, err := ws.ReadConfig()
			if err != nil {
				return err
			}
			allowlist := cfg.Project.VerifyCommandAllowlist

			isFallback := len(ts.Verify) == 0 && len(ts.VerifyGroups) == 0
			var groups []verify.Group
			if isFallback {
				groups, err = verify.FallbackGroups(cfg.Project)
				if err != nil {
					return err
				}
			} else {
				groups, err = verify.ResolveGroups(ts)
				if err != nil {
					return err
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			if !jsonOut {
				if isFallback {
					fmt.Fprintf(os.Stderr, "No test-strategy.yaml found, falling back to settings.json commands...\n")
				} else {
					fmt.Fprintf(os.Stderr, "Running %d verify group(s)...\n", len(groups))
				}
			}
			evidence := verify.RunGroups(ctx, groups, ws.Root, allowlist)
			evidence.Round = round
			evidence.Role = protocol.RoleTester

			if isFallback {
				var acResults []protocol.ACEvidence
				for _, cmd := range evidence.Commands {
					if cmd.Skipped {
						continue
					}
					acResults = append(acResults, protocol.ACEvidence{
						ID:       fmt.Sprintf("verify:%s", cmd.Command),
						Passed:   cmd.ExitCode == 0,
						Evidence: []string{cmd.Summary},
					})
				}
				evidence.ACResults = acResults
			}
			if len(ts.ACChecks) > 0 {
				// ac_checks 為權威判定路徑，執行與否獨立於 fallback 判斷——test-strategy
				// 只宣告 ac_checks（無 verify_commands/verify_groups）時也必須執行，否則
				// guard 要求 check 結果但 verify 永不產生，形成死循環。對每條綁定 ac_checks
				// 的 AC 實際執行命令，以 exit code 決定 passed，併入 verify.json 的 ac_results。
				// timeout 預算獨立於 verify groups（另起 context），避免 groups 耗盡預算後
				// ac_checks 全被 ctx cancel 記成假失敗。
				acCtx, acCancel := context.WithTimeout(context.Background(), timeout)
				acResults := verify.RunACChecks(acCtx, ts.ACChecks, ws.Root, allowlist)
				acCancel()
				evidence.ACResults = append(evidence.ACResults, acResults...)
				for _, ac := range acResults {
					if !ac.Passed {
						evidence.Passed = false
					}
				}
			}

			roundDir := ws.RoundDir(featureID, round)
			if err := os.MkdirAll(roundDir, 0o755); err != nil {
				return fmt.Errorf("create round dir: %w", err)
			}
			outData, err := json.MarshalIndent(evidence, "", "  ")
			if err != nil {
				return err
			}
			outPath := filepath.Join(roundDir, protocol.VerifyFile)
			if err := os.WriteFile(outPath, outData, 0o644); err != nil {
				return err
			}

			if jsonOut {
				fmt.Println(string(outData))
			} else {
				printVerifySummary(os.Stdout, evidence)
			}

			if !evidence.Passed {
				cmd.SilenceUsage = true
				cmd.SilenceErrors = true
				return fmt.Errorf("verification failed")
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&round, "round", 0, "round number (default: current round from state.json)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "overall timeout for all groups")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

// printVerifySummary 印出 verify 結果摘要表格（GROUP / COMMAND / EXIT / DURATION），
// 含 verify groups 的 commands 與每條 ac_checks AC 的 check 執行結果（group 欄為 AC ID）。
func printVerifySummary(w io.Writer, ev protocol.VerifyEvidence) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%-12s %-40s %6s %10s\n", "GROUP", "COMMAND", "EXIT", "DURATION")
	fmt.Fprintf(w, "%-12s %-40s %6s %10s\n",
		pad("-", 12), pad("-", 40), pad("-", 6), pad("-", 10))

	for _, c := range ev.Commands {
		printVerifyRow(w, c)
	}
	for _, ac := range ev.ACResults {
		for _, c := range ac.Checks {
			printVerifyRow(w, c)
		}
	}

	fmt.Fprintln(w)
	if ev.Passed {
		fmt.Fprintln(w, "PASSED")
	} else {
		fmt.Fprintln(w, "FAILED")
	}
}

// printVerifyRow 印出摘要表格的單一 command 列；skipped 顯示 SKIP、
// ctx 逾時/取消（Error 非空）顯示原因取代 exit code。
func printVerifyRow(w io.Writer, c protocol.VerifyCommand) {
	status := fmt.Sprintf("%d", c.ExitCode)
	dur := fmt.Sprintf("%dms", c.DurationMs)
	if c.Skipped {
		status = "SKIP"
		dur = "-"
	} else if c.Error != "" {
		status = strings.ToUpper(c.Error)
	}
	cmdDisplay := c.Command
	if len(cmdDisplay) > 40 {
		cmdDisplay = cmdDisplay[:37] + "..."
	}
	fmt.Fprintf(w, "%-12s %-40s %6s %10s\n", c.Group, cmdDisplay, status, dur)
}

// pad 回傳 s 重複 n 次組成的字串，用於畫摘要表格的分隔線。
func pad(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
