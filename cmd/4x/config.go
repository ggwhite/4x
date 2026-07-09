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
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show all user configuration",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cfg, err := protocol.ReadUserConfig()
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			if jsonOutput {
				fmt.Println(string(data))
				return nil
			}
			path, _ := protocol.UserConfigPath()
			fmt.Printf("# %s\n", path)
			fmt.Print(string(data))
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cfg, err := protocol.ReadUserConfig()
			if err != nil {
				return err
			}

			key := args[0]
			value, err := configValue(cfg, key)
			if err != nil {
				return err
			}

			if jsonOutput {
				return printJSON(struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				}{key, value})
			}
			fmt.Println(value)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// configValue 把 user config 的某個 key 解析成可顯示字串（與文字模式輸出一致），
// 未設定者回傳 "(not set...)" 形式。未知 key／field 回 error。
func configValue(cfg protocol.UserConfig, key string) (string, error) {
	parts := strings.Split(key, ".")
	switch {
	case len(parts) == 1:
		switch key {
		case "locale":
			return valOrDefault(cfg.Locale, "en"), nil
		case "theme":
			return valOrDefault(cfg.Theme, ""), nil
		case "default_runner":
			return valOrDefault(cfg.DefaultRunner, ""), nil
		default:
			return "", fmt.Errorf("unknown config key: %s", key)
		}

	case len(parts) == 3 && parts[0] == "runner":
		runnerName, field := parts[1], parts[2]
		rc, ok := cfg.Runners[runnerName]
		if !ok {
			return "(not set)", nil
		}
		switch field {
		case "command":
			return valOrDefault(rc.Command, ""), nil
		case "model":
			return valOrDefault(rc.Model, ""), nil
		case "tty":
			return fmt.Sprintf("%v", protocol.BoolVal(rc.Tty)), nil
		case "stdin":
			return fmt.Sprintf("%v", protocol.BoolVal(rc.Stdin)), nil
		case "quiet":
			return fmt.Sprintf("%v", protocol.BoolVal(rc.Quiet)), nil
		default:
			return "", fmt.Errorf("unknown runner field: %s", field)
		}

	case len(parts) == 3 && parts[0] == "role":
		roleName, field := parts[1], parts[2]
		rc, ok := cfg.Roles[roleName]
		if !ok {
			return "(not set)", nil
		}
		switch field {
		case "model":
			return valOrDefault(rc.Model, ""), nil
		case "deep_model":
			return valOrDefault(rc.DeepModel, ""), nil
		case "runner":
			return valOrDefault(rc.Runner, ""), nil
		case "parallel_reviewers":
			return intValOrDefault(rc.ParallelReviewers, 1), nil
		case "angles_per_reviewer":
			return intValOrDefault(rc.AnglesPerReviewer, 0), nil
		default:
			return "", fmt.Errorf("unknown role field: %s", field)
		}

	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

func newConfigSetCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
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
				case "runner":
					rc.Runner = value
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
			if jsonOutput {
				return printJSON(struct {
					Key   string `json:"key"`
					Value string `json:"value"`
					Path  string `json:"path"`
				}{key, value, path})
			}
			fmt.Printf("Set %s = %s in %s\n", key, value, path)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// intValOrDefault 回傳整數設定值的顯示字串；val <= 0 視為未設定，顯示預設值提示。
func intValOrDefault(val, def int) string {
	if val <= 0 {
		return fmt.Sprintf("(not set, default: %d)", def)
	}
	return fmt.Sprintf("%d", val)
}

// valOrDefault 回傳字串設定值的顯示字串；空字串視為未設定，顯示預設值提示（無預設則 "(not set)"）。
func valOrDefault(val, def string) string {
	if val == "" {
		if def != "" {
			return fmt.Sprintf("(not set, default: %s)", def)
		}
		return "(not set)"
	}
	return val
}
