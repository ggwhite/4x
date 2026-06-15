package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/ggwhite/4x/internal/doctor"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

// newDoctorCmd 建立 `4x doctor` 指令：對合併後的設定與 workspace 完整性做一次性、
// 純 read-only 的健康檢查，輸出依 section 分組的 PASS/WARN/FAIL 報告。
// exit code：任一 FAIL → 1；只有 WARN 或全 PASS → 0。
func newDoctorCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check settings and workspace health before running",
		Args:  cobra.NoArgs,
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			report, err := doctor.Diagnose(doctor.Options{Root: ws.Root})
			if err != nil {
				return err
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(report, "", "  ")
				fmt.Println(string(data))
			} else {
				printDoctorReport(report)
			}

			if report.HasFail() {
				os.Exit(1)
			}
			return nil
		}),
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// printDoctorReport 以人類可讀格式輸出報告：依 section 分組，每條帶 emoji 前綴，結尾印 summary。
func printDoctorReport(report doctor.Report) {
	var sections []string
	bySection := map[string][]doctor.Check{}
	for _, c := range report.Checks {
		if _, ok := bySection[c.Section]; !ok {
			sections = append(sections, c.Section)
		}
		bySection[c.Section] = append(bySection[c.Section], c)
	}
	sort.SliceStable(sections, func(i, j int) bool {
		return sectionOrder(sections[i]) < sectionOrder(sections[j])
	})

	var pass, warn, fail int
	for _, section := range sections {
		fmt.Printf("── %s ──\n", section)
		for _, c := range bySection[section] {
			switch c.Severity {
			case doctor.SeverityPass:
				pass++
			case doctor.SeverityWarn:
				warn++
			case doctor.SeverityFail:
				fail++
			}
			fmt.Printf("  %s %s — %s\n", severityIcon(c.Severity), c.Name, c.Detail)
		}
		fmt.Println()
	}
	fmt.Printf("Summary: %d PASS, %d WARN, %d FAIL\n", pass, warn, fail)
}

// severityIcon 把 Severity 對應到輸出用 emoji。
func severityIcon(s doctor.Severity) string {
	switch s {
	case doctor.SeverityPass:
		return "✅"
	case doctor.SeverityWarn:
		return "⚠️"
	case doctor.SeverityFail:
		return "❌"
	default:
		return "•"
	}
}

// sectionOrder 決定 section 的輸出順序（與檢查執行順序一致）。
func sectionOrder(section string) int {
	switch section {
	case "settings":
		return 0
	case "runners":
		return 1
	case "roles":
		return 2
	case "workspace":
		return 3
	default:
		return 99
	}
}
