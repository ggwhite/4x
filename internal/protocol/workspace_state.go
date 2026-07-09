package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
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
//
// 校正走 UpdateState 的加鎖 CAS，而非「鎖外讀快照 → 記憶體改 → 盲寫」：存活與否的判斷
// 以臨界區內重讀的磁碟值為準。否則 crash 後殘留 Active=true+dead pid，使用者重啟新 run
// （寫入 live pid）與 status/clean 觸發的 ReconcileActive 併發時，ReconcileActive 會用鎖外
// 舊快照（dead pid）算出 Active=false 再盲寫，覆蓋掉健康 live run 的 Active，害新 run 被誤判中止。
// 傳入的 *s 僅作為「是否值得取鎖」的廉價前置判斷，最終以磁碟現況回寫給呼叫端。
func (w *Workspace) ReconcileActive(featureID string, s *State) error {
	if !s.Active {
		return nil
	}
	if ProcessAlive(s.Pid) {
		return nil
	}
	updated, err := w.UpdateState(featureID, func(disk *State) error {
		if !disk.Active || ProcessAlive(disk.Pid) {
			return ErrSkipStateWrite
		}
		disk.Active = false
		if disk.StopReason == "" {
			disk.StopReason = "process-gone"
		}
		disk.Pid = 0
		return nil
	})
	if err != nil {
		return err
	}
	*s = updated
	return nil
}

// stateLockPath 回傳 feature 專屬的 advisory lock 檔路徑（與 state.json 同目錄）。
// 對同一 feature 的所有加鎖寫入都取這支鎖，序列化跨程序的 read-modify-write。
func (w *Workspace) stateLockPath(featureID string) string {
	return filepath.Join(w.FeatureDir(featureID), ".state.lock")
}

// writeStateLocked 是 WriteState 的實際寫入 body，但「不取鎖」：設 UpdatedAt、清 SubPhase、
// marshal、atomic rename。供已持鎖的 UpdateState 內部呼叫，避免同程序二次取鎖自我死鎖。
//
// 採用 write-to-temp + rename 而非直接 os.WriteFile：後者會先 truncate 再寫，
// 在這段時間內 concurrent 的 ReadState 可能讀到截斷或半寫的 JSON 而 Unmarshal 失敗。
// 改用同目錄 temp file 寫完再 atomic rename 覆蓋，讓讀者永遠看到完整的舊檔或完整的新檔。
func (w *Workspace) writeStateLocked(featureID string, s State) error {
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

// WriteState 寫入 feature 的 state.json，全程持有 feature 專屬 advisory 排他鎖。
//
// 鎖只序列化 writer 之間（batch/parallel、dashboard done、進行中的 4x run），
// ReadState 不取鎖、不受影響（靠 atomic rename 讀到完整檔）。取鎖逾時回
// ErrStateLockTimeout 包裝的錯誤而非無限阻塞。
func (w *Workspace) WriteState(featureID string, s State) error {
	lock, err := acquireFileLock(w.stateLockPath(featureID), stateLockTimeout)
	if err != nil {
		return fmt.Errorf("write state %s: %w", featureID, err)
	}
	defer func() { _ = lock.release() }()
	return w.writeStateLocked(featureID, s)
}

// UpdateState 是唯一的「加鎖 read-modify-write」入口：取鎖 → 讀最新磁碟 state →
// 呼叫 mutate 修改 → 原子寫回 → 釋放鎖，回傳寫入後（或未寫時的現況）的 State。
//
// 這讓「讀舊值 → 改 → 寫回」整段落在同一臨界區，消除兩個 writer 各自用過時快照
// 覆蓋彼此的 lost-update。回傳的 State 供呼叫端做後續 AppendEvent/SyncFeatureStatus，
// 不必自己再讀一次。
//
// mutate 回傳值決定寫入行為：
//   - nil：寫回並回傳 (寫入後 State, nil)。
//   - ErrSkipStateWrite：不寫、回傳 (未修改的磁碟現況 State, nil)——供「在臨界區內拿到
//     最新磁碟值後才決定不寫」的條件式跳過與 CAS 護欄使用。
//   - 其他 error：不寫、回傳 (零值 State, 包裝過的 error)。
//
// 重要限制：mutate 回呼內「不得」再呼叫 WriteState/UpdateState（同程序二次取鎖會 self-deadlock）；
// 只能修改傳入的 *State 或回傳哨兵/error，由 UpdateState 負責寫回。
func (w *Workspace) UpdateState(featureID string, mutate func(s *State) error) (State, error) {
	lock, err := acquireFileLock(w.stateLockPath(featureID), stateLockTimeout)
	if err != nil {
		return State{}, fmt.Errorf("update state %s: %w", featureID, err)
	}
	defer func() { _ = lock.release() }()

	s, err := w.ReadState(featureID)
	if err != nil {
		return State{}, fmt.Errorf("update state %s: %w", featureID, err)
	}
	// 保留一份未修改的磁碟現況：ErrSkipStateWrite 時回傳它（而非 mutate 途中改了一半
	// 的 in-memory 值），符合「跳過寫入 → 回傳磁碟現況」語意。
	disk := s
	if err := mutate(&s); err != nil {
		if errors.Is(err, ErrSkipStateWrite) {
			return disk, nil
		}
		return State{}, fmt.Errorf("update state %s: %w", featureID, err)
	}
	if err := w.writeStateLocked(featureID, s); err != nil {
		return State{}, fmt.Errorf("update state %s: %w", featureID, err)
	}
	return s, nil
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

// TotalCost 加總 events.jsonl 中所有 run-end 事件的 cost_usd，作為該 feature
// 跨行程（含中斷重啟）的權威總花費，不依賴 per-role/iteration 比對。
// events.jsonl 不存在時回傳 (0, nil)（新 feature 尚無歷史）。
func (w *Workspace) TotalCost(featureID string) (float64, error) {
	eventsPath := filepath.Join(w.FeatureDir(featureID), EventsFile)
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var total float64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev struct {
			Type    string  `json:"type"`
			CostUSD float64 `json:"cost_usd"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "run-end" {
			total += ev.CostUSD
		}
	}
	return total, nil
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
