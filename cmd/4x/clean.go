package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

// newCleanCmd 建立 `4x clean` 命令，清理已完成（done/abandoned）feature 的
// workspace artifacts（logs、rounds、reports、state.json、events.jsonl）。
// feature 定義 .4x/features/*.yaml 永遠保留；無參數清全部，帶 feature-id 只清指定者。
func newCleanCmd() *cobra.Command {
	var dryRun, force bool

	cmd := &cobra.Command{
		Use:   "clean [feature-id]",
		Short: "Remove workspace artifacts for completed features",
		Long: `Clean up .4x/{feature-id}/ directories for done or abandoned features.

Removes logs, rounds, reports, and state files.
Feature definitions (.4x/features/*.yaml) are always preserved.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				return cleanSingle(ws, args[0], dryRun, force)
			}
			return cleanAll(ws, dryRun, force)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "List cleanable features without deleting")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func cleanSingle(ws *protocol.Workspace, prefix string, dryRun, force bool) error {
	featureID, err := ws.ResolveFeatureID(prefix)
	if err != nil {
		return err
	}

	candidates, err := ws.CleanableFeatures()
	if err != nil {
		return err
	}

	var target *protocol.CleanCandidate
	for i := range candidates {
		if candidates[i].FeatureID == featureID {
			target = &candidates[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("feature %s is not cleanable (must be done/abandoned with workspace)", featureID)
	}

	if dryRun {
		fmt.Printf("  %-30s %s\n", target.FeatureID, protocol.HumanSize(target.Size))
		return nil
	}

	if !force {
		printCleanWarning()
		fmt.Printf("  %-30s %s\n", target.FeatureID, protocol.HumanSize(target.Size))
		fmt.Println()
		if !confirmPrompt("Clean this feature?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	freed, err := ws.CleanFeature(featureID)
	if err != nil {
		return err
	}
	fmt.Printf("Cleaned %s, freed %s\n", featureID, protocol.HumanSize(freed))
	return nil
}

func cleanAll(ws *protocol.Workspace, dryRun, force bool) error {
	candidates, err := ws.CleanableFeatures()
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		fmt.Println("Nothing to clean.")
		return nil
	}

	var total int64
	for _, c := range candidates {
		total += c.Size
	}

	if dryRun {
		printCleanWarning()
		printCandidates(candidates, total)
		return nil
	}

	if !force {
		printCleanWarning()
		printCandidates(candidates, total)
		fmt.Println()
		if !confirmPrompt("Clean all?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var freed int64
	var cleaned int
	for _, c := range candidates {
		f, err := ws.CleanFeature(c.FeatureID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skip %s: %v\n", c.FeatureID, err)
			continue
		}
		freed += f
		cleaned++
	}
	fmt.Printf("Cleaned %d features, freed %s\n", cleaned, protocol.HumanSize(freed))
	return nil
}

func printCleanWarning() {
	fmt.Println("⚠ Warning: Cleaned features will lose detailed logs, reports, and round")
	fmt.Println("  history in the dashboard. Feature definitions and status are preserved.")
	fmt.Println()
}

func printCandidates(candidates []protocol.CleanCandidate, total int64) {
	fmt.Printf("Found %d cleanable features (done/abandoned):\n", len(candidates))
	for _, c := range candidates {
		fmt.Printf("  %-30s %s\n", c.FeatureID, protocol.HumanSize(c.Size))
	}
	fmt.Printf("  %-30s %s\n", "Total:", protocol.HumanSize(total))
}

func confirmPrompt(msg string) bool {
	fmt.Printf("%s [y/N] ", msg)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line)) == "y"
}
