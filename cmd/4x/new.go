package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/vcshub"
	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var (
		repos       []string
		jsonOutput  bool
		customID    string
		desc        string
		subtasks    []string
		rules       []string
		depends     []string
		priority    int
		profileName string
		issueRefs   []string
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

			cfg, cerr := ws.ReadConfig()
			if cerr != nil {
				if jsonOutput {
					return jsonError(cerr.Error())
				}
				return cerr
			}
			idf := feature.ResolveIDFormat(cfg.FeatureIDPrefix, cfg.FeatureIDDigits)

			// issueTrackerRepos 是 issue_tracker.enabled 時要建立/連結 issue 的 repo 名稱清單：
			// 沿用 --repo 宣告的 repos；monorepo 或未指定 --repo 時預設單一 "." repo。
			issueTrackerRepos := repos
			if len(issueTrackerRepos) == 0 {
				issueTrackerRepos = []string{"."}
			}

			if cfg.IssueTracker.Enabled {
				for _, repoName := range issueTrackerRepos {
					path := repoPath(cwd, cfg, repoName)
					if err := vcshub.New(path).Preflight(path); err != nil {
						err = fmt.Errorf("issue tracker preflight failed for repo %q: %w", repoName, err)
						if jsonOutput {
							return jsonError(err.Error())
						}
						return err
					}
				}
			}

			opts := feature.CreateOpts{
				Name:        name,
				Description: description,
				CustomID:    customID,
				Subtasks:    parsedSubtasks,
				Rules:       rules,
				Depends:     depends,
				Repos:       repos,
				Profile:     profileName,
				IDFormat:    idf,
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

			var issueLines []string
			if cfg.IssueTracker.Enabled {
				issueRefByRepo := make(map[string]string, len(issueRefs))
				for _, raw := range issueRefs {
					repoName, ref := parseIssueRef(raw)
					if repoName == "" && len(issueTrackerRepos) == 1 {
						repoName = issueTrackerRepos[0]
					}
					if repoName != "" {
						issueRefByRepo[repoName] = ref
					}
				}

				for _, repoName := range issueTrackerRepos {
					path := repoPath(cwd, cfg, repoName)
					hub := vcshub.New(path)
					var id, url string
					var issueErr error
					if ref, ok := issueRefByRepo[repoName]; ok {
						id, url, issueErr = hub.GetIssue(path, ref)
					} else {
						id, url, issueErr = hub.CreateIssue(path, f.Name, f.Description)
					}
					if issueErr != nil {
						warning := fmt.Sprintf("repo %s: %v", repoName, issueErr)
						f.Warnings = append(f.Warnings, warning)
						issueLines = append(issueLines, fmt.Sprintf("  issue failed [%s]: %v", repoName, issueErr))
						continue
					}
					f.Issues = append(f.Issues, feature.IssueRef{Repo: repoName, ID: id, URL: url})
					issueLines = append(issueLines, fmt.Sprintf("  issue [%s]: %s", repoName, url))
				}

				if err := ws.SaveFeature(f); err != nil {
					if jsonOutput {
						return jsonError(err.Error())
					}
					return err
				}
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
			for _, line := range issueLines {
				fmt.Println(line)
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
	cmd.Flags().StringVar(&profileName, "profile", "", "pipeline profile written to feature YAML (e.g. full/normal/quick)")
	cmd.Flags().StringSliceVar(&issueRefs, "issue", nil, `link an existing issue in "repo:id-or-url" format (can be repeated); repo prefix optional for single-repo features`)
	return cmd
}

// parseIssueRef 解析 --issue 的值：http(s) 開頭一律無 repo 前綴（純 URL）；
// 否則第一個冒號前為 repo、之後為 ref；皆非則整串視為 ref（無 repo 前綴）。
func parseIssueRef(s string) (repo, ref string) {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return "", s
	}
	if idx := strings.Index(s, ":"); idx != -1 {
		return s[:idx], s[idx+1:]
	}
	return "", s
}

// repoPath 將 issue tracker 的 repo 名稱解析為實際檔案路徑，沿用 protocol.ResolveRepoPaths
// 作為 repo→path 解析的單一真相源；未知名稱與 "." 皆 fallback 回 root。
func repoPath(root string, cfg protocol.Config, name string) string {
	if path, ok := protocol.ResolveRepoPaths(cfg, root)[name]; ok && path != "" {
		return path
	}
	return root
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
