package main

import (
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
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/guard"
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

// stopState 設定 state 的停止欄位並寫入磁碟，統一處理散布在多處的 stop-state boilerplate。
// 呼叫者仍需自行 return（因回傳型別不同）。
func stopState(ws *protocol.Workspace, featureID string, s *protocol.State, reason, message string) {
	s.Active = false
	s.StopReason = reason
	s.StopMessage = message
	logStateWriteErr(ws.WriteState(featureID, *s), featureID, s.Phase)
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
	var phaseOverrides []string
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

			// 解析 per-phase 臨時覆寫（--phase-override），覆寫優先序最高層、只影響本次 run。
			runOverrides, err := parsePhaseOverrides(phaseOverrides)
			if err != nil {
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
				for _, po := range phaseOverrides {
					bgArgs = append(bgArgs, "--phase-override", po)
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
				ws.SkipAutoCommit = true
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
			loopErr := runLoop(ctx, ws, runnerWs, feature, cfg, s, ops, runnerFactory, commitStrategy, manualRunner, runOverrides)

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
	cmd.Flags().StringArrayVar(&phaseOverrides, "phase-override", nil, "temporary per-phase runner/model override for this run only, format <phase>:<runner>:<model> (repeatable)")
	cmd.Flags().BoolVar(&noNotify, "no-notify", false, "disable OS notification on run completion (overrides config)")
	return cmd
}

// parsePhaseOverrides 解析 --phase-override 旗標的原始字串為 per-phase 臨時覆寫 map。
//
// 每筆格式為 <phase>:<runner>:<model>，三段以冒號分隔；runner / model 任一可留空代表
// 不覆寫該維度，但兩者不可同時為空。phase 必須通過 IsSelectablePhase，重複 phase 視為錯誤。
//
// 範例：
//
//	reviewing:gemini:    只覆寫 runner
//	testing::opus        只覆寫 model tier
//	coding:codex:sonnet  兩者都覆寫
func parsePhaseOverrides(raw []string) (map[protocol.Phase]protocol.PhaseSpec, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[protocol.Phase]protocol.PhaseSpec, len(raw))
	for _, entry := range raw {
		parts := strings.SplitN(entry, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid --phase-override %q: expected <phase>:<runner>:<model>", entry)
		}
		phase := protocol.Phase(strings.TrimSpace(parts[0]))
		runnerName := strings.TrimSpace(parts[1])
		model := strings.TrimSpace(parts[2])
		if !protocol.IsSelectablePhase(phase) {
			return nil, fmt.Errorf("invalid --phase-override %q: %q is not a selectable phase", entry, phase)
		}
		if runnerName == "" && model == "" {
			return nil, fmt.Errorf("invalid --phase-override %q: runner and model cannot both be empty", entry)
		}
		if _, dup := out[phase]; dup {
			return nil, fmt.Errorf("invalid --phase-override %q: phase %q overridden more than once", entry, phase)
		}
		out[phase] = protocol.PhaseSpec{Phase: string(phase), Runner: runnerName, Model: model}
	}
	return out, nil
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

// runLoop 保持原簽名供既有測試與 RunE closure 呼叫，內部委託給 runContext.loop。
func runLoop(ctx context.Context, ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s protocol.State, ops gitops.Ops, newRunner func(runnerName string, logPath string, model string) runner.Runner, commitStrategy string, manualRunner string, runOverrides map[protocol.Phase]protocol.PhaseSpec) error {
	rc := &runContext{
		ws:             ws,
		runnerWs:       runnerWs,
		feature:        feature,
		cfg:            cfg,
		ops:            ops,
		newRunner:      newRunner,
		commitStrategy: commitStrategy,
		manualRunner:   manualRunner,
		runOverrides:   runOverrides,
	}
	return rc.loop(ctx, s)
}
