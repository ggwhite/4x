package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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

			key := args[0]
			parts := strings.Split(key, ".")

			switch {
			case len(parts) == 1:
				switch key {
				case "locale":
					printOrDefault(cfg.Locale, "en")
				case "theme":
					printOrDefault(cfg.Theme, "")
				case "default_runner":
					printOrDefault(cfg.DefaultRunner, "")
				default:
					return fmt.Errorf("unknown config key: %s", key)
				}

			case len(parts) == 3 && parts[0] == "runner":
				runnerName, field := parts[1], parts[2]
				rc, ok := cfg.Runners[runnerName]
				if !ok {
					fmt.Println("(not set)")
					return nil
				}
				switch field {
				case "command":
					printOrDefault(rc.Command, "")
				case "model":
					printOrDefault(rc.Model, "")
				case "tty":
					fmt.Println(protocol.BoolVal(rc.Tty))
				case "stdin":
					fmt.Println(protocol.BoolVal(rc.Stdin))
				case "quiet":
					fmt.Println(protocol.BoolVal(rc.Quiet))
				default:
					return fmt.Errorf("unknown runner field: %s", field)
				}

			case len(parts) == 3 && parts[0] == "role":
				roleName, field := parts[1], parts[2]
				rc, ok := cfg.Roles[roleName]
				if !ok {
					fmt.Println("(not set)")
					return nil
				}
				switch field {
				case "model":
					printOrDefault(rc.Model, "")
				case "deep_model":
					printOrDefault(rc.DeepModel, "")
				case "parallel_reviewers":
					printIntOrDefault(rc.ParallelReviewers, 1)
				case "angles_per_reviewer":
					printIntOrDefault(rc.AnglesPerReviewer, 0)
				default:
					return fmt.Errorf("unknown role field: %s", field)
				}

			default:
				return fmt.Errorf("unknown config key: %s", key)
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

			key, value := args[0], args[1]
			parts := strings.Split(key, ".")

			switch {
			case len(parts) == 1:
				switch key {
				case "locale":
					cfg.Locale = value
				case "theme":
					cfg.Theme = value
				case "default_runner":
					cfg.DefaultRunner = value
				default:
					return fmt.Errorf("unknown config key: %s", key)
				}

			case len(parts) == 3 && parts[0] == "runner":
				runnerName, field := parts[1], parts[2]
				if cfg.Runners == nil {
					cfg.Runners = make(map[string]protocol.RunnerConfig)
				}
				rc := cfg.Runners[runnerName]
				switch field {
				case "command":
					rc.Command = value
				case "model":
					rc.Model = value
				case "tty":
					b := value == "true"
					rc.Tty = protocol.BoolPtr(b)
				case "stdin":
					b := value == "true"
					rc.Stdin = protocol.BoolPtr(b)
				case "quiet":
					b := value == "true"
					rc.Quiet = protocol.BoolPtr(b)
				case "args":
					return fmt.Errorf("args is an array field — edit ~/.4x/settings.json directly")
				default:
					return fmt.Errorf("unknown runner field: %s", field)
				}
				cfg.Runners[runnerName] = rc

			case len(parts) == 3 && parts[0] == "role":
				roleName, field := parts[1], parts[2]
				if cfg.Roles == nil {
					cfg.Roles = make(map[string]protocol.RoleConfig)
				}
				rc := cfg.Roles[roleName]
				switch field {
				case "model":
					rc.Model = value
				case "deep_model":
					rc.DeepModel = value
				case "parallel_reviewers":
					n, err := strconv.Atoi(value)
					if err != nil {
						return fmt.Errorf("parallel_reviewers must be an integer: %w", err)
					}
					rc.ParallelReviewers = n
				case "angles_per_reviewer":
					n, err := strconv.Atoi(value)
					if err != nil {
						return fmt.Errorf("angles_per_reviewer must be an integer: %w", err)
					}
					rc.AnglesPerReviewer = n
				default:
					return fmt.Errorf("unknown role field: %s", field)
				}
				cfg.Roles[roleName] = rc

			default:
				return fmt.Errorf("unknown config key: %s", key)
			}

			if err := protocol.WriteUserConfig(cfg); err != nil {
				return err
			}
			path, _ := protocol.UserConfigPath()
			fmt.Printf("Set %s = %s in %s\n", key, value, path)
			return nil
		},
	}
}

// printIntOrDefault 印出整數設定值；val <= 0 視為未設定，顯示預設值。
func printIntOrDefault(val, def int) {
	if val <= 0 {
		fmt.Printf("(not set, default: %d)\n", def)
	} else {
		fmt.Println(val)
	}
}

func printOrDefault(val, def string) {
	if val == "" {
		if def != "" {
			fmt.Printf("(not set, default: %s)\n", def)
		} else {
			fmt.Println("(not set)")
		}
	} else {
		fmt.Println(val)
	}
}
