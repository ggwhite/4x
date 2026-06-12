package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var repos []string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new feature",
		Args:  cobra.ExactArgs(1),
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

			next, err := protocol.NextFeatureNumber(ws)
			if err != nil {
				if jsonOutput {
					return jsonError(err.Error())
				}
				return err
			}
			id := protocol.GenerateFeatureID(next, name)

			repoMap := make(map[string]string)
			for _, r := range repos {
				repoMap[r] = ""
			}

			displayName := fmt.Sprintf("F%03d: %s", next, name)

			feature := protocol.Feature{
				ID:          id,
				Name:        displayName,
				Description: name,
				Status:      "not-started",
				Repos:       repoMap,
			}

			if err := ws.SaveFeature(feature); err != nil {
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
					FeatureID: id,
					Name:      displayName,
					Path:      fmt.Sprintf(".4x/features/%s.yaml", id),
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Printf("Created feature: %s (%s)\n", id, name)
			fmt.Printf("  File: .4x/features/%s.yaml\n", id)
			fmt.Println()
			fmt.Println("Edit the YAML to add description, repos, subtasks, and rules.")
			fmt.Printf("Then run: 4x run %s\n", id)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&repos, "repo", nil, "repos involved (can be repeated)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
