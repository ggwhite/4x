package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a 4x project in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			dotDir := filepath.Join(cwd, protocol.DirName)
			if _, err := os.Stat(dotDir); err == nil {
				return fmt.Errorf("%s already exists", protocol.DirName)
			}

			projectName := filepath.Base(cwd)

			cfg := protocol.Config{
				Project: protocol.ProjectConfig{
					Name: projectName,
				},
				Runners: map[string]protocol.RunnerConfig{
					"claude": {
						Command: "claude",
						Args:    []string{"-p", "{prompt}"},
						Model:   "opus",
					},
					"codex": {
						Command: "codex",
						Args:    []string{"exec", "--prompt-file", "{promptFile}"},
					},
				},
				Default: "claude",
				Roles: map[string]protocol.RoleConfig{
					"designer": {Model: "opus"},
					"coder":    {Model: "sonnet"},
					"reviewer": {Model: "sonnet", DeepModel: "opus"},
					"tester":   {Model: "sonnet"},
				},
				HubRepos: []string{},
			}

			if err := protocol.Init(cwd, cfg); err != nil {
				return err
			}

			fmt.Printf("Initialized 4x project in %s/\n", protocol.DirName)
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  4x new \"feature name\"    Create a feature")
			fmt.Println("  4x run <feature-id>      Run the loop")
			fmt.Println("  4x live                  Open dashboard")
			return nil
		},
	}
}
