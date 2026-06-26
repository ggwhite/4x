package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ggwhite/4x/internal/enrich"
	"github.com/ggwhite/4x/internal/evolution"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/spf13/cobra"
)

// evolveOpts 是 `4x evolve` 的 flag 選項。
type evolveOpts struct {
	autoRun        bool
	dryRun         bool
	minOccurrences int
	force          bool
	runnerName     string
	timeout        int
	maxRounds      int
	jsonOutput     bool
}

// evolveDeps 是 runEvolve 的可注入相依，讓 LLM 子程序與 runLoop 在測試中可用 fake 取代，
// 維持「CLI 層不直接呼叫 LLM」——所有 LLM 互動一律走 runner.Runner 介面。
type evolveDeps struct {
	// gateRunner 跑 gate role（讀 .4x/gate-input.json、寫 .4x/gate-verdicts.json）。
	gateRunner runner.Runner
	// enrichRunner 供 enrich.New 把 accepted candidate 補強為完整 feature；可為 nil（走 bare fallback）。
	enrichRunner runner.Runner
	// runFeature 對單一 enqueued feature 跑完整 meta-loop（含 worktree 隔離），回傳跑完後的 state。
	// 僅 --auto-run 時呼叫。
	runFeature func(ctx context.Context, featureID string) (protocol.State, error)
}

// newEvolveCmd 組裝 `4x evolve` 命令：把既有自我進化零件串成一條可重複跑的閉環 pipeline
// （mine → gate → enrich → enqueue →（可選）auto-run meta-loop），每輪產 .4x/evolve-report.md，
// 連續 N 輪未接受任何 candidate 即早退防空轉。每次呼叫只跑一輪，重複跑由外部（cron / 重複呼叫）驅動。
// 純 CLI 編排層，不嵌入任何 LLM API 呼叫——gate role 與 enrich 皆走 runner 子程序。
func newEvolveCmd() *cobra.Command {
	var opts evolveOpts
	cmd := &cobra.Command{
		Use:   "evolve",
		Short: "跑一輪自我進化 pipeline：mine → gate → enrich → enqueue（可選 auto-run）",
		RunE: withJsonError(&opts.jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return fmt.Errorf("not in a 4x project: %w", err)
			}
			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return err
			}

			// signal context 讓 auto-run 的 runLoop 在 Ctrl-C 時可中止（L012）。
			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			// dry-run 不 spawn runner、不跑 runLoop，故不建構 deps。
			if opts.dryRun {
				return runEvolve(ctx, ws, cfg, opts, evolveDeps{})
			}

			deps, err := buildEvolveDeps(ws, cfg, opts)
			if err != nil {
				return err
			}
			return runEvolve(ctx, ws, cfg, opts, deps)
		}),
	}
	cmd.Flags().BoolVar(&opts.autoRun, "auto-run", false, "對本輪排入的 feature 立刻跑 meta-loop（受 F098 self-mod guard 保護）")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "只讀分析：印出 mine/dedupe 摘要，不寫任何檔、不 spawn runner、不建 feature")
	cmd.Flags().IntVar(&opts.minOccurrences, "min-occurrences", protocol.DefaultFailPatternThreshold, "fail-pattern 升級為 candidate 所需的不同 feature 數門檻")
	cmd.Flags().BoolVar(&opts.force, "force", false, "覆寫 anti-spin halt，即使連續多輪未接受仍強制續跑")
	cmd.Flags().StringVar(&opts.runnerName, "runner", "", "gate / enrich / auto-run 用的 runner plugin（預設 evolution.gate_runner 或專案 default）")
	cmd.Flags().IntVar(&opts.timeout, "timeout", 3600, "LLM 子程序逾時秒數")
	cmd.Flags().IntVar(&opts.maxRounds, "max-rounds", 5, "auto-run 時每個 feature 的最大輪數")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "output as JSON")
	return cmd
}

// buildEvolveDeps 構造正式執行用的相依：gate / enrich runner 與 auto-run 的 runLoop 包裝。
// runner 名稱優先序：--runner > evolution.gate_runner > 專案 default。
func buildEvolveDeps(ws *protocol.Workspace, cfg protocol.Config, opts evolveOpts) (evolveDeps, error) {
	resolved := evolution.ResolveEvolution(cfg)
	runnerName := opts.runnerName
	if runnerName == "" {
		runnerName = resolved.GateRunner
	}
	if runnerName == "" {
		runnerName = cfg.Default
	}
	if _, ok := cfg.Runners[runnerName]; !ok {
		return evolveDeps{}, fmt.Errorf("runner %q not found in config", runnerName)
	}

	timeout := time.Duration(opts.timeout) * time.Second
	dot := ws.DotDir()

	// gate model：evolution.gate_model 優先，否則依 gate role 的 tier 解析，皆無則空字串（用 runner 預設）。
	gateModel := resolved.GateModel
	if gateModel == "" {
		if m, err := protocol.ResolveModel(cfg, runnerName, protocol.RoleGate); err == nil {
			gateModel = m
		}
	}
	gateRunner := runner.NewRunner(ws, runnerName, cfg.Runners[runnerName], timeout,
		filepath.Join(dot, "evolve-gate.log"), gateModel)

	// enrich 用較便宜的 reviewer-tier model（與 auto-discover enrichment 同策略）。
	enrichModel := ""
	if m, err := protocol.ResolveModel(cfg, runnerName, protocol.RoleReviewer); err == nil {
		enrichModel = m
	}
	enrichRunner := runner.NewRunner(ws, runnerName, cfg.Runners[runnerName], timeout,
		filepath.Join(dot, "evolve-enrich.log"), enrichModel)

	return evolveDeps{
		gateRunner:   gateRunner,
		enrichRunner: enrichRunner,
		runFeature:   makeEvolveRunFeature(ws, cfg, runnerName, opts.timeout, opts.maxRounds),
	}, nil
}

// makeEvolveRunFeature 回傳對單一 feature 跑完整 meta-loop 的閉包，沿用 batch.go runFeature 的
// worktree 隔離與 runLoop 編排。ctx 一路傳入 runLoop，確保 Ctrl-C 可中止（L012）。
func makeEvolveRunFeature(ws *protocol.Workspace, cfg protocol.Config, runnerName string, timeout, maxRounds int) func(context.Context, string) (protocol.State, error) {
	return func(ctx context.Context, featureID string) (protocol.State, error) {
		f, err := ws.LoadFeature(featureID)
		if err != nil {
			return protocol.State{}, err
		}
		if err := ws.InitFeatureDir(featureID); err != nil {
			return protocol.State{}, err
		}

		ops := gitops.New(ws.Root, ws, cfg)
		runnerWs := ws
		commitStrategy := "never"
		if cfg.Isolation == "worktree" {
			wtPath, wtErr := ops.SetupWorktree(featureID, f.Repos)
			if wtErr != nil {
				return protocol.State{}, fmt.Errorf("worktree setup: %w", wtErr)
			}
			runnerWs = &protocol.Workspace{Root: wtPath}
			ws.SkipAutoCommit = true
			defer func() { ws.SkipAutoCommit = false }()
			commitStrategy = "per-round"
		}

		s := protocol.State{
			FeatureID: featureID,
			Phase:     protocol.PhaseInit,
			MaxRounds: maxRounds,
			Active:    true,
			Runner:    runnerName,
			CreatedAt: time.Now(),
			Pid:       os.Getpid(),
		}
		if err := ws.WriteState(featureID, s); err != nil {
			return protocol.State{}, err
		}

		runnerFactory := func(rn, logPath, model string) runner.Runner {
			return runner.NewRunner(runnerWs, rn, cfg.Runners[rn], time.Duration(timeout)*time.Second, logPath, model)
		}
		runErr := runLoop(ctx, ws, runnerWs, f, cfg, s, ops, runnerFactory, commitStrategy, runnerName, nil)

		final, rerr := ws.ReadState(featureID)
		if rerr != nil {
			return protocol.State{}, runErr
		}
		return final, runErr
	}
}

// runEvolve 執行一輪 evolve pipeline。dry-run 只讀分析；否則先檢查 anti-spin halt，
// 再依序 mine → gate（pre/role/post）→ enrich → enqueue →（可選）auto-run，最後寫 report 與 state。
func runEvolve(ctx context.Context, ws *protocol.Workspace, cfg protocol.Config, opts evolveOpts, deps evolveDeps) error {
	resolved := evolution.ResolveEvolution(cfg)
	dot := ws.DotDir()

	if opts.dryRun {
		if opts.autoRun && !opts.jsonOutput {
			fmt.Println("warning: --auto-run ignored in --dry-run mode")
		}
		return runEvolveDryRun(ws, resolved, opts)
	}

	statePath := filepath.Join(dot, protocol.EvolveStateFile)
	estate, err := evolution.LoadEvolveState(statePath)
	if err != nil {
		return err
	}

	// anti-spin：達門檻且未 --force → 早退，不 mine/gate，report 標 Halted，exit 0。
	if estate.ShouldHalt(resolved.MaxIdleRounds) && !opts.force {
		result := evolution.EvolveRoundResult{
			Round:               estate.Round,
			Halted:              true,
			ConsecutiveNoAccept: estate.ConsecutiveNoAccept,
		}
		if werr := writeEvolveReport(dot, result); werr != nil {
			return werr
		}
		if opts.jsonOutput {
			return printJSON(struct {
				Round    int  `json:"round"`
				Mined    int  `json:"mined"`
				Gated    int  `json:"gated"`
				Accepted int  `json:"accepted"`
				Enqueued int  `json:"enqueued"`
				Halted   bool `json:"halted"`
			}{estate.Round, 0, 0, 0, 0, true})
		}
		fmt.Printf("evolve halted: %d consecutive idle round(s) >= max_idle_rounds %d. Use --force to override.\n",
			estate.ConsecutiveNoAccept, resolved.MaxIdleRounds)
		return nil
	}

	round := estate.Round + 1

	// 1. mine：掃描歷史失敗訊號，去重後合併入 candidate pool 並存檔。
	// feature 清單一次讀取，掃描器與去重、gate 皆共用同一份。
	existing, err := ws.ListFeatures()
	if err != nil {
		return err
	}
	existingPool, err := protocol.LoadCandidates(filepath.Join(dot, protocol.CandidatesFile))
	if err != nil {
		return err
	}
	all, learnings := scanEvolveCandidates(ws, existing, opts.minOccurrences)
	keptNew := protocol.DedupeCandidates(all, existing, existingPool.Candidates)
	merged := make([]protocol.Candidate, 0, len(existingPool.Candidates)+len(keptNew))
	merged = append(merged, existingPool.Candidates...)
	merged = append(merged, keptNew...)
	pool := protocol.CandidatePool{Version: 1, GeneratedAt: time.Now(), Candidates: merged, Learnings: learnings}
	if err := pool.Save(filepath.Join(dot, protocol.CandidatesFile)); err != nil {
		return fmt.Errorf("save candidates: %w", err)
	}

	// 2. gate pre：對既有 feature 與彼此去重，倖存者寫 gate-input.json。
	gateInput, preDropped := evolution.PreVeto(merged, existing, resolved.DedupThreshold)
	if err := (protocol.CandidatePool{Version: 1, Candidates: gateInput}).Save(filepath.Join(dot, protocol.GateInputFile)); err != nil {
		return fmt.Errorf("save gate-input: %w", err)
	}
	deduped := (len(all) - len(keptNew)) + len(preDropped)

	// 3. gate role：runner 子程序讀 gate-input.json，寫 gate-verdicts.json。空輸入則略過 LLM。
	var verdicts []evolution.Verdict
	if len(gateInput) > 0 {
		prompt, perr := buildGatePrompt(ws, cfg)
		if perr != nil {
			return perr
		}
		if _, rerr := deps.gateRunner.Run(ctx, prompt); rerr != nil {
			return fmt.Errorf("gate runner: %w", rerr)
		}
		verdicts, perr = evolution.ParseVerdicts(filepath.Join(dot, protocol.GateVerdictsFile))
		if perr != nil {
			return fmt.Errorf("gate role produced no parseable verdicts: %w", perr)
		}
	}

	// 4. gate post：套用不可翻硬否決與 convergence cap，通過者寫 accepted-candidates.json。
	accepted, rejected := evolution.PostVeto(gateInput, verdicts, existing, resolved)
	if err := (protocol.CandidatePool{Version: 1, Candidates: accepted}).Save(filepath.Join(dot, protocol.AcceptedCandidatesFile)); err != nil {
		return fmt.Errorf("save accepted-candidates: %w", err)
	}

	// 5+6. enrich + enqueue：每個 accepted 物化成 not-started feature（enrich 失敗則 bare fallback）。
	idf := feat.ResolveIDFormat(cfg.FeatureIDPrefix, cfg.FeatureIDDigits)
	enqueued := enqueueAccepted(ctx, ws, accepted, deps.enrichRunner, idf)

	// 7. auto-run（可選）：對每個排入的 feature 跑 meta-loop，受 F098 self-mod guard 標記。
	var autoRan []evolution.AutoRunResult
	if opts.autoRun && deps.runFeature != nil {
		autoRan = autoRunEnqueued(ctx, ws, enqueued, deps.runFeature)
	}

	// 8. anti-spin 計數更新 + 持久化。
	if len(accepted) == 0 {
		estate.ConsecutiveNoAccept++
	} else {
		estate.ConsecutiveNoAccept = 0
	}
	estate.Version = 1
	estate.Round = round
	estate.LastRunAt = time.Now()
	if err := estate.Save(statePath); err != nil {
		return fmt.Errorf("save evolve-state: %w", err)
	}

	// 9. 寫 report。
	result := evolution.EvolveRoundResult{
		Round:               round,
		Mined:               gateInput,
		Deduped:             deduped,
		Accepted:            accepted,
		Rejected:            rejected,
		Enqueued:            enqueued,
		AutoRan:             autoRan,
		Halted:              false,
		ConsecutiveNoAccept: estate.ConsecutiveNoAccept,
	}
	if err := writeEvolveReport(dot, result); err != nil {
		return err
	}

	if opts.jsonOutput {
		return printJSON(struct {
			Round    int `json:"round"`
			Mined    int `json:"mined"`
			Gated    int `json:"gated"`
			Accepted int `json:"accepted"`
			Enqueued int `json:"enqueued"`
		}{round, len(all), len(gateInput), len(accepted), len(enqueued)})
	}

	fmt.Printf("evolve round %d: mined %d → gate %d → accepted %d → enqueued %d (auto-ran %d)\n",
		round, len(all), len(gateInput), len(accepted), len(enqueued), len(autoRan))
	return nil
}

// runEvolveDryRun 只讀分析：mine 掃描 + dedupe + PreVeto 皆在記憶體，印摘要到 stdout，
// 不寫任何 .4x/ 檔、不 spawn runner、不建 feature。
func runEvolveDryRun(ws *protocol.Workspace, resolved evolution.ResolvedEvolution, opts evolveOpts) error {
	dot := ws.DotDir()
	// feature 清單一次讀取，掃描器與去重、PreVeto 皆共用同一份。
	existing, err := ws.ListFeatures()
	if err != nil {
		return err
	}
	existingPool, err := protocol.LoadCandidates(filepath.Join(dot, protocol.CandidatesFile))
	if err != nil {
		return err
	}
	all, _ := scanEvolveCandidates(ws, existing, opts.minOccurrences)
	keptNew := protocol.DedupeCandidates(all, existing, existingPool.Candidates)
	merged := make([]protocol.Candidate, 0, len(existingPool.Candidates)+len(keptNew))
	merged = append(merged, existingPool.Candidates...)
	merged = append(merged, keptNew...)
	gateInput, _ := evolution.PreVeto(merged, existing, resolved.DedupThreshold)

	if opts.jsonOutput {
		return printJSON(struct {
			DryRun       bool `json:"dryRun"`
			Scanned      int  `json:"scanned"`
			New          int  `json:"new"`
			WouldEnqueue int  `json:"wouldEnqueue"`
		}{true, len(all), len(keptNew), len(gateInput)})
	}

	fmt.Printf("dry-run: scanned %d candidate(s), %d new after dedupe, %d would be sent to gate\n",
		len(all), len(keptNew), len(gateInput))
	fmt.Println("dry-run: nothing written, no runner spawned, no feature created.")
	return nil
}

// enqueueAccepted 把每個 accepted candidate 物化成 not-started feature 並存檔。
// enrichRunner 非 nil 時先經 LLM 補強；補強失敗或 discarded 則退回 bare candidate（裁決 3）。
func enqueueAccepted(ctx context.Context, ws *protocol.Workspace, accepted []protocol.Candidate, enrichRunner runner.Runner, idf feat.IDFormat) []evolution.EnqueuedFeature {
	var enricher *enrich.Enricher
	if enrichRunner != nil {
		// autoApprove=true → enrich 成功時 status 即 not-started（裁決 2：閘門即核准）。
		enricher = enrich.New(ws, enrichRunner, true)
	}

	var enqueued []evolution.EnqueuedFeature
	for _, c := range accepted {
		next, nerr := feat.NextNumber(ws, idf)
		if nerr != nil {
			slog.Warn("evolve: next feature number failed", "title", c.Title, "error", nerr)
			continue
		}
		id := feat.GenerateFeatureID(next, c.Title, idf)
		name := idf.FormatDisplayName(next, c.Title)

		var f feat.Feature
		enriched := false
		if enricher != nil {
			result, eerr := enricher.Enrich(ctx, evolution.CandidateToDiscovered(c))
			switch {
			case eerr != nil:
				slog.Warn("evolve: enrichment error, falling back to bare feature", "title", c.Title, "error", eerr)
				f = evolution.BareFeature(c, id, name)
			case result.Discarded:
				slog.Info("evolve: enrichment discarded, falling back to bare feature", "title", c.Title, "reason", result.Reason)
				f = evolution.BareFeature(c, id, name)
			default:
				f = result.Feature
				f.ID = id
				f.Name = name
				f.Status = feat.StatusNotStarted
				enriched = true
			}
		} else {
			f = evolution.BareFeature(c, id, name)
		}

		if serr := ws.SaveFeature(f); serr != nil {
			slog.Warn("evolve: save feature failed", "title", c.Title, "error", serr)
			continue
		}
		enqueued = append(enqueued, evolution.EnqueuedFeature{ID: id, Name: name, Enriched: enriched})
	}
	return enqueued
}

// autoRunEnqueued 對每個排入的 feature 跑 meta-loop，並用 F098 self-mod guard 判斷是否需人工 approve。
// 觸及受保護路徑且未核准者不視為自動完成，於 report 標 SelfModBlocked（不繞過 guard）。
func autoRunEnqueued(ctx context.Context, ws *protocol.Workspace, enqueued []evolution.EnqueuedFeature, runFeature func(context.Context, string) (protocol.State, error)) []evolution.AutoRunResult {
	var results []evolution.AutoRunResult
	for _, ef := range enqueued {
		st, rerr := runFeature(ctx, ef.ID)
		if rerr != nil {
			slog.Warn("evolve auto-run: feature run failed", "feature", ef.ID, "error", rerr)
		}
		blocked := guard.SelfModNeedsApproval(st, false)
		if blocked {
			slog.Warn("evolve auto-run: self-mod guard blocked auto-complete — use 4x done --approve-self-mod",
				"feature", ef.ID, "paths", st.SelfModPaths)
		}
		results = append(results, evolution.AutoRunResult{
			ID:             ef.ID,
			FinalStatus:    string(st.Phase),
			SelfModBlocked: blocked,
		})
	}
	return results
}

// scanEvolveCandidates 跑三個 best-effort 掃描器（escalation / stuck / fail-pattern），
// 回傳彙整後的 candidate 與 fail-pattern 衍生的 learnings。
// features 由呼叫端傳入，避免重複呼叫 ListFeatures。
func scanEvolveCandidates(ws *protocol.Workspace, features []feat.Feature, minOccurrences int) ([]protocol.Candidate, []protocol.CandidateLearning) {
	esc := protocol.ScanEscalations(ws, features)
	stuck := protocol.ScanStuckFeatures(ws, features)
	failCands, learnings := protocol.ScanFailPatterns(ws, features, minOccurrences)
	all := make([]protocol.Candidate, 0, len(esc)+len(stuck)+len(failCands))
	all = append(all, esc...)
	all = append(all, stuck...)
	all = append(all, failCands...)
	return all, learnings
}

// buildGatePrompt 直接 render gate.md.tmpl（gate 不綁特定 feature），不經 run loop 的 generatePrompt。
// template 為靜態文字 + locale 前綴，故只需帶 Locale/LocaleName 與 Config/DotDir。
func buildGatePrompt(ws *protocol.Workspace, cfg protocol.Config) (string, error) {
	tmpl, err := loadRoleTemplate(ws.DotDir(), protocol.RoleGate)
	if err != nil {
		return "", err
	}
	locale, localeName := resolveLocale()
	var b strings.Builder
	data := promptData{
		Role:       protocol.RoleGate,
		Config:     cfg,
		DotDir:     ws.DotDir(),
		Locale:     locale,
		LocaleName: localeName,
	}
	if err := tmpl.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// writeEvolveReport 把單輪結果寫成 .4x/evolve-report.md。
func writeEvolveReport(dot string, r evolution.EvolveRoundResult) error {
	path := filepath.Join(dot, protocol.EvolveReportFile)
	if err := os.WriteFile(path, []byte(evolution.BuildEvolveReport(r)), 0o644); err != nil {
		return fmt.Errorf("write evolve-report: %w", err)
	}
	return nil
}
