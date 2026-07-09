package state

import (
	"log/slog"

	"github.com/ggwhite/4x/internal/protocol"
)

// FinalizeDone 將 feature 從 pending-review 推進到 done 終態，是 CLI（cmd/4x/done.go）
// 與 server（internal/server/server.go）原本各自重寫的 done 收尾序列之唯一真實來源：
// 於 UpdateState 的加鎖臨界區內 Transition→PhaseDone（role 空字串）、設 Active=false /
// StopReason="done" 並原子寫回，再於鎖外 SyncFeatureStatus、append 一條 transition event。
// 回傳寫入後的最新 State 供呼叫端使用。
//
// transition 以 UpdateState 在臨界區「讀到的最新磁碟值」為權威（server done 與 CLI done 競寫
// 時後者不會用過時快照覆蓋），呼叫端傳入的 s 快照僅為向後相容保留、不再被用來覆寫磁碟。
//
// SyncFeatureStatus 失敗採 non-fatal：呼叫它之前 UpdateState 已成功把 state.json 寫成 done，
// transition 已落地；feature_list.json 的狀態投影只影響 dashboard 顯示，失敗僅記 slog warning
// 後續跑。回傳的 error 只反映真正 fatal 的失敗（Transition / WriteState / AppendEvent），
// 避免在 state 其實已 done 時謊報整體失敗。
func FinalizeDone(ws *protocol.Workspace, featureID string, s protocol.State) (protocol.State, error) {
	_ = s // 向後相容保留；權威值改由 UpdateState 在臨界區重讀磁碟取得。
	newState, err := ws.UpdateState(featureID, func(cur *protocol.State) error {
		transitioned, terr := Transition(*cur, protocol.PhaseDone, "")
		if terr != nil {
			return terr
		}
		transitioned.Active = false
		transitioned.StopReason = "done"
		*cur = transitioned
		return nil
	})
	if err != nil {
		return protocol.State{}, err
	}

	if err := ws.SyncFeatureStatus(featureID, protocol.PhaseDone); err != nil {
		slog.Warn("sync feature status failed", "feature", featureID, "phase", protocol.PhaseDone, "error", err)
	}

	if err := ws.AppendEvent(featureID, protocol.Event{
		Type:  "transition",
		Phase: protocol.PhaseDone,
		Round: newState.Round,
	}); err != nil {
		return protocol.State{}, err
	}

	return newState, nil
}
