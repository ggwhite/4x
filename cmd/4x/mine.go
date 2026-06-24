package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

// newMineCmd 組裝 `4x mine` 命令：掃描整個 .4x/ 的歷史失敗訊號
// （escalation / stuck feature / 跨 feature 反覆 FAIL pattern），
// 彙整成 candidate pool 輸出到 .4x/candidates.json。純 CLI 層，不呼叫 LLM、不自動建 feature。
func newMineCmd() *cobra.Command {
	var minOccurrences int
	var output string
	var dryRun bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "mine",
		Short: "掃描 .4x/ 歷史失敗訊號，產出 candidate pool",
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return fmt.Errorf("not in a 4x project: %w", err)
			}

			if output == "" {
				output = filepath.Join(ws.DotDir(), protocol.CandidatesFile)
			}

			existingPool, err := protocol.LoadCandidates(output)
			if err != nil {
				return fmt.Errorf("load existing candidates: %w", err)
			}

			// 取 feature 清單一次，供掃描器與去重共用；失敗時以空 slice 繼續（少一層比對，不中斷產出）。
			existingFeatures, err := ws.ListFeatures()
			if err != nil {
				slog.Warn("mine: list features failed, scanners and dedupe will skip feature-level checks", "error", err)
				existingFeatures = []feature.Feature{}
			}

			// 三個掃描器各自 best-effort，內部錯誤只 log 不中斷。
			escalations := protocol.ScanEscalations(ws, existingFeatures)
			stuck := protocol.ScanStuckFeatures(ws, existingFeatures)
			failCands, learnings := protocol.ScanFailPatterns(ws, existingFeatures, minOccurrences)

			all := make([]protocol.Candidate, 0, len(escalations)+len(stuck)+len(failCands))
			all = append(all, escalations...)
			all = append(all, stuck...)
			all = append(all, failCands...)

			kept := protocol.DedupeCandidates(all, existingFeatures, existingPool.Candidates)

			// 合併既有 candidate 與本次新挖出的 kept：kept 已對 existingPool.Candidates 去重，
			// 故 append 不會重複。如此對未變動的歷史重跑時 pool 維持穩定（冪等），
			// 既有但尚未被 F097 消化的候選不會被覆寫遺失。
			merged := make([]protocol.Candidate, 0, len(existingPool.Candidates)+len(kept))
			merged = append(merged, existingPool.Candidates...)
			merged = append(merged, kept...)

			pool := protocol.CandidatePool{
				Version:     1,
				GeneratedAt: time.Now(),
				Candidates:  merged,
				Learnings:   learnings,
			}

			if !dryRun {
				if err := pool.Save(output); err != nil {
					return fmt.Errorf("save candidates: %w", err)
				}
			}

			if jsonOutput {
				return printJSON(struct {
					Candidates int    `json:"candidates"`
					Output     string `json:"output"`
					DryRun     bool   `json:"dryRun"`
				}{len(pool.Candidates), output, dryRun})
			}

			printMineSummary(cmd, pool, len(escalations), len(stuck), len(failCands), len(all)-len(kept), output, dryRun)
			return nil
		}),
	}

	cmd.Flags().IntVar(&minOccurrences, "min-occurrences", protocol.DefaultFailPatternThreshold,
		"fail-pattern 升級為 candidate 所需的不同 feature 數門檻")
	cmd.Flags().StringVar(&output, "output", "",
		"candidate pool 輸出路徑（預設 .4x/candidates.json）")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"只印摘要不寫檔")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

// printMineSummary 印出 mine 結果摘要：各 source 計數、去重後總數、learnings 數與輸出路徑。
func printMineSummary(cmd *cobra.Command, pool protocol.CandidatePool, escN, stuckN, failN, dropped int, output string, dryRun bool) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Scanned sources: escalation=%d stuck=%d fail-pattern=%d\n", escN, stuckN, failN)
	fmt.Fprintf(out, "Candidates: %d kept (%d deduped), learnings: %d\n", len(pool.Candidates), dropped, len(pool.Learnings))
	if dryRun {
		fmt.Fprintln(out, "Dry run — nothing written.")
		return
	}
	fmt.Fprintf(out, "Wrote %s\n", output)
}
