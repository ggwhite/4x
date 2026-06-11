package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "4x",
		Short: "Multi-role AI development loop",
		Long: `4x — Design, Code, Review, Test.

A framework for orchestrating AI agents in specialized roles.
Like 4X strategy games, 4x conquers codebases through four phases.`,
		Version: version,
	}

	root.AddCommand(
		newInitCmd(),
		newUpgradeCmd(),
		newNewCmd(),
		newRunCmd(),
		newStatusCmd(),
		newCheckCmd(),
		newTransitionCmd(),
		newEventCmd(),
		newPromptCmd(),
		newBatchCmd(),
		newLiveCmd(),
		newMonitorCmd(),
		newConfigCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
