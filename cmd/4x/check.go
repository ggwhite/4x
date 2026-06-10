package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "check <feature-id>",
		Short: "Run guardrail checks (scope, baseline, required files, verify evidence)",
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

			result := guard.Check(ws, args[0])

			if jsonOutput {
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
			} else {
				if result.Pass {
					fmt.Println("✅ All checks passed")
				} else {
					fmt.Println("❌ Check failed")
				}
				for _, e := range result.Errors {
					fmt.Printf("  ERROR: %s\n", e)
				}
				for _, w := range result.Warns {
					fmt.Printf("  WARN:  %s\n", w)
				}
			}

			if !result.Pass {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
