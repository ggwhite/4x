package gitops

import (
	"fmt"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
)

// MergeAndFinalize 承載 CLI（cmd/4x/done.go autoMergeFeature）與 server
// （internal/server/server.go handlePostDone）原本各自實作的 merge+done 編排，是兩端共用的
// 唯一真實來源：ops.Merge（或 cfg.IssueTracker.Enabled 時改走 ops.PushAndOpenMR，done 語意
// 變成「已開 MR」而非「已合併」）→ 成功時重讀最新 state 確認仍為 pending-review → state.FinalizeDone
// → commitSelfManaged（把 finalize 階段新寫入的 4x 自管路徑收乾淨）。
//
// ops.Merge 內另有 merge 前的同名前置 commit（見 monorepo.go / multirepo.go），常態下這裡的
// post-merge 呼叫是 no-op；保留它是為了 cfg.IssueTracker.Enabled 時走 PushAndOpenMR 的路徑——
// 該路徑沒有 preflight、也不會經過 Merge 內的前置 commit，仍需要有人把 pipeline 狀態收乾淨。
//
// 重讀 state 是為了保留 server 的防 stale 覆寫不變式：merge 可能耗時數秒，期間 runner 或
// ensureInactive 可能改過 state.json，若用 merge 前的 stale 值 finalize 會覆蓋其他欄位更新。
// CLI 單程序下沒有並行 writer，重讀為 no-op，安全。
//
// 結果以回傳值的組合表達，讓 CLI 與 server 各自決定後續：
//   - (MergeResult{Conflict:true}, nil)     → merge 衝突，未 finalize，呼叫端維持 pending-review
//   - (MergeResult{Error:!=""}, nil)         → 非衝突 merge 錯誤，未 finalize，呼叫端警告並維持 pending-review
//   - (MergeResult{StateChanged:true}, nil)  → merge 後 phase 已非 pending-review，未 finalize（server 回 409）
//   - (result, err) err!=nil                 → 重讀 state 或 finalize 真正失敗（fatal，server 回 500）
//   - (MergeResult{FinalState:done}, nil)    → 成功 finalize 為 done，Skipped 表非 worktree 模式，
//     或 issue_tracker.enabled 時 PushAndOpenMR 判定 worktree 存在但無 committed 變更可開 MR
//
// 並行鎖（server 的 mergeMu）由呼叫端負責包住本函式；CLI 單程序無需鎖，本函式不持鎖。
func MergeAndFinalize(root string, ws *protocol.Workspace, cfg protocol.Config, featureID, featureName string) (MergeResult, error) {
	ops := New(root, ws, cfg)
	var result MergeResult
	if cfg.IssueTracker.Enabled {
		result = ops.PushAndOpenMR(featureID, featureName)
	} else {
		result = ops.Merge(featureID, featureName)
	}
	if result.Conflict || result.Error != "" {
		return result, nil
	}

	fresh, err := ws.ReadState(featureID)
	if err != nil {
		return result, fmt.Errorf("failed to re-read state: %w", err)
	}
	if fresh.Phase != protocol.PhasePendingReview {
		result.StateChanged = true
		result.FinalState = fresh
		return result, nil
	}

	finalState, err := state.FinalizeDone(ws, featureID, fresh)
	if err != nil {
		return result, err
	}
	result.FinalState = finalState

	commitSelfManaged(root, featureID)
	return result, nil
}
