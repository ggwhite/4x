package main

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/logging"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	initLogging()
	defer logging.Shutdown()

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
		newSyncCmd(),
		newNewCmd(),
		newRunCmd(),
		newStatusCmd(),
		newCheckCmd(),
		newDoctorCmd(),
		newTransitionCmd(),
		newRetryCmd(),
		newEventCmd(),
		newPromptCmd(),
		newBatchCmd(),
		newLiveCmd(),
		newConfigCmd(),
		newDoneCmd(),
		newMergeCmd(),
		newMCPCmd(),
		newSubtaskCmd(),
		newVerifyCmd(),
		newCleanCmd(),
		newLearnCmd(),
		newMineCmd(),
		newApproveCmd(),
		newRejectCmd(),
		newGateCmd(),
		newEvolveCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// initLogging 從 ~/.4x/settings.json 讀取 logLevel 與 logRetainDays，初始化全域 slog。
// 讀取失敗或 Init 失敗都不阻擋程式啟動，fallback 到 stderr only。
func initLogging() {
	var cfg logging.Config
	if userCfg, err := protocol.ReadUserConfig(); err == nil {
		cfg.Level = userCfg.LogLevel
		cfg.RetainDays = userCfg.LogRetainDays
	}
	_ = logging.Init(cfg)
}
