package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var repos []string

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new feature",
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

			name := args[0]

			next, err := nextFeatureNumber(ws)
			if err != nil {
				return err
			}
			id := generateID(next, name)

			repoMap := make(map[string]string)
			for _, r := range repos {
				repoMap[r] = ""
			}

			feature := protocol.Feature{
				ID:          id,
				Name:        name,
				Description: name,
				Status:      "not-started",
				Repos:       repoMap,
			}

			if err := ws.SaveFeature(feature); err != nil {
				return err
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
	return cmd
}

var (
	nonAlphaNum   = regexp.MustCompile(`[^a-z0-9]+`)
	featureNumRe  = regexp.MustCompile(`^F(\d{3})-`)
)

// nextFeatureNumber 掃描現有 feature，回傳下一個流水號
func nextFeatureNumber(ws *protocol.Workspace) (int, error) {
	features, err := ws.ListFeatures()
	if err != nil {
		return 1, nil
	}
	max := 0
	for _, f := range features {
		if m := featureNumRe.FindStringSubmatch(f.ID); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > max {
				max = n
			}
		}
	}
	return max + 1, nil
}

// generateID 產生 F{NNN}-{slug} 格式的 feature ID
func generateID(num int, name string) string {
	slug := strings.ToLower(name)
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 25 {
		slug = slug[:25]
		slug = strings.TrimRight(slug, "-")
	}
	return fmt.Sprintf("F%03d-%s", num, slug)
}
