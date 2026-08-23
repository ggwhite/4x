package guard

import (
	"fmt"
	"strings"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// checkSharedPathsPollution 偵測「反向污染」：feature 執行期間有人改了主工作區的 shared_path。
//
// merge-back 一律以 worktree 版覆寫主工作區，所以執行期間主工作區的同名檔案若被改動
// （例如平行 feature 已 merge-back 過同一個檔案），那份內容會在 4x done 時被無聲蓋掉。
// 本檢查讓它在每輪 4x check 就被攔下，而不是等到改動消失才發現。
//
// 只在 multi-repo（cfg.Workspace.Repos 非空）啟用：monorepo 下 worktree 是真正的 git
// worktree，根層檔案本來就被 merge，shared_paths 無意義，在該模式啟用只會產生偽陽性。
//
// 順帶承擔基線的建立：SetupWorktree 執行時 Designer 尚未宣告 shared_paths，
// 那時的 UpsertSharedPathsBaseline 是 no-op；本函式是唯一每輪都跑、又讀得到最新宣告的位置，
// 故在此 upsert。基線因此建於「4x 首次觀測到宣告的那次 4x check」，而不是 SetupWorktree 當下：
// orchestrator 只在 coding／amending 之後強制呼叫 guard.Check（handlePostCoder 對其餘 phase 直接
// 早退），designing 收尾是否跑 4x check 取決於 role 契約而非程式碼，故取樣最晚可能延到 coding 收尾。
// 反向污染的保護窗口從取樣那一刻起算，在此之前主工作區的改動會被當成原始快照寫進基線。
// 少了這一步，需求 1 的 preflight 與本檢查會全程 fail-open。
func checkSharedPathsPollution(ws *protocol.Workspace, featureID string, r *CheckResult) {
	cfg, err := ws.ReadConfig()
	if err != nil || len(cfg.Workspace.Repos) == 0 {
		return
	}
	feature, err := ws.LoadFeature(featureID)
	if err != nil || len(feature.SharedPaths) == 0 {
		return
	}
	mainRoot := gitops.MainRootFor(ws.Root, featureID)
	if mainRoot == "" {
		return
	}

	if err := gitops.UpsertSharedPathsBaseline(mainRoot, featureID, feature.SharedPaths); err != nil {
		r.Warns = append(r.Warns, fmt.Sprintf("shared-paths baseline upsert failed, drift detection skipped: %v", err))
		return
	}

	// unbaselined 在此恆為空：上方的 upsert 已為每個宣告補上 key，缺 key 的情形只可能出現在
	// 不經 guard 的路徑（如執行期間手改 YAML 後直接 4x done），由 gitops 的 preflight 出 note。
	drifted, _, baselineFound := gitops.DriftedSharedPaths(mainRoot, featureID, feature.SharedPaths)
	if !baselineFound {
		// upsert 成功後理論上不該走到這裡；保留是為了防禦基線檔在兩次呼叫之間被外力刪除。
		r.Warns = append(r.Warns, "shared-paths baseline missing, drift detection skipped")
		return
	}
	if len(drifted) == 0 {
		return
	}

	// 不累加 RetryableErrors：重跑同一 role 修不了，需人為介入（比照 checkScope 的邊界違規）。
	// 解除指引共用 gitops.SharedPathsDriftHint（唯一來源，不寫第二份），且刻意不提供「revert 主工作區」——
	// merge-back 以 worktree 版覆寫主工作區，revert 只會讓平行 feature 已落地的內容再被抹掉。
	// worktree 路徑用 gitops.Dir(mainRoot, featureID) 算：ws.Root 可能是主工作區也可能是
	// worktree，只有這個算法在兩種情形都指向 worktree。
	r.Pass = false
	r.Errors = append(r.Errors, fmt.Sprintf(
		"shared_paths modified in main workspace during run (must match the baseline snapshot 4x took when it first observed the declaration): %s; %s",
		strings.Join(drifted, ", "),
		gitops.SharedPathsDriftHint(gitops.Dir(mainRoot, featureID), gitops.SharedPathsBaselineFile(mainRoot, featureID))))
}
