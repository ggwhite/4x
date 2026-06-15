package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var (
		repos      []string
		jsonOutput bool
		customID   string
		desc       string
		subtasks   []string
		rules      []string
		depends    []string
		priority   int
	)

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new feature",
		Long: `Create a new feature with optional metadata.

Examples:
  4x new "Dashboard SPA file split"
  4x new "Global settings" --id global-settings --desc "Add ~/.4x/settings.json support"
  4x new "Auth refactor" --subtask "extract-middleware:Extract auth middleware" --subtask "add-tests:Add integration tests"
  4x new "Batch mode" --rule "spec: docs/design/batch-spec.md" --depends F034 --priority 2`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			name := args[0]

			// 既有行為：未指定 --desc 時 description 預設為原始 name（非 display name）。
			description := name
			if desc != "" {
				description = desc
			}

			var parsedSubtasks []feature.Subtask
			for _, s := range subtasks {
				st, err := parseSubtask(s)
				if err != nil {
					if jsonOutput {
						return jsonError(err.Error())
					}
					return err
				}
				parsedSubtasks = append(parsedSubtasks, st)
			}

			// 傳「原始 name」給 Create，使 ID 由原始 name 產生並沿用既有截斷規則
			// （若傳 display name，"F001: " 前綴會被併入 slug 產生 F001-f001-... 的重複前綴）。
			opts := feature.CreateOpts{
				Name:        name,
				Description: description,
				CustomID:    customID,
				Subtasks:    parsedSubtasks,
				Rules:       rules,
				Depends:     depends,
				Repos:       repos,
			}
			if cmd.Flags().Changed("priority") {
				opts.Priority = &priority
			}

			f, err := feature.Create(ws, opts)
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			var num int
			fmt.Sscanf(f.ID, "F%d-", &num)
			f.Name = fmt.Sprintf("F%03d: %s", num, name)
			if err := ws.SaveFeature(f); err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}

			if jsonOutput {
				result := struct {
					FeatureID string `json:"featureId"`
					Name      string `json:"name"`
					Path      string `json:"path"`
				}{
					FeatureID: f.ID,
					Name:      f.Name,
					Path:      fmt.Sprintf(".4x/features/%s.yaml", f.ID),
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Printf("Created feature: %s (%s)\n", f.ID, name)
			fmt.Printf("  File: .4x/features/%s.yaml\n", f.ID)
			if len(parsedSubtasks) > 0 {
				fmt.Printf("  Subtasks: %d\n", len(parsedSubtasks))
			}
			fmt.Println()
			fmt.Printf("Run: 4x run %s\n", f.ID)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&repos, "repo", nil, "repos involved (can be repeated)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().StringVar(&customID, "id", "", "custom slug for feature ID (skips auto-truncation)")
	cmd.Flags().StringVar(&desc, "desc", "", "feature description (defaults to name)")
	cmd.Flags().StringSliceVar(&subtasks, "subtask", nil, `subtask in "id:name" format (can be repeated)`)
	cmd.Flags().StringSliceVar(&rules, "rule", nil, "rule reference (can be repeated)")
	cmd.Flags().StringSliceVar(&depends, "depends", nil, "dependency feature ID (can be repeated)")
	cmd.Flags().IntVar(&priority, "priority", 0, "priority level (0=critical, 1=high, 2=medium, 3=low)")
	return cmd
}

// parseSubtask 解析 "id:name" 或 "id:name:description" 格式的 subtask 字串。
func parseSubtask(s string) (feature.Subtask, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return feature.Subtask{}, fmt.Errorf("subtask format must be \"id:name\" or \"id:name:description\", got %q", s)
	}
	st := feature.Subtask{
		ID:   strings.TrimSpace(parts[0]),
		Name: strings.TrimSpace(parts[1]),
	}
	if len(parts) == 3 {
		st.Description = strings.TrimSpace(parts[2])
	}
	return st, nil
}
