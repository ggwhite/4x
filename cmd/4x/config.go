package main

import (
	"encoding/json"
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage user-level configuration (~/.4x/settings.json)",
	}

	cmd.AddCommand(
		newConfigGetCmd(),
		newConfigSetCmd(),
		newConfigListCmd(),
	)
	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show all user configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := protocol.ReadUserConfig()
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			path, _ := protocol.UserConfigPath()
			fmt.Printf("# %s\n", path)
			fmt.Print(string(data))
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := protocol.ReadUserConfig()
			if err != nil {
				return err
			}
			switch args[0] {
			case "locale":
				if cfg.Locale == "" {
					fmt.Println("(not set, default: en)")
				} else {
					fmt.Println(cfg.Locale)
				}
			default:
				return fmt.Errorf("unknown config key: %s", args[0])
			}
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := protocol.ReadUserConfig()
			if err != nil {
				return err
			}
			switch args[0] {
			case "locale":
				cfg.Locale = args[1]
			default:
				return fmt.Errorf("unknown config key: %s", args[0])
			}
			if err := protocol.WriteUserConfig(cfg); err != nil {
				return err
			}
			path, _ := protocol.UserConfigPath()
			fmt.Printf("Set %s = %s in %s\n", args[0], args[1], path)
			return nil
		},
	}
}
