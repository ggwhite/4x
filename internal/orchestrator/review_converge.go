package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ggwhite/4x/internal/guard"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// runReviewConvergence 在 reviewing phase 偵測到 CONDITIONAL PASS（PASS/CONDITIONAL PASS、
// 無 critical、但有 warning）時，於「同一 round、同一 phase」內派 mini-coder 收掉這些 warning，
// 重跑 reviewer 確認後再交回主迴圈轉 testing——不再把 warning 放行到後續 phase 拖到 accepting
// 才爆整輪 retry。全程維持 protocol.PhaseReviewing、round 不變，複用 deep-reviewing 自癒循環的
// mini-coder 子 role 模式，不需新增任何 state machine 轉換。
//
// 回傳 (cont, changed, err)：
//   - cont 為 true 表示收斂已結束（或不適用），主迴圈應照常呼叫 NextPhaseAfter——它讀最終
//     review-report.md 判定：乾淨 / 仍 CONDITIONAL PASS → testing；翻出 critical → amending。
//   - changed 為 true 表示 mini-coder 至少執行過一次，收斂已（可能）套用程式碼變更；parallel
//     路徑據此判定本輪 tester 平行產出的 verify.json 已 stale，須轉入 testing 讓 tester 重跑。
//   - cont 為 false 且 err 為 nil 表示 mini-coder scope 越界已落入 needs-attention 終態，主迴圈應 break。
//   - err 非 nil 表示 hard error 或 context cancel，直接中止。
//
// 進入條件：僅當目前 round 的 review-report.md 為 ReviewConditionalPass；純 PASS / 純 FAIL 一律
// 不進入，直接回 (true, false, nil) 讓主迴圈照常轉換（保證不改變純 PASS 與純 FAIL 流程）。
func (r *Runner) runReviewConvergence(ctx context.Context, s *protocol.State, pc protocol.ProfileConfig) (cont bool, changed bool, err error) {
	featureID := r.featureID()
	round := s.Round

	if !ReviewConditionalPassAtRound(r.Ws, featureID, round, protocol.ReviewReport) {
		return true, false, nil
	}

	maxFix := protocol.ResolveMaxFixRounds(r.Cfg, protocol.RoleReviewer)

	reviewRunnerManual, reviewModelManual := protocol.EffectiveManual(r.RunOverrides, protocol.PhaseReviewing, r.ManualRunner)
	reviewRunner, err := protocol.ResolvePhaseRunner(r.Cfg, r.Feature, pc, protocol.PhaseReviewing, reviewRunnerManual)
	if err != nil {
		StopState(r.Ws, featureID, s, "runner-error", fmt.Sprintf("review-convergence runner resolution failed: %v", err))
		return false, false, fmt.Errorf("review-convergence runner resolution failed: %w", err)
	}
	reviewModel, err := protocol.ResolvePhaseModel(r.Cfg, r.Feature, pc, protocol.PhaseReviewing, protocol.RoleReviewer, reviewRunner, reviewModelManual)
	if err != nil {
		StopState(r.Ws, featureID, s, "model-error", fmt.Sprintf("review-convergence reviewer model resolution failed: %v", err))
		return false, false, fmt.Errorf("review-convergence reviewer model resolution failed: %w", err)
	}
	_, coderModelManual := protocol.EffectiveManual(r.RunOverrides, protocol.PhaseCoding, r.ManualRunner)
	coderModel, err := protocol.ResolvePhaseModel(r.Cfg, r.Feature, pc, protocol.PhaseCoding, protocol.RoleCoder, reviewRunner, coderModelManual)
	if err != nil {
		StopState(r.Ws, featureID, s, "model-error", fmt.Sprintf("review-convergence mini-coder model resolution failed: %v", err))
		return false, false, fmt.Errorf("review-convergence mini-coder model resolution failed: %w", err)
	}
	// mini-coder 預設沿用 coder model，但 roles.mini-coder.model 若有設定則優先。
	miniCoderModel, err := protocol.ResolveMiniCoderModel(r.Cfg, reviewRunner, coderModel)
	if err != nil {
		StopState(r.Ws, featureID, s, "model-error", fmt.Sprintf("review-convergence mini-coder model resolution failed: %v", err))
		return false, false, fmt.Errorf("review-convergence mini-coder model resolution failed: %w", err)
	}

	for iter := 1; iter <= maxFix; iter++ {
		// 已乾淨 PASS 或翻 FAIL（非 CONDITIONAL PASS）→ 收斂完成，交回主迴圈。
		if !ReviewConditionalPassAtRound(r.Ws, featureID, round, protocol.ReviewReport) {
			break
		}
		fmt.Printf("[round %d] reviewing — conditional-pass convergence iteration %d/%d\n", round, iter, maxFix)

		// mini-coder：只修 review-report.md 的 warning 項（WithConditionalSource 指向 review-report）。
		s.Role = protocol.RoleMiniCoder
		s.SubPhase = protocol.SubPhaseFixing
		if yielded, werr := r.writeActiveState(featureID, s); werr != nil {
			return false, changed, fmt.Errorf("write state (review mini-coder): %w", werr)
		} else if yielded {
			return false, changed, nil
		}
		changed = true
		if ok, rerr := r.runReviewSubRole(ctx, s, protocol.RoleMiniCoder, reviewRunner, miniCoderModel,
			runner.ReviewFixLogFileName(round, iter), round, iter,
			prompt.WithConditionalSource(protocol.ReviewReport)); !ok || rerr != nil {
			return ok, changed, rerr
		}

		if r.CommitStrategy == "per-round" && r.RunnerWs.Root != r.Ws.Root {
			if cerr := r.Ops.Commit(r.RunnerWs.Root, featureID, fmt.Sprintf("wip(%s): round %d review-fix %d", featureID, round, iter)); cerr != nil {
				slog.Error("auto-commit review-fix failed", "feature", featureID, "round", round, "iteration", iter, "error", cerr)
			} else {
				slog.Info("auto-commit", "feature", featureID, "round", round, "iteration", iter, "strategy", "review-fix")
			}
		}

		// mini-coder scope 越界 → needs-attention、中止主迴圈（對齊 deepReviewSelfHeal 的 scope-exceed 處理）。
		if guardResult := guard.Check(r.Ws, featureID, r.Ops); !guardResult.Pass {
			reason := strings.Join(guardResult.Errors, "; ")
			s.Phase = protocol.PhaseNeedsAttention
			StopState(r.Ws, featureID, s, "scope-exceed", "review-fix scope exceeded: "+reason)
			LogSyncErr(r.Ws.SyncFeatureStatus(featureID, protocol.PhaseNeedsAttention), featureID, protocol.PhaseNeedsAttention)
			r.Ws.AppendEvent(featureID, protocol.Event{
				Type: "guard-fail", Phase: protocol.PhaseReviewing, Role: protocol.RoleMiniCoder,
				Round: round, Detail: s.StopMessage, Runner: s.Runner,
			})
			return false, changed, nil
		}

		// 重生 review-package.md：mini-coder 已改碼（並可能在 per-round + worktree 模式推進 HEAD），
		// 收斂路徑不經 coding/amending→reviewing 轉換（generateReviewPackage 唯一觸發點，見
		// orchestrator.go），若不重生，重跑的 reviewer 會讀到修正前的 stale diff、無法確認 warning
		// 是否已修好。s.BaseCommit 於首次進 coding 時擷取、收斂全程不變，直接沿用。
		r.generateReviewPackage(round, s.BaseCommit)

		// 重跑 reviewer：覆寫 review-report.md 產生新 verdict（實現「處理後重跑一次 reviewer 確認」）。
		s.Role = protocol.RoleReviewer
		s.SubPhase = protocol.SubPhaseReviewing
		if yielded, werr := r.writeActiveState(featureID, s); werr != nil {
			return false, changed, fmt.Errorf("write state (review re-run): %w", werr)
		} else if yielded {
			return false, changed, nil
		}
		if ok, rerr := r.runReviewSubRole(ctx, s, protocol.RoleReviewer, reviewRunner, reviewModel,
			runner.IterationLogFileName(round, string(protocol.RoleReviewer), iter+1), round, 0); !ok || rerr != nil {
			return ok, changed, rerr
		}
	}

	// 收斂結束：跑滿上限仍 CONDITIONAL PASS → 非阻塞降級（放行 testing，維持 warning 不阻擋語意），
	// 額外記一筆殘留警示 event；絕不因收斂未竟主動打回 amending。
	if ReviewConditionalPassAtRound(r.Ws, featureID, round, protocol.ReviewReport) {
		r.Ws.AppendEvent(featureID, protocol.Event{
			Type: "conditional-pass-residual", Phase: protocol.PhaseReviewing, Role: protocol.RoleReviewer,
			Round: round, Runner: s.Runner, Notify: protocol.NotifyWarning,
			Detail: fmt.Sprintf("conditional-pass warnings remain after %d convergence iterations; passing through to testing", maxFix),
		})
		fmt.Printf("[round %d] reviewing — conditional-pass residual after %d iterations, passing through to testing\n", round, maxFix)
	}

	s.Role = protocol.RoleReviewer
	s.SubPhase = ""
	if yielded, werr := r.writeActiveState(featureID, s); werr != nil {
		return false, changed, fmt.Errorf("write state (review convergence end): %w", werr)
	} else if yielded {
		return false, changed, nil
	}
	return true, changed, nil
}

// runReviewSubRole 在 reviewing phase 內 spawn 一個收斂子 role（mini-coder / reviewer）。薄
// wrapper，委派給 phase-agnostic 的 runSubRole，phase 固定 reviewing（全程維持不變）。opts 用於
// 對 mini-coder 注入 WithConditionalSource 讓其改讀 review-report.md。
func (r *Runner) runReviewSubRole(ctx context.Context, s *protocol.State, role protocol.Role, runnerName, model, logName string, round, iteration int, opts ...prompt.Option) (bool, error) {
	return r.runSubRole(ctx, s, protocol.PhaseReviewing, role, runnerName, model, logName, round, iteration, opts...)
}
