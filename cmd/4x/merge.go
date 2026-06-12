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

			exec.Command("git", "-C", wtDir, "add", "-A").Run()
			if exec.Command("git", "-C", wtDir, "diff", "--cached", "--quiet").Run() != nil {
				f, _ := ws.LoadFeature(featureID)
				msg := fmt.Sprintf("fix(%s): resolve merge conflicts", featureID)
				if f.Name != "" {
					msg = fmt.Sprintf("fix(%s): resolve merge conflicts — %s", featureID, f.Name)
				}
				exec.Command("git", "-C", wtDir, "commit", "-m", msg).Run()
			}

			branch := worktree.Branch(featureID)
			f, _ := ws.LoadFeature(featureID)
			name := featureID
			if f.Name != "" {
				name = f.Name
			}
			mergeMsg := fmt.Sprintf("Merge branch '%s' — %s", branch, name)

			out, err := exec.Command("git", "-C", ws.Root, "merge", branch, "-m", mergeMsg).CombinedOutput()
			if err != nil {
				return fmt.Errorf("merge still has conflicts: %s", string(out))
			}

			if err := worktree.Cleanup(ws.Root, featureID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: cleanup failed: %v\n", err)
			}

			fmt.Printf("Merged and cleaned up branch %s.\n", branch)
			return nil
		},
	}
}
