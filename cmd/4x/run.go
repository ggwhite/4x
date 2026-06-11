package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var runnerName string
	var maxRounds int
	var timeout int
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "run <feature-id>",
		Short: "Run the Design-Code-Review-Test loop for a feature",
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
			feature, err := ws.LoadFeature(featureID)
			if err != nil {
				return err
			}

			cfg, err := ws.ReadConfig()
			if err != nil {
				return err
			}

			if runnerName == "" {
				runnerName = cfg.Default
			}
			runnerCfg, ok := cfg.Runners[runnerName]
			if !ok {
				return fmt.Errorf("runner %q not found in config", runnerName)
			}

			if maxRounds <= 0 {
				maxRounds = 5
			}

			depResult := guard.CheckDependencies(ws, featureID)
			if !depResult.Pass {
				for _, e := range depResult.Errors {
					fmt.Fprintf(os.Stderr, "  blocked: %s\n", e)
				}
				return fmt.Errorf("feature %s has unmet dependencies", featureID)
			}

			// worktree isolation：runner 在獨立 worktree 內執行
			var runnerWs *protocol.Workspace
			var wtPath string
			if cfg.Isolation == "worktree" {
				var err error
				wtPath, err = setupWorktree(ws.Root, featureID)
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
				s = existing
				s.Active = true
				s.Runner = runnerName
				if s.Phase == protocol.PhaseDone {
					return fmt.Errorf("feature %s is already done", featureID)
				}
			}

			if err := ws.WriteState(featureID, s); err != nil {
				return err
			}

			ws.AppendEvent(featureID, protocol.Event{
				Type:  "run-start",
				Phase: s.Phase,
				Role:  state.PhaseToRole(s.Phase),
			})

			if dryRun {
				return dryRunLoop(ws, feature, cfg, s)
			}

			r := runner.NewRunner(runnerWs, runnerName, runnerCfg, time.Duration(timeout)*time.Second)
			loopErr := runLoop(ws, feature, cfg, s, r)

			if wtPath != "" && s.Phase == protocol.PhaseDone {
				cleanupWorktree(ws.Root, featureID, wtPath)
			}

			return loopErr
		},
	}

	cmd.Flags().StringVar(&runnerName, "runner", "", "runner plugin name (default: config default)")
	cmd.Flags().IntVar(&maxRounds, "max-rounds", 0, "max iteration rounds (default: 5)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "plugin timeout in seconds")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print prompts without calling plugin")
	return cmd
}

func generatePrompt(ws *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, role protocol.Role, round int) (string, error) {
	tmpl, err := loadRoleTemplate(role)
	if err != nil {
		return "", fmt.Errorf("no template for role %s: %w", role, err)
	}
	locale, localeName := resolveLocale()
	var roleInc []string
	if rc, ok := cfg.Roles[string(role)]; ok {
		roleInc = rc.Includes
	}
	data := promptData{
		Feature:          feature,
		Project:          cfg.Project,
		Role:             role,
		Round:            round,
		Config:           cfg,
		DotDir:           ws.DotDir(),
		Locale:           locale,
		LocaleName:       localeName,
		RoleInstructions: roleInstructions(cfg, role),
		ProjectIncludes:  loadIncludes(ws.Root, cfg.Project.Includes),
		RoleIncludes:     loadIncludes(ws.Root, roleInc),
		PlanningDoc:      loadPlanningDocs(ws.Root, feature.ID),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func runLoop(ws *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s protocol.State, r runner.Runner) error {
	featureID := feature.ID
	ctx := context.Background()

	if s.Phase == protocol.PhaseInit {
		var err error
		s, err = state.Transition(s, protocol.PhaseDesigning, protocol.RoleDesigner)
		if err != nil {
			return err
		}
		ws.WriteState(featureID, s)
		syncFeatureStatus(ws, featureID, s.Phase)
	}

	for s.Active {
		phase := s.Phase
		role := state.PhaseToRole(phase)

		if phase == protocol.PhaseDone || phase == protocol.PhaseBlocked || phase == protocol.PhaseNeedsAttention {
			break
		}

		if stop, reason := state.ShouldStop(s); stop {
			s.Active = false
			s.StopReason = reason
			s.Phase = protocol.PhaseNeedsAttention
			ws.WriteState(featureID, s)
			syncFeatureStatus(ws, featureID, s.Phase)
			ws.AppendEvent(featureID, protocol.Event{Type: "escalation", Phase: s.Phase, Detail: reason})
			fmt.Printf("  stopped: %s\n", reason)
			return nil
		}

		// 清除上一輪遺留的 feature-level 產出物，避免舊文件通過新一輪的 guard 檢查
		if phase == protocol.PhaseTesting {
			os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.FinalReport))
			os.Remove(filepath.Join(ws.FeatureDir(featureID), protocol.CommitPlan))
		}

		if phase == protocol.PhaseCoding && s.Round == 1 {
			if err := captureBaselineOnce(ws, featureID, repoPathsFromFeature(feature)); err != nil {
				return err
			}
		}

		ws.AppendEvent(featureID, protocol.Event{
			Type: "phase-start", Phase: phase, Role: role, Round: s.Round,
		})

		prompt, err := generatePrompt(ws, feature, cfg, role, s.Round)
		if err != nil {
			prompt = fmt.Sprintf("You are the %s for feature %s, round %d. Read .4x/%s/ for context.", role, featureID, s.Round, featureID)
		}

		fmt.Printf("[round %d] %s (%s) — invoking %s\n", s.Round, phase, role, s.Runner)

		result, err := r.Run(ctx, prompt)
		if err != nil {
			s.Active = false
			s.StopReason = "runner-error"
			ws.WriteState(featureID, s)
			ws.AppendEvent(featureID, protocol.Event{
				Type: "run-end", Phase: phase, Role: role, Round: s.Round,
				Status: "error", Detail: err.Error(),
			})
			return err
		}

		ws.AppendEvent(featureID, protocol.Event{
			Type: "run-end", Phase: phase, Role: role, Round: s.Round,
			Status: fmt.Sprintf("exit-%d", result.ExitCode),
		})

		if runner.IsHardError(result) {
			s.Active = false
			s.StopReason = "hard-error"
			ws.WriteState(featureID, s)
			return fmt.Errorf("runner returned hard error (exit 2)")
		}

		if runner.IsSoftFail(result) {
			s.Phase = protocol.PhaseBlocked
			s.Active = false
			s.StopReason = "soft-fail"
			ws.WriteState(featureID, s)
			syncFeatureStatus(ws, featureID, protocol.PhaseBlocked)
			return nil
		}

		next, nextRole, stopReason := nextPhaseAfter(ws, featureID, s)

		newState, err := state.Transition(s, next, nextRole)
		if err != nil {
			return fmt.Errorf("loop transition %s→%s: %w", s.Phase, next, err)
		}
		s = newState
		if stopReason != "" {
			s.Active = false
			s.StopReason = stopReason
		}
		ws.WriteState(featureID, s)
		syncFeatureStatus(ws, featureID, s.Phase)

		ws.AppendEvent(featureID, protocol.Event{
			Type: "transition", Phase: s.Phase, Role: s.Role, Round: s.Round,
		})
	}

	switch s.Phase {
	case protocol.PhaseDone:
		s.Active = false
		s.StopReason = "done"
		ws.WriteState(featureID, s)
		syncFeatureStatus(ws, featureID, protocol.PhaseDone)
		fmt.Printf("\nFeature %s complete (%d rounds)\n", featureID, s.Round)
	case protocol.PhaseNeedsAttention, protocol.PhaseBlocked:
		if s.Active {
			s.Active = false
			if s.StopReason == "" {
				s.StopReason = "escalation"
			}
			ws.WriteState(featureID, s)
		}
	}
	return nil
}

// nextPhaseAfter 根據目前 phase 和 artifacts 決定下一個 phase，第三個回傳值為 escalation 停止原因
func nextPhaseAfter(ws *protocol.Workspace, featureID string, s protocol.State) (protocol.Phase, protocol.Role, string) {
	switch s.Phase {
	case protocol.PhaseDesigning:
		return protocol.PhaseCoding, protocol.RoleCoder, ""

	case protocol.PhaseCoding, protocol.PhaseAmending:
		if esc := readEscalation(ws, featureID, s.Round); esc.Needed {
			if isDesignerEscalation(esc.Reason) {
				return protocol.PhaseDesigning, protocol.RoleDesigner, ""
			}
			return protocol.PhaseNeedsAttention, "", esc.Reason
		}
		return protocol.PhaseReviewing, protocol.RoleReviewer, ""

	case protocol.PhaseReviewing:
		if reviewPassed(ws, featureID, s.Round) {
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
		// guard 已包含 verify.json passed 檢查，不需重複讀取
		result := guard.CheckTestingToAccepting(ws, featureID, s.Round)
		if result.Pass {
			return protocol.PhaseAccepting, protocol.RoleDesigner, ""
		}
		// guard 失敗：若 verify 未通過 → amending；否則缺少 artifact → needs-attention
		if !verifyPassed(ws, featureID, s.Round) {
			return protocol.PhaseAmending, protocol.RoleCoder, ""
		}
		return protocol.PhaseNeedsAttention, "", strings.Join(result.Errors, "; ")

	case protocol.PhaseAccepting:
		return protocol.PhaseDone, "", ""

	default:
		return protocol.PhaseDone, "", ""
	}
}

func reviewPassed(ws *protocol.Workspace, featureID string, round int) bool {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.ReviewReport))
	if err != nil {
		return false
	}
	result := parseReviewVerdict(string(data))
	return result.Passed && result.CriticalCount == 0
}

// parseReviewVerdict 從 review-report.md 擷取 verdict 與 critical issue 計數
func parseReviewVerdict(content string) protocol.ReviewResult {
	lines := strings.Split(content, "\n")
	var result protocol.ReviewResult
	inVerdict := false
	verdictFound := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)

		if strings.Contains(upper, "[CRITICAL]") {
			result.CriticalCount++
		}

		if strings.HasPrefix(trimmed, "## Verdict") {
			inVerdict = true
			continue
		}
		if inVerdict && !verdictFound && trimmed != "" {
			if strings.HasPrefix(upper, "FAIL") {
				result.Passed = false
			} else {
				result.Passed = true
			}
			verdictFound = true
		}
	}

	return result
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

// isDesignerEscalation 判斷 escalation 是否應回到 Designer 而非停下來等人
// spec-mismatch / criteria-wrong 是 Designer 能自行修正的問題
func isDesignerEscalation(reason string) bool {
	return reason == "spec-mismatch" || reason == "criteria-wrong"
}

func readEscalation(ws *protocol.Workspace, featureID string, round int) protocol.Escalation {
	roundDir := ws.RoundDir(featureID, round)
	data, err := os.ReadFile(filepath.Join(roundDir, protocol.EscalationFile))
	if err != nil {
		return protocol.Escalation{}
	}
	var esc protocol.Escalation
	json.Unmarshal(data, &esc)
	return esc
}

func captureBaselineOnce(ws *protocol.Workspace, featureID string, repoPaths []string) error {
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
	if err := guard.CaptureBaseline(ws, featureID, repoPaths); err != nil {
		return fmt.Errorf("capture baseline: %w", err)
	}
	return nil
}

func repoPathsFromFeature(f protocol.Feature) []string {
	if len(f.Repos) == 0 {
		return []string{"."}
	}
	var paths []string
	for _, p := range f.Repos {
		paths = append(paths, p)
	}
	return paths
}

func dryRunLoop(ws *protocol.Workspace, feature protocol.Feature, cfg protocol.Config, s protocol.State) error {
	phases := []struct {
		phase protocol.Phase
		role  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
	}

	for _, p := range phases {
		fmt.Printf("=== %s (%s) ===\n", p.phase, p.role)
		prompt, err := generatePrompt(ws, feature, cfg, p.role, 1)
		if err != nil {
			fmt.Printf("  (error: %v)\n\n", err)
			continue
		}
		fmt.Println(prompt)
		fmt.Println()
	}
	return nil
}

// setupWorktree 為 feature 建立 git worktree，回傳 worktree 路徑。
// branch 命名 4x/<featureID>，worktree 放在 .worktrees/4x/<featureID>/。
// worktree 內建 .4x symlink 指回主 worktree 的 .4x/。
func setupWorktree(root, featureID string) (string, error) {
	wtDir := filepath.Join(root, ".worktrees", "4x", featureID)
	branch := "4x/" + featureID

	ensureGitignore(root, ".worktrees/")

	if _, err := os.Stat(wtDir); err == nil {
		dotLink := filepath.Join(wtDir, protocol.DirName)
		if _, err := os.Lstat(dotLink); os.IsNotExist(err) {
			os.Symlink(filepath.Join(root, protocol.DirName), dotLink)
		}
		return wtDir, nil
	}

	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return "", err
	}

	out, err := exec.Command("git", "-C", root, "worktree", "add", wtDir, "-b", branch).CombinedOutput()
	if err != nil {
		out2, err2 := exec.Command("git", "-C", root, "worktree", "add", wtDir, branch).CombinedOutput()
		if err2 != nil {
			return "", fmt.Errorf("git worktree add: %s\n%s", string(out), string(out2))
		}
	}

	dotDir := filepath.Join(root, protocol.DirName)
	link := filepath.Join(wtDir, protocol.DirName)
	os.RemoveAll(link)
	if err := os.Symlink(dotDir, link); err != nil {
		return "", fmt.Errorf("symlink .4x: %w", err)
	}

	return wtDir, nil
}

func cleanupWorktree(root, featureID, wtPath string) {
	os.Remove(filepath.Join(wtPath, protocol.DirName))
	exec.Command("git", "-C", root, "worktree", "remove", wtPath).Run()
	fmt.Printf("worktree removed: %s (branch 4x/%s preserved)\n", wtPath, featureID)
}

func ensureGitignore(root, pattern string) {
	path := filepath.Join(root, ".gitignore")
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(pattern + "\n")
}
