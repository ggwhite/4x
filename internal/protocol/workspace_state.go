package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReadState 讀取 feature 的 state.json
func (w *Workspace) ReadState(featureID string) (State, error) {
	return readJSON[State](filepath.Join(w.FeatureDir(featureID), StateFile))
}

// ReconcileActive 以 process 存在為權威來源校正 Active 欄位。
// 若 state 記錄 Active=true 但 PID 已不存在，自動將 Active 設為 false 並回寫。
func (w *Workspace) ReconcileActive(featureID string, s *State) error {
	if !s.Active {
		return nil
	}
	if ProcessAlive(s.Pid) {
		return nil
	}
	s.Active = false
	if s.StopReason == "" {
		s.StopReason = "process-gone"
	}
	s.Pid = 0
	return w.WriteState(featureID, *s)
}

// WriteState 寫入 feature 的 state.json。
//
// 採用 write-to-temp + rename 而非直接 os.WriteFile：後者會先 truncate 再寫，
// 在這段時間內 concurrent 的 ReadState 可能讀到截斷或半寫的 JSON 而 Unmarshal 失敗。
// 改用同目錄 temp file 寫完再 atomic rename 覆蓋，讓讀者永遠看到完整的舊檔或完整的新檔。
func (w *Workspace) WriteState(featureID string, s State) error {
	s.UpdatedAt = time.Now()
	// SubPhase 僅在 deep-reviewing phase 內有意義；離開該 phase 時一律清空，
	// 作為各退出路徑的單一收斂點，避免殘留的 subPhase 誤導 dashboard 或 resume 推斷。
	if s.Phase != PhaseDeepReviewing {
		s.SubPhase = ""
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(w.FeatureDir(featureID), StateFile, ".state-*.json", data, 0o644)
}

// AppendEvent 追加一行到 events.jsonl
func (w *Workspace) AppendEvent(featureID string, evt Event) error {
	if evt.Timestamp == "" {
		evt.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(
		filepath.Join(w.FeatureDir(featureID), EventsFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644,
	)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// RequestStop 在 feature dir 下原子寫入 stop signal 檔，請求 run loop 停止該 feature。
//
// 採 signal file（對齊既有 BatchStopFile 機制）而非直接改寫 state.json：state.json
// 的唯一 writer 收斂為 run loop，外部（如 MCP stop）只下「請求停止」信號，避免兩個
// writer 競寫整份 state.json 而用過時快照覆蓋掉 loop 剛寫入的 phase／round 進度。
//
// 語意上 stop 為「請求」：若目標 feature 已無存活 loop，signal 不會被消費，
// 留待既有 ReconcileActive 在下次 ReadState 校正 Active。
func (w *Workspace) RequestStop(featureID string) error {
	return AtomicWriteFile(w.FeatureDir(featureID), StopFile, ".stop-*", []byte("mcp-stop\n"), 0o644)
}

// StopRequested 回傳 feature dir 下是否存在 stop signal 檔。
func (w *Workspace) StopRequested(featureID string) bool {
	_, err := os.Stat(filepath.Join(w.FeatureDir(featureID), StopFile))
	return err == nil
}

// ClearStopSignal 刪除 feature 的 stop signal 檔；檔案不存在時不視為錯誤（比照 ClearBatchConflict）。
func (w *Workspace) ClearStopSignal(featureID string) error {
	return clearFile(filepath.Join(w.FeatureDir(featureID), StopFile))
}

// WorktreePath 從 events.jsonl 解析 feature 的 worktree 路徑。
// 若 feature 未使用 worktree 或 events.jsonl 不存在，回傳空字串。
//
// 掃描整個 events.jsonl 並回傳「最後一個」符合 `type==run-output` 且 detail
// 以 "worktree: " 開頭的路徑（最近一次 run 的 worktree）。re-run 多次後舊事件
// 會被新事件推到後段，只看前幾行會掃不到或回到已被移除的舊 worktree，故須掃全部行。
func (w *Workspace) WorktreePath(featureID string) string {
	eventsPath := filepath.Join(w.FeatureDir(featureID), EventsFile)
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		return ""
	}
	latest := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev struct {
			Type   string `json:"type"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "run-output" && strings.HasPrefix(ev.Detail, "worktree: ") {
			latest = strings.TrimPrefix(ev.Detail, "worktree: ")
		}
	}
	return latest
}

