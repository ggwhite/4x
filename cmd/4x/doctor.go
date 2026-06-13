package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/doctor"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check runner installation status and LLM usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, _ := protocol.Find(cwd)
			report := doctor.GenerateReport(ws)

			if jsonOutput {
				data, _ := json.MarshalIndent(report, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			if len(report.Runners) == 0 {
				fmt.Printf("No runners configured.\n\n")
			}
			if !report.CcusageAvailable {
				fmt.Printf("ccusage not found. Install with: %s\n\n", report.CcusageHint)
			}

			for _, r := range report.Runners {
				printRunnerCard(r, report.CcusageAvailable)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func printRunnerCard(r doctor.RunnerUsage, ccusageAvail bool) {
	status := "\033[31m✗\033[0m"
	ver := ""
	if r.Installed {
		status = "\033[32m✓\033[0m"
		if r.Version != "" {
			ver = " \033[90m" + r.Version + "\033[0m"
		}
	}
	fmt.Printf("── %s %s%s ──\n", r.Name, status, ver)

	if r.Block5h != nil {
		printBlock("5h", r.Block5h, 300)
	}

	if r.Block7d != nil {
		printBlock("7d", r.Block7d, 168*60)
	}

	if r.Recent7d != nil {
		fmt.Printf("  7d   %s tokens, $%.2f (%d days)\n",
			formatTokens(r.Recent7d.TotalTokens), r.Recent7d.TotalCost, r.Recent7d.Days)
	}

	if r.Block5h == nil && r.Block7d == nil && r.Recent7d == nil && ccusageAvail {
		fmt.Printf("  No usage data\n")
	}

	fmt.Println()
}

func printBlock(label string, b *doctor.UsageBlock, totalMin int) {
	remaining := time.Duration(b.Projection.RemainingMinutes) * time.Minute
	endTime, _ := time.Parse(time.RFC3339, b.EndTime)
	resetStr := endTime.Local().Format("15:04")
	if totalMin > 300 {
		resetStr = fmtDuration(remaining)
	}

	elapsed := totalMin - b.Projection.RemainingMinutes
	if elapsed < 0 {
		elapsed = 0
	}
	pct := float64(elapsed) / float64(totalMin) * 100
	bar := renderBar(pct, 20)

	fmt.Printf("  %s   %s %.0f%% (%s left, resets %s)\n", label, bar, pct, fmtDuration(remaining), resetStr)
	fmt.Printf("       $%.2f, %s tok", b.CostUSD, formatTokens(b.TotalTokens))
	if b.BurnRate.CostPerHour > 0 {
		fmt.Printf(", $%.0f/hr burn", b.BurnRate.CostPerHour)
	}
	fmt.Println()
}

func renderBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	color := "\033[32m"
	if pct >= 80 {
		color = "\033[31m"
	} else if pct >= 50 {
		color = "\033[33m"
	}
	return color + strings.Repeat("█", filled) + "\033[90m" + strings.Repeat("░", width-filled) + "\033[0m"
}

func fmtDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
