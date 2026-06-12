package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/worktree"
	"github.com/spf13/cobra"
)

func newMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge <feature-id>",
		Short: "Complete merge after resolving conflicts",
		Long:  "Use after '4x done' reported a merge conflict and you resolved it in the worktree.",
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

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}

			s, err := ws.ReadState(featureID)
			if err != nil {
				return fmt.Errorf("cannot read state for %s: %w", featureID, err)
			}
			if s.Phase != protocol.PhaseDone {
				return fmt.Errorf("feature %s is in phase %q, not done (run '4x done %s' first)", featureID, s.Phase, featureID)
			}

			wtDir := worktree.Dir(ws.Root, featureID)
			if _, err := os.Stat(wtDir); err != nil {
				return fmt.Errorf("no worktree found at %s", wtDir)
			}

			f, _ := ws.LoadFeature(featureID)
			name := featureID
			if f.Name != "" {
				name = f.Name
			}

			if err := exec.Command("git", "-C", wtDir, "add", "-A").Run(); err != nil {
				return fmt.Errorf("git add in worktree failed: %w", err)
			}
			if exec.Command("git", "-C", wtDir, "diff", "--cached", "--quiet").Run() != nil {
				msg := fmt.Sprintf("fix(%s): resolve merge conflicts — %s", featureID, name)
				if out, err := exec.Command("git", "-C", wtDir, "commit", "-m", msg).CombinedOutput(); err != nil {
					return fmt.Errorf("commit in worktree failed: %s", string(out))
				}
			}

			result := worktree.Merge(ws.Root, featureID, name)
			if result.Conflict {
				fmt.Println("Merge still has conflicts:")
				for _, file := range result.Files {
					fmt.Printf("  conflict: %s\n", file)
				}
				return nil
			}
			if result.Error != "" {
				return fmt.Errorf("merge failed: %s", result.Error)
			}

			fmt.Printf("Merged and cleaned up branch %s.\n", worktree.Branch(featureID))
			return nil
		},
	}
}
