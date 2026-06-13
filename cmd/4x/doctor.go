package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

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

			printRunners(report)
			printUsage(report)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func printRunners(report doctor.DoctorReport) {
	installed := 0
	for _, r := range report.Runners {
		if r.Installed {
			installed++
		}
	}
	fmt.Printf("── Runners (%d/%d installed) ──\n", installed, len(report.Runners))

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "  RUNNER\tCOMMAND\tSTATUS\tVERSION\n")
	fmt.Fprintf(w, "  ──────\t───────\t──────\t───────\n")
	for _, r := range report.Runners {
		status := "✗ not found"
		version := "-"
		if r.Installed {
			status = "✓ installed"
			if r.Version != "" {
				version = r.Version
			}
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", r.Name, r.Command, status, version)
	}
	w.Flush()
	fmt.Println()
}

func printUsage(report doctor.DoctorReport) {
	if !report.CcusageAvailable {
		fmt.Printf("── Usage ──\n")
		fmt.Printf("  ccusage not found. Install with: %s\n\n", report.CcusageHint)
		return
	}

	if len(report.Usage) == 0 {
		fmt.Printf("── Usage (via ccusage) ──\n")
		fmt.Printf("  No usage data found.\n\n")
		return
	}

	fmt.Printf("── Usage (via ccusage) ──\n")
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "  DATE\tAGENTS\tTOKENS\tCOST\n")
	fmt.Fprintf(w, "  ────\t──────\t──────\t────\n")

	var totalTokens int64
	var totalCost float64
	for _, e := range report.Usage {
		agents := "-"
		if md, ok := e.Metadata["agents"]; ok {
			if arr, ok := md.([]any); ok {
				names := make([]string, 0, len(arr))
				for _, a := range arr {
					if s, ok := a.(string); ok {
						names = append(names, s)
					}
				}
				agents = strings.Join(names, ",")
			}
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t$%.2f\n", e.Period, agents, formatTokens(e.TotalTokens), e.TotalCost)
		totalTokens += e.TotalTokens
		totalCost += e.TotalCost
	}
	fmt.Fprintf(w, "  \t\t─────\t─────\n")
	fmt.Fprintf(w, "  Total\t\t%s\t$%.2f\n", formatTokens(totalTokens), totalCost)
	w.Flush()
	fmt.Println()
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
