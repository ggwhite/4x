package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/ggwhite/4x/internal/orchestrator"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/spf13/cobra"
)

func newRetryCmd() *cobra.Command {
	var toPhase string
	var phaseOverrides []string
	var note string

	cmd := &cobra.Command{
		Use:   "retry <feature-id>",
		Short: "Recover from needs-attention or blocked and re-run",
		Long: `Transition the feature from needs-attention or blocked back to a working phase,
then immediately run the loop. Equivalent to 'transition --to <phase> && run'.

When --to is omitted, the target phase is auto-detected from the role recorded in
state.json (the role that was stuck before entering needs-attention/blocked); if it
cannot be derived, it falls back to 'accepting'. Use --to to force a specific phase,
e.g. --to amending to go back to coding.`,
		Args: cobra.ExactArgs(1),
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

			// 提早驗證，錯誤訊息在 retry 本身就報，不必等轉發給子程序 run 才炸。
			if _, err := orchestrator.ParsePhaseOverrides(phaseOverrides); err != nil {
				return err
			}

			// 未帶 --to 時傳空 Phase，交由 retryTransition 在臨界區內依 cur.Role 自動偵測。
			newState, from, autodetected, err := retryTransition(ws, featureID, protocol.Phase(toPhase))
			if err != nil {
				return err
			}

			if autodetected {
				fmt.Printf("auto-detected target phase from role %q: %s\n", newState.Role, newState.Phase)
			}
			fmt.Printf("%s → %s, launching run...\n", from, newState.Phase)

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cannot determine executable path: %w", err)
			}
			c := exec.Command(exe, buildRetryRunArgs(featureID, phaseOverrides, note)...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			_ = newState
			return c.Run()
		},
	}

	cmd.Flags().StringVar(&toPhase, "to", "", "target phase to recover to (default: auto-detect from state.json role, fallback accepting)")
	cmd.Flags().StringArrayVar(&phaseOverrides, "phase-override", nil, "temporary per-phase runner/model override for the relaunched run, format <phase>:<runner>:<model> (repeatable)")
	cmd.Flags().StringVar(&note, "note", "", "one-shot free-text note injected into the first role of this run only (not persisted to feature description)")
	return cmd
}

// buildRetryRunArgs 組出 retry 轉發給子程序 `4x run` 的參數清單，把 --phase-override
// 逐筆轉發下去，讓 retry 恢復的執行也能沿用使用者指定的 per-phase runner/model override；
// note 非空時另轉發 --note，讓一次性 note 注入 retry 重啟後的第一個 role。
func buildRetryRunArgs(featureID string, phaseOverrides []string, note string) []string {
	args := []string{"run", featureID}
	for _, po := range phaseOverrides {
		args = append(args, "--phase-override", po)
	}
	if note != "" {
		args = append(args, "--note", note)
	}
	return args
}

// retryTransition 從 needs-attention / blocked 轉回目標 phase，回傳新 state、原始 phase 與是否自動偵測。
//
// explicitTarget 為空字串代表呼叫端未帶 --to，需在 UpdateState 臨界區內用最新磁碟值
// （cur.Role / cur.Round）自動偵測 target；反推不出（RoleToPhase 回 ""）才 fallback accepting。
// 第三個回傳值 autodetected 僅在「未帶 explicitTarget 且成功由 role 推導」時為 true；
// 帶 explicitTarget 或走 fallback accepting 時皆為 false。
//
// 供測試直接呼叫，不含 exec 邏輯。
func retryTransition(ws *protocol.Workspace, featureID string, explicitTarget protocol.Phase) (protocol.State, protocol.Phase, bool, error) {
	// fromPhase / resolvedTarget / toRole / autodetected 在 mutate 內以最新磁碟值填入，
	// 供 closure 外的 SyncFeatureStatus / AppendEvent 與回傳使用。
	var (
		fromPhase      protocol.Phase
		resolvedTarget protocol.Phase
		toRole         protocol.Role
		autodetected   bool
	)
	newState, err := ws.UpdateState(featureID, func(cur *protocol.State) error {
		// 守衛以臨界區內讀到的最新磁碟值重判，避免依舊快照放行。
		if cur.Phase != protocol.PhaseNeedsAttention && cur.Phase != protocol.PhaseBlocked {
			return fmt.Errorf("retry only works from needs-attention or blocked (current phase: %s)", cur.Phase)
		}
		fromPhase = cur.Phase
		// target 解析下沉進臨界區：未帶 --to 時用 cur.Role / cur.Round 的最新磁碟值
		// 自動偵測，反推不出才 fallback accepting。
		resolvedTarget = explicitTarget
		if resolvedTarget == "" {
			if p := state.RoleToPhase(cur.Role, cur.Round); p != "" {
				resolvedTarget = p
				autodetected = true
			} else {
				resolvedTarget = protocol.PhaseAccepting
			}
		}
		toRole = state.PhaseToRole(resolvedTarget)
		transitioned, terr := state.Transition(*cur, resolvedTarget, toRole)
		if terr != nil {
			return fmt.Errorf("cannot transition %s → %s: %w", cur.Phase, resolvedTarget, terr)
		}
		transitioned.Active = true
		// 人為介入旗標：child 的 4x run recovery 要尊重此手動 phase，
		// 不被 SmartResumePhase 依磁碟 artifacts 重推導覆蓋（RecoverState 消費後清除）。
		transitioned.ManualPhase = true
		// retry 是一次明確的人為介入，殘留的 no-progress 計數與 stop reason 不該延續到
		// 新一輪執行——否則殘留值（如 =3）會讓 retry 後任何 phase 一啟動就被
		// no-progress 偵測秒殺打回 needs-attention，須手動改 state.json 才能真正重試。
		transitioned.ConsecutiveNoProgress = 0
		transitioned.StopReason = ""
		*cur = transitioned
		return nil
	})
	if err != nil {
		return protocol.State{}, "", false, err
	}

	if err := ws.SyncFeatureStatus(featureID, resolvedTarget); err != nil {
		slog.Warn("sync feature status failed", "feature", featureID, "phase", resolvedTarget, "error", err)
	}
	ws.AppendEvent(featureID, protocol.Event{
		Phase: resolvedTarget,
		Type:  "transition",
		Role:  toRole,
		Round: newState.Round,
	})

	return newState, fromPhase, autodetected, nil
}
