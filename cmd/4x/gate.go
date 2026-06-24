package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/evolution"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

// gateOpts 是 `4x gate` 的 flag 選項；pre/post 互斥，必須擇一。
type gateOpts struct {
	pre  bool
	post bool
	json bool
}

// newGateCmd 組裝 `4x gate` 命令：對 candidate pool 套用 F097 價值閘門的雙層 veto。
// 純 CLI 層，不呼叫 LLM——gate role 由 runner 在 --pre 與 --post 之間執行（F099 編排）。
func newGateCmd() *cobra.Command {
	var opts gateOpts
	cmd := &cobra.Command{
		Use:   "gate",
		Short: "對 candidate feature 套用 evolve 價值閘門 veto（pre/post）",
		RunE: withJsonError(&opts.json, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			return runGate(cwd, opts)
		}),
	}
	cmd.Flags().BoolVar(&opts.pre, "pre", false, "PRE-veto：去重 candidates.json 產出 gate-input.json")
	cmd.Flags().BoolVar(&opts.post, "post", false, "POST-veto：套用 gate-verdicts.json 產出 accepted-candidates.json")
	cmd.Flags().BoolVar(&opts.json, "json", false, "output as JSON")
	return cmd
}

// runGate 依 opts 執行 PRE 或 POST veto。pre/post 必須擇一，皆給或皆未給回 usage error。不呼叫 LLM。
func runGate(dir string, opts gateOpts) error {
	if opts.pre == opts.post {
		return fmt.Errorf("specify exactly one of --pre or --post")
	}
	ws, err := protocol.Find(dir)
	if err != nil {
		return fmt.Errorf("not in a 4x project: %w", err)
	}
	cfg, err := ws.LoadMergedConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	resolved := evolution.ResolveEvolution(cfg)
	dot := ws.DotDir()

	if opts.pre {
		return runGatePre(ws, dot, resolved, opts.json)
	}
	return runGatePost(ws, dot, resolved, opts.json)
}

// runGatePre 讀 candidates.json，對既有 feature 做 Jaccard 去重，把倖存者寫成 gate-input.json。
func runGatePre(ws *protocol.Workspace, dot string, cfg evolution.ResolvedEvolution, jsonOutput bool) error {
	pool, err := protocol.LoadCandidates(filepath.Join(dot, protocol.CandidatesFile))
	if err != nil {
		return err
	}
	existing, err := ws.ListFeatures()
	if err != nil {
		return err
	}
	kept, dropped := evolution.PreVeto(pool.Candidates, existing, cfg.DedupThreshold)
	out := protocol.CandidatePool{Version: 1, Candidates: kept}
	if err := out.Save(filepath.Join(dot, protocol.GateInputFile)); err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(struct {
			Phase   string `json:"phase"`
			Kept    int    `json:"kept"`
			Dropped int    `json:"dropped"`
		}{"pre", len(kept), len(dropped)})
	}
	fmt.Printf("pre-veto: kept %d, dropped %d (duplicate)\n", len(kept), len(dropped))
	return nil
}

// runGatePost 讀 gate-input.json 與 gate-verdicts.json，套用不可翻硬否決與 cap，
// 把通過者寫成 accepted-candidates.json，並印出每筆否決原因。
func runGatePost(ws *protocol.Workspace, dot string, cfg evolution.ResolvedEvolution, jsonOutput bool) error {
	pool, err := protocol.LoadCandidates(filepath.Join(dot, protocol.GateInputFile))
	if err != nil {
		return err
	}
	verdicts, err := evolution.ParseVerdicts(filepath.Join(dot, protocol.GateVerdictsFile))
	if err != nil {
		return err
	}
	existing, err := ws.ListFeatures()
	if err != nil {
		return err
	}
	accepted, rejected := evolution.PostVeto(pool.Candidates, verdicts, existing, cfg)
	out := protocol.CandidatePool{Version: 1, Candidates: accepted}
	if err := out.Save(filepath.Join(dot, protocol.AcceptedCandidatesFile)); err != nil {
		return err
	}
	if jsonOutput {
		rej := make([]gateRejection, 0, len(rejected))
		for _, r := range rejected {
			rej = append(rej, gateRejection{Title: r.Title, Reason: r.Reason})
		}
		return printJSON(struct {
			Phase    string          `json:"phase"`
			Accepted int             `json:"accepted"`
			Rejected []gateRejection `json:"rejected"`
		}{"post", len(accepted), rej})
	}
	fmt.Printf("post-veto: accepted %d, rejected %d\n", len(accepted), len(rejected))
	for _, r := range rejected {
		fmt.Printf("  reject %q: %s\n", r.Title, r.Reason)
	}
	return nil
}

// gateRejection 是 gate --post --json 輸出中單一被否決 candidate 的標題與原因。
type gateRejection struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}
