package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/health"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

// healthCheckExecutor 回傳一個執行單一 health check command 的 executor，
// 每個 command 以 sh -c 執行並套用 per-command timeout（timeoutSec <= 0 時預設 30 秒），
// 失敗時把 command 與輸出寫到 stderr 方便排查。
func healthCheckExecutor(ctx context.Context, timeoutSec int) func(cmd string) error {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return func(cmd string) error {
		cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cmdCtx, "sh", "-c", cmd).CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  health check failed: %s\n%s\n", cmd, string(out))
		}
		return err
	}
}

// logStateWriteErr 在終止／失敗／收尾路徑記錄 WriteState 失敗，取代靜默的 `_ =` 丟棄。
// 這些路徑不改變回傳或控制流（state 已盡力寫出），但失敗必須留下可排查的記錄，
// 避免磁碟 state.json 與記憶體狀態不一致時毫無線索。err 為 nil 時不做任何事。
func logStateWriteErr(err error, featureID string, phase protocol.Phase) {
	if err != nil {
		slog.Error("write state failed", "feature", featureID, "phase", phase, "error", err)
	}
}

// logSyncErr 在終止／失敗／收尾路徑記錄 SyncFeatureStatus 失敗，語意同 logStateWriteErr。
// feature_list.json 狀態同步失敗只影響 dashboard 顯示、不阻斷流程，故僅記錄不回傳。
func logSyncErr(err error, featureID string, phase protocol.Phase) {
	if err != nil {
		slog.Error("sync feature status failed", "feature", featureID, "phase", phase, "error", err)
	}
}

// startBackgroundRun 以 background 方式啟動 run 子程序，將其 stdout/stderr 導向 logPath，
// 讓早期錯誤（config 載入、worktree setup、runner not found 等）可事後檢視而非進 /dev/null。
// Start 後子程序已持有 fd 副本，父程序立即關閉自身副本並背景 Wait 回收 zombie。
// 回傳啟動後的 *os.Process（供取 PID）。
func startBackgroundRun(binPath string, args []string, dir, logPath string) (*os.Process, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open run log: %w", err)
	}
	bgCmd := exec.Command(binPath, args...)
	bgCmd.Dir = dir
	bgCmd.Stdin = nil
	bgCmd.Stdout = logFile
	bgCmd.Stderr = logFile
	if err := bgCmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("failed to start run: %w", err)
	}
	logFile.Close()
	go bgCmd.Wait()
	return bgCmd.Process, nil
}

// worktreeExitHints 依 run 結束時的最終 phase 與 commit 策略，回傳要印給使用者的
// worktree 相關提示行（不含前綴換行）。done/pending-review 回傳 branch 與 merge 指令；
// 其餘結束狀態（needs-attention/blocked/中斷/loopErr）回傳 worktree 路徑與清理提示，
// 讓孤兒 worktree 可見。wtPath 為空（非 worktree 模式）時回 nil。
// 提示由呼叫端走 stdout 印出，確保 background/JSON 模式下會寫進 run.log 不遺失。
func worktreeExitHints(wtPath, featureID string, finalPhase protocol.Phase, commitStrategy string) []string {
	if wtPath == "" {
		return nil
	}
	if finalPhase == protocol.PhaseDone || finalPhase == protocol.PhasePendingReview {
		if commitStrategy == "never" {
			return nil
		}
		return []string{
			fmt.Sprintf("  branch: 4x/%s", featureID),
			fmt.Sprintf("  to merge: git merge 4x/%s && git worktree remove %s && git branch -d 4x/%s", featureID, wtPath, featureID),
		}
	}
	return []string{
		fmt.Sprintf("  worktree preserved at: %s (state: %s)", wtPath, finalPhase),
		fmt.Sprintf("  inspect changes there; when done clean up with: git worktree remove %s && git branch -d 4x/%s", wtPath, featureID),
	}
}

func newRunCmd() *cobra.Command {
	var runnerName string
	var maxRounds int
	var timeout int
	var dryRun bool
	var jsonOutput bool
	var profileFlag string
	var noNotify bool

	cmd := &cobra.Command{
		Use:   "run <feature-id>",
		Short: "Run the Design-Code-Review-Test loop for a feature",
		Args:  cobra.ExactArgs(1),
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
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
			feature, err := ws.LoadFeature(featureID)
			if err != nil {
				return err
			}

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return err
			}

			syncPlugins(ws.Root, cfg)

			// manualRunner 保存使用者顯式指定的 --runner（覆寫優先序最高層，全 phase 套用）；
			// 未指定時為空，讓 per-phase profile/feature override 與 default_runner 生效。
			manualRunner := runnerName
			if runnerName == "" {
				runnerName = cfg.Default
			}
			_, ok := cfg.Runners[runnerName]
			if !ok {
				return fmt.Errorf("runner %q not found in config", runnerName)
			}

			if maxRounds <= 0 {
				maxRounds = 5
			}

			// 提早驗證 --profile（unknown profile / 缺 coder）；空值時回 full/auto 不報錯。
			if _, _, err := protocol.ResolveProfile(cfg, feature, profileFlag); err != nil {
				return err
			}

			if jsonOutput {
				bgArgs := []string{"run", featureID}
				if runnerName != "" {
					bgArgs = append(bgArgs, "--runner", runnerName)
				}
				if profileFlag != "" {
					bgArgs = append(bgArgs, "--profile", profileFlag)
				}
				if maxRounds > 0 {
					bgArgs = append(bgArgs, "--max-rounds", fmt.Sprintf("%d", maxRounds))
				}
				if timeout > 0 {
					bgArgs = append(bgArgs, "--timeout", fmt.Sprintf("%d", timeout))
				}
				if dryRun {
					bgArgs = append(bgArgs, "--dry-run")
				}

				// background 子程序的 stdout/stderr 導向 feature 目錄下的 run.log，
				// 讓 config 載入、worktree setup、runner not found、ResolveProfile 等
				// 早期錯誤可事後檢視，而非進 /dev/null。不 inherit 父程序 stderr（背景化會失去 tty）。
				if err := ws.InitFeatureDir(featureID); err != nil {
					return err
				}
				logPath := filepath.Join(ws.FeatureDir(featureID), "run.log")
				proc, err := startBackgroundRun(os.Args[0], bgArgs, cwd, logPath)
				if err != nil {
					return err
				}

				result := struct {
					FeatureID string `json:"featureId"`
					Runner    string `json:"runner"`
					MaxRounds int    `json:"maxRounds"`
					PID       int    `json:"pid"`
					LogPath   string `json:"logPath"`
				}{
					FeatureID: featureID,
					Runner:    runnerName,
					MaxRounds: maxRounds,
					PID:       proc.Pid,
					LogPath:   logPath,
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			depResult := guard.CheckDependencies(ws, featureID)
			if !depResult.Pass {
				for _, e := range depResult.Errors {
					slog.Warn("dependency blocked", "feature", featureID, "reason", e)
				}
				return fmt.Errorf("feature %s has unmet dependencies", featureID)
			}

			// worktree isolation：runner 在獨立 worktree 內執行
			ops := gitops.New(ws.Root, ws, cfg)
			var runnerWs *protocol.Workspace
			var wtPath string
			if cfg.Isolation == "worktree" {
				var err error
				wtPath, err = ops.SetupWorktree(featureID, feature.Repos)
				if err != nil {
					return fmt.Errorf("worktree setup: %w", err)
				}
				runnerWs = &protocol.Workspace{Root: wtPath}
				fmt.Printf("worktree: %s\n", wtPath)
			} else {
				runnerWs = ws
			}

			if err := ws.InitFeatureDir(featureID); err != nil {
				return err
			}

			s := protocol.State{
				FeatureID: featureID,
				Phase:     protocol.PhaseInit,
				MaxRounds: maxRounds,
				Active:    true,
				Runner:    runnerName,
				CreatedAt: time.Now(),
			}

			if existing, err := ws.ReadState(featureID); err == nil {
				if existing.Active && protocol.ProcessAlive(existing.Pid) {
					return fmt.Errorf("feature %s is already running (pid %d)", featureID, existing.Pid)
				}
				s = existing
				s.Active = true
				s.Runner = runnerName
				s.StopReason = ""
				s.StopMessage = ""
				newMax := s.Round + maxRounds
				if newMax > s.MaxRounds {
					s.MaxRounds = newMax
				}
				if s.Phase == protocol.PhaseDone {
					return fmt.Errorf("feature %s is already done", featureID)
				}
				// crash recovery：先前 active 但 process 已死，state.json 的 phase 可能與磁碟
				// artifacts 不一致。對需要校正的 phase，以 smartResumePhase 依實際 artifacts
				// 推斷的 phase 為準，而非盲信 state.json。
				// - blocked / needs-attention：維持既有行為（任何 round 都校正）。
				// - 其他工作 phase（coding…accepting）：僅在已進入 coding（round > 0）時校正；
				//   init / designing / pending-review 不適用，維持原本 resume 行為。
				if needsResumeRecovery(s) {
					resumePhase, resumeRole, resumeSub := smartResumePhase(ws, featureID, s.Round, cfg)
					if resumePhase != s.Phase {
						fmt.Printf("  recovering %s → %s (round %d, max rounds: %d)\n", s.Phase, resumePhase, s.Round, s.MaxRounds)
						ns, err := state.RecoverTo(s, resumePhase, resumeRole)
						if err != nil {
							return fmt.Errorf("recovery transition %s → %s: %w", s.Phase, resumePhase, err)
						}
						s = ns
					}
					// 即使 phase 未變（crash 仍在 deep-reviewing），也要還原 subPhase，
					// 讓 dashboard 顯示與後續 partial-resume 推斷有正確起點。
					s.SubPhase = resumeSub
				}
			}

			found := false
			for _, r := range s.Runners {
				if r == runnerName {
					found = true
					break
				}
			}
			if !found {
				s.Runners = append(s.Runners, runnerName)
			}

			// profile-select UI：無 --profile flag、非 resume（s.Profile 為空）、feature YAML
			// 未指定 profile、非 dry-run，且 stdin/stdout 皆為互動式終端機時，列出可選 profile
			// 讓使用者選；其餘情況沿用既有解析（feature YAML / default_profile / priority auto-select）。
			if profileFlag == "" && s.Profile == "" && feature.Profile == "" && !dryRun && isInteractiveTerminal() {
				sel, serr := selectProfileInteractive(os.Stdin, os.Stdout, cfg, feature)
				if serr != nil {
					return serr
				}
				profileFlag = sel
			}

			// 決定本次 run 的 profile：--profile（含互動選定）優先，否則沿用 resume 既有值，
			// 再否則依 default_profile / priority auto-select（或無 profiles 區段時回 full）。
			profileOverride := profileFlag
			if profileOverride == "" {
				profileOverride = s.Profile
			}
			profileName, _, err := protocol.ResolveProfile(cfg, feature, profileOverride)
			if err != nil {
				return err
			}
			s.Profile = profileName

			s.Pid = os.Getpid()
			if err := ws.WriteState(featureID, s); err != nil {
				return err
			}

			ws.AppendEvent(featureID, protocol.Event{
				Type:   "run-start",
				Phase:  s.Phase,
				Role:   state.PhaseToRole(s.Phase),
				Runner: runnerName,
			})

			slog.Info("feature run started", "feature", featureID, "runner", runnerName, "maxRounds", maxRounds, "profile", s.Profile)

			if dryRun {
				return dryRunLoop(ws, feature, cfg, s)
			}

			commitStrategy := cfg.Commit
			if commitStrategy == "" {
				commitStrategy = "per-round"
			}

			signal.Ignore(syscall.SIGPIPE)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			defer func() {
				cur, err := ws.ReadState(featureID)
				if err != nil {
					return
				}
				if cur.Active {
					cur.Active = false
					cur.Pid = 0
					if cur.StopReason == "" {
						cur.StopReason = "process-exit"
						cur.StopMessage = fmt.Sprintf("process exited unexpectedly during %s (round %d)", cur.Phase, cur.Round)
					}
					logStateWriteErr(ws.WriteState(featureID, cur), featureID, cur.Phase)
					logSyncErr(ws.SyncFeatureStatus(featureID, cur.Phase), featureID, cur.Phase)
					ws.AppendEvent(featureID, protocol.Event{
						Type:   "run-end",
						Phase:  cur.Phase,
						Role:   cur.Role,
						Round:  cur.Round,
						Status: "interrupted",
						Detail: cur.StopReason,
						Runner: cur.Runner,
						Notify: protocol.NotifyWarning,
					})
				}
			}()

			// runnerFactory 依 phase 解析出的 runner 名稱建立 runner，取代過去 closure 固定單一 runner。
			runnerFactory := func(rn string, logPath string, model string) runner.Runner {
				return runner.NewRunner(runnerWs, rn, cfg.Runners[rn], time.Duration(timeout)*time.Second, logPath, model)
			}
			loopErr := runLoop(ctx, ws, runnerWs, feature, cfg, s, ops, runnerFactory, commitStrategy, manualRunner)

			finalState, err := ws.ReadState(featureID)
			if err != nil {
				// ReadState 失敗時降級為 blocked，避免把實際成功的 run 誤判成成功 run-end，
				// 也讓後續通知改走「失敗」分支而非推送 body 為空的誤導訊息。
				slog.Warn("failed to read final state for notification", "feature", featureID, "error", err)
				finalState = protocol.State{Phase: protocol.PhaseBlocked}
			}

			// run 正常結束（done / pending-review）時補一筆帶 Notify 的 run-end event，
			// 讓 dashboard 能在成功完成時推播（中斷 / escalation / guard-fail 已各自 emit notify event）。
			if finalState.Phase == protocol.PhaseDone || finalState.Phase == protocol.PhasePendingReview {
				ws.AppendEvent(featureID, protocol.Event{
					Type:   "run-end",
					Phase:  finalState.Phase,
					Role:   finalState.Role,
					Round:  finalState.Round,
					Status: string(finalState.Phase),
					Runner: finalState.Runner,
					Notify: protocol.NotifySuccess,
				})
			}

			// CLI 結束時推送一則 OS 原生通知，受 --no-notify 與 merged config Notifications 閘控。
			if !noNotify && protocol.NotificationsEnabled(cfg) {
				level, title, body := runOutcome(featureID, finalState, loopErr)
				sendSystemNotification(level, title, body)
			}

			if wtPath != "" {
				doneOrPending := finalState.Phase == protocol.PhaseDone || finalState.Phase == protocol.PhasePendingReview
				// on-done 策略：成功收尾時才補一次 commit（side effect 留在此處，hint 純文字另算）。
				if doneOrPending && commitStrategy == "on-done" {
					if err := ops.Commit(wtPath, featureID, fmt.Sprintf("feat(%s): %s", featureID, feature.Name)); err != nil {
						slog.Error("auto-commit failed", "feature", featureID, "error", err)
					} else {
						slog.Info("auto-commit", "feature", featureID, "strategy", commitStrategy)
					}
				}
				for _, line := range worktreeExitHints(wtPath, featureID, finalState.Phase, commitStrategy) {
					fmt.Println(line)
				}
			}

			return loopErr
		}),
	}

	cmd.Flags().StringVar(&runnerName, "runner", "", "runner plugin name (default: config default)")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "max iteration rounds (default: 5)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "plugin timeout in seconds")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print prompts without calling plugin")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "start run and return JSON immediately")
	// --profile 與 --only 互斥；目前 Go run 指令沒有 --only（屬 skill 編排層），
	// 故此處不註冊互斥規則，未來若新增 --only 再加 MarkFlagsMutuallyExclusive。
	cmd.Flags().StringVar(&profileFlag, "profile", "", "pipeline profile (full/normal/quick or custom); overrides priority-based auto-select")
	cmd.Flags().BoolVar(&noNotify, "no-notify", false, "disable OS notification on run completion (overrides config)")
	return cmd
}

// isInteractiveTerminal 回報 stdin 與 stdout 是否皆為互動式終端機（char device）。
// 用 os.Stat 的 ModeCharDevice 判斷，避免引入 golang.org/x/term 重量依賴；
// 任一為 pipe/redirect（背景、CI、--json 子程序）時回 false，不進互動選單。
func isInteractiveTerminal() bool {
	return isCharDevice(os.Stdin) && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// profileOptions 回傳可選 profile 名稱清單（cfg.Profiles ∪ DefaultProfiles），
// 依 canonical 順序（full/normal/quick）排在前、其餘自訂 profile 字母序在後，供互動選單列舉。
func profileOptions(cfg protocol.Config) []string {
	seen := map[string]bool{}
	var ordered []string
	for _, name := range []string{"full", "normal", "quick"} {
		if _, ok := protocol.DefaultProfiles()[name]; ok {
			ordered = append(ordered, name)
			seen[name] = true
		}
	}
	var custom []string
	for name := range cfg.Profiles {
		if !seen[name] {
			custom = append(custom, name)
			seen[name] = true
		}
	}
	sort.Strings(custom)
	return append(ordered, custom...)
}

// selectProfileInteractive 用互動式選單讓使用者選取 pipeline profile，支援上下鍵導航。
// 預設項為 cfg.DefaultProfile（未設定時為 full）。回傳選定的 profile 名稱。
func selectProfileInteractive(_ io.Reader, _ io.Writer, cfg protocol.Config, feature feat.Feature) (string, error) {
	options := profileOptions(cfg)
	if len(options) == 0 {
		return "", nil
	}
	def := cfg.DefaultProfile
	if def == "" {
		def = "full"
	}

	defaults := protocol.DefaultProfiles()
	lookupProfile := func(name string) protocol.ProfileConfig {
		if pc, ok := cfg.Profiles[name]; ok {
			return pc
		}
		if pc, ok := defaults[name]; ok {
			return pc
		}
		return protocol.ProfileConfig{}
	}

	huhOptions := make([]huh.Option[string], 0, len(options))
	for _, name := range options {
		pc := lookupProfile(name)
		phases := make([]string, 0, len(pc.Phases))
		for _, ps := range pc.Phases {
			phases = append(phases, ps.Phase)
		}
		label := fmt.Sprintf("%s  [%s]", name, strings.Join(phases, " → "))
		huhOptions = append(huhOptions, huh.NewOption(label, name))
	}

	var selected string
	km := huh.NewDefaultKeyMap()
	km.Quit.SetKeys("ctrl+c", "esc")
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Select pipeline profile for %s", feature.ID)).
				Options(huhOptions...).
				Value(&selected),
		),
	).WithKeyMap(km).Run()
	if err != nil {
		return "", fmt.Errorf("profile selection cancelled")
	}
	return selected, nil
}

// promptOption 在 promptData 組好、模板 render 前對其做最後調整，
// 供平行 deep review 注入 ReviewerIndex / AssignedAngles / PartialReports 等額外欄位。
type promptOption func(*promptData)

// deepReviewPartialName 回傳平行 deep review 中第 index 個 sub-reviewer 的 partial report
// 檔名（deep-review-partial-<index>.md，index 為 1-based）。
func deepReviewPartialName(index int) string {
	return fmt.Sprintf("deep-review-partial-%d.md", index)
}

// deepPartialComplete 判斷單一 deep-review-partial 檔是否已完整寫出。
// 完整判準：檔案存在、去空白後非空，且含 partial 模板的結尾段落標記 `## Statistics`
// （sub-reviewer 半截輸出時通常缺此段，避免半成品被誤判為完整而漏跑）。
func deepPartialComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return strings.Contains(string(data), "## Statistics")
}

// missingDeepPartials 回傳 1..want 中尚未完整的 partial index 清單（升冪）。
// resume 時用來判斷哪些 sub-reviewer 需補跑；首次執行時所有 index 皆缺，回傳完整清單。
func missingDeepPartials(roundDir string, want int) []int {
	var missing []int
	for i := 1; i <= want; i++ {
		if !deepPartialComplete(filepath.Join(roundDir, deepReviewPartialName(i))) {
			missing = append(missing, i)
		}
	}
	return missing
}

// withParallelDeepReviewer 把第 index/count 個 sub-reviewer 的 angle 指派與 partial report
// 檔名注入 promptData，讓 deep-reviewer 模板只 render 被分配的 angle 並輸出到 partial report。
func withParallelDeepReviewer(index, count int, angles []int, partialName string) promptOption {
	return func(d *promptData) {
		d.ReviewerIndex = index
		d.ReviewerCount = count
		d.AssignedAngles = angles
		d.PartialReportName = partialName
	}
}

// withSynthesizerReports 把所有 sub-reviewer 的 partial report 完整內文注入 promptData，
// 供 synthesizer 模板內嵌合併（不是只給路徑）。
func withSynthesizerReports(reports []includeContent) promptOption {
	return func(d *promptData) {
		d.ReviewerCount = len(reports)
		d.PartialReports = reports
	}
}

func generatePrompt(ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, role protocol.Role, round, iteration int, opts ...promptOption) (string, error) {
	tmpl, err := loadRoleTemplate(role)
	if err != nil {
		return "", fmt.Errorf("no template for role %s: %w", role, err)
	}
	locale, localeName := resolveLocale()
	var roleInc []string
	if rc, ok := cfg.Roles[string(role)]; ok {
		roleInc = rc.Includes
	}
	var repoMap map[string]string
	if len(cfg.Workspace.Repos) > 0 {
		if runnerWs.Root != ws.Root {
			// worktree 模式：組合目錄下 repo 子目錄以 name 命名，使用相對路徑讓 coder 在正確邊界內作業
			featureRepos := make(map[string]bool, len(feature.Repos))
			for _, r := range feature.Repos {
				featureRepos[r] = true
			}
			repoMap = make(map[string]string, len(cfg.Workspace.Repos))
			for name := range cfg.Workspace.Repos {
				if len(feature.Repos) > 0 && !featureRepos[name] {
					continue
				}
				repoMap[name] = name
			}
		} else {
			repoMap = protocol.ResolveFeatureRepoPaths(feature, cfg, ws.Root)
		}
	}
	data := promptData{
		Feature:             feature,
		Project:             cfg.Project,
		Role:                role,
		Round:               round,
		Iteration:           iteration,
		Config:              cfg,
		DotDir:              runnerWs.DotDir(),
		Locale:              locale,
		LocaleName:          localeName,
		RoleInstructions:    roleInstructions(cfg, role),
		ProjectIncludes:     append(loadIncludes(ws.Root, cfg.Project.Includes), discoverConventionFiles(ws.Root, cfg.Project.Includes)...),
		RoleIncludes:        loadIncludes(ws.Root, roleInc),
		PlanningDoc:         loadPlanningDocs(ws.Root, feature, cfg.DesignDocDirs),
		RepoMap:             repoMap,
		ProfileInstructions: loadProfiles(ws, feature.ID, cfg),
	}
	for _, opt := range opts {
		opt(&data)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// promptResult 是 prefetch goroutine 透過 channel 回傳的生成結果。
type promptResult struct {
	prompt string
	err    error
}

// promptPrefetch 保存一個已在背景啟動的 prompt 預生成任務；以 role+round 為 key，
// 消費端 mismatch 時丟棄並退回同步生成，確保不會用錯 prompt。
type promptPrefetch struct {
	role  protocol.Role
	round int
	ch    chan promptResult
}

// prefetchablePhase 回報某 phase 是否會在主迴圈頂端走同步 generatePrompt（line 530），
// 只有這些 phase 值得預生成 prompt。reviewing 僅在非平行模式下才走頂端路徑
// （平行 runReviewTestParallel 自行呼叫 generatePrompt，不經頂端）。
func prefetchablePhase(phase protocol.Phase, cfg protocol.Config) bool {
	switch phase {
	case protocol.PhaseDesignReviewing, protocol.PhaseCoding, protocol.PhaseAmending, protocol.PhaseTesting, protocol.PhaseAccepting:
		return true
	case protocol.PhaseReviewing:
		return !cfg.ParallelReviewTest
	default:
		return false
	}
}

func runLoop(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s protocol.State, ops gitops.Ops, newRunner func(runnerName string, logPath string, model string) runner.Runner, commitStrategy string, manualRunner string) error {
	if ops == nil {
		ops = gitops.New(ws.Root, ws, cfg)
	}
	featureID := feature.ID

	// 解析本次 run 的 active profile：用 s.Profile 當 override 確保與啟動時一致。
	// 無 profiles 區段時回 full（所有 role 啟用），既有行為不變。
	profileName, pc, err := protocol.ResolveProfile(cfg, feature, s.Profile)
	if err != nil {
		return err
	}
	s.Profile = profileName

	if s.Phase == protocol.PhaseInit {
		hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")
		initHooks := resolveHooks(cfg, feature, protocol.PhaseDesigning)
		if err := executePhaseHooks(ctx, ws, featureID, &s, initHooks["pre"], protocol.PhaseDesigning, "pre", hookLogDir); err != nil {
			return err
		}

		var err error
		s, err = state.Transition(s, protocol.PhaseDesigning, protocol.RoleDesigner)
		if err != nil {
			return err
		}
		if err := ws.WriteState(featureID, s); err != nil {
			return fmt.Errorf("write state (init→designing): %w", err)
		}
		if err := ws.SyncFeatureStatus(featureID, s.Phase); err != nil {
			slog.Warn("sync feature status failed", "feature", featureID, "phase", s.Phase, "error", err)
		}

		if err := executePhaseHooks(ctx, ws, featureID, &s, initHooks["post"], protocol.PhaseDesigning, "post", hookLogDir); err != nil {
			return err
		}
	}

	// resume 時清除當前 phase 可能的半成品 artifact，防止 SIGKILL 後 nextPhaseAfter
	// 讀到不完整的 report 誤以為 phase 完成
	cleanStaleArtifact(ws, featureID, s.Phase, s.Round)

	// F082：清掉殘留的 stop signal，避免上一輪遺留的請求誤觸本輪一啟動即停
	//（比照 batch 對 BatchStopFile 的處理）。
	if err := ws.ClearStopSignal(featureID); err != nil {
		slog.Warn("clear stop signal failed", "feature", featureID, "error", err)
	}

	designerEscalations := 0
	const maxDesignerEscalations = 2

	// P3：per-round auto-commit 改為背景執行，不阻塞下一個 phase 的 prompt 生成與 runner 啟動。
	// defer Wait 一次涵蓋 runLoop 所有 return 路徑，確保 process exit / batch 結束前背景 commit 都完成。
	var commitWG sync.WaitGroup
	defer commitWG.Wait()

	// P1：保存上一輪 transition 後背景啟動的 prompt 預生成；消費端以 role+round 比對。
	var pending *promptPrefetch

	for s.Active {
		if ctx.Err() != nil {
			s.Active = false
			s.StopReason = "interrupted"
			s.StopMessage = fmt.Sprintf("%s phase interrupted by signal (round %d)", s.Phase, s.Round)
			if err := ws.WriteState(featureID, s); err != nil {
				slog.Warn("write state failed", "feature", featureID, "error", err)
			}
			return ctx.Err()
		}

		// F082：消費 MCP stop 請求。run loop 為 state.json 的唯一 writer，
		// 在此收斂 Active=false 並清除 signal，避免外部直接改寫 state.json 競寫。
		if ws.StopRequested(featureID) {
			s.Active = false
			s.StopReason = "mcp-stop"
			if err := ws.ClearStopSignal(featureID); err != nil {
				slog.Warn("clear stop signal failed", "feature", featureID, "error", err)
			}
			if err := ws.WriteState(featureID, s); err != nil {
				slog.Warn("write state failed", "feature", featureID, "error", err)
			}
			break
		}

		phase := s.Phase
		role := state.PhaseToRole(phase)

		if phase == protocol.PhaseDone || phase == protocol.PhasePendingReview || phase == protocol.PhaseBlocked || phase == protocol.PhaseNeedsAttention || phase == protocol.PhaseAbandoned {
			break
		}

		// pass-through：role 不在 active profile 時，沿成功路徑的下一個合法邊跳過，
		// 不呼叫 runner、不檢查 artifact、不跑 guard。coder 永遠啟用故不會被跳過。
		if role != "" && !pc.EnablesRole(role) {
			next, nextRole := successorPhase(phase)
			newState, err := state.Transition(s, next, nextRole)
			if err != nil {
				return fmt.Errorf("pass-through transition %s→%s: %w", phase, next, err)
			}
			s = newState
			if err := ws.WriteState(featureID, s); err != nil {
				return fmt.Errorf("write state (skip %s): %w", phase, err)
			}
			logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "phase-skipped", Phase: phase, Role: role, Round: s.Round,
				Runner: s.Runner, Detail: "role not in profile " + profileName,
			})
			fmt.Printf("[round %d] %s — skipped (not in profile %s)\n", s.Round, phase, profileName)
			continue
		}

		// S6：reviewing phase 啟用平行 reviewer + tester（兩者皆 read-only、共用 worktree）。
		if phase == protocol.PhaseReviewing && cfg.ParallelReviewTest &&
			pc.EnablesRole(protocol.RoleReviewer) && pc.EnablesRole(protocol.RoleTester) {
			cont, err := runReviewTestParallel(ctx, ws, runnerWs, feature, cfg, &s, ops, newRunner, pc, manualRunner)
			if err != nil {
				return err
			}
			if !cont {
				break
			}
			continue
		}

		// F063：deep-reviewing phase 由自癒循環接管 — deep reviewer FAIL 時在同一 phase
		// 內 spawn mini-coder + re-verifier 修正，通過才放行 accepting，不回主迴圈重跑整條流程。
		if phase == protocol.PhaseDeepReviewing {
			cont, err := runDeepReviewPhase(ctx, ws, runnerWs, feature, cfg, &s, ops, newRunner, commitStrategy, manualRunner)
			if err != nil {
				return err
			}
			if !cont {
				break
			}
			continue
		}

		if stop, reason := state.ShouldStop(s); stop {
			s.Active = false
			s.StopReason = "no-progress"
			s.StopMessage = reason
			s.Phase = protocol.PhaseNeedsAttention
			if err := ws.WriteState(featureID, s); err != nil {
				slog.Warn("write state failed", "feature", featureID, "error", err)
			}
			if err := ws.SyncFeatureStatus(featureID, s.Phase); err != nil {
				slog.Warn("sync feature status failed", "feature", featureID, "error", err)
			}
			ws.AppendEvent(featureID, protocol.Event{Type: "escalation", Phase: s.Phase, Detail: reason, Runner: s.Runner, Notify: protocol.NotifyWarning})
			slog.Info("run stopped", "feature", featureID, "reason", reason, "round", s.Round)
			fmt.Printf("  stopped: %s\n", reason)
			return nil
		}

		// 清除上一輪遺留的 escalation，避免 designer escalation 後 coder 重跑又讀到舊的
		if phase == protocol.PhaseCoding || phase == protocol.PhaseAmending {
			os.Remove(filepath.Join(ws.RoundDir(featureID, s.Round), protocol.EscalationFile))
		}

		// 清除上一輪遺留的 feature-level 產出物，避免舊文件通過新一輪的 guard 檢查
		if phase == protocol.PhaseTesting || phase == protocol.PhaseAmending {
			os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport))
		}

		// F046：testing phase 啟動 Tester 前，先跑環境 health check（F045 pre-testing
		// hooks 已於上一輪迴圈底部執行完畢）。失敗且 recovery 無法救回則 escalate。
		if phase == protocol.PhaseTesting {
			testStrat, tsErr := ws.ReadTestStrategy(featureID)
			if tsErr != nil {
				slog.Warn("read test-strategy failed", "feature", featureID, "error", tsErr)
			}
			hc := health.ResolveHealthCheck(cfg.HealthCheck, testStrat.HealthCheck)
			if hc != nil {
				fmt.Printf("[round %d] testing — running health check\n", s.Round)
				if err := health.RunHealthCheck(*hc, healthCheckExecutor(ctx, hc.Timeout)); err != nil {
					ws.AppendEvent(featureID, protocol.Event{
						Type: "health-check-failed", Phase: s.Phase, Role: protocol.RoleTester,
						Round: s.Round, Detail: err.Error(), Runner: s.Runner,
					})
					newState, transErr := state.Transition(s, protocol.PhaseNeedsAttention, "")
					if transErr != nil {
						return fmt.Errorf("health check transition: %w", transErr)
					}
					s = newState
					s.Active = false
					s.StopReason = "health-check-failed"
					s.StopMessage = err.Error()
					logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
					logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
					slog.Info("run stopped", "feature", featureID, "reason", "health-check-failed", "round", s.Round)
					fmt.Printf("  health check failed, escalated to needs-attention\n")
					return nil
				}
				fmt.Printf("[round %d] testing — health check passed\n", s.Round)
			}
		}

		if phase == protocol.PhaseCoding && s.Round == 1 {
			if err := captureBaselineOnce(ws, ops, featureID, feature.Repos); err != nil {
				return err
			}
		}

		// deep-reviewing phase 已由 runDeepReviewPhase 接管（含 deep_model 解析與跳過邏輯），
		// 故此處 role 必不為 deep-reviewer，直接依覆寫優先序逐 phase 解析 runner 與 model。
		phaseRunner, err := protocol.ResolvePhaseRunner(cfg, feature, pc, phase, manualRunner)
		if err != nil {
			s.Active = false
			s.StopReason = "runner-error"
			s.StopMessage = fmt.Sprintf("runner resolution for %s failed: %v", phase, err)
			logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
			return fmt.Errorf("runner resolution failed: %w", err)
		}
		// ResolvePhaseModel 的最後一個參數是手動指定的 model tier（對應未來的 --model flag），
		// 不是 runner 名稱；目前 CLI 尚無 --model flag，故一律傳空字串。
		model, err := protocol.ResolvePhaseModel(cfg, feature, pc, phase, role, phaseRunner, "")
		if err != nil {
			s.Active = false
			s.StopReason = "model-error"
			s.StopMessage = fmt.Sprintf("model resolution for %s failed: %v", role, err)
			logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
			return fmt.Errorf("model resolution failed: %w", err)
		}

		ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: phase, Role: role, Round: s.Round,
			Runner: phaseRunner, Model: model,
		})

		slog.Info("phase transition", "feature", featureID, "phase", phase, "role", role, "round", s.Round, "runner", phaseRunner, "model", model)

		// P1：優先取用上一輪背景預生成的 prompt（role+round 比對）；prefetch 失敗或無
		// matching prefetch 則退回同步 generatePrompt，同步亦失敗才用 minimal prompt。
		var prompt string
		gotPrefetch := false
		if pending != nil && pending.role == role && pending.round == s.Round {
			res := <-pending.ch
			if res.err == nil {
				prompt = res.prompt
				gotPrefetch = true
			}
		}
		pending = nil // 已消費或不匹配，一律清掉避免下一輪誤用
		if !gotPrefetch {
			p, gerr := generatePrompt(ws, runnerWs, feature, cfg, role, s.Round, 0)
			if gerr != nil {
				p = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, s.Round, featureID)
			}
			prompt = p
		}

		logPath := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(s.Round, string(role)))
		r := newRunner(phaseRunner, logPath, model)

		commitWG.Wait()

		if runnerWs.Root != ws.Root {
			syncFeatureToWorktree(ws, runnerWs, featureID, s.Round)
		}

		// 背景即時 sync：runner 執行期間每 2 秒把 worktree 的 protocol 檔案同步回 main
		var stopSync func()
		if runnerWs.Root != ws.Root {
			stopSync = startLiveSync(runnerWs, ws, featureID, s.Round)
		}

		if model != "" {
			fmt.Printf("[round %d] %s (%s) — invoking %s (model: %s)\n", s.Round, phase, role, phaseRunner, model)
		} else {
			fmt.Printf("[round %d] %s (%s) — invoking %s\n", s.Round, phase, role, phaseRunner)
		}

		slog.Info("plugin invocation", "feature", featureID, "role", role, "runner", phaseRunner, "model", model, "round", s.Round, "status", "started")
		invokeStart := time.Now()
		result, err := r.Run(ctx, prompt)
		invokeDur := time.Since(invokeStart)
		slog.Info("plugin invocation", "feature", featureID, "role", role, "runner", phaseRunner, "model", model, "round", s.Round, "status", "completed", "duration_ms", invokeDur.Milliseconds())

		if stopSync != nil {
			stopSync()
		}
		if runnerWs.Root != ws.Root {
			if serr := syncFeatureFromWorktree(runnerWs, ws, featureID, s.Round); serr != nil {
				slog.Warn("sync from worktree failed", "feature", featureID, "round", s.Round, "error", serr)
			}
		}
		if err != nil {
			if ctx.Err() == context.Canceled {
				s.Active = false
				s.StopReason = "interrupted"
				s.StopMessage = fmt.Sprintf("%s (%s) interrupted by signal (round %d)", role, phase, s.Round)
				logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
				return ctx.Err()
			}
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "runner-error"
			s.StopMessage = fmt.Sprintf("%s runner failed during %s (round %d): %v", role, phase, s.Round, err)
			logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: phase, Role: role, Round: s.Round,
				Status: "error", Detail: err.Error(),
				Runner: phaseRunner, Model: model,
			})
			return err
		}

		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: phase, Role: role, Round: s.Round,
			Status: fmt.Sprintf("exit-%d", result.ExitCode),
			Runner: phaseRunner, Model: model,
		})

		if runner.IsHardError(result) {
			s.Active = false
			s.StopReason = "hard-error"
			s.StopMessage = fmt.Sprintf("%s runner returned hard error (exit 2) during %s (round %d)", role, phase, s.Round)
			logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
			return fmt.Errorf("runner returned hard error (exit 2)")
		}

		if runner.IsSoftFail(result) {
			s.Phase = protocol.PhaseBlocked
			s.Active = false
			s.StopReason = "soft-fail"
			s.StopMessage = fmt.Sprintf("%s runner returned soft-fail (exit %d) during %s (round %d)", role, runner.ExitSoftFail, phase, s.Round)
			logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
			logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked), featureID, protocol.PhaseBlocked)
			return nil
		}

		if phase == protocol.PhaseCoding || phase == protocol.PhaseAmending {
			guardResult := guard.Check(ws, featureID, ops)
			if !guardResult.Pass {
				s.Phase = protocol.PhaseNeedsAttention
				s.Active = false
				guardMsg := strings.Join(guardResult.Errors, "; ")
				s.StopReason = "guard-fail"
				s.StopMessage = guardMsg
				logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
				logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
				ws.AppendEvent(featureID, protocol.Event{
					Type: "guard-fail", Phase: phase, Role: role, Round: s.Round,
					Detail: guardMsg, Runner: s.Runner, Notify: protocol.NotifyError,
				})
				return nil
			}

			// P3：guard 通過後才在背景啟動 per-round auto-commit。guard.Check 已先觀察乾淨的
			// working tree，背景 commit 不再與 guard 競爭，也不阻塞下一個 phase 的啟動。
			// 行為變更：guard 失敗（→ needs-attention）那輪不再自動 wip-commit，未提交 diff 便於人工檢視。
			if commitStrategy == "per-round" && runnerWs.Root != ws.Root {
				commitWG.Add(1)
				go func(wtRoot string, round int) {
					defer commitWG.Done()
					if err := ops.Commit(wtRoot, featureID, fmt.Sprintf("wip(%s): round %d", featureID, round)); err != nil {
						slog.Error("auto-commit failed", "feature", featureID, "round", round, "error", err)
					} else {
						slog.Info("auto-commit", "feature", featureID, "round", round, "strategy", "per-round")
					}
				}(runnerWs.Root, s.Round)
			}
		}

		next, nextRole, stopReason := nextPhaseAfter(ws, featureID, s)

		if (next == protocol.PhaseNeedsAttention || next == protocol.PhaseBlocked) && nextRole == "" {
			nextRole = role
		}

		loopHooks := resolveHooks(cfg, feature, next)
		hookLogDir := filepath.Join(ws.FeatureDir(featureID), "hook-logs")

		if err := executePhaseHooks(ctx, ws, featureID, &s, loopHooks["pre"], next, "pre", hookLogDir); err != nil {
			return err
		}

		// 防止 coder ↔ designer 無限 escalation 循環
		if next == protocol.PhaseDesigning && phase != protocol.PhaseInit {
			designerEscalations++
			if designerEscalations > maxDesignerEscalations {
				next = protocol.PhaseNeedsAttention
				nextRole = role
				stopReason = fmt.Sprintf("escalation-loop: designer escalated %d times in round %d", designerEscalations, s.Round)
				fmt.Printf("  ⚠ stopping: coder↔designer escalation loop detected (%d times)\n", designerEscalations)
			}
		}

		newState, err := state.Transition(s, next, nextRole)
		if err != nil {
			return fmt.Errorf("loop transition %s→%s: %w", s.Phase, next, err)
		}

		// W1：reviewer FAIL（→ amending）時追蹤連續無進展輪次。
		// 用轉換前的 s.Round 讀本輪 review-report.md 的失敗計數（與平行路徑共用 applyAmendingProgress）。
		if next == protocol.PhaseAmending {
			applyAmendingProgress(ws, featureID, &newState, s.Round)
		}

		s = newState
		if stopReason != "" {
			s.Active = false
			s.StopMessage = stopReason
			if strings.HasPrefix(stopReason, "missing-artifact:") {
				s.StopReason = "missing-artifact"
			} else if strings.HasPrefix(stopReason, "escalation-loop:") {
				s.StopReason = "escalation"
			} else {
				s.StopReason = "escalation"
			}
		}
		if err := ws.WriteState(featureID, s); err != nil {
			return fmt.Errorf("write state (%s): %w", s.Phase, err)
		}
		logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)

		ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
			Runner: s.Runner,
		})

		if err := executePhaseHooks(ctx, ws, featureID, &s, loopHooks["post"], next, "post", hookLogDir); err != nil {
			return err
		}

		// P1：post-hooks 跑完後，若下一輪會走頂端同步 generatePrompt，背景預生成其 prompt，
		// 與下一輪頂端的 syncFeatureToWorktree/startLiveSync 並行。放在 post-hooks 之後啟動，
		// 完全避開「hook 改寫 prompt 輸入檔（planning docs / includes）→ prefetch 讀到舊內容」的競爭。
		// key 用 PhaseToRole(s.Phase) 與 s.Round，確保與消費端的 role/round 比對一致。
		if s.Active {
			nextRole := state.PhaseToRole(s.Phase)
			if prefetchablePhase(s.Phase, cfg) && nextRole != "" && pc.EnablesRole(nextRole) {
				ch := make(chan promptResult, 1)
				pending = &promptPrefetch{role: nextRole, round: s.Round, ch: ch}
				go func(role protocol.Role, round int) {
					p, gerr := generatePrompt(ws, runnerWs, feature, cfg, role, round, 0)
					ch <- promptResult{prompt: p, err: gerr}
				}(nextRole, s.Round)
			}
		}
	}

	switch s.Phase {
	case protocol.PhasePendingReview:
		s.Active = false
		s.StopReason = "pending-review"
		logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
		logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhasePendingReview), featureID, protocol.PhasePendingReview)
		fmt.Printf("\nFeature %s ready for review (%d rounds). Run '4x done %s' to complete.\n", featureID, s.Round, featureID)
	case protocol.PhaseDone:
		s.Active = false
		s.StopReason = "done"
		logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
		logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseDone), featureID, protocol.PhaseDone)
		fmt.Printf("\nFeature %s complete (%d rounds)\n", featureID, s.Round)
	case protocol.PhaseNeedsAttention, protocol.PhaseBlocked:
		if s.Active {
			s.Active = false
			if s.StopReason == "" {
				s.StopReason = "escalation"
			}
			if s.StopMessage == "" {
				s.StopMessage = fmt.Sprintf("%s stopped with %s (round %d)", featureID, s.Phase, s.Round)
			}
			logStateWriteErr(ws.WriteState(featureID, s), featureID, s.Phase)
		}
	}
	return nil
}

// nextPhaseAfter 根據目前 phase 和 artifacts 決定下一個 phase，第三個回傳值為 escalation 停止原因
func nextPhaseAfter(ws *protocol.Workspace, featureID string, s protocol.State) (protocol.Phase, protocol.Role, string) {
	switch s.Phase {
	case protocol.PhaseDesigning:
		brief := filepath.Join(ws.FeatureDir(featureID), protocol.TaskBrief)
		if _, err := os.Stat(brief); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.TaskBrief
		}
		criteria := filepath.Join(ws.FeatureDir(featureID), protocol.Criteria)
		if _, err := os.Stat(criteria); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.Criteria
		}
		return protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer, ""

	case protocol.PhaseDesignReviewing:
		report := filepath.Join(ws.FeatureDir(featureID), protocol.DesignReviewReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.DesignReviewReport
		}
		if reviewPassedAtPath(report) {
			return protocol.PhaseCoding, protocol.RoleCoder, ""
		}
		return protocol.PhaseDesigning, protocol.RoleDesigner, ""

	case protocol.PhaseCoding, protocol.PhaseAmending:
		if esc := readEscalation(ws, featureID, s.Round); esc.Needed {
			if isDesignerEscalation(esc.Reason) {
				return protocol.PhaseDesigning, protocol.RoleDesigner, ""
			}
			return protocol.PhaseNeedsAttention, "", esc.Reason
		}
		report := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.CoderReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.CoderReport
		}
		return protocol.PhaseReviewing, protocol.RoleReviewer, ""

	case protocol.PhaseReviewing:
		report := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.ReviewReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.ReviewReport
		}
		if reviewPassed(ws, featureID, s.Round, protocol.ReviewReport) {
			return protocol.PhaseTesting, protocol.RoleTester, ""
		}
		return protocol.PhaseAmending, protocol.RoleCoder, ""

	case protocol.PhaseTesting:
		if esc := readEscalation(ws, featureID, s.Round); esc.Needed {
			if isDesignerEscalation(esc.Reason) {
				return protocol.PhaseDesigning, protocol.RoleDesigner, ""
			}
			return protocol.PhaseNeedsAttention, "", esc.Reason
		}
		result := guard.CheckTestingToAccepting(ws, featureID, s.Round)
		if result.Pass {
			return protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer, ""
		}
		if !verifyPassed(ws, featureID, s.Round) {
			if reviewPassed(ws, featureID, s.Round, protocol.TestReport) {
				return protocol.PhaseNeedsAttention, "",
					"verify.json missing but test-report verdict is PASS — tester likely could not run `4x verify`"
			}
			return protocol.PhaseAmending, protocol.RoleCoder, ""
		}
		return protocol.PhaseNeedsAttention, "", strings.Join(result.Errors, "; ")

	case protocol.PhaseDeepReviewing:
		// deep-reviewing 由 runDeepReviewPhase 完整接管（F063）：在正常執行路徑上，
		// 主迴圈一遇到此 phase 即呼叫 runDeepReviewPhase 並 continue/break，不會落到這裡。
		// 此 case 僅保留供 dry-run 診斷等間接查詢使用，回傳值需符合 F063 設計意圖：
		// deep-review FAIL 後走 needs-attention（自癒已在 phase 內完成），不回 amending。
		report := filepath.Join(ws.RoundDir(featureID, s.Round), protocol.DeepReviewReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.DeepReviewReport
		}
		if reviewPassed(ws, featureID, s.Round, protocol.DeepReviewReport) {
			return protocol.PhaseAccepting, protocol.RoleAcceptor, ""
		}
		return protocol.PhaseNeedsAttention, "", "deep-review self-heal exhausted"

	case protocol.PhaseAccepting:
		report := filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport)
		if _, err := os.Stat(report); err != nil {
			return protocol.PhaseNeedsAttention, "", "missing-artifact: " + protocol.FinalReport
		}
		return protocol.PhasePendingReview, "", ""

	default:
		return protocol.PhaseDone, "", ""
	}
}

// successorPhase 回傳成功路徑上的下一個 phase 與其 role，皆為合法 state 邊。
// 用於 pass-through 跳過未啟用的 role；pending-review 的 role 為空字串。
func successorPhase(p protocol.Phase) (protocol.Phase, protocol.Role) {
	switch p {
	case protocol.PhaseDesigning:
		return protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer
	case protocol.PhaseDesignReviewing:
		return protocol.PhaseCoding, protocol.RoleCoder
	case protocol.PhaseCoding:
		return protocol.PhaseReviewing, protocol.RoleReviewer
	case protocol.PhaseReviewing:
		return protocol.PhaseTesting, protocol.RoleTester
	case protocol.PhaseTesting:
		return protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer
	case protocol.PhaseDeepReviewing:
		return protocol.PhaseAccepting, protocol.RoleAcceptor
	case protocol.PhaseAccepting:
		return protocol.PhasePendingReview, ""
	default:
		return p, state.PhaseToRole(p)
	}
}

// runReviewTestParallel 在 reviewing phase 同時跑 reviewer 與 tester（皆 read-only、共用
// worktree），兩者完成後合併判定。回傳 (cont, err)：cont 為 true 表示主迴圈應 continue
// 接手後續 phase（deep-reviewing 或 amending）；cont 為 false 且 err 為 nil 表示已落入
// 終止狀態（blocked / needs-attention），主迴圈應 break；err 非 nil 表示 hard error 直接中止。
func runReviewTestParallel(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(runnerName string, logPath string, model string) runner.Runner, pc protocol.ProfileConfig, manualRunner string) (bool, error) {
	featureID := feature.ID
	round := s.Round

	// reviewer 依 reviewing phase、tester 依其 canonical testing phase 各自解析 runner/model，
	// 平行模式下兩者可用不同 runner（共用 worktree）。
	resolveErr := func(what string, err error) (bool, error) {
		s.Active = false
		s.StopReason = "model-error"
		s.StopMessage = fmt.Sprintf("%s resolution failed: %v", what, err)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		return false, fmt.Errorf("%s resolution failed: %w", what, err)
	}

	reviewRunner, err := protocol.ResolvePhaseRunner(cfg, feature, pc, protocol.PhaseReviewing, manualRunner)
	if err != nil {
		return resolveErr("reviewer runner", err)
	}
	reviewModel, err := protocol.ResolvePhaseModel(cfg, feature, pc, protocol.PhaseReviewing, protocol.RoleReviewer, reviewRunner, "")
	if err != nil {
		return resolveErr("reviewer model", err)
	}
	testRunner, err := protocol.ResolvePhaseRunner(cfg, feature, pc, protocol.PhaseTesting, manualRunner)
	if err != nil {
		return resolveErr("tester runner", err)
	}
	testModel, err := protocol.ResolvePhaseModel(cfg, feature, pc, protocol.PhaseTesting, protocol.RoleTester, testRunner, "")
	if err != nil {
		return resolveErr("tester model", err)
	}

	if runnerWs.Root != ws.Root {
		syncFeatureToWorktree(ws, runnerWs, featureID, round)
	}

	type runOutcome struct {
		role       protocol.Role
		runnerName string
		model      string
		result     *runner.Result
		err        error
	}

	runRole := func(role protocol.Role, runnerName, model string) runOutcome {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: protocol.PhaseReviewing, Role: role, Round: round,
			Runner: runnerName, Model: model,
		})
		prompt, err := generatePrompt(ws, runnerWs, feature, cfg, role, round, 0)
		if err != nil {
			prompt = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, round, featureID)
		}
		logPath := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(round, string(role)))
		r := newRunner(runnerName, logPath, model)
		res, runErr := r.Run(ctx, prompt)
		return runOutcome{role: role, runnerName: runnerName, model: model, result: res, err: runErr}
	}

	var stopSync func()
	if runnerWs.Root != ws.Root {
		stopSync = startLiveSync(runnerWs, ws, featureID, round)
	}

	fmt.Printf("[round %d] reviewing — running reviewer (%s) + tester (%s) in parallel\n", round, reviewRunner, testRunner)

	var wg sync.WaitGroup
	outcomes := make([]runOutcome, 2)
	wg.Add(2)
	go func() { defer wg.Done(); outcomes[0] = runRole(protocol.RoleReviewer, reviewRunner, reviewModel) }()
	go func() { defer wg.Done(); outcomes[1] = runRole(protocol.RoleTester, testRunner, testModel) }()
	wg.Wait()

	if stopSync != nil {
		stopSync()
	}
	if runnerWs.Root != ws.Root {
		if serr := syncFeatureFromWorktree(runnerWs, ws, featureID, round); serr != nil {
			slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "error", serr)
		}
	}

	// runner 執行錯誤：context cancel → interrupted；其餘 → runner-error needs-attention。
	for _, o := range outcomes {
		if o.err != nil {
			if ctx.Err() == context.Canceled {
				s.Active = false
				s.StopReason = "interrupted"
				s.StopMessage = fmt.Sprintf("%s interrupted by signal (round %d)", o.role, round)
				logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
				return false, ctx.Err()
			}
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "runner-error"
			s.StopMessage = fmt.Sprintf("%s runner failed (round %d): %v", o.role, round, o.err)
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: protocol.PhaseReviewing, Role: o.role, Round: round,
				Status: "error", Detail: o.err.Error(), Runner: o.runnerName, Model: o.model,
			})
			return false, o.err
		}
	}

	for _, o := range outcomes {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseReviewing, Role: o.role, Round: round,
			Status: fmt.Sprintf("exit-%d", o.result.ExitCode), Runner: o.runnerName, Model: o.model,
		})
	}

	for _, o := range outcomes {
		if runner.IsHardError(o.result) {
			s.Active = false
			s.StopReason = "hard-error"
			s.StopMessage = fmt.Sprintf("%s runner returned hard error (exit 2) during parallel review (round %d)", o.role, round)
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			return false, fmt.Errorf("runner returned hard error (exit 2)")
		}
	}
	for _, o := range outcomes {
		if runner.IsSoftFail(o.result) {
			s.Phase = protocol.PhaseBlocked
			s.Active = false
			s.StopReason = "soft-fail"
			s.StopMessage = fmt.Sprintf("%s runner returned soft-fail (exit %d) during parallel review (round %d)", o.role, runner.ExitSoftFail, round)
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked), featureID, protocol.PhaseBlocked)
			return false, nil
		}
	}

	guardResult := guard.Check(ws, featureID, ops)
	if !guardResult.Pass {
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		guardMsg := strings.Join(guardResult.Errors, "; ")
		s.StopReason = "guard-fail"
		s.StopMessage = guardMsg
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "guard-fail", Phase: protocol.PhaseReviewing, Round: round,
			Detail: guardMsg, Runner: s.Runner,
		})
		return false, nil
	}

	// 合併判定。先確認 reviewer 與 tester 的 artifact 完整。
	reviewReport := filepath.Join(ws.RoundDir(featureID, round), protocol.ReviewReport)
	if _, err := os.Stat(reviewReport); err != nil {
		return parallelNeedsAttention(ws, featureID, s, "missing-artifact: "+protocol.ReviewReport)
	}

	// tester escalation 優先處理（可回 designer 或停下等人）。
	if esc := readEscalation(ws, featureID, round); esc.Needed {
		if isDesignerEscalation(esc.Reason) {
			return parallelTransition(ws, featureID, s, protocol.PhaseAmending, protocol.RoleCoder)
		}
		return parallelNeedsAttention(ws, featureID, s, esc.Reason)
	}

	reviewOK := reviewPassed(ws, featureID, round, protocol.ReviewReport)
	verifyOK := verifyPassed(ws, featureID, round)

	if !reviewOK {
		return parallelTransition(ws, featureID, s, protocol.PhaseAmending, protocol.RoleCoder)
	}
	// verify.json 缺失（tester 跑不了 `4x verify`）但 test-report verdict 是 PASS
	// → needs-attention 讓人介入，而非靜默 amending 形成無限迴圈
	if !verifyOK {
		testReportOK := reviewPassed(ws, featureID, round, protocol.TestReport)
		if testReportOK {
			return parallelNeedsAttention(ws, featureID, s,
				"verify.json missing but test-report verdict is PASS — tester likely could not run `4x verify`")
		}
		return parallelTransition(ws, featureID, s, protocol.PhaseAmending, protocol.RoleCoder)
	}

	// 兩者皆 PASS：tester 必須備齊 final-report 等抵達 accepting 的 artifact。
	if testGuard := guard.CheckTestingToAccepting(ws, featureID, round); !testGuard.Pass {
		return parallelNeedsAttention(ws, featureID, s, strings.Join(testGuard.Errors, "; "))
	}

	// 沿合法邊兩跳：reviewing→testing→deep-reviewing，由主迴圈在 deep-reviewing 接手。
	if cont, err := parallelTransition(ws, featureID, s, protocol.PhaseTesting, protocol.RoleTester); !cont || err != nil {
		return cont, err
	}
	return parallelTransition(ws, featureID, s, protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer)
}

// applyAmendingProgress 在轉入 amending 時更新 W1 無進展追蹤欄位（就地修改 st）。
// 以 reviewRound（轉換前的 review 輪次）讀 review-report.md 的失敗計數，與上輪基準比較：
// 持平或更差 → ConsecutiveNoProgress++；改善 → 歸零。首輪（尚無基準且 cur > 0）僅建立
// LastFailCount，不 increment。序列路徑與平行 review/test 路徑共用此 helper，確保
// ShouldStop（ConsecutiveNoProgress >= 3）在兩種模式下行為一致，不因路徑漂移而失效。
func applyAmendingProgress(ws *protocol.Workspace, featureID string, st *protocol.State, reviewRound int) {
	cur := reviewFailCount(ws, featureID, reviewRound)
	// 首輪 amending（基準為 0 且本輪有失敗）僅建立基準；額外要求 cur > 0 避免
	// review-report 缺失/格式異常使 cur 恆為 0 時「首輪」條件每輪成立、永遠無法遞增。
	if st.LastFailCount == 0 && st.ConsecutiveNoProgress == 0 && cur > 0 {
		// 僅建立基準，不 increment
	} else if cur >= st.LastFailCount {
		st.ConsecutiveNoProgress++
	} else {
		st.ConsecutiveNoProgress = 0
	}
	st.LastFailCount = cur
}

// parallelTransition 執行一次合法 state 轉換並寫回，供平行 review/test 合併後推進 phase。
// 轉入 amending 時套用與序列路徑相同的 W1 無進展追蹤（applyAmendingProgress），
// 用轉換前的 round 讀 review 失敗計數，確保 ShouldStop 在平行模式下同樣生效。
func parallelTransition(ws *protocol.Workspace, featureID string, s *protocol.State, to protocol.Phase, role protocol.Role) (bool, error) {
	reviewRound := s.Round
	newState, err := state.Transition(*s, to, role)
	if err != nil {
		return false, fmt.Errorf("parallel transition %s→%s: %w", s.Phase, to, err)
	}
	if to == protocol.PhaseAmending {
		applyAmendingProgress(ws, featureID, &newState, reviewRound)
	}
	*s = newState
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (%s): %w", s.Phase, err)
	}
	logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round, Runner: s.Runner,
	})
	return true, nil
}

// parallelNeedsAttention 把 state 落入 needs-attention 並寫回，回傳 (false, nil) 讓主迴圈 break。
func parallelNeedsAttention(ws *protocol.Workspace, featureID string, s *protocol.State, reason string) (bool, error) {
	s.Phase = protocol.PhaseNeedsAttention
	s.Active = false
	s.StopMessage = reason
	if strings.HasPrefix(reason, "missing-artifact:") {
		s.StopReason = "missing-artifact"
	} else {
		s.StopReason = "escalation"
	}
	logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
	logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
	return false, nil
}

// runDeepReviewParallel 在 deep-reviewing phase 內平行 spawn len(groups) 個 sub-reviewer，
// 各自只跑分配到的 review angle 並寫出 deep-review-partial-<i>.md，全部完成後再 spawn 一個
// synthesizer 把所有 partial report 合併成單一 deep-review-report.md（格式與單 agent 完全相同）。
// 全程維持 deep-reviewing phase。sub-reviewer 與 synthesizer 皆 read-only，共用同一 worktree。
//
// 回傳 (ok, err)：語意同 runDeepSubRole；ok 為 true 時 deep-review-report.md 已產出，
// caller 接續走 reviewPassed → accepting / self-heal 分支。
func runDeepReviewParallel(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(runnerName string, logPath string, model string) runner.Runner, runnerName, deepModel string, groups [][]int, round int) (bool, error) {
	featureID := feature.ID

	if runnerWs.Root != ws.Root {
		syncFeatureToWorktree(ws, runnerWs, featureID, round)
	}
	var stopSync func()
	if runnerWs.Root != ws.Root {
		stopSync = startLiveSync(runnerWs, ws, featureID, round)
	}
	// cleanup 停掉 live sync 並把 worktree 內的 report 同步回主 workspace；可安全重複呼叫。
	cleanup := func() {
		if stopSync != nil {
			stopSync()
			stopSync = nil
		}
		if runnerWs.Root != ws.Root {
			if serr := syncFeatureFromWorktree(runnerWs, ws, featureID, round); serr != nil {
				slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "error", serr)
			}
		}
	}

	type runOutcome struct {
		index  int
		result *runner.Result
		err    error
	}

	// resume：跳過已完整寫出 partial 的 sub-reviewer，只補跑缺少的 index。
	// missing 為空時整個 sub-reviewer 階段跳過，直接進 synthesizer
	//（涵蓋「synthesizer 掛掉、partial 都在 → 只重跑 synthesizer」）。
	// partial index 與 angle group 的對應固定（idx=i+1 → groups[idx-1]），補跑時沿用原分配。
	missing := missingDeepPartials(runnerWs.RoundDir(featureID, round), len(groups))

	outcomes := make([]runOutcome, len(missing))
	if len(missing) > 0 {
		fmt.Printf("[round %d] deep-reviewing — running %d parallel sub-reviewers (%s, model: %s)\n", round, len(missing), runnerName, deepModel)

		var wg sync.WaitGroup
		for slot, idx := range missing {
			wg.Add(1)
			go func(slot, idx int) {
				defer wg.Done()
				angles := groups[idx-1]
				partialName := deepReviewPartialName(idx)
				ws.AppendEvent(featureID, protocol.Event{
					Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
					Runner: runnerName, Model: deepModel,
				})
				prompt, perr := generatePrompt(ws, runnerWs, feature, cfg, protocol.RoleDeepReviewer, round, 0,
					withParallelDeepReviewer(idx, len(groups), angles, partialName))
				if perr != nil {
					prompt = fmt.Sprintf("You are deep sub-reviewer %d for feature %s, round %d. Read .4x/%s/ for context.", idx, featureID, round, featureID)
				}
				logPath := filepath.Join(runner.LogDir(ws, featureID), runner.DeepReviewerLogFileName(round, idx))
				r := newRunner(runnerName, logPath, deepModel)
				res, runErr := r.Run(ctx, prompt)
				outcomes[slot] = runOutcome{index: idx, result: res, err: runErr}
			}(slot, idx)
		}
		wg.Wait()
	} else {
		fmt.Printf("[round %d] deep-reviewing — all %d partials present, resuming at synthesizer (%s)\n", round, len(groups), runnerName)
	}

	// runner 執行錯誤分類：context cancel → interrupted；其餘 → runner-error needs-attention。
	for _, o := range outcomes {
		if o.err != nil {
			cleanup()
			if ctx.Err() == context.Canceled {
				s.Active = false
				s.StopReason = "interrupted"
				s.StopMessage = fmt.Sprintf("deep-reviewer interrupted by signal (round %d)", round)
				logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
				return false, ctx.Err()
			}
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "runner-error"
			s.StopMessage = fmt.Sprintf("deep-reviewer runner failed (round %d): %v", round, o.err)
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
				Status: "error", Detail: o.err.Error(), Runner: runnerName, Model: deepModel,
			})
			return false, o.err
		}
	}
	for _, o := range outcomes {
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer, Round: round,
			Status: fmt.Sprintf("exit-%d", o.result.ExitCode), Runner: runnerName, Model: deepModel,
		})
	}
	for _, o := range outcomes {
		if runner.IsHardError(o.result) {
			cleanup()
			s.Active = false
			s.StopReason = "hard-error"
			s.StopMessage = fmt.Sprintf("deep-reviewer runner returned hard error (exit 2) (round %d)", round)
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			return false, fmt.Errorf("runner returned hard error (exit 2)")
		}
	}
	for _, o := range outcomes {
		if runner.IsSoftFail(o.result) {
			cleanup()
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "deep-reviewer-soft-fail"
			s.StopMessage = fmt.Sprintf("deep-reviewer runner returned soft-fail (exit %d) (round %d)", runner.ExitSoftFail, round)
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
			return false, nil
		}
	}

	// 驗證每個 sub-reviewer 都寫出 partial report，並讀入完整內文供 synthesizer 內嵌。
	var partials []includeContent
	for i := 1; i <= len(groups); i++ {
		name := deepReviewPartialName(i)
		data, rerr := os.ReadFile(filepath.Join(runnerWs.RoundDir(featureID, round), name))
		if rerr != nil {
			cleanup()
			return parallelNeedsAttention(ws, featureID, s, "missing-artifact: "+name)
		}
		partials = append(partials, includeContent{Path: name, Content: string(data)})
	}

	// synthesizer 合併所有 partial report 成單一 deep-review-report.md。
	s.Role = protocol.RoleSynthesizer
	s.SubPhase = protocol.SubPhaseSynthesizing
	if err := ws.WriteState(featureID, *s); err != nil {
		cleanup()
		return false, fmt.Errorf("write state (synthesizer): %w", err)
	}
	// synthesizer 只做文本合併、不讀原始碼，用獨立的便宜 model（預設 sonnet tier，
	// 可由 roles.synthesizer.model 覆寫）。解析失敗時 fallback 回 deepModel，不中斷 run。
	synthModel := deepModel
	if m, mErr := protocol.ResolveModel(cfg, runnerName, protocol.RoleSynthesizer); mErr == nil {
		synthModel = m
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
		Runner: runnerName, Model: synthModel,
	})
	synthPrompt, perr := generatePrompt(ws, runnerWs, feature, cfg, protocol.RoleSynthesizer, round, 0,
		withSynthesizerReports(partials))
	if perr != nil {
		synthPrompt = fmt.Sprintf("You are the deep review synthesizer for feature %s, round %d. Read .4x/%s/ for context.", featureID, round, featureID)
	}
	synthLog := filepath.Join(runner.LogDir(ws, featureID), runner.LogFileName(round, string(protocol.RoleSynthesizer)))
	synthRunner := newRunner(runnerName, synthLog, synthModel)
	fmt.Printf("[round %d] deep-reviewing (synthesizer) — invoking %s (model: %s)\n", round, runnerName, synthModel)
	synthRes, synthErr := synthRunner.Run(ctx, synthPrompt)
	if synthErr != nil {
		cleanup()
		if ctx.Err() == context.Canceled {
			s.Active = false
			s.StopReason = "interrupted"
			s.StopMessage = fmt.Sprintf("deep-review synthesizer interrupted by signal (round %d)", round)
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		s.StopReason = "runner-error"
		s.StopMessage = fmt.Sprintf("deep-review synthesizer runner failed (round %d): %v", round, synthErr)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
			Status: "error", Detail: synthErr.Error(), Runner: runnerName, Model: synthModel,
		})
		return false, synthErr
	}
	ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleSynthesizer, Round: round,
		Status: fmt.Sprintf("exit-%d", synthRes.ExitCode), Runner: runnerName, Model: synthModel,
	})
	if runner.IsHardError(synthRes) {
		cleanup()
		s.Active = false
		s.StopReason = "hard-error"
		s.StopMessage = fmt.Sprintf("deep-review synthesizer returned hard error (exit 2) (round %d)", round)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(synthRes) {
		cleanup()
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		s.StopReason = "synthesizer-soft-fail"
		s.StopMessage = fmt.Sprintf("deep-review synthesizer returned soft-fail (exit %d) (round %d)", runner.ExitSoftFail, round)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
		return false, nil
	}

	cleanup()
	// sub-reviewer 與 synthesizer 皆 read-only：跑一次 guardrail 確認沒越界改檔。
	if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleDeepReviewer); !ok || err != nil {
		return ok, err
	}
	return true, nil
}

// runDeepReviewPhase 在 deep-reviewing phase 內執行自癒循環：先跑 deep reviewer，FAIL 時
// 不回主迴圈，而是在同一 phase 內反覆 spawn mini-coder（只修被點名的 issue）與 re-verifier
// （只驗舊 issue + 掃本輪新 diff），通過才推進 accepting；最多跑 max_fix_rounds 輪，超過則
// 維持 FAIL 報告並 escalate 到 needs-attention。
//
// 回傳 (cont, err)：cont 為 true 表示主迴圈應 continue（已推進 accepting 或跳過 deep review）；
// cont 為 false 且 err 為 nil 表示已落入終止狀態（needs-attention / blocked），主迴圈應 break；
// err 非 nil 表示 hard error 或 context cancel，直接中止。
func runDeepReviewPhase(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, ops gitops.Ops, newRunner func(runnerName string, logPath string, model string) runner.Runner, commitStrategy string, manualRunner string) (bool, error) {
	featureID := feature.ID
	round := s.Round

	// active profile 用於解析 mini-coder 的 coder model 與 deep-reviewing phase 的 runner 覆寫。
	_, pc, err := protocol.ResolveProfile(cfg, feature, s.Profile)
	if err != nil {
		s.Active = false
		s.StopReason = "profile-error"
		s.StopMessage = fmt.Sprintf("deep-reviewer profile resolution failed: %v", err)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		return false, fmt.Errorf("resolve profile: %w", err)
	}

	// deep-reviewing phase 的 runner 依覆寫優先序解析；其下所有子 role（deep-reviewer、
	// mini-coder、re-verifier、synthesizer）皆共用此 runner，model 行為各自維持既有語意。
	deepRunner, err := protocol.ResolvePhaseRunner(cfg, feature, pc, protocol.PhaseDeepReviewing, manualRunner)
	if err != nil {
		s.Active = false
		s.StopReason = "runner-error"
		s.StopMessage = fmt.Sprintf("deep-reviewer runner resolution failed: %v", err)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		return false, fmt.Errorf("deep runner resolution failed: %w", err)
	}

	// 1. 解析 deep_model（deep_model 掛在 reviewer role 上）；未設定時跳過 deep review 直接 accepting。
	deepModel, err := protocol.ResolveDeepModel(cfg, deepRunner, protocol.RoleReviewer)
	if err != nil {
		s.Active = false
		s.StopReason = "model-error"
		s.StopMessage = fmt.Sprintf("deep-reviewer model resolution failed: %v", err)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		return false, fmt.Errorf("deep model resolution failed: %w", err)
	}
	if deepModel == "" {
		newState, err := state.Transition(*s, protocol.PhaseAccepting, protocol.RoleAcceptor)
		if err != nil {
			return false, fmt.Errorf("skip deep-review transition: %w", err)
		}
		*s = newState
		if err := ws.WriteState(featureID, *s); err != nil {
			return false, fmt.Errorf("write state (skip deep-review): %w", err)
		}
		logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: round,
			Runner: s.Runner, Detail: "deep_model not configured, skipping deep review",
		})
		fmt.Printf("[round %d] deep-reviewing — skipped (no deep_model configured)\n", round)
		return true, nil
	}

	// 2. 跑 deep reviewer：依設定走平行 N sub-reviewer + synthesizer，或 fallback 單 agent。
	// SubPhaseReviewing 在分支前設定，平行與單 agent fallback 兩條路徑共用。
	s.Role = protocol.RoleDeepReviewer
	s.SubPhase = protocol.SubPhaseReviewing
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (deep-reviewer): %w", err)
	}
	groups := protocol.GroupReviewAngles(
		protocol.ResolveParallelReviewers(cfg, protocol.RoleDeepReviewer),
		protocol.ResolveAnglesPerReviewer(cfg, protocol.RoleDeepReviewer),
		protocol.DeepReviewAngleCount)
	if len(groups) > 1 {
		// 平行模式：N sub-reviewer 各寫 partial report，synthesizer 合併成 deep-review-report.md。
		if ok, err := runDeepReviewParallel(ctx, ws, runnerWs, feature, cfg, s, ops, newRunner, deepRunner, deepModel, groups, round); !ok || err != nil {
			return ok, err
		}
	} else {
		// fallback 單 agent：deep reviewer 直接輸出 deep-review-report.md（現行行為）。
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleDeepReviewer, deepRunner, deepModel, runner.LogFileName(round, string(protocol.RoleDeepReviewer)), round, 0); !ok || err != nil {
			return ok, err
		}
		if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleDeepReviewer); !ok || err != nil {
			return ok, err
		}
	}
	reportPath := filepath.Join(ws.RoundDir(featureID, round), protocol.DeepReviewReport)
	if _, statErr := os.Stat(reportPath); statErr != nil {
		return parallelNeedsAttention(ws, featureID, s, "missing-artifact: "+protocol.DeepReviewReport)
	}

	// 3. PASS → accepting。
	if reviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
		autoDiscoverFeatures(ws, feature, cfg, round)
		return deepTransitionAccepting(ws, featureID, s)
	}

	// 4. FAIL → 內部自癒循環。
	maxFix := protocol.ResolveMaxFixRounds(cfg, protocol.RoleDeepReviewer)
	coderModel, err := protocol.ResolvePhaseModel(cfg, feature, pc, protocol.PhaseCoding, protocol.RoleCoder, deepRunner, "")
	if err != nil {
		s.Active = false
		s.StopReason = "model-error"
		s.StopMessage = fmt.Sprintf("coder model resolution failed: %v", err)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		return false, fmt.Errorf("coder model resolution failed: %w", err)
	}
	reviewModel, err := protocol.ResolveModel(cfg, deepRunner, protocol.RoleReviewer)
	if err != nil {
		s.Active = false
		s.StopReason = "model-error"
		s.StopMessage = fmt.Sprintf("reviewer model resolution failed: %v", err)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		return false, fmt.Errorf("reviewer model resolution failed: %w", err)
	}

	for iter := 1; iter <= maxFix; iter++ {
		fmt.Printf("[round %d] deep-reviewing — self-heal iteration %d/%d\n", round, iter, maxFix)

		// 4a. mini-coder（model = coder model，不用昂貴 deep_model），phase 維持 deep-reviewing。
		s.Role = protocol.RoleMiniCoder
		s.SubPhase = protocol.SubPhaseFixing
		if err := ws.WriteState(featureID, *s); err != nil {
			return false, fmt.Errorf("write state (mini-coder): %w", err)
		}
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleMiniCoder, deepRunner, coderModel, runner.DeepFixLogFileName(round, iter), round, iter); !ok || err != nil {
			return ok, err
		}

		// mini-coder 改了 source code：worktree + per-round 模式下比照 coder 即時 commit。
		if commitStrategy == "per-round" && runnerWs.Root != ws.Root {
			if err := ops.Commit(runnerWs.Root, featureID, fmt.Sprintf("wip(%s): round %d deep-fix %d", featureID, round, iter)); err != nil {
				slog.Error("auto-commit deep-fix failed", "feature", featureID, "round", round, "iteration", iter, "error", err)
			} else {
				slog.Info("auto-commit", "feature", featureID, "round", round, "iteration", iter, "strategy", "deep-fix")
			}
		}

		// 4b. guard 檢查：mini-coder 改動超出原始 scope → 寫 FAIL 報告 + escalation，停下等人。
		if guardResult := guard.Check(ws, featureID, ops); !guardResult.Pass {
			reason := strings.Join(guardResult.Errors, "; ")
			writeDeepReviewFailReport(ws, featureID, round, "scope-exceed", reason)
			writeDeepEscalation(ws, featureID, round, "scope-change", "mini-coder scope-exceed: "+reason)
			s.Phase = protocol.PhaseNeedsAttention
			s.Active = false
			s.StopReason = "scope-exceed"
			s.StopMessage = "deep-fix scope exceeded: " + reason
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "guard-fail", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleMiniCoder,
				Round: round, Detail: s.StopMessage, Runner: s.Runner,
			})
			return false, nil
		}

		// 4c. re-verifier（model = reviewer model，scoped 驗證，不用昂貴 opus），read-only。
		s.Role = protocol.RoleReVerifier
		s.SubPhase = protocol.SubPhaseReverifying
		if err := ws.WriteState(featureID, *s); err != nil {
			return false, fmt.Errorf("write state (re-verifier): %w", err)
		}
		if ok, err := runDeepSubRole(ctx, ws, runnerWs, feature, cfg, s, newRunner,
			protocol.RoleReVerifier, deepRunner, reviewModel, runner.DeepReverifyLogFileName(round, iter), round, iter); !ok || err != nil {
			return ok, err
		}
		if ok, err := deepGuardCheck(ws, featureID, s, ops, protocol.RoleReVerifier); !ok || err != nil {
			return ok, err
		}

		// 4d. re-verifier 已把 deep-review-report.md 的 Verdict 改 PASS → accepting。
		if reviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
			autoDiscoverFeatures(ws, feature, cfg, round)
			return deepTransitionAccepting(ws, featureID, s)
		}
	}

	// 5. 跑滿 maxFix 仍 FAIL → 維持 FAIL 報告 + escalate 到 needs-attention。
	writeDeepEscalation(ws, featureID, round, "blocker",
		fmt.Sprintf("deep-review self-heal exhausted after %d iterations", maxFix))
	s.Phase = protocol.PhaseNeedsAttention
	s.Active = false
	s.StopReason = "self-heal-exhausted"
	s.StopMessage = fmt.Sprintf("deep-review self-heal exhausted after %d iterations", maxFix)
	logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
	logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "escalation", Phase: protocol.PhaseDeepReviewing, Role: protocol.RoleDeepReviewer,
		Round: round, Detail: s.StopMessage, Runner: s.Runner, Notify: protocol.NotifyWarning,
	})
	fmt.Printf("[round %d] deep-reviewing — self-heal exhausted (%d iterations), escalating\n", round, maxFix)
	return false, nil
}

// runDeepSubRole 在 deep-reviewing phase 內 spawn 一個子 role（deep-reviewer / mini-coder /
// re-verifier），處理 phase-start/run-end event、prompt 產生、runner 執行與 worktree 同步，
// 並分類 context cancel / runner error / hard error / soft fail。phase 全程維持 deep-reviewing。
//
// 回傳 (ok, err)：ok 為 true 表示 runner 正常結束，caller 可繼續；ok 為 false 且 err 為 nil
// 表示已寫入終止狀態（needs-attention / blocked）；err 非 nil 表示 hard error 或 cancel。
func runDeepSubRole(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s *protocol.State, newRunner func(runnerName string, logPath string, model string) runner.Runner, role protocol.Role, runnerName, model, logName string, round, iteration int) (bool, error) {
	featureID := feature.ID

	ws.AppendEvent(featureID, protocol.Event{
		Type: "phase-start", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
		Runner: runnerName, Model: model,
	})

	prompt, err := generatePrompt(ws, runnerWs, feature, cfg, role, round, iteration)
	if err != nil {
		prompt = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, round, featureID)
	}
	logPath := filepath.Join(runner.LogDir(ws, featureID), logName)
	r := newRunner(runnerName, logPath, model)

	if runnerWs.Root != ws.Root {
		syncFeatureToWorktree(ws, runnerWs, featureID, round)
	}
	var stopSync func()
	if runnerWs.Root != ws.Root {
		stopSync = startLiveSync(runnerWs, ws, featureID, round)
	}

	if model != "" {
		fmt.Printf("[round %d] deep-reviewing (%s) — invoking %s (model: %s)\n", round, role, runnerName, model)
	} else {
		fmt.Printf("[round %d] deep-reviewing (%s) — invoking %s\n", round, role, runnerName)
	}

	result, runErr := r.Run(ctx, prompt)

	if stopSync != nil {
		stopSync()
	}
	if runnerWs.Root != ws.Root {
		if serr := syncFeatureFromWorktree(runnerWs, ws, featureID, round); serr != nil {
			slog.Warn("sync from worktree failed", "feature", featureID, "round", round, "role", role, "error", serr)
		}
	}

	if runErr != nil {
		if ctx.Err() == context.Canceled {
			s.Active = false
			s.StopReason = "interrupted"
			s.StopMessage = fmt.Sprintf("deep-reviewing (%s) interrupted by signal (round %d)", role, round)
			logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
			return false, ctx.Err()
		}
		s.Phase = protocol.PhaseNeedsAttention
		s.Active = false
		s.StopReason = "runner-error"
		s.StopMessage = fmt.Sprintf("deep-reviewing (%s) runner failed (round %d): %v", role, round, runErr)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
			Status: "error", Detail: runErr.Error(), Runner: runnerName, Model: model,
		})
		return false, runErr
	}

	ws.AppendEvent(featureID, protocol.Event{
		Type: "run-end", Phase: protocol.PhaseDeepReviewing, Role: role, Round: round,
		Status: fmt.Sprintf("exit-%d", result.ExitCode), Runner: runnerName, Model: model,
	})

	if runner.IsHardError(result) {
		s.Active = false
		s.StopReason = "hard-error"
		s.StopMessage = fmt.Sprintf("deep-reviewing (%s) runner returned hard error (exit 2) (round %d)", role, round)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		return false, fmt.Errorf("runner returned hard error (exit 2)")
	}
	if runner.IsSoftFail(result) {
		s.Phase = protocol.PhaseBlocked
		s.Active = false
		s.StopReason = "soft-fail"
		s.StopMessage = fmt.Sprintf("deep-reviewing (%s) runner returned soft-fail (exit %d) (round %d)", role, runner.ExitSoftFail, round)
		logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
		logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseBlocked), featureID, protocol.PhaseBlocked)
		return false, nil
	}
	return true, nil
}

// deepGuardCheck 在 deep-reviewing phase 內對 read-only 子 role（deep-reviewer / re-verifier）
// 跑 guardrail 檢查；失敗時落入 needs-attention 並回傳 (false, nil)。
func deepGuardCheck(ws *protocol.Workspace, featureID string, s *protocol.State, ops gitops.Ops, role protocol.Role) (bool, error) {
	guardResult := guard.Check(ws, featureID, ops)
	if guardResult.Pass {
		return true, nil
	}
	s.Phase = protocol.PhaseNeedsAttention
	s.Active = false
	guardMsg := strings.Join(guardResult.Errors, "; ")
	s.StopReason = "guard-fail"
	s.StopMessage = guardMsg
	logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
	logSyncErr(ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "guard-fail", Phase: protocol.PhaseDeepReviewing, Role: role,
		Round: s.Round, Detail: guardMsg, Runner: s.Runner,
	})
	return false, nil
}

// deepTransitionAccepting 把 state 從 deep-reviewing 推進到 accepting 並寫回，
// 供自癒循環在 deep review PASS 時放行。
func deepTransitionAccepting(ws *protocol.Workspace, featureID string, s *protocol.State) (bool, error) {
	newState, err := state.Transition(*s, protocol.PhaseAccepting, protocol.RoleAcceptor)
	if err != nil {
		return false, fmt.Errorf("deep-review→accepting transition: %w", err)
	}
	*s = newState
	// 離開 deep-reviewing：清空 subPhase，使續跑的主迴圈持有的 *s 與磁碟一致。
	s.SubPhase = ""
	if err := ws.WriteState(featureID, *s); err != nil {
		return false, fmt.Errorf("write state (accepting): %w", err)
	}
	logSyncErr(ws.SyncFeatureStatus(featureID, s.Phase), featureID, s.Phase)
	ws.AppendEvent(featureID, protocol.Event{
		Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round, Runner: s.Runner,
	})
	return true, nil
}

// autoDiscoverFeatures 在 final deep review PASS 後執行：parse deep-review-report.md 的
// [NEW-FEATURE] 標記，去重、套數量上限後建立新 feature，並寫出 discovered-features.md 摘要。
// 只在兩個 deep review PASS return 點（首次 PASS、self-heal 後 re-verifier 改 PASS）被呼叫，
// 中間輪與 FAIL/needs-attention 路徑都到不了，因此等同「只在 final deep review 觸發」。
// 為 best-effort：任何錯誤只記 log，絕不中斷 accepting 轉換。
func autoDiscoverFeatures(ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, round int) {
	if !cfg.AutoDiscoverFeatures {
		return
	}

	reportPath := filepath.Join(ws.RoundDir(feature.ID, round), protocol.DeepReviewReport)
	data, err := os.ReadFile(reportPath)
	if err != nil {
		slog.Warn("auto-discover: read deep-review report failed", "feature", feature.ID, "round", round, "error", err)
		return
	}

	cands := protocol.ParseDiscoveredFeatures(string(data))
	if len(cands) == 0 {
		return
	}

	existing, _ := ws.ListFeatures()
	kept := protocol.DedupeDiscovered(cands, existing)

	max := protocol.ResolveMaxDiscoveredFeatures(cfg)
	var capped []protocol.DiscoveredFeature
	if len(kept) > max {
		capped = kept[max:]
		kept = kept[:max]
	}

	// skipped 為被去重濾掉的候選（出現在 cands 但不在 kept/capped 中）。
	keptOrCapped := make(map[string]struct{})
	for _, d := range kept {
		keptOrCapped[d.Title] = struct{}{}
	}
	for _, d := range capped {
		keptOrCapped[d.Title] = struct{}{}
	}
	var skipped []protocol.DiscoveredFeature
	for _, c := range cands {
		if _, ok := keptOrCapped[c.Title]; !ok {
			skipped = append(skipped, c)
		}
	}

	var createdList []discoveredCreated
	for _, d := range kept {
		next, nerr := feat.NextNumber(ws)
		if nerr != nil {
			slog.Warn("auto-discover: next feature number failed", "feature", feature.ID, "title", d.Title, "error", nerr)
			continue
		}
		id := feat.GenerateFeatureID(next, d.Title)
		f := feat.Feature{
			ID:          id,
			Name:        fmt.Sprintf("F%03d: %s", next, d.Title),
			Description: d.Description,
			Status:      feat.StatusNotStarted,
		}
		if serr := ws.SaveFeature(f); serr != nil {
			slog.Warn("auto-discover: save feature failed", "feature", feature.ID, "title", d.Title, "error", serr)
			continue
		}
		createdList = append(createdList, discoveredCreated{id: id, title: d.Title})
		ws.AppendEvent(feature.ID, protocol.Event{
			Type: "feature-discovered", Phase: protocol.PhaseDeepReviewing, Round: round, Detail: id,
		})
	}

	writeDiscoveredFeaturesReport(ws, feature.ID, createdList, skipped, capped)

	fmt.Printf("[round %d] auto-discovered %d feature(s)\n", round, len(createdList))
}

// discoveredCreated 記錄一筆已建立的 feature（id 與 title），供摘要報告列出。
type discoveredCreated struct{ id, title string }

// writeDiscoveredFeaturesReport 寫出 .4x/{featureID}/discovered-features.md 摘要：
// 列出已建立、因重複略過、因超過上限略過的候選 feature。
func writeDiscoveredFeaturesReport(ws *protocol.Workspace, featureID string, created []discoveredCreated, skipped, capped []protocol.DiscoveredFeature) {
	var b strings.Builder
	b.WriteString("# Discovered Features\n\n")

	b.WriteString("## Created\n")
	if len(created) == 0 {
		b.WriteString("None\n")
	} else {
		for _, c := range created {
			fmt.Fprintf(&b, "- %s — %s\n", c.id, c.title)
		}
	}

	b.WriteString("\n## Skipped (duplicate)\n")
	if len(skipped) == 0 {
		b.WriteString("None\n")
	} else {
		for _, d := range skipped {
			fmt.Fprintf(&b, "- %s\n", d.Title)
		}
	}

	b.WriteString("\n## Capped (over limit)\n")
	if len(capped) == 0 {
		b.WriteString("None\n")
	} else {
		for _, d := range capped {
			fmt.Fprintf(&b, "- %s\n", d.Title)
		}
	}

	path := filepath.Join(ws.FeatureDir(featureID), "discovered-features.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		slog.Error("write discovered-features report failed", "feature", featureID, "error", err)
	}
}

// writeDeepReviewFailReport 由 CLI 在 deep-reviewing 終止場景（如 mini-coder scope-exceed）
// 直接寫出 FAIL 的 deep-review-report.md，標注原因供 dashboard 與 acceptor 辨識。
func writeDeepReviewFailReport(ws *protocol.Workspace, featureID string, round int, reason, detail string) {
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.DeepReviewReport)
	content := fmt.Sprintf("# Deep Review Report — Round %d\n\n## Summary\nFAIL — %s\n\n## Issues\n### [CRITICAL] %s\n%s\n\n## Verdict\nFAIL\n",
		round, reason, reason, detail)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		slog.Error("write deep-review FAIL report failed", "feature", featureID, "round", round, "error", err)
	}
}

// writeDeepEscalation 由 CLI 在 deep-reviewing 終止場景寫出 escalation.json，讓 resume 與
// dashboard 能辨識升級原因（scope-change / blocker）。
func writeDeepEscalation(ws *protocol.Workspace, featureID string, round int, reason, detail string) {
	esc := protocol.Escalation{Needed: true, Reason: reason, Detail: detail}
	data, _ := json.Marshal(esc)
	path := filepath.Join(ws.RoundDir(featureID, round), protocol.EscalationFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Error("write deep-review escalation failed", "feature", featureID, "round", round, "error", err)
	}
}

func reviewPassed(ws *protocol.Workspace, featureID string, round int, reportFile string) bool {
	roundDir := ws.RoundDir(featureID, round)
	return reviewPassedAtPath(filepath.Join(roundDir, reportFile))
}

func reviewPassedAtPath(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	result := parseReviewVerdict(string(data))
	return result.Passed && result.CriticalCount == 0 && result.WarningCount == 0
}

// parseReviewVerdict 從 review-report.md 擷取 verdict 與 critical/warning issue 計數
func parseReviewVerdict(content string) protocol.ReviewResult {
	lines := strings.Split(content, "\n")
	var result protocol.ReviewResult
	inVerdict := false
	verdictFound := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)

		// 只計行首的 issue tag（### [WARNING] 或 [WARNING] 開頭），
		// 避免把正文中引述上一輪 issue 的文字誤計為本輪 issue。
		if strings.HasPrefix(upper, "[CRITICAL]") || strings.HasPrefix(upper, "### [CRITICAL]") ||
			strings.HasPrefix(upper, "#### [CRITICAL]") {
			result.CriticalCount++
		}
		if strings.HasPrefix(upper, "[WARNING]") || strings.HasPrefix(upper, "### [WARNING]") ||
			strings.HasPrefix(upper, "#### [WARNING]") {
			result.WarningCount++
		}

		if strings.HasPrefix(trimmed, "## Verdict") {
			inVerdict = true
			continue
		}
		if inVerdict && !verdictFound && trimmed != "" {
			// strip markdown bold/italic（**PASS** → PASS）
			clean := strings.ToUpper(strings.Trim(trimmed, "*_"))
			if strings.HasPrefix(clean, "PASS") || strings.HasPrefix(clean, "CONDITIONAL PASS") {
				result.Passed = true
			}
			verdictFound = true
		}
	}

	return result
}

// reviewFailCount 回傳指定 round 的 review-report.md 失敗計數（critical + warning）。
// 供 run loop 判斷連續輪次是否有進展（失敗數下降）使用；report 不存在時回 0。
func reviewFailCount(ws *protocol.Workspace, featureID string, round int) int {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.ReviewReport))
	if err != nil {
		return 0
	}
	result := parseReviewVerdict(string(data))
	return result.CriticalCount + result.WarningCount
}

// verifyPassed 檢查 verify.json 的 passed 欄位，用於 guard 失敗時判斷是測試未通過還是 artifact 缺失
func verifyPassed(ws *protocol.Workspace, featureID string, round int) bool {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.VerifyFile))
	if err != nil {
		return false
	}
	var ve protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ve); err != nil {
		return false
	}
	return ve.Passed
}

// cleanStaleArtifact 只清除當前 phase 的「半成品」output artifact。
// resume 場景下（SIGKILL、runner-error、crash），runner 可能寫了不完整的 report，
// 若不清除，nextPhaseAfter 會誤認 phase 完成而跳過該步驟。
// 反之，已完成的 report 一律保留——crash 重啟不得讓前一輪已驗收的產出消失。
// 完整性判準依 artifact 種類，由各 *Complete helper 判斷。
func cleanStaleArtifact(ws *protocol.Workspace, featureID string, phase protocol.Phase, round int) {
	roundDir := ws.RoundDir(featureID, round)
	switch phase {
	case protocol.PhaseCoding, protocol.PhaseAmending:
		removeIfIncomplete(filepath.Join(roundDir, protocol.CoderReport), coderReportComplete)
	case protocol.PhaseReviewing:
		removeIfIncomplete(filepath.Join(roundDir, protocol.ReviewReport), reviewReportComplete)
	case protocol.PhaseDesignReviewing:
		removeIfIncomplete(filepath.Join(ws.FeatureDir(featureID), protocol.DesignReviewReport), reviewReportComplete)
	case protocol.PhaseTesting:
		// test-report 與 verify.json 成對；verify.json 可解析即視為該 phase 完整。
		verifyPath := filepath.Join(roundDir, protocol.VerifyFile)
		if verifyEvidenceComplete(verifyPath) {
			return
		}
		os.Remove(filepath.Join(roundDir, protocol.TestReport))
		os.Remove(verifyPath)
	case protocol.PhaseDeepReviewing:
		removeIfIncomplete(filepath.Join(roundDir, protocol.DeepReviewReport), reviewReportComplete)
	case protocol.PhaseAccepting:
		removeIfIncomplete(filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport), nonEmptyFile)
	}
}

// removeIfIncomplete 在 path 指向的 artifact 不完整時移除它（讓該步驟重跑）；完整則原樣保留。
// complete 回傳該檔是否為完整產出。檔案不存在則無事可做。
func removeIfIncomplete(path string, complete func(string) bool) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	if complete(path) {
		return
	}
	os.Remove(path)
}

// coderReportComplete 判斷 coder-report.md 是否完整：非空且含 template 終止區段標記 `## Verification`。
func coderReportComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return strings.Contains(string(data), "## Verification")
}

// reviewReportComplete 判斷 review / deep-review report 是否完整：非空且含可解析的 `## Verdict` 區段。
func reviewReportComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return reportHasVerdict(string(data))
}

// verifyEvidenceComplete 判斷 verify.json 是否完整：可成功 unmarshal 成 protocol.VerifyEvidence
// （與 verifyPassed 相同的解析路徑，但不要求 passed=true——FAIL 的 evidence 同樣是完整產出）。
func verifyEvidenceComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var ve protocol.VerifyEvidence
	return json.Unmarshal(data, &ve) == nil
}

// nonEmptyFile 判斷 path 指向的檔案是否存在且去除空白後非空。
func nonEmptyFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(data)) > 0
}

// reportHasVerdict 判斷 review-report 內容是否含可解析的 `## Verdict` 區段。
// 與 parseReviewVerdict 的掃描方式一致：在 `## Verdict` header 後找到首個非空行即算辨識成功。
func reportHasVerdict(content string) bool {
	lines := strings.Split(content, "\n")
	inVerdict := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Verdict") {
			inVerdict = true
			continue
		}
		if inVerdict && trimmed != "" {
			return true
		}
	}
	return false
}

// needsResumeRecovery 判斷 resume 一個 crash（先前 active、process 已死）的 feature 時，
// 是否需要用 smartResumePhase 依實際 artifacts 校正 state.json 的 phase。
//   - blocked / needs-attention：任何 round 都校正（維持既有 recovery 行為）。
//   - 已進入 coding 後的工作 phase（coding/reviewing/testing/deep-reviewing/amending/accepting）
//     且 round > 0：校正，使 state.json 與磁碟 artifacts 一致。
//   - init / designing / pending-review / done 等：不校正，維持原 resume 行為。
func needsResumeRecovery(s protocol.State) bool {
	if s.Phase == protocol.PhaseBlocked || s.Phase == protocol.PhaseNeedsAttention {
		return true
	}
	if s.Round <= 0 {
		return false
	}
	switch s.Phase {
	case protocol.PhaseDesignReviewing, protocol.PhaseCoding, protocol.PhaseReviewing, protocol.PhaseTesting,
		protocol.PhaseDeepReviewing, protocol.PhaseAmending, protocol.PhaseAccepting:
		return true
	default:
		return false
	}
}

// smartResumePhase 檢查當前 round 的 artifacts 決定 resume 起點。
// 已完成的步驟不重跑；只從第一個缺失或失敗的步驟開始。
// 「已完成」採與 cleanStaleArtifact 相同的完整性判準（*Complete helper），而非裸存在性檢查：
// crash 發生於當前 phase、report 寫到一半時，半成品檔雖存在但不完整，必須回該 phase 重跑，
// 不可因檔案存在就把 phase 往前推進。
func smartResumePhase(ws *protocol.Workspace, featureID string, round int, cfg protocol.Config) (protocol.Phase, protocol.Role, protocol.SubPhase) {
	if round == 0 {
		return protocol.PhaseDesigning, protocol.RoleDesigner, ""
	}
	roundDir := ws.RoundDir(featureID, round)

	designReviewPath := filepath.Join(ws.FeatureDir(featureID), protocol.DesignReviewReport)
	if reviewReportComplete(designReviewPath) {
		if !reviewPassedAtPath(designReviewPath) {
			return protocol.PhaseDesigning, protocol.RoleDesigner, ""
		}
	} else if _, err := os.Stat(designReviewPath); err == nil {
		return protocol.PhaseDesignReviewing, protocol.RoleDesignReviewer, ""
	}

	if !coderReportComplete(filepath.Join(roundDir, protocol.CoderReport)) {
		return protocol.PhaseCoding, protocol.RoleCoder, ""
	}

	if !reviewReportComplete(filepath.Join(roundDir, protocol.ReviewReport)) {
		return protocol.PhaseReviewing, protocol.RoleReviewer, ""
	}
	if !reviewPassed(ws, featureID, round, protocol.ReviewReport) {
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	// testing 的完整性以 verify.json 可解析為準（與 cleanStaleArtifact 一致）；
	// test-report 與 verify.json 成對產出，verify.json 不完整即代表該 phase 未跑完。
	if !verifyEvidenceComplete(filepath.Join(roundDir, protocol.VerifyFile)) {
		return protocol.PhaseTesting, protocol.RoleTester, ""
	}
	if !verifyPassed(ws, featureID, round) {
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	if !reviewReportComplete(filepath.Join(roundDir, protocol.DeepReviewReport)) {
		// deep-review report 不完整：依磁碟上的 partial 狀態推斷中斷在哪個子步驟，
		// 讓 resume 只補跑缺少的部分（partial 全到齊 → synthesizer；否則 → sub-reviewer）。
		sub := deepResumeSubPhase(ws, featureID, round, cfg)
		role := protocol.RoleDeepReviewer
		if sub == protocol.SubPhaseSynthesizing {
			role = protocol.RoleSynthesizer
		}
		return protocol.PhaseDeepReviewing, role, sub
	}
	if !reviewPassed(ws, featureID, round, protocol.DeepReviewReport) {
		// deep-review FAIL → amending（同輪修正、Round++），與正常流程的
		// parallelTransition(..., PhaseAmending, ...) 及上方 review / verify FAIL 路徑一致；
		// 不再回傳 PhaseCoding（會被誤判為開新 coding 輪而覆蓋前輪報告）。
		return protocol.PhaseAmending, protocol.RoleCoder, ""
	}

	return protocol.PhaseAccepting, protocol.RoleAcceptor, ""
}

// deepResumeSubPhase 在 deep-review report 不完整時，依磁碟上的 partial 檔推斷 crash 中斷的子步驟：
//   - want<=1（單 agent 模式，無 partial）→ SubPhaseReviewing（重跑單一 deep-reviewer）。
//   - 有任何 partial 缺失/不完整 → SubPhaseReviewing（補跑缺少的 sub-reviewer）。
//   - partial 全到齊但 report 不完整 → SubPhaseSynthesizing（只重跑 synthesizer）。
//
// want 用與 runDeepReviewPhase 完全相同的純函式重算（GroupReviewAngles 輸入相同 → 輸出相同），
// 確保 resume 推斷的並行度與當初執行時一致。
func deepResumeSubPhase(ws *protocol.Workspace, featureID string, round int, cfg protocol.Config) protocol.SubPhase {
	want := len(protocol.GroupReviewAngles(
		protocol.ResolveParallelReviewers(cfg, protocol.RoleDeepReviewer),
		protocol.ResolveAnglesPerReviewer(cfg, protocol.RoleDeepReviewer),
		protocol.DeepReviewAngleCount))
	if want <= 1 {
		return protocol.SubPhaseReviewing
	}
	if len(missingDeepPartials(ws.RoundDir(featureID, round), want)) > 0 {
		return protocol.SubPhaseReviewing
	}
	return protocol.SubPhaseSynthesizing
}

// isDesignerEscalation 判斷 escalation 是否應回到 Designer 而非停下來等人
// spec-mismatch / criteria-wrong 是 Designer 能自行修正的問題
func isDesignerEscalation(reason string) bool {
	return reason == "spec-mismatch" || reason == "criteria-wrong" || reason == "scope-change"
}

func readEscalation(ws *protocol.Workspace, featureID string, round int) protocol.Escalation {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.EscalationFile))
	if err != nil {
		return protocol.Escalation{}
	}
	var esc protocol.Escalation
	if err := json.Unmarshal(data, &esc); err != nil {
		esc.Needed = true
		esc.Reason = fmt.Sprintf("malformed escalation.json: %v", err)
		return esc
	}
	return esc
}

func captureBaselineOnce(ws *protocol.Workspace, ops gitops.Ops, featureID string, featureRepos []string) error {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile)
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("baseline path is a directory: %s", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("check baseline: %w", err)
	}
	if err := ops.CaptureBaseline(featureID, featureRepos); err != nil {
		return fmt.Errorf("capture baseline: %w", err)
	}
	return nil
}

func dryRunLoop(ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s protocol.State) error {
	phases := []struct {
		phase protocol.Phase
		role  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer},
		{protocol.PhaseAccepting, protocol.RoleAcceptor},
	}

	for _, p := range phases {
		fmt.Printf("=== %s (%s) ===\n", p.phase, p.role)
		prompt, err := generatePrompt(ws, ws, feature, cfg, p.role, 1, 0)
		if err != nil {
			fmt.Printf("  (error: %v)\n\n", err)
			continue
		}
		fmt.Println(prompt)
		fmt.Println()
	}
	return nil
}

// syncFeatureToWorktree 將主 workspace 的 feature 目錄複製到 worktree，
// 確保 runner 能讀到最新的 protocol 檔案（task-brief、上一輪 report 等）
func syncFeatureToWorktree(main, wt *protocol.Workspace, featureID string, round int) {
	srcDir := main.FeatureDir(featureID)
	dstDir := wt.FeatureDir(featureID)
	os.MkdirAll(dstDir, 0o755)

	// feature YAML（.4x/features/{id}.yaml）— runner 需要它來跑 `4x verify`
	srcYAML := filepath.Join(main.DotDir(), protocol.FeaturesDir, featureID+".yaml")
	dstFeaturesDir := filepath.Join(wt.DotDir(), protocol.FeaturesDir)
	os.MkdirAll(dstFeaturesDir, 0o755)
	gitops.CopyFileIfExists(srcYAML, filepath.Join(dstFeaturesDir, featureID+".yaml"))

	// state + feature-level 檔案
	for _, name := range []string{protocol.StateFile, protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile, protocol.DesignReviewReport} {
		gitops.CopyFileIfExists(filepath.Join(srcDir, name), filepath.Join(dstDir, name))
	}

	// 當前 round 目錄
	if round > 0 {
		srcRound := main.RoundDir(featureID, round)
		dstRound := wt.RoundDir(featureID, round)
		os.MkdirAll(dstRound, 0o755)
		entries, _ := os.ReadDir(srcRound)
		for _, e := range entries {
			if !e.IsDir() {
				gitops.CopyFileIfExists(filepath.Join(srcRound, e.Name()), filepath.Join(dstRound, e.Name()))
			}
		}
	}
}

// syncFeatureFromWorktree 將 worktree 裡 runner 寫的 protocol 檔案複製回主 workspace。
// 回傳彙整後的 error（任一 MkdirAll / ReadDir / CopyFileIfExists 失敗），讓 caller 能在
// disk full 等情況印出真因，而非只看到下游的 missing-artifact。來源檔不存在不算 error。
func syncFeatureFromWorktree(wt, main *protocol.Workspace, featureID string, round int) error {
	srcDir := wt.FeatureDir(featureID)
	dstDir := main.FeatureDir(featureID)
	var errs []string
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		errs = append(errs, err.Error())
	}

	// feature-level 檔案
	for _, name := range []string{
		protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile,
		protocol.DesignReviewReport, protocol.FinalReport,
	} {
		if _, err := gitops.CopyFileIfNewer(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	// round 目錄
	srcRound := wt.RoundDir(featureID, round)
	dstRound := main.RoundDir(featureID, round)
	if err := os.MkdirAll(dstRound, 0o755); err != nil {
		errs = append(errs, err.Error())
	}
	entries, err := os.ReadDir(srcRound)
	if err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Sprintf("read round dir: %v", err))
	}
	for _, e := range entries {
		if !e.IsDir() {
			if _, err := gitops.CopyFileIfNewer(filepath.Join(srcRound, e.Name()), filepath.Join(dstRound, e.Name())); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("sync from worktree: %s", strings.Join(errs, "; "))
	}
	return nil
}

// startLiveSync 啟動背景 goroutine，每 2 秒將 worktree 的 protocol 檔案同步回 main workspace。
// 回傳的 stop function 為阻塞式：close(done) 後 wg.Wait() 確保 in-flight 的 sync 完成才返回，
// 避免 caller 隨即執行的 final sync 與背景 sync 競爭寫同一批檔案。
func startLiveSync(wt, main *protocol.Workspace, featureID string, round int) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := syncFeatureFromWorktree(wt, main, featureID, round); err != nil {
					slog.Warn("live sync failed", "feature", featureID, "round", round, "error", err)
				}
			}
		}
	}()
	return func() {
		close(done)
		wg.Wait()
	}
}
